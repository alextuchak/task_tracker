package internal_test

import (
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

func TestConfigAcceptsSettleWindowEqualToShutdownPhase(t *testing.T) {
	t.Setenv("CONFIG_PATH", "../config.yaml")
	cfg, err := internal.NewConfig()
	require.NoError(t, err)

	cfg.Shutdown.Phase = cfg.Outbox.SettleWindow()

	require.NoError(t, cfg.Validate(), "the settle window is allowed to use the whole phase")
}
