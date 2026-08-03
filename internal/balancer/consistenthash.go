package balancer

import (
	"hash/fnv"
	"slices"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/Sachinxmpl/gobalancer/internal/config"
)

const defaultVNodes = 150

type ring struct {
	points      []uint64
	owner       map[uint64]string
	fingerprint uint64
}

func fnv64a(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// Places vnodes virtual nodes per backend on the ring
func buildRing(pool []config.Backend, vnodes int, hash func(string) uint64) *ring {
	r := &ring{owner: make(map[uint64]string, len(pool)*vnodes)}

	for i := range pool {
		for v := 0; v < vnodes; v++ {
			h := hash(pool[i].Addr + "#" + strconv.Itoa(v))
			if _, taken := r.owner[h]; taken {
				continue
			}
			r.owner[h] = pool[i].Addr
			r.points = append(r.points, h)
		}
	}
	slices.Sort(r.points)

	return r
}

// Returns the address owning key: the first ring point at or clockwise
func (r *ring) lookup(key string, hash func(string) uint64) (string, bool) {
	if len(r.points) == 0 {
		return "", false
	}
	h := hash(key)
	idx := sort.Search(len(r.points), func(i int) bool { return r.points[i] >= h })
	if idx == len(r.points) {
		idx = 0
	}
	return r.owner[r.points[idx]], true
}

// Identified a pool by its set of addresses, order-independent so ring is build only when membership actually changes
func fingerprint(pool []config.Backend) uint64 {
	addrs := make([]string, len(pool))
	for i := range pool {
		addrs[i] = pool[i].Addr
	}
	sort.Strings(addrs)
	h := fnv.New64a()
	for _, a := range addrs {
		h.Write([]byte(a))
		h.Write([]byte{0})
	}
	return h.Sum64()
}

type ConsistentHash struct {
	vnodes int
	mu     sync.Mutex
	ring   atomic.Pointer[ring]
}

func NewConsistentHash(vnodes int) *ConsistentHash {
	return &ConsistentHash{vnodes: vnodes}
}

func (c *ConsistentHash) Pick(key string, pool []config.Backend) (*config.Backend, error) {
	if len(pool) == 0 {
		return nil, ErrNoBackends
	}
	r := c.ringFor(pool)
	addr, ok := r.lookup(key, fnv64a)
	if !ok {
		return nil, ErrNoBackends
	}
	for i := range pool {
		if pool[i].Addr == addr {
			return &pool[i], nil
		}
	}
	return nil, ErrNoBackends
}

// Returns a ring for the current pool, rebuilding only when membership
// changed. The common case (unchanged pool) is a lock-free atomic load.
func (c *ConsistentHash) ringFor(pool []config.Backend) *ring {
	fp := fingerprint(pool)
	if r := c.ring.Load(); r != nil && r.fingerprint == fp {
		return r
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := c.ring.Load(); r != nil && r.fingerprint == fp {
		return r // another goroutine rebuilt it while we waited
	}
	r := buildRing(pool, c.vnodes, fnv64a)
	r.fingerprint = fp
	c.ring.Store(r)
	return r
}
