package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	approvalcoord "github.com/inference-gateway/cli/internal/services/approvalcoord"
	components "github.com/inference-gateway/cli/internal/ui/components"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// TestQuestionFormReproChain checks the coordinator -> shared StateManager ->
// QuestionFormView render path in isolation.
func TestQuestionFormReproChain(t *testing.T) {
	sm := services.NewStateManager(false)
	coord := approvalcoord.NewService(approvalcoord.Options{StateManager: sm})

	ch := make(chan []domain.UserQuestionAnswer, 1)
	if cmd := coord.HandleUserQuestionRequested(domain.UserQuestionRequestedEvent{
		Questions: []domain.UserQuestion{
			{Header: "Backend", Question: "Which storage backend?", Options: []domain.UserQuestionOption{{Label: "sqlite"}, {Label: "postgres"}}},
		},
		ResponseChan: ch,
	}); cmd == nil {
		t.Fatal("expected a status command from the coordinator")
	}

	st := sm.GetUserQuestionUIState()
	if st == nil || len(st.Questions) != 1 {
		t.Fatalf("coordinator did not set the shared user question state: %+v", st)
	}

	fv := components.NewQuestionFormView(styles.NewProvider(domain.NewThemeProvider()), sm)
	fv.SetWidth(80)
	if out := fv.Render(); !strings.Contains(out, "Backend") || !strings.Contains(out, "sqlite") {
		t.Fatalf("form did not render from shared StateManager:\n%q", out)
	}
}

// TestChatApplication_QuestionFormRendersOnEvent reproduces the FULL live path:
// build a real ChatApplication from the container, drive the
// UserQuestionRequestedEvent through Update(), and assert the form appears in
// the rendered chat interface.
func TestChatApplication_QuestionFormRendersOnEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gateway.Run = false
	cfg.Storage.Enabled = false

	c := container.NewServiceContainer(cfg)

	model := "test/model"
	app := NewChatApplication(
		cfg,
		[]string{model},
		model,
		domain.VersionInfo{},
		c.GetAgentManager(),
		c.GetAgentService(),
		c.GetBackgroundTaskService(),
		c.GetConversationOptimizer(),
		c.GetConversationRepository(),
		c.GetFileService(),
		c.GetImageService(),
		c.GetSkillsService(),
		c.GetGitHubIssueService(),
		c.GetMCPManager(),
		c.GetMessageQueue(),
		c.GetModelService(),
		c.GetPricingService(),
		c.GetSessionRolloverManager(),
		c.GetStateManager(),
		c.GetTaskRetentionService(),
		c.GetThemeService(),
		c.GetToolService(),
		c.GetShortcutRegistry(),
		c.GetToolRegistry(),
		c.GetA2ATaskCoordinator(),
		c.GetApprovalCoordinator(),
		c.GetChatCompletionRunner(),
		c.GetDirectExecutionService(),
		c.GetToolExecutionCoordinator(),
	)

	c.GetStateManager().SetDimensions(120, 40)
	_, _ = app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	ch := make(chan []domain.UserQuestionAnswer, 1)
	_, _ = app.Update(domain.UserQuestionRequestedEvent{
		Questions: []domain.UserQuestion{
			{Header: "Backend", Question: "Which storage backend?", Options: []domain.UserQuestionOption{{Label: "sqlite"}, {Label: "postgres"}}},
		},
		ResponseChan: ch,
	})

	st := c.GetStateManager().GetUserQuestionUIState()
	if st == nil || len(st.Questions) != 1 {
		t.Fatalf("question state not set after Update: %+v", st)
	}

	out := app.viewContent()
	if !strings.Contains(out, "Backend") || !strings.Contains(out, "sqlite") {
		t.Fatalf("form not present in rendered chat interface; got:\n%s", out)
	}

	_, _ = app.Update(domain.ToolExecutionProgressEvent{
		ToolCallID: "call_1", ToolName: "AskUserQuestion", Status: "running", Message: "Processing...",
	})
	_, _ = app.Update(domain.ToolCallUpdateEvent{ToolCallID: "call_1", ToolName: "AskUserQuestion"})

	if st2 := c.GetStateManager().GetUserQuestionUIState(); st2 == nil {
		t.Fatal("question state was cleared by a tool progress/update tick")
	}
	out2 := app.viewContent()
	if !strings.Contains(out2, "Backend") {
		t.Fatalf("form disappeared after a tool progress tick; got:\n%s", out2)
	}

	_, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if c.GetStateManager().GetUserQuestionUIState() != nil {
		t.Fatal("form was not cleared after Enter/submit")
	}
	select {
	case answers := <-ch:
		if len(answers) != 1 || len(answers[0].SelectedLabels) != 1 || answers[0].SelectedLabels[0] != "sqlite" {
			t.Fatalf("unexpected answers delivered: %+v", answers)
		}
	default:
		t.Fatal("no answers delivered on the channel after Enter/submit (tool would block forever)")
	}
}
