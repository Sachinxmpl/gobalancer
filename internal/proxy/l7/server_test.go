package l7

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/Sachinxmpl/gobalancer/internal/balancer"
	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/health"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func testServer(t *testing.T) *Server {
	t.Helper()

	cfg := &config.Config{
		Mode:   config.ModeL7,
		Listen: "127.0.0.1:0",
		Timeouts: config.Timeouts{
			Read:  config.Duration(5 * time.Second),
			Write: config.Duration(5 * time.Second),
			Idle:  config.Duration(30 * time.Second),
		},
	}
	bal, _ := balancer.New(config.AlgRoundRobin)
	reg := health.NewRegistry()

	s := New(cfg, config.NewStore(cfg), bal, reg,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.ShutDown(ctx); err != nil {
			t.Errorf("ShutDown: %v", err)
		}
	})
	return s
}

func TestServer_ServesHTTP(t *testing.T) {
	s := testServer(t)

	client := &http.Client{
		Timeout: 500 * time.Millisecond,
	}

	res, err := client.Get("http://" + s.Addr().String() + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)

	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
}

func TestServer_StartFailsOnBusyAddress(t *testing.T) {
	s := testServer(t)

	cfg := &config.Config{Mode: config.ModeL7, Listen: s.Addr().String()}
	bal, _ := balancer.New(config.AlgRoundRobin)
	other := New(cfg, config.NewStore(cfg), bal, health.NewRegistry(),
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := other.Start(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		other.ShutDown(ctx)
		t.Fatal("Start on a busy address succeeded, want an error")
	}
}

func TestServer_ShutdownStopsServing(t *testing.T) {
	s := testServer(t)
	addr := s.Addr().String()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.ShutDown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	client := &http.Client{
		Timeout: 500 * time.Millisecond,
	}

	resp, err := client.Get("http://" + addr + "/")
	if err == nil {
		resp.Body.Close()
		t.Fatal("request succeeded after shutdown")
	}
}
