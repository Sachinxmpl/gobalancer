package reload

import (
	"log/slog"

	"github.com/Sachinxmpl/gobalancer/cmd/gobalancer/logger"
	"github.com/Sachinxmpl/gobalancer/internal/config"
	"github.com/Sachinxmpl/gobalancer/internal/health"
)

// Loads and validates the config at the path. If valid, swapt it into store and reconciles health
func Apply(path string, store *config.Store, reg *health.Registry, mgr *health.Manager, log *slog.Logger) error {
	logger.Debug("reload requested", "path", path)
	newCfg, err := config.Load(path)
	if err != nil {
		log.Error("config reload failed", "path", path, "err", err)
		return err
	}
	old := store.Load()
	warnRestartRequired(old, newCfg, log)

	// atomic swatp
	store.Publish(newCfg)

	added, removed := reg.Reconcile(newCfg.BackendAddrs())
	mgr.Sync(added, removed)

	log.Info("config reloaded", "backends", len(newCfg.BackendAddrs()), "added", len(added), "removed", len(removed))

	return nil
}

// Warn when reload changes a filed that needes restart
func warnRestartRequired(old, new *config.Config, log *slog.Logger) {
	if old.Mode != new.Mode {
		log.Warn("mode change needs a restart to take effect", "old", old.Mode, "new", new.Mode)
	}
	if old.Listen != new.Listen {
		log.Warn("listen address change needs a restart to take effect", "old", old.Listen, "new", new.Listen)
	}
	if old.Balancer != new.Balancer {
		log.Warn("balancer algorithm change needs a restart to take effect", "old", old.Balancer, "new", new.Balancer)
	}
}
