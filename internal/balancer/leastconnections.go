package balancer

import (
	"math"
	"sync/atomic"

	"github.com/Sachinxmpl/loadgate/internal/config"
)

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
