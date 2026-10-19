package app

import (
	"testing"

	require "github.com/stretchr/testify/require"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
)

func TestChatApplication_NextStepSuggestionLifecycle(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gateway.Run = false
	cfg.Storage.Enabled = false

	c := container.NewServiceContainer(cfg)
	model := "test/model"
	app := NewChatApplication(
		cfg,
		[]string{model},
		model,
		tui.VersionInfo{},
		c.GetAgentSupervisor(),
		c.GetAgentService(),
		c.GetBackgroundTaskService(),
		c.GetBackgroundTaskRegistry(),
		c.GetConversationOptimizer(),
		c.GetConversationRepository(),
		c.GetFileService(),
		c.GetImageService(),
		c.GetSkillsService(),
		c.GetGitHubIssueService(),
		c.GetMCPSupervisor(),
		c.GetMessageQueue(),
		c.GetModelService(),
		c.GetPricingService(),
		c.GetSessionRollover(),
		c.GetStateStore(),
		c.GetTaskRetentionService(),
		c.GetThemeService(),
		c.GetToolService(),
		c.GetShortcutRegistry(),
		c.GetToolRegistry(),
		c.GetApprovalCoordinator(),
		c.GetChatCompletionRunner(),
		c.GetDirectExecutionService(),
		c.GetToolExecutionCoordinator(),
		c.GetShellHistoryStorage(),
		c.GetTokenEstimator(),
		nil,
	)

	c.GetStateStore().SetDimensions(120, 40)
	_, _ = app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	iv, ok := app.inputView.(*components.InputView)
	require.True(t, ok)

	_, _ = app.Update(tui.NextStepSuggestionEvent{Suggestion: "run the failing test"})
	require.True(t, iv.HasNextStepSuggestion())
	require.Equal(t, "", iv.GetInput())

	_, _ = iv.Update(tea.FocusMsg{})
	_, _ = iv.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	require.False(t, iv.HasNextStepSuggestion())
	_, _ = app.Update(tui.NextStepSuggestionEvent{Suggestion: "late"})
	require.False(t, iv.HasNextStepSuggestion())
	require.Equal(t, "r", iv.GetInput())

	iv.ClearInput()
	_, _ = app.Update(tui.NextStepSuggestionEvent{Suggestion: "again"})
	require.True(t, iv.HasNextStepSuggestion())
	_, _ = app.Update(tui.NextStepSuggestionEvent{})
	require.False(t, iv.HasNextStepSuggestion())
	require.Equal(t, "", iv.GetInput())
}
