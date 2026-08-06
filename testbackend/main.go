package main

import (
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

// HTTP backend for test
// ENV -> PORT, NAME, DELAY
// /health returns 200 unless toggled

func main(){
	name := getEnv("NAME", "backend")
	port := getEnv("PORT", "8080")
	delay, _ := time.ParseDuration(getEnv("DELAY", "0ms"))

	var healthy atomic.Bool
	healthy.Store(true)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if healthy.Load(){
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(503)
	})

	mux.HandleFunc("/toggle", func(w http.ResponseWriter, r *http.Request) {
		healthy.Store(!healthy.Load())
		fmt.Fprintf(w, "healthy=%v\n", healthy.Load())
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if delay > 0{
			time.Sleep(delay)
		}
		fmt.Fprintf(w, "served by %s\n", name)
	})

	demosrv := &http.Server{
		Addr: ":" + port,
		Handler: mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	panic(demosrv.ListenAndServe())

}

func getEnv(k, dflt string) string{
	if v := os.Getenv(k); v != ""{
		return v 
	}
	return dflt
}