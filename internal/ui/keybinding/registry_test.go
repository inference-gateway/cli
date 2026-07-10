package keybinding_test

import (
	"testing"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	config "github.com/inference-gateway/cli/config"
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
	registry := keybinding.NewRegistry(nil)
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
		case "global_quit":
			foundQuit = true
		case "tools_toggle_tool_expansion":
			foundToggle = true
		}
	}

	if !foundQuit {
		t.Error("Expected 'global_quit' action to be registered")
	}
	if !foundToggle {
		t.Error("Expected 'tools_toggle_tool_expansion' action to be registered")
	}
}

func TestKeyResolution(t *testing.T) {
	registry := keybinding.NewRegistry(nil)
	mockContext := newTestContext(domain.ViewStateChat, "test message")

	action := registry.ResolveKey("ctrl+c", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+c to resolve to an action")
	} else if action.ID != "global_quit" {
		t.Errorf("Expected ctrl+c to resolve to 'global_quit', got %s", action.ID)
	}

	action = registry.ResolveKey("ctrl+o", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+o to resolve to an action")
	} else if action.ID != "tools_toggle_tool_expansion" {
		t.Errorf("Expected ctrl+o to resolve to 'tools_toggle_tool_expansion', got %s", action.ID)
	}

	action = registry.ResolveKey("ctrl+r", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+r to resolve to an action")
	} else if action.ID != "display_toggle_raw_format" {
		t.Errorf("Expected ctrl+r to resolve to 'display_toggle_raw_format', got %s", action.ID)
	}

	action = registry.ResolveKey("ctrl+z", mockContext)
	if action != nil {
		t.Errorf("Expected ctrl+z to not resolve to any action, got %s", action.ID)
	}
}

func TestViewContextFiltering(t *testing.T) {
	registry := keybinding.NewRegistry(nil)

	chatContext := newTestContext(domain.ViewStateChat, "test message")

	chatActions := registry.GetActiveActions(chatContext)
	hasToggleAction := false
	for _, action := range chatActions {
		if action.ID == "tools_toggle_tool_expansion" {
			hasToggleAction = true
			break
		}
	}
	if !hasToggleAction {
		t.Error("Expected tools_toggle_tool_expansion to be available in chat view")
	}

	modelContext := newTestContext(domain.ViewStateModelSelection, "")

	modelActions := registry.GetActiveActions(modelContext)
	hasToggleActionInModel := false
	for _, action := range modelActions {
		if action.ID == "tools_toggle_tool_expansion" {
			hasToggleActionInModel = true
			break
		}
	}
	if hasToggleActionInModel {
		t.Error("Expected tools_toggle_tool_expansion to NOT be available in model selection view")
	}
}

func TestActionHandlers(t *testing.T) {
	registry := keybinding.NewRegistry(nil)
	mockContext := newTestContext(domain.ViewStateChat, "")

	action := registry.ResolveKey("ctrl+c", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+c to resolve to quit action")
	} else {
		cmd := action.Handler(mockContext, tea.KeyPressMsg{})
		if cmd == nil {
			t.Error("Expected quit handler to return a command")
		}
	}

	action = registry.ResolveKey("ctrl+o", mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+o to resolve to toggle action")
	} else {
		initialCallCount := mockContext.ToggleToolResultExpansionCallCount()
		_ = action.Handler(mockContext, tea.KeyPressMsg{})

		if mockContext.ToggleToolResultExpansionCallCount() != initialCallCount+1 {
			t.Error("Expected toggle handler to call ToggleToolResultExpansion()")
		}
	}
}

func TestHelpShortcutGeneration(t *testing.T) {
	registry := keybinding.NewRegistry(nil)
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
	registry := keybinding.NewRegistry(nil)

	emptyInputContext := newTestContext(domain.ViewStateChat, "")
	action := registry.ResolveKey("enter", emptyInputContext)
	if action == nil {
		t.Fatal("Expected enter key to resolve to chat_enter_key_handler even when input is empty")
	} else if action.ID != "chat_enter_key_handler" {
		t.Errorf("Expected enter to resolve to 'chat_enter_key_handler', got %s", action.ID)
	}

	nonEmptyInputContext := newTestContext(domain.ViewStateChat, "hello")
	action = registry.ResolveKey("enter", nonEmptyInputContext)
	if action == nil {
		t.Fatal("Expected enter key to resolve to chat_enter_key_handler when input has content")
	} else if action.ID != "chat_enter_key_handler" {
		t.Errorf("Expected enter to resolve to 'chat_enter_key_handler', got %s", action.ID)
	}
}

