package balancer

import (
	"errors"
	"sync"
	"testing"

	"github.com/Sachinxmpl/gobalancer/internal/config"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func testPool(n int) []config.Backend {
	pool := make([]config.Backend, n)
	for i := range pool {
		pool[i] = config.Backend{Addr: string(rune('a' + i)), Weight: 1}
	}
	return pool
}

func TestRoundRobin_Order(t *testing.T) {
	pool := testPool(3)
	var rr RoundRobin

	want := []string{"a", "b", "c", "a", "b", "c", "a"}
	for i, w := range want {
		got, err := rr.Pick("", pool)
		if err != nil {
			t.Fatalf("pick %d: %v", i, err)
		}
		if got.Addr != w {
			t.Errorf("pick %d = %q, want %q", i, got.Addr, w)
		}
	}
}

func TestRoundRobin_EmptyPool(t *testing.T) {
	var rr RoundRobin

	for _, pool := range [][]config.Backend{nil, {}} {
		got, err := rr.Pick("", pool)
		if !errors.Is(err, ErrNoBackends) {
			t.Errorf("Pick(%v) error = %v, want ErrNoBackends", pool, err)
		}
		if got != nil {
			t.Errorf("Pick(%v) = %v, want nil backend alongside the error", pool, got)
		}
	}
}

func TestRoundRobin_ConcurrentDistribution(t *testing.T) {
	const (
		goroutines = 50
		picks      = 60
		backends   = 3
	)

	pool := testPool(backends)
	var rr RoundRobin

	counts := make([]map[string]int, goroutines)
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			local := make(map[string]int, backends)
			for i := 0; i < picks; i++ {
				b, err := rr.Pick("", pool)
				if err != nil {
					t.Errorf("goroutine %d pick %d: %v", g, i, err)
					return
				}
				local[b.Addr]++
			}
			counts[g] = local
		}(g)
	}
	wg.Wait()

	total := make(map[string]int, backends)
	for _, local := range counts {
		for addr, n := range local {
			total[addr] += n
		}
	}

	want := goroutines * picks / backends
	for _, b := range pool {
		if total[b.Addr] != want {
			t.Errorf("backend %q chosen %d times, want exactly %d", b.Addr, total[b.Addr], want)
		}
	}
}

// weightest RR tests

func weightedPool(weights ...int) []config.Backend {
	pool := make([]config.Backend, len(weights))
	names := []string{"A", "B", "C", "D", "E"}
	for i, w := range weights {
		pool[i] = config.Backend{
			Addr: names[i], Weight: w,
		}
	}
	return pool
}

func TestWeightedRoundRobin_SmoothOrder(t *testing.T) {
	pool := weightedPool(5, 1, 1) // A=5, B=1, C=1
	wrr := NewWeightedRoundRobin()

	want := []string{"A", "A", "B", "A", "C", "A", "A"}
	for i, w := range want {
		got, err := wrr.Pick("", pool)
		if err != nil {
			t.Fatalf("pick %d: %v", i, err)
		}
		if got.Addr != w {
			t.Errorf("pick %d = %q, want %q (full sequence must be A A B A C A A)", i, got.Addr, w)
		}
	}
}

func TestWeightedRoundRobin_Distribution(t *testing.T) {
	pool := weightedPool(3, 1) // A should get 3x B
	wrr := NewWeightedRoundRobin()

	counts := map[string]int{}
	const picks = 4000 // multiple of total weight (4)
	for i := 0; i < picks; i++ {
		got, _ := wrr.Pick("", pool)
		counts[got.Addr]++
	}

	// Exact ratio ->  the algorithm is periodic with period = total weight
	if counts["A"] != 3000 || counts["B"] != 1000 {
		t.Errorf("distribution A=%d B=%d, want A=3000 B=1000", counts["A"], counts["B"])
	}
}

func TestWeightedRoundRobin_EqualWeightsIsRoundRobin(t *testing.T) {
	pool := weightedPool(1, 1, 1)
	wrr := NewWeightedRoundRobin()

	want := []string{"A", "B", "C", "A", "B", "C"}
	for i, w := range want {
		got, _ := wrr.Pick("", pool)
		if got.Addr != w {
			t.Errorf("pick %d = %q, want %q", i, got.Addr, w)
		}
	}
}

// Least Connections
type fakeCounter map[string]int64

func (f fakeCounter) Conns(addr string) int64 {
	return f[addr]
}

func TestLeastConnections_PicksFewest(t *testing.T) {
	pool := []config.Backend{{Addr: "a"}, {Addr: "b"}, {Addr: "c"}}
	counts := fakeCounter{"a": 5, "b": 1, "c": 9}
	lc := NewLeastConnections(counts)

	for i := 0; i < 10; i++ {
		got, err := lc.Pick("", pool)
		if err != nil {
			t.Fatal(err)
		}
		if got.Addr != "b" {
			t.Errorf("pick %d = %q, want b", i, got.Addr)
		}
	}
}

func TestLeastConnections_TiesSpread(t *testing.T) {
	pool := []config.Backend{{Addr: "a"}, {Addr: "b"}, {Addr: "c"}}
	counts := fakeCounter{"a": 0, "b": 0, "c": 0} // cold start all tied
	lc := NewLeastConnections(counts)

	seen := map[string]int{}
	for i := 0; i < 300; i++ {
		got, _ := lc.Pick("", pool)
		seen[got.Addr]++
	}
	// round-robin tie-breaker must spread the cold-start burst
	for _, addr := range []string{"a", "b", "c"} {
		if seen[addr] != 100 {
			t.Errorf("tie spread: %q got %d, want 100 (herding on ties)", addr, seen[addr])
		}
	}
}
