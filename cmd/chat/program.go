package chat

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	app "github.com/inference-gateway/cli/internal/presentation/tui/app"
	colors "github.com/inference-gateway/cli/internal/presentation/tui/styles/colors"
)

// runChatProgram runs the chat TUI over services until the user quits, then
// prints the conversation and exitMessage for the session it ended on. The
// in-process and the daemon chat share it. A daemon chat that attached to a
// running turn, or a prompt still open, follows it once the program runs.
func runChatProgram(services *container.ServiceContainer, cfg *config.Config, models []string, defaultModel string, versionInfo tui.VersionInfo, exitMessage func(sessionID string) string) error {
	application := app.NewChatApplication(
		cfg,
		models,
		defaultModel,
		versionInfo,
		services.GetAgentSupervisor(),
		services.GetAgentService(),
		services.GetBackgroundTaskService(),
		services.GetBackgroundTaskRegistry(),
		services.GetConversationOptimizer(),
		services.GetConversationRepository(),
		services.GetFileService(),
		services.GetImageService(),
		services.GetSkillsService(),
		services.GetGitHubIssueService(),
		services.GetMCPSupervisor(),
		services.GetMessageQueue(),
		services.GetModelService(),
		services.GetPricingService(),
		services.GetSessionRollover(),
		services.GetStateStore(),
		services.GetTaskRetentionService(),
		services.GetThemeService(),
		services.GetToolService(),
		services.GetShortcutRegistry(),
		services.GetToolRegistry(),
		services.GetApprovalCoordinator(),
		services.GetChatCompletionRunner(),
		services.GetDirectExecutionService(),
		services.GetToolExecutionCoordinator(),
		services.GetShellHistoryStorage(),
		services.GetTokenEstimator(),
		services.GetNextStepSuggestionRunner(),
	)
	application.SetTranscriptStore(services.GetStorage())
	application.SetSessionTeleporter(services.GetSessionTeleporter())
	program := tea.NewProgram(application)
	notifier := programNotifier{program: program}
	services.SetUINotifier(notifier)
	if thread := services.GetDaemonThread(); thread != nil && thread.Unfollowed() {
		go notifier.Notify(tui.FollowRunEvent{})
	}

	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	defer stopHeartbeat()
	go runUIHeartbeat(heartbeatCtx, notifier, constants.UIHeartbeatInterval)

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("error running chat interface: %w", err)
	}

	application.PrintConversationHistory()

	endedSessionID := application.GetCurrentConversationID()
	if endedSessionID != "" {
		fmt.Println()
	}
	fmt.Println(colors.CreateColoredText(exitMessage(endedSessionID), colors.DimColor))
	return nil
}
