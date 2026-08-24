package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

func main() {
	label := flag.String("label", "run", "row label")
	in := flag.String("in", "", "loadgen results csv (time_nanos,latency_ms,class)")
	flag.Parse()

	f, err := os.Open(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	var total, ok int
	var first, last int64 = -1, -1
	var lats []time.Duration
	var latSum time.Duration

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(sc.Text(), ",")
		if len(p) != 3 {
			continue
		}
		ns, err := strconv.ParseInt(p[0], 10, 64)
		if err != nil {
			continue
		}
		us, _ := strconv.ParseInt(p[1], 10, 64)

		total++
		if first < 0 || ns < first {
			first = ns
		}
		if ns > last {
			last = ns
		}
		if p[2] == "ok" {
			ok++
			d := time.Duration(us) * time.Microsecond
			lats = append(lats, d)
			latSum += d
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	span := time.Duration(last - first)
	var thr float64
	if span > 0 {
		thr = float64(ok) / span.Seconds()
	}
	var okPct float64
	if total > 0 {
		okPct = 100 * float64(ok) / float64(total)
	}
	var mean time.Duration
	if ok > 0 {
		mean = latSum / time.Duration(ok)
	}
	slices.Sort(lats)

	fmt.Printf("%s %d %.1f %.0f %v %v %v %v %v %v\n",
		*label, total, okPct, thr,
		pct(lats, 50), pct(lats, 90), pct(lats, 99), pct(lats, 99.9), mean, last3(lats))
}

func pct(s []time.Duration, p float64) time.Duration {
	if len(s) == 0 {
		return 0
	}
	i := int(p / 100 * float64(len(s)))
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}

func last3(s []time.Duration) time.Duration {
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1]
}
