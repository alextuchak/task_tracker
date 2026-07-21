package closer

import (
	"fmt"
	"time"
)

type Config struct {
	Total time.Duration `yaml:"total" env-default:"20s"`
	Phase time.Duration `yaml:"phase" env-default:"10s"`
}

func (c *Config) Validate() error {
	if c.Total <= 0 || c.Phase <= 0 {
		return fmt.Errorf("timeouts must be positive, got total: %s, phase: %s", c.Total, c.Phase)
	}
	if c.Phase > c.Total {
		return fmt.Errorf("phase timeout %s must not exceed total %s", c.Phase, c.Total)
	}
	return nil
}
