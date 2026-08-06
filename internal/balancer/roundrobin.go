package balancer

import (
	"sync/atomic"

	"github.com/Sachinxmpl/gobalancer/cmd/gobalancer/logger"
	"github.com/Sachinxmpl/gobalancer/internal/config"
)

type RoundRobin struct {
	n atomic.Uint64
}

func (r *RoundRobin) Pick(_ string, pool []config.Backend) (*config.Backend, error) {
	if len(pool) == 0 {
		logger.Error("round robin: no backends available")
		return nil, ErrNoBackends
	}

	i := (r.n.Add(1) - 1) % uint64(len(pool))

	logger.Debug("round robin: picked backend", "backend", pool[i].Addr)
	return &pool[i], nil
}
