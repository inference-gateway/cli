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

func TestActiveActionMembership(t *testing.T) {
	tests := []struct {
		name        string
		view        domain.ViewState
		inputText   string
		actionID    string
		wantPresent bool
	}{
		{
			name:        "global quit registered in chat view",
			view:        domain.ViewStateChat,
			inputText:   "test message",
			actionID:    "global_quit",
			wantPresent: true,
		},
		{
			name:        "tool expansion toggle registered in chat view",
			view:        domain.ViewStateChat,
			inputText:   "test message",
			actionID:    "tools_toggle_tool_expansion",
			wantPresent: true,
		},
		{
			name:        "tool expansion toggle filtered out in model selection view",
			view:        domain.ViewStateModelSelection,
			inputText:   "",
			actionID:    "tools_toggle_tool_expansion",
			wantPresent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := keybinding.NewRegistry(nil)
			if registry == nil {
				t.Fatal("Expected registry to be created")
			}

			mockContext := newTestContext(tt.view, tt.inputText)
			actions := registry.GetActiveActions(mockContext)
			if tt.wantPresent && len(actions) == 0 {
				t.Fatal("Expected default actions to be registered")
			}

			present := false
			for _, action := range actions {
				if action.ID == tt.actionID {
					present = true
					break
				}
			}
			if present != tt.wantPresent {
				t.Errorf("Expected %s present=%v in view %v, got %v", tt.actionID, tt.wantPresent, tt.view, present)
			}
		})
	}
}

func TestKeyResolution(t *testing.T) {
	tests := []struct {
		name      string
		inputText string
		key       string
		wantID    string
	}{
		{
			name:      "ctrl+c resolves to global quit",
			inputText: "test message",
			key:       "ctrl+c",
			wantID:    "global_quit",
		},
		{
			name:      "ctrl+o resolves to tool expansion toggle",
			inputText: "test message",
			key:       "ctrl+o",
			wantID:    "tools_toggle_tool_expansion",
		},
		{
			name:      "ctrl+r resolves to raw format toggle",
			inputText: "test message",
			key:       "ctrl+r",
			wantID:    "display_toggle_raw_format",
		},
		{
			name:      "ctrl+z resolves to no action",
			inputText: "test message",
			key:       "ctrl+z",
			wantID:    "",
		},
		{
			name:      "enter resolves to enter handler when input is empty",
			inputText: "",
			key:       "enter",
			wantID:    "chat_enter_key_handler",
		},
		{
			name:      "enter resolves to enter handler when input has content",
			inputText: "hello",
			key:       "enter",
			wantID:    "chat_enter_key_handler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := keybinding.NewRegistry(nil)
			mockContext := newTestContext(domain.ViewStateChat, tt.inputText)

			action := registry.ResolveKey(tt.key, mockContext)
			if tt.wantID == "" {
				if action != nil {
					t.Errorf("Expected %s to not resolve to any action, got %s", tt.key, action.ID)
				}
				return
			}
			if action == nil {
				t.Fatalf("Expected %s to resolve to an action", tt.key)
			}
			if action.ID != tt.wantID {
				t.Errorf("Expected %s to resolve to '%s', got %s", tt.key, tt.wantID, action.ID)
			}
		})
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

	expected := []struct {
		key         string
		description string
	}{
		{key: "ctrl+c", description: "exit application"},
		{key: "ctrl+o", description: "expand/collapse tool results"},
		{key: "ctrl+r", description: "toggle raw/rendered markdown"},
	}

	for _, want := range expected {
		t.Run(want.key, func(t *testing.T) {
			for _, shortcut := range shortcuts {
				if shortcut.Key == want.key && shortcut.Description == want.description {
					return
				}
			}
			t.Errorf("Expected shortcut %q (%s) in help", want.key, want.description)
		})
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
