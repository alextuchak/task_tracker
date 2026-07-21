package cache

import "errors"

type Config struct {
	Addr     string `yaml:"addr" env-default:"localhost:6379"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db" env-default:"0"`
}

func (c *Config) Validate() error {
	if c.Addr == "" {
		return errors.New("addr is required")
	}
	if c.DB < 0 {
		return errors.New("db must not be negative")
	}
	return nil
}
