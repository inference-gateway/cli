package app

import (
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	"testing"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"

	tea "charm.land/bubbletea/v2"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	shortcutsmocks "github.com/inference-gateway/cli/tests/mocks/shortcuts"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"
)

type teaConversationRenderer struct {
	*tuimocks.FakeConversationRenderer
}

func (t *teaConversationRenderer) Init() tea.Cmd { return nil }

func (t *teaConversationRenderer) Update(tea.Msg) (tea.Model, tea.Cmd) { return t, nil }

func (t *teaConversationRenderer) View() tea.View { return tea.NewView("") }

type teaStatusComponent struct {
	*tuimocks.FakeStatusComponent
}

func (t *teaStatusComponent) Init() tea.Cmd { return nil }

func (t *teaStatusComponent) Update(tea.Msg) (tea.Model, tea.Cmd) { return t, nil }

func (t *teaStatusComponent) View() tea.View { return tea.NewView("") }

type teaHelpBarComponent struct {
	*tuimocks.FakeHelpBarComponent
}

func (t *teaHelpBarComponent) Init() tea.Cmd { return nil }

func (t *teaHelpBarComponent) Update(tea.Msg) (tea.Model, tea.Cmd) { return t, nil }

func (t *teaHelpBarComponent) View() tea.View { return tea.NewView("") }

type teaInputStatusBarComponent struct{}

func (t *teaInputStatusBarComponent) Init() tea.Cmd { return nil }

func (t *teaInputStatusBarComponent) Update(tea.Msg) (tea.Model, tea.Cmd) { return t, nil }

func (t *teaInputStatusBarComponent) View() tea.View { return tea.NewView("") }

func (t *teaInputStatusBarComponent) SetWidth(int) {}

func (t *teaInputStatusBarComponent) SetHeight(int) {}

func (t *teaInputStatusBarComponent) SetInputText(string) {}

func (t *teaInputStatusBarComponent) UpdateMCPStatus(*tui.MCPServerStatus) {}

func (t *teaInputStatusBarComponent) Focus() bool { return false }

func (t *teaInputStatusBarComponent) Blur() {}

func (t *teaInputStatusBarComponent) IsFocused() bool { return false }

func (t *teaInputStatusBarComponent) SelectNext() {}

func (t *teaInputStatusBarComponent) SelectPrev() {}

func (t *teaInputStatusBarComponent) SelectedAction() tui.StatusIndicatorAction {
	return tui.StatusIndicatorActionNone
}

func (t *teaInputStatusBarComponent) Render() string { return "" }

func newInputRoutingTestApp(t *testing.T, view tui.ViewState, draft string) (*ChatApplication, *components.InputView) {
	t.Helper()

	stateManager := statemanager.NewStateManager(false)
	if err := stateManager.TransitionToView(tui.ViewStateChat); err != nil {
		t.Fatalf("transitioning to chat: %v", err)
	}
	if view != tui.ViewStateChat {
		if err := stateManager.TransitionToView(view); err != nil {
			t.Fatalf("transitioning to %s: %v", view, err)
		}
	}

	modelService := &convmocks.FakeModelService{}
	inputView := components.NewInputViewWithName(modelService, t.TempDir(), scheddomain.SubagentHistoryMemoryOnly, nil)
	inputView.SetText(draft)
	messageQueue := &convmocks.FakeMessageQueue{}
	messageQueue.IsEmptyReturns(true)

	return &ChatApplication{
		stateManager:     stateManager,
		conversationView: &teaConversationRenderer{FakeConversationRenderer: &tuimocks.FakeConversationRenderer{}},
		statusView:       &teaStatusComponent{FakeStatusComponent: &tuimocks.FakeStatusComponent{}},
		inputView:        inputView,
		helpBar:          &teaHelpBarComponent{FakeHelpBarComponent: &tuimocks.FakeHelpBarComponent{}},
		inputStatusBar:   &teaInputStatusBarComponent{},
		messageQueue:     messageQueue,
	}, inputView
}

func printableKey(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(text)[0], Text: text}
}

func TestModelSelectionSearchDoesNotLeakIntoInput(t *testing.T) {
	app, inputView := newInputRoutingTestApp(t, tui.ViewStateModelSelection, "draft prompt")

	modelService := &convmocks.FakeModelService{}
	app.modelSelector = components.NewModelSelector(
		[]string{"openai/gpt-4o", "deepseek/deepseek-chat"},
		modelService,
		nil,
		nil,
		nil,
	)

	for _, key := range []tea.KeyPressMsg{
		printableKey("/"),
		printableKey("d"),
		printableKey("e"),
		printableKey("e"),
		printableKey("p"),
	} {
		_, _ = app.Update(key)
	}

	if got := inputView.GetInput(); got != "" {
		t.Fatalf("selector search keys leaked into disabled input, got %q", got)
	}

	pumpApp(app, tea.KeyPressMsg{Code: tea.KeyEnter}, 10)
	_, _ = app.Update(tea.FocusMsg{})

	if got := inputView.GetInput(); got != "draft prompt" {
		t.Fatalf("input after model selection = %q, want restored draft", got)
	}
}

func TestConversationSelectionDeleteKeysDoNotLeakIntoInput(t *testing.T) {
	app, inputView := newInputRoutingTestApp(t, tui.ViewStateConversationSelection, "existing draft")

	repo := &shortcutsmocks.FakePersistentConversationRepository{}
	selector := components.NewConversationSelector(repo, nil)
	model, _ := selector.Update(tui.ConversationsLoadedEvent{
		Conversations: []any{
			convdomain.ConversationSummary{ID: "conv-1", Title: "Conversation 1"},
			convdomain.ConversationSummary{ID: "conv-2", Title: "Conversation 2"},
		},
	})
	app.conversationSelector = model.(*components.ConversationSelectorImpl)

	for _, key := range []tea.KeyPressMsg{
		printableKey("d"),
		printableKey("y"),
		printableKey("d"),
		printableKey("y"),
		printableKey("d"),
		printableKey("y"),
	} {
		_, _ = app.Update(key)
	}

	if got := inputView.GetInput(); got != "" {
		t.Fatalf("conversation selector keys leaked into disabled input, got %q", got)
	}

	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	assertClearsStatus(t, cmd)
	_, _ = app.Update(tea.FocusMsg{})

	if got := inputView.GetInput(); got != "existing draft" {
		t.Fatalf("input after conversation selection = %q, want restored draft", got)
	}
}

func assertClearsStatus(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command that clears the transient status")
	}

	msg := cmd()
	if isClearStatusEvent(msg) {
		return
	}

	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected SetStatusEvent or BatchMsg, got %T", msg)
	}

	for _, sub := range batch {
		if isClearStatusEvent(sub()) {
			return
		}
	}

	t.Fatal("expected a blank non-spinner SetStatusEvent")
}

func isClearStatusEvent(msg tea.Msg) bool {
	status, ok := msg.(tui.SetStatusEvent)
	return ok && status.Message == "" && !status.Spinner && status.StatusType == tui.StatusDefault
}
