package main

import (
	"flag"

	"github.com/Sachinxmpl/gobalancer/internal/config"
)

func Check(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	path := fs.String("c", "config.yaml", "path to the config file")
	logLevel := fs.String("log-level", "info", "debug, info, warn or error")
	logFormat := fs.String("log-format", "text", "json or text")
	if err := fs.Parse(args); err != nil {
		return err
	}

	log, err := newLogger(*logLevel, *logFormat)
	if err != nil {
		return err
	}

	if _, err := config.Load(*path); err != nil {
		return err
	}

	log.Info("config validated", "path", *path)
	return nil
}
