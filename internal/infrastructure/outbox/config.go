package outbox

import (
	"fmt"
	"time"
)

type Config struct {
	Tick        time.Duration `yaml:"tick" env-default:"1s"`
	Budget      time.Duration `yaml:"budget" env-default:"10s"`
	DedupTTL    time.Duration `yaml:"dedup_ttl" env-default:"1h"`
	Batch       int           `yaml:"batch" env-default:"100"`
	Workers     int           `yaml:"workers" env-default:"4"`
	MaxAttempts int           `yaml:"max_attempts" env-default:"8"`
}

func (c *Config) Validate() error {
	if c.Tick <= 0 {
		return fmt.Errorf("tick must be positive, got: %s", c.Tick)
	}
	if c.Budget <= 0 {
		return fmt.Errorf("budget must be positive, got: %s", c.Budget)
	}
	if c.Batch < 1 {
		return fmt.Errorf("batch must be positive, got: %d", c.Batch)
	}
	if c.Workers < 1 {
		return fmt.Errorf("workers must be positive, got: %d", c.Workers)
	}
	if c.MaxAttempts < 1 {
		return fmt.Errorf("max_attempts must be positive, got: %d", c.MaxAttempts)
	}
	return nil
}
