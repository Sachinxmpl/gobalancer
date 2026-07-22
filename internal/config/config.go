package config

import (
	"crypto/tls"
	"fmt"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

type Mode string

const (
	ModeL4 Mode = "l4"
	ModeL7 Mode = "l7"
)

type Algorithm string

const (
	AlgRoundRobin     Algorithm = "round_robin"
	AlgWeightedRR     Algorithm = "weighted_round_robin"
	AlgLeastConns     Algorithm = "least_connections"
	AlgConsistentHash Algorithm = "consistent_hash"
)

type Duration time.Duration

func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("duration must be string such as %q: %w", "2s", err)
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Whole configuration
type Config struct {
	Mode      Mode      `yaml:"mode"`
	Listen    string    `yaml:"listen"`
	Balancer  Algorithm `yaml:"balancer"`
	TLS       *TLS      `yaml:"tls"`
	Timeouts  Timeouts  `yaml:"timeouts"`
	Health    Health    `yaml:"health"`
	RateLimit RateLimit `yaml:"rate_limit"`

	Routes []Route `yaml:"routes"`

	Pools map[string][]Backend `yaml:"pools"`

	TLSCert *tls.Certificate `yaml:"-"`
}

type TLS struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type Timeouts struct {
	Dial    Duration `yaml:"dial"`
	Read    Duration `yaml:"read"`
	Write   Duration `yaml:"write"`
	Idle    Duration `yaml:"idle"`
	Request Duration `yaml:"request"`
	Drain   Duration `yaml:"drain"`
}

type Health struct {
	Active  ActiveHealth  `yaml:"active"`
	Passive PassiveHealth `yaml:"passive"`
}

type ActiveHealth struct {
	Interval Duration `yaml:"interval"`
	Timeout  Duration `yaml:"timeout"`
	Rise     int      `yaml:"rise"`
}

type PassiveHealth struct {
	Fall     int      `yaml:"fall"`
	Cooldown Duration `yaml:"cooldown"`
}

type RateLimit struct {
	GlobalRPS    int `yaml:"global_rps"`
	PerClientRPS int `yaml:"per_client_rps"`
}

type Route struct {
	Match RouteMatch `yaml:"match"`
	Pool  string     `yaml:"pool"`
}

type RouteMatch struct {
	PathPrefix string `yaml:"path_prefix"`
}

type Backend struct {
	Addr   string `yaml:"addr"`
	Weight int    `yaml:"weight"`
}

// Returns every backend across every pool, in a deterministic order (pools sorted by name, backends in file order).
func (c *Config) AllBackends() []Backend {
	names := make([]string, 0, len(c.Pools))
	for name := range c.Pools {
		names = append(names, name)
	}
	sort.Strings(names)

	// fix to consider may be later -> using len(c.Pools), using len(allbackends) will be optimal for capacity, may be first calculate count of allbackends and use that as capacity.
	out := make([]Backend, 0, len(c.Pools))
	for _, name := range names {
		out = append(out, c.Pools[name]...)
	}
	return out
}
