package listener

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/Sachinxmpl/gobalancer/internal/config"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func testServer(t *testing.T) *Server {
	t.Helper()

	cfg := &config.Config{
		Mode:   config.ModeL4,
		Listen: "127.0.0.1:0",
		Pools: map[string][]config.Backend{
			"default": {{
				Addr: "127.0.0.1:9001", Weight: 1,
			}},
		},
	}

	s := New(cfg.Listen, config.NewStore(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.ShutDown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})

	return s
}

func TestServer_AcceptsConnections(t *testing.T) {
	s := testServer(t)

	for i := 0; i < 5; i++ {
		conn, err := net.Dial("tcp", s.Addr().String())
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}

		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := io.ReadAll(conn); err != nil {
			t.Errorf("read %d: %v", i, err)
		}
		conn.Close()
	}
}

func TestServer_StartFailOnBusyAddress(t *testing.T) {
	s := testServer(t)

	copyServerOnSameAddr := New(s.Addr().String(), config.NewStore(&config.Config{}), slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := copyServerOnSameAddr.Start(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		copyServerOnSameAddr.ShutDown(ctx)
		t.Fatal("start on a busy address succeeded, expected an error")
	}
}

func TestServer_ShutdownStopsAccepting(t *testing.T) {
	s := testServer(t)
	addr := s.Addr().String()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.ShutDown(ctx); err != nil {
		t.Fatalf("Shutdown :%v", err)
	}

	conn, err := net.Dial("tcp", addr)
	if err == nil {
		conn.Close()
		t.Fatal("dial succeeded after Shutdown, expected connection refused")
	}
}

func TestServer_ShutdownIsIdempotent(t *testing.T) {
	s := testServer(t)

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.ShutDown(ctx)
		cancel()
		if err != nil {
			t.Fatalf("Shutdown %d: %v", i, err)
		}
	}
}

func TestServer_AcceptLoopExitsOnClosedListener(t *testing.T) {
	s := testServer(t)

	// Closing the listener out from under the accept loop is what Shutdown does, the loop must treat it as a clean exit, not a transient error to retry.
	s.ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.ShutDown(ctx); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Shutdown after manual close: %v", err)
	}
}
