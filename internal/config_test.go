package internal_test

import (
	"os"
	"path/filepath"
	"task_tracker/internal"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

func TestTheAuthSectionIsReadAsOneBlock(t *testing.T) {
	body := "mysql:\n  dsn: user:pass@tcp(127.0.0.1:3306)/db\n" +
		"auth:\n  secret: 0123456789abcdef0123456789abcdef\n  ttl: 7h\n  password_cost: 12\n"
	path := filepath.Join(t.TempDir(), "auth.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	t.Setenv("CONFIG_PATH", path)

	cfg, err := internal.NewConfig()
	require.NoError(t, err)

	assert.Equal(t, 12, cfg.Auth.PasswordCost)
	assert.Equal(t, 7*time.Hour, cfg.Auth.Identity.TTL)
	assert.Len(t, cfg.Auth.Identity.Secret, 32)
}

// A key nobody reads is a setting nobody applied: almost every field has a
// default, so a typo looks exactly like the shipped value until production
// disagrees. trusted_cidrs has no default at all — its typo empties the trust
// list and puts the load balancer under the anonymous per-IP limit.
func TestATypoInTheConfigIsRefused(t *testing.T) {
	base := "mysql:\n  dsn: user:pass@tcp(127.0.0.1:3306)/db\n" +
		"auth:\n  secret: 0123456789abcdef0123456789abcdef\n"
	cases := map[string]string{
		"misspelled key":        base + "rate_limit:\n  request: 5\n",
		"misspelled nested key": base + "redis:\n  tasksttl: 90m\n",
		"unknown whole section": base + "totally_unknown_section:\n  x: 1\n",
		"misspelled trust list": base + "rate_limit_public:\n  trusted_cidr: [10.0.0.0/8]\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "c.yaml")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			t.Setenv("CONFIG_PATH", path)

			_, err := internal.NewConfig()

			require.Error(t, err, "the key was ignored and the default silently kept")
		})
	}
}
