package listener

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Sachinxmpl/gobalancer/internal/balancer"
	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/health"
)

type attempt struct {
	at time.Time
	ok bool
}

// roundTrip does one request through the loadbalancer: connect, send a payload, and
// confirm the echo comes back. A down backend manifests here as a missing echo
// (loadbalancer accepts the client, then closes it because it could not reach a
// backend), NOT as a dial failure — so we check the echo, not just the connect.
func roundTrip(addr string, payload []byte) bool {
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := conn.Write(payload); err != nil {
		return false
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		return false
	}
	return bytes.Equal(buf, payload)
}

// Measures the client-visible error window when a backend is
// killed under steady traffic. It proves goal G3: failover under one second.
func TestFailoverTime(t *testing.T) {
	if testing.Short() {
		t.Skip("failover timing test takes ~1.8s")
	}

	const (
		workers  = 16
		warmup   = 300 * time.Millisecond
		postKill = 1500 * time.Millisecond
	)
	payload := []byte("ping-ping-ping\n")

	addr0, stop0 := echoBackend(t)
	addr1, stop1 := echoBackend(t)
	addr2, stop2 := echoBackend(t)
	defer stop1()
	defer stop2()

	var kill0 sync.Once
	killBackend0 := func() { kill0.Do(stop0) }
	defer killBackend0()

	cfg := &config.Config{
		Mode:   config.ModeL4,
		Listen: "127.0.0.1:0",
		Health: config.Health{Passive: config.PassiveHealth{Fall: 3}},
		Timeouts: config.Timeouts{
			Dial:  config.Duration(300 * time.Millisecond),
			Drain: config.Duration(time.Second),
		},
		Pools: map[string][]config.Backend{
			"default": {
				{Addr: addr0, Weight: 1},
				{Addr: addr1, Weight: 1},
				{Addr: addr2, Weight: 1},
			},
		},
	}
	reg := health.NewRegistry()
	reg.Reconcile(cfg.BackendAddrs())

	bal, err := balancer.New(config.AlgRoundRobin, reg)
	if err != nil {
		t.Fatal(err)
	}

	srv := New(cfg.Listen, config.NewStore(cfg), bal, reg,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.ShutDown(ctx)
	})
	front := srv.Addr().String()

	var (
		stop    = make(chan struct{})
		wg      sync.WaitGroup
		records = make([][]attempt, workers)
	)
	for w := range workers {
		wg.Add(1)
		// exits when: stop is closed.
		go func(w int) {
			defer wg.Done()
			var local []attempt
			for {
				select {
				case <-stop:
					records[w] = local
					return
				default:
				}
				ok := roundTrip(front, payload)
				local = append(local, attempt{at: time.Now(), ok: ok})
			}
		}(w)
	}

	// Warm up: all backends healthy, traffic flowing.
	time.Sleep(warmup)

	// Kill backend 0 and mark the instant.
	killTime := time.Now()
	killBackend0()

	// Keep traffic flowing while failover happens and settles.
	time.Sleep(postKill)
	close(stop)
	wg.Wait()

	var (
		preKillErrors int
		lastErr       time.Time
		postKillTotal int
		postKillErr   int
	)
	for _, rec := range records {
		for _, a := range rec {
			if a.at.Before(killTime) {
				if !a.ok {
					preKillErrors++
				}
				continue
			}
			postKillTotal++
			if !a.ok {
				postKillErr++
				if a.at.After(lastErr) {
					lastErr = a.at
				}
			}
		}
	}

	// Sanity: with every backend healthy, warmup traffic must be clean. If it
	// is not, the harness is broken and any delta would be meaningless.
	if preKillErrors > 0 {
		t.Fatalf("harness broken: %d errors before the kill (all backends were healthy)", preKillErrors)
	}
	if postKillTotal == 0 {
		t.Fatal("no requests recorded after the kill; rate too low to measure")
	}

	if postKillErr == 0 {
		t.Logf("failover: 0 client-visible errors after kill (%d requests) — delta ~= 0", postKillTotal)
		return
	}

	delta := lastErr.Sub(killTime)
	t.Logf("failover: delta = %v (%d/%d post-kill requests failed before traffic settled)",
		delta, postKillErr, postKillTotal)

	if delta >= time.Second {
		t.Errorf("failover took %v, want < 1s (G3)", delta)
	}
}
