package identity

import (
	"errors"
	"fmt"
	"time"
)

const (
	minSecretLen = 32
	// bcrypt allows 4, but a production password hash must not be that cheap;
	// the test harness passes its own cost directly and never goes through here
	minCost = 10
	maxCost = 15
)

type Config struct {
	Secret       string        `yaml:"secret"`
	TTL          time.Duration `yaml:"ttl" env-default:"24h"`
	PasswordCost int           `yaml:"password_cost" env-default:"10"`
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
	if c.PasswordCost < minCost || c.PasswordCost > maxCost {
		return fmt.Errorf("password_cost must be %d..%d, got: %d", minCost, maxCost, c.PasswordCost)
	}
	return nil
}
