package identity

import (
	"errors"
	"fmt"
	"time"
)

const minSecretLen = 32

type Config struct {
	Secret string        `yaml:"secret"`
	TTL    time.Duration `yaml:"ttl" env-default:"24h"`
}

func (c *Config) Validate() error {
	if c.Secret == "" {
		return errors.New("secret is required")
	}
	if len(c.Secret) < minSecretLen {
		return fmt.Errorf("secret must be at least %d bytes, got: %d", minSecretLen, len(c.Secret))
	}
	if c.TTL <= 0 {
		return fmt.Errorf("ttl must be positive, got: %s", c.TTL)
	}
	return nil
}