// TestResolveKeyPressMsg exercises the key.Matches dispatch path with a real
// tea.KeyPressMsg, complementing the string-based ResolveKey tests.
func TestResolveKeyPressMsg(t *testing.T) {
	registry := keybinding.NewRegistry(nil)
	mockContext := newTestContext(domain.ViewStateChat, "test message")

	msg := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	if got := msg.String(); got != "ctrl+c" {
		t.Fatalf("test setup: msg.String() = %q, want ctrl+c", got)
	}

	action := registry.Resolve(msg, mockContext)
	if action == nil {
		t.Fatal("Expected ctrl+c key press to resolve to an action")
	}
	if action.ID != "global_quit" {
		t.Errorf("Expected ctrl+c to resolve to 'global_quit', got %s", action.ID)
	}
}

// TestDefaultsSingleSourceOfTruth is the drift guard: every runtime action must
// have its keys defined in config.GetDefaultKeybindings() (the single source of
// truth), and no two actions active in the same view may share a key.
func TestDefaultsSingleSourceOfTruth(t *testing.T) {
	registry := keybinding.NewRegistry(nil)
	defaults := config.GetDefaultKeybindings()

	type claim struct {
		actionID string
		global   bool
	}
	claimed := make(map[string][]claim)

	for _, action := range registry.ListAllActions() {
		def, ok := defaults[action.ID]
		if !ok {
			t.Errorf("action %s has no entry in config.GetDefaultKeybindings()", action.ID)
			continue
		}
		if len(def.Keys) == 0 {
			t.Errorf("action %s has an empty default key list", action.ID)
		}

		for _, k := range action.Binding.Keys() {
			for _, view := range action.Context.Views {
				claimed[string(rune(view))+"|"+k] = append(claimed[string(rune(view))+"|"+k], claim{action.ID, false})
			}
			if len(action.Context.Views) == 0 {
				claimed["global|"+k] = append(claimed["global|"+k], claim{action.ID, true})
			}
		}
	}

	for key, claims := range claimed {
		if len(claims) > 1 {
			t.Errorf("key %q is bound by multiple actions in the same view scope: %v", key, claims)
		}
	}
}

func TestActionRegistration(t *testing.T) {
	registry := keybinding.NewRegistry(nil)

	customAction := &keybinding.KeyAction{
		ID:       "test_action",
		Category: "test",
		Binding:  key.NewBinding(key.WithKeys("ctrl+shift+t"), key.WithHelp("ctrl+shift+t", "test action")),
		Handler: func(app keybinding.KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
			return nil
		},
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
	resolvedAction := registry.ResolveKey("ctrl+shift+t", mockContext)
	if resolvedAction == nil {
		t.Fatal("Expected custom action to be resolved")
	} else if resolvedAction.ID != "test_action" {
		t.Errorf("Expected resolved action to be 'test_action', got %s", resolvedAction.ID)
	}
}

func TestActionConflictDetection(t *testing.T) {
	registry := keybinding.NewRegistry(nil)

	conflictingAction := &keybinding.KeyAction{
		ID:       "test_conflicting_action",
		Category: "test",
		Binding:  key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "conflicting action")),
		Handler: func(app keybinding.KeyHandlerContext, keyMsg tea.KeyPressMsg) tea.Cmd {
			return nil
		},
		Context: keybinding.KeyContext{
			Views: []domain.ViewState{domain.ViewStateChat},
		},
	}

	err := registry.Register(conflictingAction)
	if err != nil {
		t.Errorf("Expected registration to succeed (key sharing is allowed), got error: %v", err)
	}
}

func TestDeleteWordBackwardBindings(t *testing.T) {
	wantID := config.ActionID(config.NamespaceTextEditing, "delete_word_backward")
	wordDeleteKeys := []string{"ctrl+w", "alt+backspace", "ctrl+backspace"}

	assertResolves := func(t *testing.T, registry *keybinding.Registry) {
		t.Helper()
		ctx := newTestContext(domain.ViewStateChat, "hello world")
		for _, key := range wordDeleteKeys {
			action := registry.ResolveKey(key, ctx)
			if action == nil {
				t.Fatalf("expected %q to resolve to an action", key)
			}
			if action.ID != wantID {
				t.Errorf("expected %q to resolve to %q, got %q", key, wantID, action.ID)
			}
		}
	}

	t.Run("registry defaults", func(t *testing.T) {
		assertResolves(t, keybinding.NewRegistry(nil))
	})

	t.Run("default config overrides", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Chat.Keybindings = config.KeybindingsConfig{
			Enabled:  true,
			Bindings: config.GetDefaultKeybindings(),
		}
		assertResolves(t, keybinding.NewRegistry(cfg))
	})
}
