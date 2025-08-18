package keybinding

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/ui"
)

type testKeyHandlerContext struct {
	currentView       domain.ViewState
	inputText         string
	expandToggleCalls int
	messageSentCalls  int
	stateManager      *services.StateManager
}

func (t *testKeyHandlerContext) GetStateManager() *services.StateManager {
	if t.stateManager == nil {
		// Create a minimal state manager for testing
		t.stateManager = services.NewStateManager(false)
		// Transition to the desired view
		_ = t.stateManager.TransitionToView(t.currentView)
	}
	return t.stateManager
}

func (t *testKeyHandlerContext) GetServices() *container.ServiceContainer {
	return nil
}

func (t *testKeyHandlerContext) GetConversationView() ui.ConversationRenderer {
	return &testConversationRenderer{}
}

func (t *testKeyHandlerContext) GetInputView() ui.InputComponent {
	return &testInputComponent{text: t.inputText}
}

func (t *testKeyHandlerContext) GetStatusView() ui.StatusComponent {
	return &testStatusComponent{}
}

func (t *testKeyHandlerContext) ToggleToolResultExpansion() {
	t.expandToggleCalls++
}

func (t *testKeyHandlerContext) SendMessage() tea.Cmd {
	t.messageSentCalls++
	return nil
}

func (t *testKeyHandlerContext) CancelCurrentOperation() tea.Cmd {
	return nil
}

func (t *testKeyHandlerContext) HasPendingApproval() bool {
	return false
}

func (t *testKeyHandlerContext) GetPageSize() int {
	return 20
}

func (t *testKeyHandlerContext) ApproveToolCall() tea.Cmd {
	return nil
}

func (t *testKeyHandlerContext) DenyToolCall() tea.Cmd {
	return nil
}

// Test implementations of interfaces

type testConversationRenderer struct{}

func (t *testConversationRenderer) SetConversation([]domain.ConversationEntry) {}

func (t *testConversationRenderer) GetScrollOffset() int {
	return 0
}

func (t *testConversationRenderer) CanScrollUp() bool {
	return false
}

func (t *testConversationRenderer) CanScrollDown() bool {
	return false
}

func (t *testConversationRenderer) ToggleToolResultExpansion(index int) {}

func (t *testConversationRenderer) ToggleAllToolResultsExpansion() {}

func (t *testConversationRenderer) IsToolResultExpanded(index int) bool {
	return false
}

func (t *testConversationRenderer) SetWidth(width int) {}

func (t *testConversationRenderer) SetHeight(height int) {}

func (t *testConversationRenderer) Render() string {
	return ""
}

type testInputComponent struct {
	text   string
	cursor int
}

func (t *testInputComponent) GetInput() string {
	return t.text
}

func (t *testInputComponent) SetText(text string) {
	t.text = text
}

func (t *testInputComponent) GetCursor() int {
	return t.cursor
}

func (t *testInputComponent) SetCursor(pos int) {
	t.cursor = pos
}

func (t *testInputComponent) NavigateHistoryUp() {
	// Test implementation - no-op
}

func (t *testInputComponent) NavigateHistoryDown() {
	// Test implementation - no-op
}

func (t *testInputComponent) ClearInput() {}

func (t *testInputComponent) SetPlaceholder(text string) {}

func (t *testInputComponent) SetWidth(width int) {}

func (t *testInputComponent) SetHeight(height int) {}

func (t *testInputComponent) Render() string {
	return ""
}

func (t *testInputComponent) HandleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (t *testInputComponent) CanHandle(key tea.KeyMsg) bool {
	return false
}

type testStatusComponent struct{}

func (t *testStatusComponent) ShowStatus(message string) {}

func (t *testStatusComponent) ShowError(message string) {}

func (t *testStatusComponent) ShowSpinner(message string) {}

func (t *testStatusComponent) ClearStatus() {}

func (t *testStatusComponent) IsShowingError() bool {
	return false
}

