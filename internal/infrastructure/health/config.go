package health

import (
	"fmt"
	"time"
)

type Config struct {
	CheckTimeout time.Duration `yaml:"check_timeout" env-default:"2s"`
}

func (c *Config) Validate() error {
	if c.CheckTimeout <= 0 {
		return fmt.Errorf("check_timeout must be positive, got: %s", c.CheckTimeout)
	}
	return nil
}
