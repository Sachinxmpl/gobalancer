package balancer

import (
	"errors"
	"fmt"
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

func New(alg config.Algorithm) (Balancer, error) {
	switch alg {
	case config.AlgRoundRobin:
		return &RoundRobin{}, nil
	case config.AlgWeightedRR, config.AlgLeastConns, config.AlgConsistentHash:
		return nil, fmt.Errorf("balancer %q: not implemented yet", alg)
	default:
		return nil, fmt.Errorf("balancer %q: unknown", alg)
	}
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
