package persistence

import (
	"errors"
	"fmt"
	"time"
)

type Config struct {
	DSN             string        `yaml:"dsn"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime" env-default:"5m"`
	MaxOpenConns    int           `yaml:"max_open_conns" env-default:"25"`
	MaxIdleConns    int           `yaml:"max_idle_conns" env-default:"25"`
}

func (c *Config) Validate() error {
	if c.DSN == "" {
		return errors.New("dsn is required")
	}
	if c.MaxOpenConns <= 0 {
		return fmt.Errorf("max_open_conns must be positive, got: %d", c.MaxOpenConns)
	}
	if c.MaxIdleConns < 0 {
		return fmt.Errorf("max_idle_conns must not be negative, got: %d", c.MaxIdleConns)
	}
	if c.ConnMaxLifetime <= 0 {
		return fmt.Errorf("conn_max_lifetime must be positive, got: %s", c.ConnMaxLifetime)
	}
	return nil
}
