package balancer

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/health"
)

var ErrNoBackends = errors.New("no backends available")

type Balancer interface {

	// Takes input pool(Already filtered by caller as healthy)
	// Returns a pointer aliases pool's backing array
	// no copying
	// callers load snapshot once per connection, hold it until conn ends (never modified) -> safe to return pointer into slices
	// key  -> identifies the client (clientIp in L4, header in l7). Statesless algo's simply ignores this
	Pick(key string, pool []config.Backend) (*config.Backend, error)
}

func New(alg config.Algorithm, counter ConnCounter) (Balancer, error) {
	switch alg {
	case config.AlgRoundRobin:
		return &RoundRobin{}, nil
	case config.AlgWeightedRR:
		return NewWeightedRoundRobin(), nil
	case config.AlgLeastConns:
		if counter == nil {
			return nil, fmt.Errorf("balancer %q: needs a connection counter", alg)
		}
		return NewLeastConnections(counter), nil
	case config.AlgConsistentHash:
		return nil, fmt.Errorf("balancer %q: not implemented yet", alg)
	default:
		return nil, fmt.Errorf("balancer %q: unknown", alg)
	}
}

// Round Robin
type RoundRobin struct {
	n atomic.Uint64
}

func (r *RoundRobin) Pick(_ string, pool []config.Backend) (*config.Backend, error) {
	if len(pool) == 0 {
		return nil, ErrNoBackends
	}

	i := (r.n.Add(1) - 1) % uint64(len(pool))

	return &pool[i], nil
}

// weighted Round Robin
type WeightedRoundRobin struct {
	mu      sync.Mutex
	current map[string]int
}

func NewWeightedRoundRobin() *WeightedRoundRobin {
	return &WeightedRoundRobin{current: make(map[string]int)}
}

func (w *WeightedRoundRobin) Pick(_ string, pool []config.Backend) (*config.Backend, error) {
	if len(pool) == 0 {
		return nil, ErrNoBackends
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	total := 0
	best := -1
	bestScore := 0
	for i := range pool {
		addr := pool[i].Addr
		w.current[addr] += pool[i].Weight
		total += pool[i].Weight

		if best == -1 || w.current[addr] > bestScore {
			best = i
			bestScore = w.current[addr]
		}
	}
	w.current[pool[best].Addr] -= total

	return &pool[best], nil
}

// Least connections
type ConnCounter interface {
	Conns(addr string) int64
}

type LeastConnections struct {
	counter ConnCounter
	rr      atomic.Uint64
}

func NewLeastConnections(counter ConnCounter) *LeastConnections {
	return &LeastConnections{counter: counter}
}

func (l *LeastConnections) Pick(_ string, pool []config.Backend) (*config.Backend, error) {
	if len(pool) == 0 {
		return nil, ErrNoBackends
	}

	//snapshot count once, all passes below are consitent
	counts := make([]int64, len(pool))
	min := int64(math.MaxInt64)
	for i := range pool {
		counts[i] = l.counter.Conns(pool[i].Addr)
		if counts[i] < min {
			min = counts[i]
		}
	}

	// count the ties, the round-robin among them
	ties := 0
	for _, c := range counts {
		if c == min {
			ties++
		}
	}
	pick := int(l.rr.Add(1)-1) % ties

	for i := range pool {
		if counts[i] == min {
			if pick == 0 {
				return &pool[i], nil
			}
			pick--
		}
	}

	return &pool[len(pool)-1], nil
}

func HealthyBackends(pool []config.Backend, reg *health.Registry) []config.Backend {
	out := make([]config.Backend, 0, len(pool))
	for _, b := range pool {
		if reg.Get(b.Addr).Admits() {
			out = append(out, b)
		}
	}
	return out
}
