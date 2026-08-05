package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/Sachinxmpl/gobalancer/internal/balancer"
	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/health"
	"github.com/Sachinxmpl/gobalancer/internal/listener"
	"github.com/Sachinxmpl/gobalancer/internal/proxy/l7"
	"github.com/Sachinxmpl/gobalancer/internal/reload"
)

type server interface {
	Start() error
	ShutDown(context.Context) error
}

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

	registry := health.NewRegistry()
	added, _ := registry.Reconcile(cfg.BackendAddrs())

	balancer, err := balancer.New(cfg.Balancer, registry)
	if err != nil {
		return err
	}

	mgr := health.NewManager(registry, cfg.Health, log)
	mgr.Sync(added, nil)
	defer mgr.Stop()

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

	var srv server
	switch cfg.Mode {
	case config.ModeL4:
		srv = listener.New(listener.Options{Addr: cfg.Listen, Store: store, Balancer: balancer, Registry: registry, Log: log})
	case config.ModeL7:
		srv = l7.New(l7.Options{Config: cfg, Store: store, Balancer: balancer, Registry: registry, Log: log})
	}

	if err := srv.Start(); err != nil {
		return err
	}

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	// exists then shutdown context is cancelled (SIGINT/SIGTERM)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				if err := reload.Apply(*path, store, registry, mgr, log); err != nil {
					log.Error("reloaded failed, keeping old config", "err", err)
				}
			}
		}
	}()

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
