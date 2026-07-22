package config

import (
	"strings"
	"testing"
)

const minimal = `
mode: l4
listen: "127.0.0.1:8080"
pools:
  default:
    - addr: "127.0.0.1:9001"
`

func SampleTest(t *testing.T) {
	c, err := Parse(strings.NewReader(minimal))
	if err != nil {
		t.Fatalf("minimal config should be valid: %v", err)
	}
	t.Logf("balancer=%q dial=%v rise=%d fall=%d weight=%d",
		c.Balancer, c.Timeouts.Dial, c.Health.Active.Rise,
		c.Health.Passive.Fall, c.Pools["default"][0].Weight)

	bad := []string{
		"balancr: round_robin\n" + minimal,         // misspelled key
		strings.Replace(minimal, "l4", "tcp", 1),   // bad mode
		minimal + "  - addr: \"127.0.0.1:9001\"\n", // duplicate address
		strings.Replace(minimal, "l4", "l7", 1),    // l7 with no routes
		"",                                         // empty file
	}
	for i, src := range bad {
		if _, err := Parse(strings.NewReader(src)); err == nil {
			t.Errorf("case %d: expected an error, got none", i)
		} else {
			t.Logf("case %d rejected: %v", i, err)
		}
	}
}
