package main

import (
	"log/slog"

	"github.com/Sachinxmpl/gobalancer/cmd/gobalancer/logger"
)

func newLogger(level, format string) (*slog.Logger, error) {
	return logger.NewLogger(level, format)
}
