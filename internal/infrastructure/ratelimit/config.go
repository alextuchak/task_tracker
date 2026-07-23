package ratelimit

import (
	"fmt"
	"net"
	"time"
)

type Config struct {
	TrustedCIDRs []string      `yaml:"trusted_cidrs"`
	Requests     int           `yaml:"requests" env-default:"100"`
	Window       time.Duration `yaml:"window" env-default:"1m"`
}

func (c *Config) Validate() error {
	if c.Requests < 1 {
		return fmt.Errorf("requests must be positive, got: %d", c.Requests)
	}
	if c.Window <= 0 {
		return fmt.Errorf("window must be positive, got: %s", c.Window)
	}
	for _, cidr := range c.TrustedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid trusted cidr %q: %w", cidr, err)
		}
	}
	return nil
}

func (c *Config) TrustedNets() []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(c.TrustedCIDRs))
	for _, cidr := range c.TrustedCIDRs {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}
