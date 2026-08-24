package debug

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

// http server that mounts Go's pprof handlers
// Exposes Go's pprof profiling endpoints on private debug port.
// Bounded to localhost only , not publicly reachable

type Server struct {
	httpSrv *http.Server
	ln      net.Listener
	log     *slog.Logger
}

func NewServer(addr string, log *slog.Logger) *Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return &Server{
		httpSrv: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		log: log,
	}
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.httpSrv.Addr, err)
	}
	s.ln = ln

	go func(ln net.Listener) {
		err := s.httpSrv.Serve(ln)
		if err != nil && err != http.ErrServerClosed {
			s.log.Error("debug server stopped unexpectedly", "err", err)
		}
	}(ln)

	s.log.Info("debug server listening", "addr", ln.Addr().String())
	return nil
}

func (s *Server) ShutDown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}
