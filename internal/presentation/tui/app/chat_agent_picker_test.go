package app

import (
	"testing"

	require "github.com/stretchr/testify/require"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
)

func TestAgentRowsListMainFirstWithItsCapability(t *testing.T) {
	rows := agentRows([]agentdomain.SubagentInfo{
		{Name: "explore", Description: "Read-only search agent.", ReadOnly: true},
		{Name: agentdomain.DefaultMainAgentName, Description: "The default main agent."},
	})

	require.Equal(t, []components.PickerRow{
		{Value: agentdomain.DefaultMainAgentName, Label: agentdomain.DefaultMainAgentName, Tag: readWriteBadge, TagColor: "warning", Detail: "The default main agent."},
		{Value: "explore", Label: "explore", Tag: readOnlyBadge, TagColor: "success", Detail: "Read-only search agent."},
	}, rows)
}
