package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Sachinxmpl/gobalancer/cmd/gobalancer/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// server exposes metrics on its own port
type Server struct {
	httpSrv *http.Server
	ln      net.Listener
}

func NewServer(addr string, m *Metrics) *Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.Registry(), promhttp.HandlerOpts{}))

	return &Server{
		httpSrv: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
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
			logger.Error("metrics server stopped unexpectedly", "err", err)
		}
	}(ln)

	logger.Info("metrics server listening", "addr", ln.Addr().String())
	return nil
}

func (s *Server) ShutDown(ctx context.Context) error {
	logger.Info("shutting down metrics server")
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) Addr() net.Addr {
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}
