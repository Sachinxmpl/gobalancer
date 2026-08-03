package balancer

import (
	"sync/atomic"

	"github.com/Sachinxmpl/gobalancer/internal/config"
)

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
