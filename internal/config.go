package internal

import (
	"errors"
	"fmt"
	"os"
	"task_tracker/internal/identity"
	"task_tracker/internal/infrastructure/cache"
	"task_tracker/internal/infrastructure/config"
	"task_tracker/internal/infrastructure/email"
	"task_tracker/internal/infrastructure/health"
	"task_tracker/internal/infrastructure/lifecycle"
	"task_tracker/internal/infrastructure/persistence"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

func NewConfig() (*Config, error) {
	var cfg Config
	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = "config.yaml"
	}
	if err := cleanenv.ReadConfig(path, &cfg); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return &cfg, nil
}

type Config struct {
	AppName    string                  `env-default:"task-tracker"`
	AppVersion string                  `env:"APP_VERSION" env-default:"dev"`
	Env        string                  `env:"ENV" env-default:"local"`
	Redis      cache.Config            `yaml:"redis"`
	Auth       identity.Config         `yaml:"auth"`
	Email      email.Config            `yaml:"email"`
	HTTP       HTTPConfig              `yaml:"http"`
	MySQL      persistence.Config      `yaml:"mysql"`
	Shutdown   lifecycle.CloserConfig  `yaml:"shutdown"`
	Startup    lifecycle.StarterConfig `yaml:"startup"`
	Health     health.Config           `yaml:"health"`
}

type HTTPConfig struct {
	Addr         string        `yaml:"addr" env-default:":8080"`
	ReadTimeout  time.Duration `yaml:"read_timeout" env-default:"10s"`
	WriteTimeout time.Duration `yaml:"write_timeout" env-default:"15s"`
	IdleTimeout  time.Duration `yaml:"idle_timeout" env-default:"60s"`
}

func (c *HTTPConfig) Validate() error {
	if c.Addr == "" {
		return errors.New("addr is required")
	}
	if c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 {
		return fmt.Errorf("timeouts must be positive, got read: %s, write: %s, idle: %s",
			c.ReadTimeout, c.WriteTimeout, c.IdleTimeout)
	}
	return nil
}

func (c *Config) Validate() error {
	if err := config.ValidateField("startup", &c.Startup); err != nil {
		return err
	}
	if err := config.ValidateField("http", &c.HTTP); err != nil {
		return err
	}
	if err := config.ValidateField("mysql", &c.MySQL); err != nil {
		return err
	}
	if err := config.ValidateField("redis", &c.Redis); err != nil {
		return err
	}
	if err := config.ValidateField("shutdown", &c.Shutdown); err != nil {
		return err
	}
	if err := config.ValidateField("health", &c.Health); err != nil {
		return err
	}
	if err := config.ValidateField("auth", &c.Auth); err != nil {
		return err
	}
	if err := config.ValidateField("email", &c.Email); err != nil {
		return err
	}
	return nil
}
