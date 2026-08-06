package config

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Sachinxmpl/gobalancer/cmd/gobalancer/logger"
	"gopkg.in/yaml.v3"
)

//	default values for optional settings
//
// must satisfy Validate.go
const (
	defaultBalancer = AlgRoundRobin

	defaultDialTimeout    = 2 * time.Second
	defaultReadTimeout    = 30 * time.Second
	defaultWriteTimeout   = 30 * time.Second
	defaultIdleTimeout    = 60 * time.Second
	defaultRequestTimeout = 30 * time.Second
	defaultDrainTimeout   = 15 * time.Second

	defaultProbeInterval = 2 * time.Second
	defaultProbeTimeout  = 500 * time.Millisecond
	defaultRise          = 2
	defaultFall          = 3
	defaultCooldown      = 10 * time.Second

	defaultWeight = 1
)

// Prepares config : parse -> default -> validate , resolve TLS keypair
// returned *Config -> ready to publish to store
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		logger.Error(fmt.Sprintf("reading config %s: %v", path, err))
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	c, err := Parse(bytes.NewReader(raw))
	if err != nil {
		logger.Error(fmt.Sprintf("%s: %v", path, err))
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	logger.Debug(fmt.Sprintf("config %s: parsed successfully", path))

	if err := c.loadTLS(); err != nil {
		logger.Error(fmt.Sprintf("%s: %v", path, err))
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return c, nil
}

// Decoes YAMl, applies defaults, and runs checks (one that can be made by looking at values)
// doesnot touch filesystem
func Parse(r io.Reader) (*Config, error) {
	var c Config

	dec := yaml.NewDecoder(r)
	// misspelled key must be error
	dec.KnownFields(true)

	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			logger.Error("config is empty")
			return nil, errors.New("config is empty")
		}
		logger.Error(fmt.Sprintf("parsing config: %v", err))
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// apply defaults before validation
	c.applyDefaults()

	if err := c.Validate(); err != nil {
		logger.Error(fmt.Sprintf("validating config: %v", err))
		return nil, err
	}
	return &c, nil
}

// Rule -> zero vlaue === not set
// for RateLimits zero is legal -> means unlimited
func (c *Config) applyDefaults() {
	if c.Balancer == "" {
		c.Balancer = defaultBalancer
	}

	setDefaultDuration(&c.Timeouts.Dial, defaultDialTimeout)
	setDefaultDuration(&c.Timeouts.Read, defaultReadTimeout)
	setDefaultDuration(&c.Timeouts.Write, defaultWriteTimeout)
	setDefaultDuration(&c.Timeouts.Idle, defaultIdleTimeout)
	setDefaultDuration(&c.Timeouts.Request, defaultRequestTimeout)
	setDefaultDuration(&c.Timeouts.Drain, defaultDrainTimeout)

	setDefaultDuration(&c.Health.Active.Interval, defaultProbeInterval)
	setDefaultDuration(&c.Health.Active.Timeout, defaultProbeTimeout)
	if c.Health.Active.Rise == 0 {
		c.Health.Active.Rise = defaultRise
	}

	if c.Health.Passive.Fall == 0 {
		c.Health.Passive.Fall = defaultFall
	}
	setDefaultDuration(&c.Health.Passive.Cooldown, defaultCooldown)

	for _, pool := range c.Pools {
		for i := range pool {
			if pool[i].Weight == 0 {
				pool[i].Weight = defaultWeight
			}
		}
	}
}

func setDefaultDuration(d *Duration, def_value time.Duration) {
	if *d == 0 {
		*d = Duration(def_value)
	}
}

// Resolves the keypair if configured
func (c *Config) loadTLS() error {
	if c.TLS == nil {
		logger.Debug("TLS is not configured, skipping TLS keypair loading")
		return nil
	}

	cert, err := tls.LoadX509KeyPair(c.TLS.Cert, c.TLS.Key)
	if err != nil {
		logger.Error(fmt.Sprintf("tls: loading keypair (cert %q, key %q): %v", c.TLS.Cert, c.TLS.Key, err))
		return fmt.Errorf("tls: loading keypair (cert %q, key %q): %w",
			c.TLS.Cert, c.TLS.Key, err)
	}

	c.TLSCert = &cert
	logger.Debug(fmt.Sprintf("tls: loaded keypair (cert %q, key %q)", c.TLS.Cert, c.TLS.Key))
	return nil
}
