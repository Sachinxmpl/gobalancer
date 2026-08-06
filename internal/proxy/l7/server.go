package l7

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Sachinxmpl/gobalancer/internal/balancer"
	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/health"
	"github.com/Sachinxmpl/gobalancer/internal/metrics"
	"github.com/Sachinxmpl/gobalancer/internal/ratelimit"
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
	limiter  *ratelimit.Limiter

	httpSrv *http.Server
	ln      net.Listener

	transport *http.Transport
	metrics   *metrics.Metrics
}

type Options struct {
	Config   *config.Config
	Store    *config.Store
	Balancer balancer.Balancer
	Registry *health.Registry
	Limiter  *ratelimit.Limiter
	Log      *slog.Logger
	Metrics  *metrics.Metrics
}

func New(o Options) *Server {
	s := &Server{
		addr:     o.Config.Listen,
		store:    o.Store,
		balancer: o.Balancer,
		registry: o.Registry,
		limiter:  o.Limiter,
		log:      o.Log,
		metrics:  o.Metrics,
	}

	s.httpSrv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       o.Config.Timeouts.Read.Std(),
		WriteTimeout:      o.Config.Timeouts.Write.Std(),
		IdleTimeout:       o.Config.Timeouts.Idle.Std(),
		ErrorLog:          slog.NewLogLogger(o.Log.Handler(), slog.LevelWarn),
	}

	s.transport = &http.Transport{
		DialContext:           (&net.Dialer{Timeout: o.Config.Timeouts.Dial.Std()}).DialContext,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: o.Config.Timeouts.Read.Std(),
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

	scheme := "http"
	if s.store.Load().TLSCert != nil {
		ln = tls.NewListener(ln, &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return s.store.Load().TLSCert, nil
			},
		})
		scheme = "https"
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

	s.log.Info("listening", "addr", ln.Addr().String(), "mode", "l7", "scheme", scheme)

	return nil
}

// Routes one request to a pool by path, picks a healthy be, and forwards request
// Follows 4 http proxy rules
// Clear RequestURI, strip hop-by-hop header, append X-Forwarded-For, Stream the body
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if s.limiter != nil && !s.limiter.Allow(clientIP(r)) {
		s.metrics.RateLimitRejected()
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	cfg := s.store.Load()

	// websocket/upgrade -> needs raw byte piping
	if r.Header.Get("Upgrade") != "" {
		http.Error(w, "upgrade not supported in l7 mode, use l4", http.StatusNotImplemented)
		return
	}

	poolName, ok := route(cfg, r.URL.Path)
	if !ok {
		http.Error(w, "no route for path", http.StatusNotFound)
		return
	}

	pool := balancer.HealthyBackends(cfg.Pools[poolName], s.registry)
	backend, err := s.balancer.Pick(clientIP(r), pool)

	if err != nil {
		http.Error(w, "no healthy backends", http.StatusServiceUnavailable)
		return
	}

	st := s.registry.Get(backend.Addr)
	st.AddConn()
	defer st.RemoveConn()

	ctx, cancel := context.WithTimeout(r.Context(), cfg.Timeouts.Request.Std())
	defer cancel()

	out := r.Clone(ctx)
	out.RequestURI = ""
	out.URL.Scheme = "http"
	out.URL.Host = backend.Addr
	out.Host = r.Host

	stripHopByHop(out.Header)
	appendXForwardedFor(out, clientIP(r))

	resp, err := s.transport.RoundTrip(out)

	if err != nil {
		// roundtrip error -> could not reach backend
		s.registry.Get(backend.Addr).ReportFailure(cfg.Health.Passive.Fall)
		http.Error(w, "bad gateway", http.StatusBadGateway)

		s.metrics.RequestObserved(
			backend.Addr,
			http.StatusBadGateway,
			time.Since(start).Seconds(),
		)
		return
	}

	defer resp.Body.Close()
	s.registry.Get(backend.Addr).ReportSuccess()

	stripHopByHop(resp.Header)
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	s.metrics.RequestObserved(
		backend.Addr,
		resp.StatusCode,
		time.Since(start).Seconds(),
	)
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

// related to a single connection
var hopByHop = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
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
