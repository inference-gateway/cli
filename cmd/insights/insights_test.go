package insights

import (
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
)

// TestEnsureModelRequiresAModel pins the reason this helper exists: a one-shot
// command inherits no model selection from a chat session, so an unset
// agent.model with no --model must fail before the gateway is started rather
// than asking it to analyze with an empty model.
func TestEnsureModelRequiresAModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Agent.Model = ""

	err := EnsureModel(t.Context(), &container.ServiceContainer{}, cfg, "")

	require.ErrorContains(t, err, "no model specified")
	require.ErrorContains(t, err, "--model")
}

// TestModelFlagIsShared keeps `infer insights` and `infer reset insights`
// spelling the flag the same way.
func TestModelFlagIsShared(t *testing.T) {
	command := &cobra.Command{}
	AddModelFlag(command)

	flag := command.Flags().Lookup(ModelFlag)
	require.NotNil(t, flag)
	require.Equal(t, "m", flag.Shorthand)

	require.NotNil(t, NewCommand(nil).Flags().Lookup(ModelFlag))
	require.True(t, strings.HasPrefix(NewCommand(nil).Use, "insights"))
}
