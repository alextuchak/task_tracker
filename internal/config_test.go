package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validYAML = `
mysql:
  dsn: "user:pass@tcp(localhost:3306)/tasks?parseTime=true"
`

func TestNewConfigFromFileWithDefaults(t *testing.T) {
	t.Setenv("CONFIG_PATH", writeConfig(t, validYAML))

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validate error: %v", err)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("expected default addr :8080, got %q", cfg.HTTP.Addr)
	}
	if cfg.MySQL.MaxOpenConns != 25 {
		t.Errorf("expected default max open conns 25, got %d", cfg.MySQL.MaxOpenConns)
	}
	if cfg.Shutdown.Phase != 10*time.Second {
		t.Errorf("expected default phase timeout 10s, got %s", cfg.Shutdown.Phase)
	}
	if cfg.AppName != "task-tracker" {
		t.Errorf("expected default app name, got %q", cfg.AppName)
	}
}

func TestNewConfigEnvOverridesFile(t *testing.T) {
	t.Setenv("CONFIG_PATH", writeConfig(t, validYAML))
	t.Setenv("APP_VERSION", "1.2.3")
	t.Setenv("ENV", "develop")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppVersion != "1.2.3" {
		t.Errorf("expected env-provided version 1.2.3, got %q", cfg.AppVersion)
	}
	if cfg.Env != "develop" {
		t.Errorf("expected env-provided env develop, got %q", cfg.Env)
	}
}

func TestValidateMissingDSN(t *testing.T) {
	t.Setenv("CONFIG_PATH", writeConfig(t, "http:\n  addr: \":8080\"\n"))

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for empty mysql dsn")
	}
}

func TestNewConfigMissingFile(t *testing.T) {
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "nonexistent.yaml"))

	if _, err := NewConfig(); err == nil {
		t.Fatal("expected error for missing config file")
	}
}
