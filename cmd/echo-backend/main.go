package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := getenv("LISTEN_ADDR", ":9001")
	delay := parseDuration(getenv("RESPONSE_DELAY", "2s"))
	message := getenv("RESPONSE_TEXT", "response")

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "delay", delay)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(message))
	})

	logger.Info("backend listening", "addr", addr, "delay", delay)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("backend stopped", "err", err)
		os.Exit(1)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseDuration(value string) time.Duration {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 2 * time.Second
	}
	return duration
}
