package internal_test

import (
	"os"
	"path/filepath"
	"task_tracker/internal"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShippedConfigIsValid(t *testing.T) {
	t.Setenv("CONFIG_PATH", "../config.yaml")

	cfg, err := internal.NewConfig()

	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}

func TestConfigRejectsSettleWindowLongerThanShutdownPhase(t *testing.T) {
	t.Setenv("CONFIG_PATH", "../config.yaml")
	cfg, err := internal.NewConfig()
	require.NoError(t, err)

	cfg.Outbox.Budget = cfg.Shutdown.Phase

	require.ErrorContains(t, cfg.Validate(), "shutdown phase",
		"a tick that outlives the shutdown phase would be cut off mid-settlement")
}

func TestConfigRejectsWeakPasswordCost(t *testing.T) {
	t.Setenv("CONFIG_PATH", "../config.yaml")
	cases := []struct {
		name string
		cost int
	}{
		{"below the floor", 9},
		{"bcrypt minimum is not enough for production", 4},
		{"above bcrypt maximum", 32},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := internal.NewConfig()
			require.NoError(t, err)

			cfg.Auth.PasswordCost = c.cost

			require.ErrorContains(t, cfg.Validate(), "password_cost")
		})
	}
}

func TestConfigAcceptsSettleWindowEqualToShutdownPhase(t *testing.T) {
	t.Setenv("CONFIG_PATH", "../config.yaml")
	cfg, err := internal.NewConfig()
	require.NoError(t, err)

	cfg.Shutdown.Phase = cfg.Outbox.SettleWindow()

	require.NoError(t, cfg.Validate(), "the settle window is allowed to use the whole phase")
}

func TestDefaultsAloneAreConsistent(t *testing.T) {
	// only what has no sane default: everything else must come from the tags
	minimal := "mysql:\n  dsn: user:pass@tcp(127.0.0.1:3306)/db\n" +
		"auth:\n  secret: 0123456789abcdef0123456789abcdef\n"
	path := filepath.Join(t.TempDir(), "minimal.yaml")
	require.NoError(t, os.WriteFile(path, []byte(minimal), 0o600))
	t.Setenv("CONFIG_PATH", path)

	cfg, err := internal.NewConfig()
	require.NoError(t, err)

	require.NoError(t, cfg.Validate(),
		"the shutdown defaults have to fit the outbox defaults without a config file")
}
