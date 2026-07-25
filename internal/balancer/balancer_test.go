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
