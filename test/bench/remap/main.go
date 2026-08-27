package main

import (
	"fmt"
	"hash/fnv"
	"os"

	"github.com/Sachinxmpl/loadgate/internal/balancer"
	"github.com/Sachinxmpl/loadgate/internal/config"
)

// remap answers "is the 1/N remap claim real?" for consistent hashing.
// It maps many keys across N backends, then drops each backend in turn and counts how
// many keys change backend -- for LoadGate's consistent-hash ring vs naive modulo-N.
// Averaging over every possible single drop makes the result representative (not luck of
// which backend we dropped), and the average is exactly 1/N only if nothing but the
// dropped backend's own keys move.
const (
	numBackends = 10
	numKeys     = 100000
)

func main() {
	full := makePool(numBackends)
	keys := makeKeys(numKeys)

	// Consistent hash: the real ring LoadGate uses (production vnode count).
	ch, err := balancer.New(config.AlgConsistentHash, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Baseline mapping with all backends present.
	base := make([]string, numKeys)
	for i, k := range keys {
		b, _ := ch.Pick(k, full)
		base[i] = b.Addr
	}

	var chTotal, modTotal int
	chMin, chMax := numKeys, 0
	for d := range numBackends {
		reduced := dropIndex(full, d)

		moved := 0
		for i, k := range keys {
			b, _ := ch.Pick(k, reduced)
			if b.Addr != base[i] {
				moved++
			}
		}
		chTotal += moved
		chMin = min(chMin, moved)
		chMax = max(chMax, moved)

		// Naive modulo-N, for comparison: index = hash(key) % len(pool).
		modMoved := 0
		for _, k := range keys {
			if full[hash(k)%uint64(len(full))].Addr != reduced[hash(k)%uint64(len(reduced))].Addr {
				modMoved++
			}
		}
		modTotal += modMoved
	}

	p := func(x float64) float64 { return 100 * x / float64(numKeys) }
	chAvg := float64(chTotal) / float64(numBackends)
	modAvg := float64(modTotal) / float64(numBackends)

	fmt.Printf("keys=%d  backends=%d  (drop one at a time, averaged over all %d)\n", numKeys, numBackends, numBackends)
	fmt.Printf("consistent_hash moved: avg %.1f%%  (min %.1f%%, max %.1f%%)\n",
		p(chAvg), p(float64(chMin)), p(float64(chMax)))
	fmt.Printf("modulo_n        moved: avg %.1f%%\n", p(modAvg))
	fmt.Printf("ideal (1/N):           %.1f%%\n", 100.0/float64(numBackends))
}

func makePool(n int) []config.Backend {
	p := make([]config.Backend, n)
	for i := range p {
		p[i] = config.Backend{Addr: fmt.Sprintf("10.0.0.%d:9000", i+1)}
	}
	return p
}

func dropIndex(pool []config.Backend, d int) []config.Backend {
	out := make([]config.Backend, 0, len(pool)-1)
	out = append(out, pool[:d]...)
	out = append(out, pool[d+1:]...)
	return out
}

func makeKeys(n int) []string {
	k := make([]string, n)
	for i := range k {
		k[i] = fmt.Sprintf("key-%d", i)
	}
	return k
}

func hash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
