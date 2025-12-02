package keybinding_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	keybinding "github.com/inference-gateway/cli/internal/ui/keybinding"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	keybindingmocks "github.com/inference-gateway/cli/tests/mocks/keybinding"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"
)

// newTestContext creates a configured FakeKeyHandlerContext for testing
func newTestContext(currentView domain.ViewState, inputText string) *keybindingmocks.FakeKeyHandlerContext {
	fake := &keybindingmocks.FakeKeyHandlerContext{}

	stateManager := services.NewStateManager(false)
	_ = stateManager.TransitionToView(currentView)
	fake.GetStateManagerReturns(stateManager)

	fakeInput := &uimocks.FakeInputComponent{}
	fakeInput.GetInputReturns(inputText)
	fakeInput.GetCursorReturns(0)
	fake.GetInputViewReturns(fakeInput)

	fakeConversation := &uimocks.FakeConversationRenderer{}
	fake.GetConversationViewReturns(fakeConversation)

	fakeStatus := &uimocks.FakeStatusComponent{}
	fake.GetStatusViewReturns(fakeStatus)

	fake.GetPageSizeReturns(20)
	fake.GetMouseEnabledReturns(false)
	fake.GetConfigReturns(nil)
	fake.GetConversationRepositoryReturns(nil)
	fake.GetAgentServiceReturns(nil)

	fakeImageService := &domainmocks.FakeImageService{}
	fake.GetImageServiceReturns(fakeImageService)

	return fake
}

func TestRegistryCreation(t *testing.T) {
	registry := keybinding.NewRegistry()
	if registry == nil {
		t.Fatal("Expected registry to be created")
	}

	mockContext := newTestContext(domain.ViewStateChat, "test message")

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
	registry := keybinding.NewRegistry()
	mockContext := newTestContext(domain.ViewStateChat, "test message")

	action := registry.Resolve("ctrl+c", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+c to resolve to an action")
	} else if action.ID != "quit" {
		t.Errorf("Expected ctrl+c to resolve to 'quit', got %s", action.ID)
	}

	action = registry.Resolve("ctrl+o", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+o to resolve to an action")
	} else if action.ID != "toggle_tool_expansion" {
		t.Errorf("Expected ctrl+o to resolve to 'toggle_tool_expansion', got %s", action.ID)
	}

	action = registry.Resolve("ctrl+r", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+r to resolve to an action")
	} else if action.ID != "toggle_raw_format" {
		t.Errorf("Expected ctrl+r to resolve to 'toggle_raw_format', got %s", action.ID)
	}

	action = registry.Resolve("ctrl+z", mockContext)
	if action != nil {
		t.Errorf("Expected ctrl+z to not resolve to any action, got %s", action.ID)
	}
}

func TestViewContextFiltering(t *testing.T) {
	registry := keybinding.NewRegistry()

	chatContext := newTestContext(domain.ViewStateChat, "test message")

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

	modelContext := newTestContext(domain.ViewStateModelSelection, "")

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
	registry := keybinding.NewRegistry()
	mockContext := newTestContext(domain.ViewStateChat, "")

	action := registry.Resolve("ctrl+c", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+c to resolve to quit action")
	} else {
		cmd := action.Handler(mockContext, tea.KeyMsg{})
		if cmd == nil {
			t.Error("Expected quit handler to return a command")
		}
	}

	action = registry.Resolve("ctrl+o", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+o to resolve to toggle action")
	} else {
		initialCallCount := mockContext.ToggleToolResultExpansionCallCount()
		_ = action.Handler(mockContext, tea.KeyMsg{})

		if mockContext.ToggleToolResultExpansionCallCount() != initialCallCount+1 {
			t.Error("Expected toggle handler to call ToggleToolResultExpansion()")
		}
	}
}

func TestHelpShortcutGeneration(t *testing.T) {
	registry := keybinding.NewRegistry()
	mockContext := newTestContext(domain.ViewStateChat, "test message")

	shortcuts := registry.GetHelpShortcuts(mockContext)
	if len(shortcuts) == 0 {
		t.Fatal("Expected help shortcuts to be generated")
	}

	foundQuit := false
	foundToggle := false
	foundRaw := false
	for _, shortcut := range shortcuts {
		if shortcut.Key == "ctrl+c" && shortcut.Description == "exit application" {
			foundQuit = true
		}
		if shortcut.Key == "ctrl+o" && shortcut.Description == "expand/collapse tool results" {
			foundToggle = true
		}
		if shortcut.Key == "ctrl+r" && shortcut.Description == "toggle raw/rendered markdown" {
			foundRaw = true
		}
	}

	if !foundQuit {
		t.Error("Expected quit shortcut in help")
	}
	if !foundToggle {
		t.Error("Expected toggle shortcut in help")
	}
	if !foundRaw {
		t.Error("Expected raw format shortcut in help (ctrl+r)")
	}
}

func TestConditionalKeyBindings(t *testing.T) {
	registry := keybinding.NewRegistry()

	emptyInputContext := newTestContext(domain.ViewStateChat, "")
	action := registry.Resolve("enter", emptyInputContext)
	if action == nil {
		t.Fatal("Expected enter key to resolve to enter_key_handler even when input is empty")
	} else if action.ID != "enter_key_handler" {
		t.Errorf("Expected enter to resolve to 'enter_key_handler', got %s", action.ID)
	}

	nonEmptyInputContext := newTestContext(domain.ViewStateChat, "hello")
	action = registry.Resolve("enter", nonEmptyInputContext)
	if action == nil {
		t.Fatal("Expected enter key to resolve to enter_key_handler when input has content")
	} else if action.ID != "enter_key_handler" {
		t.Errorf("Expected enter to resolve to 'enter_key_handler', got %s", action.ID)
	}
}

func TestLayerPriority(t *testing.T) {
	registry := keybinding.NewRegistry()

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
	registry := keybinding.NewRegistry()

	customAction := &keybinding.KeyAction{
		ID:          "test_action",
		Keys:        []string{"ctrl+shift+t"},
		Description: "test action",
		Category:    "test",
		Handler: func(app keybinding.KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
			return nil
		},
		Priority: 100,
		Enabled:  true,
		Context: keybinding.KeyContext{
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
	} else if retrievedAction.ID != "test_action" {
		t.Errorf("Expected retrieved action ID to be 'test_action', got %s", retrievedAction.ID)
	}

	mockContext := newTestContext(domain.ViewStateChat, "")
	resolvedAction := registry.Resolve("ctrl+shift+t", mockContext)
	if resolvedAction == nil {
		t.Fatal("Expected custom action to be resolved")
	} else if resolvedAction.ID != "test_action" {
		t.Errorf("Expected resolved action to be 'test_action', got %s", resolvedAction.ID)
	}
}

func TestActionConflictDetection(t *testing.T) {
	registry := keybinding.NewRegistry()

	conflictingAction := &keybinding.KeyAction{
		ID:          "conflicting_action",
		Keys:        []string{"ctrl+c"},
		Description: "conflicting action",
		Category:    "test",
		Handler: func(app keybinding.KeyHandlerContext, keyMsg tea.KeyMsg) tea.Cmd {
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
