package starter

import (
	"fmt"
	"time"
)

type Config struct {
	Timeout time.Duration `yaml:"timeout" env-default:"15s"`
}

func (c *Config) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got: %s", c.Timeout)
	}
	return nil
}
