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
	return nil
}
