package l7

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Sachinxmpl/gobalancer/internal/balancer"
	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/health"
)

// readHeaderTimeout bounds how long a client may take to send its request headers.
// Prevents slowloris attacks.
const readHeaderTimeout = 5 * time.Second

type Server struct {
	addr     string
	store    *config.Store
	balancer balancer.Balancer
	registry *health.Registry
	log      *slog.Logger

	httpSrv *http.Server
	ln      net.Listener
}

func New(cfg *config.Config, store *config.Store, bal balancer.Balancer, reg *health.Registry, log *slog.Logger) *Server {
	s := &Server{
		addr:     cfg.Listen,
		store:    store,
		balancer: bal,
		registry: reg,
		log:      log,
	}

	s.httpSrv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       cfg.Timeouts.Read.Std(),
		WriteTimeout:      cfg.Timeouts.Write.Std(),
		IdleTimeout:       cfg.Timeouts.Idle.Std(),
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	return s
}

// Binds the listen addr and serves HTTP in background
// like l4, bind failure is returned synchronously before any goroutine exists
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.addr, err)
	}
	s.ln = ln

	//exists when: Shutdown (or close ) stops the server making Serve return
	// http.ErrServerClosed
	go func(ln net.Listener) {
		err := s.httpSrv.Serve(ln)
		if err != nil && err != http.ErrServerClosed {
			s.log.Error("http server stopped unexpectedly", "err", err)
		}
	}(ln)

	s.log.Info("listening", "addr", ln.Addr().String(), "mode", "l7")

	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Addr reports the address actually bound (differs from the configured address
// when the config asks for port 0). Valid only after a successful Start.
func (s *Server) Addr() net.Addr {
	return s.ln.Addr()
}

// stops accepting new requests and wait for in-flight ones to finished
// http.Server.Shudown() already implements drain
func (s *Server) ShutDown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}
