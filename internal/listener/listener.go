package listener

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sachinxmpl/gobalancer/internal/balancer"
	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/health"
	"github.com/Sachinxmpl/gobalancer/internal/proxy"
)

const (
	acceptBackOffMin = 5 * time.Millisecond
	acceptBackOfMax  = time.Second
)

type Server struct {
	addr     string
	store    *config.Store
	balancer balancer.Balancer
	registry *health.Registry
	log      *slog.Logger

	ln net.Listener
	wg sync.WaitGroup

	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	shutdown bool

	connID atomic.Uint64
}

func New(addr string, store *config.Store, bal balancer.Balancer, reg *health.Registry, log *slog.Logger) *Server {
	return &Server{
		addr:     addr,
		store:    store,
		balancer: bal,
		registry: reg,
		log:      log,
		conns:    make(map[net.Conn]struct{}),
	}
}

// binds listen address and begins accepting connections in background
// a bind failure is reported synchronously before any goroutine exists
// Every successful start -> paired with a shutdown
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.addr, err)
	}
	s.ln = ln

	s.wg.Add(1)

	// exists when -> Shutdown closes the listener, this makes Accept return net.ErrClosed
	go func() {
		defer s.wg.Done()
		s.acceptLoop()
	}()

	s.log.Info("listening", "addr", ln.Addr().String())

	return nil
}

// Addr reports the address actually bound (differs from the configured address
// when the config asks for port 0). Valid only after a successful Start.
func (s *Server) Addr() net.Addr {
	return s.ln.Addr()
}

func (s *Server) acceptLoop() {
	backoff := acceptBackOffMin

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			// listener closed by Shutdown -> only way out of loop, it not a failure
			if errors.Is(err, net.ErrClosed) {
				return
			}

			// anything else -> process most liekly out of file descriptors, try exponentially
			s.log.Warn("accept failed, retrying", "err", err, "backoff", backoff)
			time.Sleep(backoff)
			backoff = min(backoff*2, acceptBackOfMax)
			continue
		}
		backoff = acceptBackOffMin

		// Shutdown may have started between accept returning and here,
		if !s.track(conn) {
			conn.Close()
			continue
		}

		s.wg.Add(1)

		go func() {
			defer s.wg.Done()
			defer s.untrack(conn)
			s.handle(conn)
		}()
	}
}

// register conn as live, reports false if shutdown has already begun (caller must conn itself)
func (s *Server) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shutdown {
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

func healthyBackends(pool []config.Backend, reg *health.Registry) []config.Backend {
	out := make([]config.Backend, 0, len(pool))
	for _, b := range pool {
		if reg.Get(b.Addr).Admits() {
			out = append(out, b)
		}
	}
	return out
}

// Serves one client connection: pick a healthy backend, dial it , and replay bytes both ways until connection ends
// exists when proxy.L4 returns, which its drain deadline bounds
func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	log := s.log.With(
		"conn_id", s.connID.Add(1),
		"client", conn.RemoteAddr().String(),
	)

	cfg := s.store.Load()

	pool := healthyBackends(cfg.L4Pool(), s.registry)

	backend, err := s.balancer.Pick(clientKey(conn), pool)
	if err != nil {
		log.Warn("no backend for connection", "err", err)
		return
	}
	log = log.With("backend", backend.Addr)

	up, err := net.DialTimeout("tcp", backend.Addr, cfg.Timeouts.Dial.Std())
	if err != nil {
		s.registry.Get(backend.Addr).ReportFailure(cfg.Health.Passive.Fall)
		log.Warn("dial backend failed", "err", err)
		return
	}

	s.registry.Get(backend.Addr).ReportSuccess()

	log.Debug("proxying")
	proxy.L4(conn, up, cfg.Timeouts.Drain.Std())
}

func clientKey(conn net.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

// Stops accepting new connections and waits for the live ones to finish
// if ctx epires first remaining connections are force-closed and ctx.Err() returned
func (s *Server) ShutDown(ctx context.Context) error {
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return nil
	}
	s.shutdown = true
	s.mu.Unlock()

	// close lisener
	closeErr := s.ln.Close()

	drained := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(drained)
	}()

	select {
	case <-drained:
		return closeErr
	case <-ctx.Done():
		s.closeAll()
		<-drained
		return ctx.Err()
	}
}

func (s *Server) closeAll() {
	s.mu.Lock()
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	if len(conns) == 0 {
		return
	}

	s.log.Warn("drain deadline expired, force-closing connections", "count", len(conns))

	for _, conn := range conns {
		conn.Close()
	}
}
