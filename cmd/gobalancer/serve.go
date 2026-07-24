package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/listener"
)

func Serve(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	path := fs.String("c", "config.yaml", "path to the config file")
	logLevel := fs.String("log-level", "info", "debug, info, warn or error")
	logFormat := fs.String("log-format", "json", "json or text")
	if err := fs.Parse(args); err != nil {
		return err
	}

	log, err := newLogger(*logLevel, *logFormat)
	if err != nil {
		return err
	}

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}

	store := config.NewStore(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting",
		"config", *path,
		"mode", cfg.Mode,
		"listen", cfg.Listen,
		"balancer", cfg.Balancer,
		"pools", len(cfg.Pools),
		"backends", len(cfg.AllBackends()),
	)

	srv := listener.New(cfg.Listen, store, log)
	if err := srv.Start(); err != nil {
		return err
	}

	<-ctx.Done()
	stop()

	drain := store.Load().Timeouts.Drain.Std()
	log.Info("shutdown signal received, draining", "timeout", drain)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()

	if err := srv.ShutDown(shutdownCtx); err != nil {
		log.Warn("drain did not complete cleanly", "err", err)
	}

	log.Info("stopped")
	return nil
}
