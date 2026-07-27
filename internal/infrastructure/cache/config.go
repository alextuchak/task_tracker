package cache

import (
	"errors"
	"fmt"
	"time"
)

type Config struct {
	Addr     string        `yaml:"addr" env-default:"localhost:6379"`
	Password string        `yaml:"password"`
	DB       int           `yaml:"db" env-default:"0"`
	TasksTTL time.Duration `yaml:"tasks_ttl" env-default:"5m"`
	// Everything backed by redis here fails open, so a dead server must be
	// discovered fast: the driver's own defaults spend ~1.7s retrying before
	// the caller ever sees an error.
	DialTimeout   time.Duration `yaml:"dial_timeout" env-default:"1s"`
	ReadTimeout   time.Duration `yaml:"read_timeout" env-default:"500ms"`
	WriteTimeout  time.Duration `yaml:"write_timeout" env-default:"500ms"`
	MaxRetries    int           `yaml:"max_retries" env-default:"1"`
	DialerRetries int           `yaml:"dialer_retries" env-default:"1"`
}

func (c *Config) Validate() error {
	if c.Addr == "" {
		return errors.New("addr is required")
	}
	if c.DB < 0 {
		return errors.New("db must not be negative")
	}
	if c.TasksTTL <= 0 {
		return fmt.Errorf("tasks_ttl must be positive, got: %s", c.TasksTTL)
	}
	if c.DialTimeout <= 0 {
		return fmt.Errorf("dial_timeout must be positive, got: %s", c.DialTimeout)
	}
	if c.ReadTimeout <= 0 || c.WriteTimeout <= 0 {
		return fmt.Errorf("read_timeout and write_timeout must be positive, got: %s and %s",
			c.ReadTimeout, c.WriteTimeout)
	}
	// the driver reads 0 as "use my default of 3 retries", which is the
	// slow behaviour this config exists to avoid
	if c.MaxRetries < -1 || c.MaxRetries == 0 {
		return fmt.Errorf("max_retries must be -1 (none) or at least 1, got: %d", c.MaxRetries)
	}
	if c.DialerRetries < 1 {
		return fmt.Errorf("dialer_retries must be positive, got: %d", c.DialerRetries)
	}
	return nil
}