func (t *testStatusComponent) IsShowingSpinner() bool {
	return false
}

func (t *testStatusComponent) SetTokenUsage(usage string) {}

func (t *testStatusComponent) SetWidth(width int) {}

func (t *testStatusComponent) SetHeight(height int) {}

func (t *testStatusComponent) Render() string {
	return ""
}

func TestRegistryCreation(t *testing.T) {
	registry := NewRegistry()
	if registry == nil {
		t.Fatal("Expected registry to be created")
	}

	mockContext := &testKeyHandlerContext{
		currentView: domain.ViewStateChat,
		inputText:   "test message",
	}

	actions := registry.GetActiveActions(mockContext)

	if len(actions) == 0 {
		t.Fatal("Expected default actions to be registered")
	}

	foundQuit := false
	foundToggle := false
	for _, action := range actions {
		switch action.ID {
		case "quit":
			foundQuit = true
		case "toggle_tool_expansion":
			foundToggle = true
		}
	}

	if !foundQuit {
		t.Error("Expected 'quit' action to be registered")
	}
	if !foundToggle {
		t.Error("Expected 'toggle_tool_expansion' action to be registered")
	}
}

func TestKeyResolution(t *testing.T) {
	registry := NewRegistry()
	mockContext := &testKeyHandlerContext{
		currentView: domain.ViewStateChat,
		inputText:   "test message",
	}

	action := registry.Resolve("ctrl+c", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+c to resolve to an action")
	}
	if action.ID != "quit" {
		t.Errorf("Expected ctrl+c to resolve to 'quit', got %s", action.ID)
	}

	action = registry.Resolve("ctrl+r", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+r to resolve to an action")
	}
	if action.ID != "toggle_tool_expansion" {
		t.Errorf("Expected ctrl+r to resolve to 'toggle_tool_expansion', got %s", action.ID)
	}

	action = registry.Resolve("ctrl+z", mockContext)
	if action != nil {
		t.Errorf("Expected ctrl+z to not resolve to any action, got %s", action.ID)
	}
}

func TestViewContextFiltering(t *testing.T) {
	registry := NewRegistry()

	chatContext := &testKeyHandlerContext{
		currentView: domain.ViewStateChat,
		inputText:   "test message",
	}

	chatActions := registry.GetActiveActions(chatContext)
	hasToggleAction := false
	for _, action := range chatActions {
		if action.ID == "toggle_tool_expansion" {
			hasToggleAction = true
			break
		}
	}
	if !hasToggleAction {
		t.Error("Expected toggle_tool_expansion to be available in chat view")
	}

	modelContext := &testKeyHandlerContext{
		currentView: domain.ViewStateModelSelection,
	}

	modelActions := registry.GetActiveActions(modelContext)
	hasToggleActionInModel := false
	for _, action := range modelActions {
		if action.ID == "toggle_tool_expansion" {
			hasToggleActionInModel = true
			break
		}
	}
	if hasToggleActionInModel {
		t.Error("Expected toggle_tool_expansion to NOT be available in model selection view")
	}
}

func TestActionHandlers(t *testing.T) {
	registry := NewRegistry()
	mockContext := &testKeyHandlerContext{
		currentView: domain.ViewStateChat,
	}

	action := registry.Resolve("ctrl+c", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+c to resolve to quit action")
	}

	cmd := action.Handler(mockContext, tea.KeyMsg{})
	if cmd == nil {
		t.Error("Expected quit handler to return a command")
	}

	action = registry.Resolve("ctrl+r", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+r to resolve to toggle action")
	}

	initialCallCount := mockContext.expandToggleCalls
	_ = action.Handler(mockContext, tea.KeyMsg{})

	if mockContext.expandToggleCalls != initialCallCount+1 {
		t.Error("Expected toggle handler to call ToggleToolResultExpansion()")
	}
}

