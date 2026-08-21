package listener

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sachinxmpl/gobalancer/internal/balancer"
	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/health"
	"github.com/Sachinxmpl/gobalancer/internal/reload"
)

func TestReload_UnderLoad(t *testing.T) {
	be1, stop1 := echoBackend(t)
	be2, stop2 := echoBackend(t)

	defer stop1()
	defer stop2()

	dir := t.TempDir()
	validA := writeConfig(t, dir, "a.yaml", be1, be2)
	validB := writeConfig(t, dir, "b.yaml", be2, be1) // same backends, different order
	invalid := filepath.Join(dir, "bad.yaml")
	os.WriteFile(invalid, []byte("mode: l4\nlisten: \":0\"\npools: {}\n"), 0o644)

	// start with validA
	cfg, err := config.Load(validA)
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}
	store := config.NewStore(cfg)
	reg := health.NewRegistry()
	added, _ := reg.Reconcile(cfg.BackendAddrs())
	mgr := health.NewManager(reg, cfg.Health, discardLogger(), nil)
	mgr.Sync(added, nil)
	defer mgr.Stop()

	bal, _ := balancer.New(config.AlgRoundRobin, reg)

	srv := New(Options{Addr: cfg.Listen, Store: store, Balancer: bal, Registry: reg, Log: discardLogger()})

	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.ShutDown(ctx)
	})
	front := srv.Addr().String()

	var (
		stopFlag atomic.Bool
		errors   atomic.Int64
		wg       sync.WaitGroup
	)
	payload := []byte("ping\n")
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() { // exits when: stopFlag is set.
			defer wg.Done()
			for !stopFlag.Load() {
				if !roundTrip(front, payload) {
					errors.Add(1)
				}
			}
		}()
	}

	// Reload 100 times: alternate two valid configs, with one invalid in the
	// middle that must be rejected without disturbing traffic.
	rejected := 0
	for i := 0; i < 100; i++ {
		var path string
		switch {
		case i == 50:
			path = invalid
		case i%2 == 0:
			path = validA
		default:
			path = validB
		}

		err := reload.Apply(path, store, reg, mgr, discardLogger())
		if path == invalid {
			if err == nil {
				t.Error("invalid config was accepted; want rejection")
			} else {
				rejected++
			}
		} else if err != nil {
			t.Errorf("reload %d (valid) failed: %v", i, err)
		}
		time.Sleep(2 * time.Millisecond) // let some traffic flow between reloads
	}

	stopFlag.Store(true)
	wg.Wait()

	if rejected != 1 {
		t.Errorf("rejected %d invalid configs, want 1", rejected)
	}
	if n := errors.Load(); n != 0 {
		t.Errorf("client saw %d errors during 100 reloads, want 0 (G2: hitless reload)", n)
	}

}

func writeConfig(t *testing.T, dir, name, a, b string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := fmt.Sprintf(`mode: l4
listen: "127.0.0.1:0"
balancer: round_robin

pools:
  default:
    - addr: "%s"
      weight: 1
    - addr: "%s"
      weight: 1
`, a, b)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
