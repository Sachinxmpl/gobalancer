package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type stubSource struct{}

func (stubSource) BackendAddrs() []string {
	return nil
}

func (stubSource) Conns(string) int64 {
	return 0
}

func (stubSource) Up(string) int64 {
	return 0
}

func TestMetrics_ExposesAndCounts(t *testing.T) {
	m := New(stubSource{})
	m.RequestObserved("10.0.0.1:9000", 200, 0.01)
	m.RequestObserved("10.0.0.1:9000", 503, 0.02)
	m.RateLimitRejected()

	srv := httptest.NewServer(promhttp.HandlerFor(m.Registry(), promhttp.HandlerOpts{}))
	defer srv.Close()
	resp, _ := http.Get(srv.URL)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	text := string(body)

	if !strings.Contains(text, `gobalancer_requests_total{backend="10.0.0.1:9000",status_class="2xx"} 1`) {
		t.Error("2xx counter missing or mislabeled")
	}
	if !strings.Contains(text, `status_class="5xx"} 1`) {
		t.Error("5xx counter missing")
	}
	if strings.Contains(text, `status_class="503"`) {
		t.Error("rae status code leaked as label cardinality violation")
	}
	if !strings.Contains(text, "gobalancer_ratelimit_rejections_total 1") {
		t.Error("rejection counter missing")
	}
	if !strings.Contains(text, "go_goroutines") {
		t.Error("Go runtime metrics (leak detector) missing")
	}
}

func TestStatusClass(t *testing.T) {
	cases := map[int]string{200: "2xx", 204: "2xx", 301: "3xx", 404: "4xx", 500: "5xx", 503: "5xx"}
	for code, want := range cases {
		if got := statusClass(code); got != want {
			t.Errorf("statusClass(%d) = %q, want %q", code, got, want)
		}
	}
}
