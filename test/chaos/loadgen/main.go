package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

// Open-loop constant-rate HTTP load generator.
// Fires a request on a fixed schedule regardless of how fast responses come back,
// so a stalled backend shows up as real latency/errors (no coordinated omission).
//
// CSV columns:  <issue_unix_nanos>,<latency_ms>,<class>
// class is one of: ok, http_<code>, timeout, error

func main() {
	target := flag.String("target", "http://localhost:8080/", "URL to hit")
	rate := flag.Int("rate", 500, "requests per second")
	dur := flag.Duration("duration", 5*time.Minute, "how long to run")
	out := flag.String("out", "results.csv", "output CSV path")
	flag.Parse()

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	// Reuse connections aggressively so the GENERATOR never becomes the bottleneck
	// (no ephemeral-port / TIME_WAIT exhaustion under open-loop bursts). If the
	// generator ran out of sockets it would record failures that the proxy never caused.
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        2000,
			MaxIdleConnsPerHost: 2000,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	tick := time.NewTicker(time.Second / time.Duration(*rate))
	defer tick.Stop()
	deadline := time.After(*dur)

	// exits when: duration elapses, then each in-flight request is waited on
	for {
		select {
		case <-deadline:
			wg.Wait()
			return
		case <-tick.C:
			issued := time.Now() // issue time, captured before the request runs
			// open loop -- doesn't wait for the response
			wg.Go(func() {
				class := do(client, *target)
				lat := time.Since(issued)
				mu.Lock()
				fmt.Fprintf(w, "%d,%d,%s\n", issued.UnixNano(), lat.Milliseconds(), class)
				mu.Unlock()
			})
		}
	}
}

// do performs one request and returns its outcome class.
func do(client *http.Client, target string) string {
	resp, err := client.Get(target)
	if err != nil {
		if ue, ok := err.(*url.Error); ok && ue.Timeout() {
			return "timeout"
		}
		return "error" // connection refused / reset / dns / etc.
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain so the connection can be reused
	if resp.StatusCode/100 == 2 {
		return "ok"
	}
	return "http_" + strconv.Itoa(resp.StatusCode)
}
