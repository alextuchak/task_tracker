package ratelimit

import (
	"fmt"
	"time"
)

type Config struct {
	Requests int           `yaml:"requests" env-default:"100"`
	Window   time.Duration `yaml:"window" env-default:"1m"`
}

func (c *Config) Validate() error {
	if c.Requests < 1 {
		return fmt.Errorf("requests must be positive, got: %d", c.Requests)
	}
	if c.Window <= 0 {
		return fmt.Errorf("window must be positive, got: %s", c.Window)
	}
	return nil
}
