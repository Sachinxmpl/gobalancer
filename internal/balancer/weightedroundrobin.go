package balancer

import (
	"sync"

	"github.com/Sachinxmpl/gobalancer/internal/config"
)

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