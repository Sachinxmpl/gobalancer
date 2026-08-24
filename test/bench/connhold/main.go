package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// connhold opens N concurrent connections to the target and holds them open for `hold`, then exits.
// Used to measure how the balancer's goroutine count and memory scale with the number of live connections.
// Keep-alive is disabled, so each request uses its own fresh connection -- conns == sockets.
func main() {
	target := flag.String("target", "http://127.0.0.1:8080/", "url to hold connections against")
	conns := flag.Int("conns", 100, "number of concurrent connections")
	hold := flag.Duration("hold", 8*time.Second, "how long to hold them open")
	flag.Parse()

	client := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), *hold)
	defer cancel()

	var mu sync.Mutex
	var failed int
	var wg sync.WaitGroup

	for range *conns {
		wg.Go(func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, *target, nil)
			resp, err := client.Do(req)
			if err != nil {
				// A dial/connection error -> we never held this connection.
				// A ctx timeout -> we held it to the deadline -- that is success.
				if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
					mu.Lock()
					failed++
					mu.Unlock()
				}
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		})
	}
	wg.Wait()

	fmt.Fprintf(os.Stderr, "conns=%d failed=%d\n", *conns, failed)
}
