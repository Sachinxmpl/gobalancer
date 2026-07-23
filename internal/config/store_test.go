package config

import (
	"fmt"
	"sync"
	"testing"
)

// generation builds a config whose Listen value and sole pool name are the same
// string, giving every snapshot an invariant a reader can check.
func generation(n int) *Config {
	name := fmt.Sprintf("pool-%d", n)
	return &Config{
		Mode:   ModeL4,
		Listen: name,
		Pools: map[string][]Backend{
			name: {{Addr: "127.0.0.1:9001", Weight: 1}},
		},
	}
}

func TestStore_LoadReturnsLatest(t *testing.T) {
	s := NewStore(generation(1))
	if got := s.Load().Listen; got != "pool-1" {
		t.Fatalf("Listen = %q, want %q", got, "pool-1")
	}

	s.Publish(generation(2))
	if got := s.Load().Listen; got != "pool-2" {
		t.Fatalf("after Publish, Listen = %q, want %q", got, "pool-2")
	}
}

// A reader holding an old snapshot keeps seeing the old values, which is what
// lets an in-flight connection finish under the config it started with.
func TestStore_OldSnapshotSurvivesPublish(t *testing.T) {
	s := NewStore(generation(1))

	held := s.Load()
	s.Publish(generation(2))

	if held.Listen != "pool-1" {
		t.Fatalf("held snapshot changed: Listen = %q, want %q", held.Listen, "pool-1")
	}
	if _, ok := held.Pools["pool-1"]; !ok {
		t.Fatalf("held snapshot lost its pool: %v", held.sortedPoolNames())
	}
}

// Only meaningful under -race: an unsynchronised *Config field would pass here
// too without it.
func TestStore_ConcurrentAccess(t *testing.T) {
	const (
		readers = 100
		writers = 100
		rounds  = 100
	)

	s := NewStore(generation(0))

	var wg sync.WaitGroup

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				c := s.Load()
				if c == nil {
					t.Error("Load returned nil")
					return
				}
				if _, ok := c.Pools[c.Listen]; !ok {
					t.Errorf("inconsistent snapshot: listen=%q pools=%v",
						c.Listen, c.sortedPoolNames())
					return
				}
			}
		}()
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				s.Publish(generation(id*rounds + j))
			}
		}(i)
	}

	wg.Wait()
}
