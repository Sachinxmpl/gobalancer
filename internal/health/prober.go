package health

import (
	"context"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/Sachinxmpl/gobalancer/internal/config"
)

// Manager runs one prober goroutine per backend.
// Prober is only thing that can move a backend back toward healthy

type Manager struct {
	reg *Registry
	log *slog.Logger

	interval time.Duration
	timeout  time.Duration
	rise     int
	cooldown time.Duration

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	stopped bool
	wg      sync.WaitGroup
}

func NewManager(reg *Registry, h config.Health, log *slog.Logger) *Manager {
	return &Manager{
		reg:      reg,
		log:      log,
		interval: h.Active.Interval.Std(),
		timeout:  h.Active.Timeout.Std(),
		rise:     h.Active.Rise,
		cooldown: h.Passive.Cooldown.Std(),
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Starts probers for newly added backends and cancels probers for removed ones
// takes the diff Registry.Reconcile returns
func (m *Manager) Sync(added, removed []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return
	}

	for _, addr := range removed {
		if cancel, ok := m.cancels[addr]; ok {
			cancel()
			delete(m.cancels, addr)
		}
	}

	for _, addr := range added {
		if _, ok := m.cancels[addr]; ok {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.cancels[addr] = cancel

		st := m.reg.Get(addr)

		m.wg.Add(1)

		go func(addr string, st *State) {
			defer m.wg.Done()
			m.probeLoop(ctx, addr, st)
		}(addr, st)
	}
}

// Probe every ticker with ctx (is cancelled)
func (m *Manager) probeLoop(ctx context.Context, addr string, st *State) {
	t := time.NewTicker(withJitter(m.interval))
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !st.BeginProbation(m.cooldown) {
				continue
			}
			ok := m.probeOnce(ctx, addr)
			st.ProbeResult(ok, m.rise)
			if ok {
				m.log.Debug("probe ok", "backend", addr, "phase", st.Phase())
			}
		}
	}
}

// Reports whether addr accepts a TCP connection within probe timeout
func (m *Manager) probeOnce(ctx context.Context, addr string) bool {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}

	conn.Close()
	return true
}

// returns d with +- 10%, so all probers don't start together
func withJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return time.Second
	}
	delta := float64(d) * 0.1
	offset := (rand.Float64()*2 - 1) * delta
	return time.Duration(float64(d) + offset)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	for addr, cancel := range m.cancels {
		cancel()
		delete(m.cancels, addr)
	}
	m.mu.Unlock()
	m.wg.Wait()
}
