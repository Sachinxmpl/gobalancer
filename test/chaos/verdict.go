package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxBurst      = time.Second
	goroutineSlop = 25
	minEvictions  = 8
)

type rec struct {
	t     time.Time     // when the request was issued
	lat   time.Duration // how long it took
	class string        // ok, http_<code>, timeout, error
}

func main() {
	recs := readResults("test/chaos/results.csv")
	sort.Slice(recs, func(i, j int) bool { return recs[i].t.Before(recs[j].t) })

	// Find contiguous non-ok bursts (by issue time) and the worst one's duration.
	var worst time.Duration
	var bursts int
	var start time.Time
	inBurst := false

	classes := map[string]int{}
	lats := make([]time.Duration, 0, len(recs))

	for _, r := range recs {
		classes[r.class]++
		lats = append(lats, r.lat)

		ok := r.class == "ok"
		if !ok && !inBurst {
			inBurst, start = true, r.t
		} else if ok && inBurst {
			if d := r.t.Sub(start); d > worst {
				worst = d
			}
			bursts++
			inBurst = false
		}
	}

	base, final, evictions := readBaselineFinal("test/chaos/summary.txt")

	g0 := evictions >= minEvictions
	g3 := worst < maxBurst
	g1 := final-base <= goroutineSlop

	fmt.Printf("chaos registered: %d backend evictions (expect ~10) -> %s\n",
		evictions, passfail(g0))
	fmt.Printf("fast failover: %d error bursts, worst = %v (limit %v) -> %s\n",
		bursts, worst.Round(time.Millisecond), maxBurst, passfail(g3))
	fmt.Printf("no leak:  goroutines %d -> %d (slop %d) -> %s\n",
		base, final, goroutineSlop, passfail(g1))

	fmt.Println("outcome breakdown:")
	for _, c := range sortedKeys(classes) {
		fmt.Printf("   %-10s %d\n", c, classes[c])
	}
	if n := len(lats); n > 0 {
		slices.Sort(lats)
		fmt.Printf("latency: p50=%v p99=%v max=%v\n",
			lats[n*50/100], lats[min(n*99/100, n-1)], lats[n-1])
	}

	if g0 && g1 && g3 {
		fmt.Println("PASS")
		return
	}
	fmt.Println("FAIL")
	os.Exit(1)
}

func passfail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

// sortedKeys returns a map's keys in stable, sorted order for deterministic output.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func readResults(path string) []rec {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	var out []rec
	sc := bufio.NewScanner(f)

	for sc.Scan() {
		parts := strings.Split(sc.Text(), ",")
		if len(parts) != 3 {
			continue
		}

		ns, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		ms, _ := strconv.ParseInt(parts[1], 10, 64)

		out = append(out, rec{
			t:     time.Unix(0, ns),
			lat:   time.Duration(ms) * time.Millisecond,
			class: parts[2],
		})
	}

	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	return out
}

func readBaselineFinal(path string) (base, final, evictions int) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for tok := range strings.FieldsSeq(string(data)) {
		if v, ok := strings.CutPrefix(tok, "baseline="); ok {
			base, _ = strconv.Atoi(v)
		}
		if v, ok := strings.CutPrefix(tok, "final="); ok {
			final, _ = strconv.Atoi(v)
		}
		if v, ok := strings.CutPrefix(tok, "evictions="); ok {
			evictions, _ = strconv.Atoi(v)
		}
	}

	return base, final, evictions
}
