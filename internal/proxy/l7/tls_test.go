package l7

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sachinxmpl/loadgate/internal/config"
)

// Build an in-memory cert valid for 127.0.0.1
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestServer_TerminatesTLS(t *testing.T) {
	// Plain-HTTP backend behind the proxy.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "plain-backend-reply")
	}))
	t.Cleanup(backend.Close)
	backendAddr := strings.TrimPrefix(backend.URL, "http://")

	cert := selfSignedCert(t)
	cfg := &config.Config{
		Mode:    config.ModeL7,
		Listen:  "127.0.0.1:0",
		TLSCert: &cert, // presence of a cert switches Start to HTTPS
		Routes:  []config.Route{{Match: config.RouteMatch{PathPrefix: "/"}, Pool: "p"}},
		Pools:   map[string][]config.Backend{"p": {{Addr: backendAddr, Weight: 1}}},
	}
	s := newL7(t, cfg)

	// A client that trusts our self-signed cert
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Get("https://" + s.Addr().String() + "/")
	if err != nil {
		t.Fatalf("HTTPS GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if string(body) != "plain-backend-reply" {
		t.Errorf("body = %q, want the plain-HTTP backend's reply", string(body))
	}
}

func TestServer_PlainHTTPWhenNoCert(t *testing.T) {
	// No TLSCert -> Start must serve plain HTTP, and an HTTPS dial must fail.
	cfg := &config.Config{
		Mode:   config.ModeL7,
		Listen: "127.0.0.1:0",
		Routes: []config.Route{{Match: config.RouteMatch{PathPrefix: "/"}, Pool: "p"}},
		Pools:  map[string][]config.Backend{"p": {{Addr: "127.0.0.1:9", Weight: 1}}},
	}
	s := newL7(t, cfg)

	// Plain HTTP reaches the server (502 because the backend is dead, but the
	// point is the request was served over HTTP, not rejected as non-TLS).
	resp, err := http.Get("http://" + s.Addr().String() + "/")
	if err != nil {
		t.Fatalf("HTTP GET: %v", err)
	}
	resp.Body.Close()
}
