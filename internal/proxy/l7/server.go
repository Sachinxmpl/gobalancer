package l7

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
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

	transport *http.Transport
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

	s.transport = &http.Transport{
		DialContext:           (&net.Dialer{Timeout: cfg.Timeouts.Dial.Std()}).DialContext,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: cfg.Timeouts.Read.Std(),
		ForceAttemptHTTP2:     false,
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

// Finds the poll for path using longest-prefix match
func route(cfg *config.Config, path string) (pool string, ok bool) {
	best := -1
	for _, r := range cfg.Routes {
		p := r.Match.PathPrefix
		if strings.HasPrefix(path, p) && len(p) > best {
			best = len(p)
			pool = r.Pool
			ok = true
		}
	}
	return pool, ok
}

// related to a single conenction
var hopByHop = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorizatoin", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// Removes connection-scoped header, including any header names inside the Connection Header
func stripHopByHop(h http.Header) {
	for _, name := range h["Connection"] {
		for tok := range strings.SplitSeq(name, ",") {
			tok = strings.TrimSpace(tok)
			if tok != "" {
				h.Del(tok)
			}
		}
	}

	for _, name := range hopByHop {
		h.Del(name)
	}
}

// appends clientIP to the reques'ts forwarding trail, preserves addresses of any earlier hops
func appendXForwardedFor(out *http.Request, clientIP string) {
	if clientIP == "" {
		return
	}
	if prior := out.Header.Get("X-Forwarded-For"); prior != "" {
		clientIP = prior + ", " + clientIP
	}
	out.Header.Set("X-Forwarded-For", clientIP)
}

// Copies every header value from src to desk
func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Addr reports the address actually bound (differs from the configured address
// when the config asks for port 0). Valid only after a successful Start.
func (s *Server) Addr() net.Addr {
	return s.ln.Addr()
}

// stops accepting new requests and wait for in-flight ones to finished
// http.Server.Shudown() already implements drain
func (s *Server) ShutDown(ctx context.Context) error {
	err := s.httpSrv.Shutdown(ctx)
	s.transport.CloseIdleConnections()
	return err
}
