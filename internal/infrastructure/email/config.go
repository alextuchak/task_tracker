package email

import (
	"errors"
	"fmt"
	"time"
)

type Config struct {
	BaseURL     string        `yaml:"base_url"`
	Timeout     time.Duration `yaml:"timeout" env-default:"3s"`
	MaxFailures uint32        `yaml:"max_failures" env-default:"3"`
	OpenFor     time.Duration `yaml:"open_for" env-default:"30s"`
}

func (c *Config) Validate() error {
	if c.BaseURL == "" {
		return errors.New("base_url is required")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got: %s", c.Timeout)
	}
	if c.MaxFailures == 0 {
		return errors.New("max_failures must be positive")
	}
	if c.OpenFor <= 0 {
		return fmt.Errorf("open_for must be positive, got: %s", c.OpenFor)
	}
	return nil
}
