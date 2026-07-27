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

	require.Error(t, cfg.Validate(),
		"a tick that outlives the shutdown phase would be cut off mid-settlement")
}