func TestHelpShortcutGeneration(t *testing.T) {
	registry := NewRegistry()
	mockContext := &testKeyHandlerContext{
		currentView: domain.ViewStateChat,
		inputText:   "test message",
	}

	shortcuts := registry.GetHelpShortcuts(mockContext)
	if len(shortcuts) == 0 {
		t.Fatal("Expected help shortcuts to be generated")
	}

	foundQuit := false
	foundToggle := false
	for _, shortcut := range shortcuts {
		if shortcut.Key == "ctrl+c" && shortcut.Description == "exit application" {
			foundQuit = true
		}
		if shortcut.Key == "ctrl+r" && shortcut.Description == "expand/collapse tool results" {
			foundToggle = true
		}
	}

	if !foundQuit {
		t.Error("Expected quit shortcut in help")
	}
	if !foundToggle {
		t.Error("Expected toggle shortcut in help")
	}
}

func TestConditionalKeyBindings(t *testing.T) {
	registry := NewRegistry()

	emptyInputContext := &testKeyHandlerContext{
		currentView: domain.ViewStateChat,
		inputText:   "",
	}
	action := registry.Resolve("enter", emptyInputContext)
	if action != nil {
		t.Error("Expected enter key to not resolve to send_message when input is empty")
	}

	nonEmptyInputContext := &testKeyHandlerContext{
		currentView: domain.ViewStateChat,
		inputText:   "hello",
	}
	action = registry.Resolve("enter", nonEmptyInputContext)
	if action == nil {
		t.Fatal("Expected enter key to resolve to send_message when input has content")
	}
	if action.ID != "send_message" {
		t.Errorf("Expected enter to resolve to 'send_message', got %s", action.ID)
	}
}

func TestLayerPriority(t *testing.T) {
	registry := NewRegistry()

	layers := registry.GetLayers()
	if len(layers) == 0 {
		t.Fatal("Expected layers to be initialized")
	}

	for i := 1; i < len(layers); i++ {
		if layers[i-1].Priority < layers[i].Priority {
			t.Errorf("Layers not sorted by priority: layer %d has priority %d, layer %d has priority %d",
				i-1, layers[i-1].Priority, i, layers[i].Priority)
		}
	}
}

func TestActionRegistration(t *testing.T) {
	registry := NewRegistry()

	customAction := &KeyAction{
		ID:          "test_action",
		Keys:        []string{"ctrl+t"},
		Description: "test action",
		Category:    "test",
		Handler: func(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
			return nil
		},
		Priority: 100,
		Enabled:  true,
		Context: KeyContext{
			Views: []domain.ViewState{domain.ViewStateChat},
		},
	}

	err := registry.Register(customAction)
	if err != nil {
		t.Fatalf("Expected custom action to register successfully, got error: %v", err)
	}

	retrievedAction := registry.GetAction("test_action")
	if retrievedAction == nil {
		t.Fatal("Expected to retrieve registered action")
	}
	if retrievedAction.ID != "test_action" {
		t.Errorf("Expected retrieved action ID to be 'test_action', got %s", retrievedAction.ID)
	}

	mockContext := &testKeyHandlerContext{
		currentView: domain.ViewStateChat,
	}
	resolvedAction := registry.Resolve("ctrl+t", mockContext)
	if resolvedAction == nil {
		t.Fatal("Expected custom action to be resolved")
	}
	if resolvedAction.ID != "test_action" {
		t.Errorf("Expected resolved action to be 'test_action', got %s", resolvedAction.ID)
	}
}

func TestActionConflictDetection(t *testing.T) {
	registry := NewRegistry()

	conflictingAction := &KeyAction{
		ID:          "conflicting_action",
		Keys:        []string{"ctrl+c"},
		Description: "conflicting action",
		Category:    "test",
		Handler: func(app KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
			return nil
		},
		Priority: 100,
		Enabled:  true,
	}

	err := registry.Register(conflictingAction)
	if err == nil {
		t.Error("Expected registration to fail due to key conflict")
	}
}
