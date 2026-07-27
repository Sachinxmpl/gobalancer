package health

import "sync"

type Registry struct {
	mu     sync.Mutex
	states map[string]*State
}

func NewRegistry() *Registry {
	return &Registry{states: make(map[string]*State)}
}

// Return the state for addr
// For address not in registry (new backend) (happens when a new config is published before reconcile has run) -> it returns a fresh healthy state rather than nil, be is trusted until proven otherwise
func (r *Registry) Get(addr string) *State {
	r.mu.Lock()
	st, ok := r.states[addr]
	r.mu.Unlock()
	if ok {
		return st
	}
	return &State{}
}

// Makes the registry's backend match addrs( of newly published config)
func (r *Registry) Reconcile(addrs []string) (added, removed []string) {
	want := make(map[string]struct{}, len(addrs))
	for _, a := range addrs {
		want[a] = struct{}{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Add newcomers,  address already present is untouched
	for a := range want {
		if _, ok := r.states[a]; !ok {
			r.states[a] = &State{}
			added = append(added, a)
		}
	}

	// Remove BE's no longer in cofig
	for a := range r.states {
		if _, ok := want[a]; !ok {
			delete(r.states, a)
			removed = append(removed, a)
		}
	}
	return added, removed
}
