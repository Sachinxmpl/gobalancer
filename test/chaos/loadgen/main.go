package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// Open-loop constant-rate http load generator.
// It fires requests on a fixed schedule regardless of how fast response comes back
// write to csv, <unix_nanos>,<ok>    ok -> 1,0

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

	// 2  sec per request timeout
	client := &http.Client{Timeout: 2 * time.Second}

	var mu sync.Mutex
	var wg sync.WaitGroup

	tick := time.NewTicker(time.Second / time.Duration(*rate))
	defer tick.Stop()
	deadline := time.After(*dur)

	// exits when: duration elapses , each in-flight request is waited on
	for {
		select {
		case <-deadline:
			wg.Wait()
			return
		case <-tick.C:
			wg.Add(1)
			// open loop -- doesn't wait for response
			go func() {
				defer wg.Done()
				ok := 0
				resp, err := client.Get(*target)
				if err == nil {
					if resp.StatusCode == http.StatusOK {
						ok = 1
					}
					resp.Body.Close()
				}
				mu.Lock()
				fmt.Fprintf(w, "%d,%d\n", time.Now().UnixNano(), ok)
				mu.Unlock()
			}()
		}
	}

}
