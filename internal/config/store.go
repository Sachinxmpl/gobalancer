package config

import (
	"sync/atomic"
)

type Store struct {
	p atomic.Pointer[Config]
}

// initializes a store holding c
func NewStore(c *Config) *Store {
	s := &Store{}
	s.p.Store(c)
	return s
}

// Returns the current snapshot
func (s *Store) Load() *Config {
	return s.p.Load()
}

// makes c -> the current snapshot, subsequent loads return it
func (s *Store) Publish(c *Config) {
	s.p.Store(c)
}
