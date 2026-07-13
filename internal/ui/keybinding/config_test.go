package keybinding_test

import (
	"reflect"
	"slices"
	"testing"

	config "github.com/inference-gateway/cli/config"
	keybinding "github.com/inference-gateway/cli/internal/ui/keybinding"
)

type configOverrideCase struct {
	name             string
	nilConfig        bool
	enabled          bool
	bindings         map[string]config.KeyBindingEntry
	action           string
	wantNil          bool
	wantKeys         []string
	wantNonEmptyKeys bool
	wantEnabled      *bool
	wantKeyContains  string
}

func assertConfigOverride(t *testing.T, action *keybinding.KeyAction, tc configOverrideCase) {
	t.Helper()

	if tc.wantKeys != nil {
		keys := action.Binding.Keys()
		if len(keys) != len(tc.wantKeys) {
			t.Errorf("Expected %d keys, got %d", len(tc.wantKeys), len(keys))
		}
		expectedKeys := make(map[string]bool, len(tc.wantKeys))
		for _, key := range tc.wantKeys {
			expectedKeys[key] = true
		}
		for _, key := range keys {
			if !expectedKeys[key] {
				t.Errorf("Unexpected key: %s", key)
			}
		}
	}
	if tc.wantNonEmptyKeys && len(action.Binding.Keys()) == 0 {
		t.Error("Expected default keys to be present")
	}
	if tc.wantEnabled != nil && action.Binding.Enabled() != *tc.wantEnabled {
		t.Errorf("Expected enabled=%v, got %v", *tc.wantEnabled, action.Binding.Enabled())
	}
	if tc.wantKeyContains != "" && !slices.Contains(action.Binding.Keys(), tc.wantKeyContains) {
		t.Error("Expected config key to be applied without runtime validation")
	}
}

// TestConfigOverrides tests how keybinding configuration is applied to the registry
func TestConfigOverrides(t *testing.T) {
	on, off := true, false

	tests := []configOverrideCase{
		{
			name:    "override replaces keys and keeps action enabled",
			enabled: true,
			bindings: map[string]config.KeyBindingEntry{
				"mode_cycle_agent_mode": {Keys: []string{"ctrl+m"}, Enabled: &on},
			},
			action:      "mode_cycle_agent_mode",
			wantKeys:    []string{"ctrl+m"},
			wantEnabled: &on,
		},
		{
			name:    "action disabled via config",
			enabled: true,
			bindings: map[string]config.KeyBindingEntry{
				"tools_toggle_tool_expansion": {Keys: []string{"ctrl+o"}, Enabled: &off},
			},
			action:      "tools_toggle_tool_expansion",
			wantEnabled: &off,
		},
		{
			name:    "multiple keys assigned to one action",
			enabled: true,
			bindings: map[string]config.KeyBindingEntry{
				"chat_enter_key_handler": {Keys: []string{"ctrl+enter", "enter"}, Enabled: &on},
			},
			action:   "chat_enter_key_handler",
			wantKeys: []string{"ctrl+enter", "enter"},
		},
		{
			name:             "defaults kept when overrides disabled",
			enabled:          false,
			action:           "global_quit",
			wantNonEmptyKeys: true,
		},
		{
			name:    "unknown action id ignored",
			enabled: true,
			bindings: map[string]config.KeyBindingEntry{
				"nonexistent_action": {Keys: []string{"ctrl+z"}, Enabled: &on},
			},
			action:  "nonexistent_action",
			wantNil: true,
		},
		{
			name:      "nil config falls back to defaults",
			nilConfig: true,
			action:    "global_quit",
		},
		{
			name:    "conflicting key applied without runtime validation",
			enabled: true,
			bindings: map[string]config.KeyBindingEntry{
				"mode_cycle_agent_mode": {Keys: []string{"ctrl+c"}, Enabled: &on},
			},
			action:          "mode_cycle_agent_mode",
			wantKeyContains: "ctrl+c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *config.Config
			if !tt.nilConfig {
				cfg = config.DefaultConfig()
				cfg.Chat.Keybindings.Enabled = tt.enabled
				if tt.bindings != nil {
					cfg.Chat.Keybindings.Bindings = tt.bindings
				}
			}

			registry := keybinding.NewRegistry(cfg)
			action := registry.GetAction(tt.action)

			if tt.wantNil {
				if action != nil {
					t.Error("Expected unknown action to be ignored, not registered")
				}
				return
			}
			if action == nil {
				t.Fatalf("Expected %s action to exist", tt.action)
			}

			assertConfigOverride(t, action, tt)
		})
	}
}

// TestListAllActions tests that ListAllActions returns all registered actions
func TestListAllActions(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := keybinding.NewRegistry(cfg)

	actions := registry.ListAllActions()
	if len(actions) == 0 {
		t.Fatal("Expected actions to be listed")
	}

	for i := 1; i < len(actions); i++ {
		prev := actions[i-1]
		curr := actions[i]

		if prev.Category > curr.Category {
			t.Errorf("Actions not sorted by category: %s > %s", prev.Category, curr.Category)
		}

		if prev.Category == curr.Category && prev.ID > curr.ID {
			t.Errorf("Actions not sorted by ID within category: %s > %s", prev.ID, curr.ID)
		}
	}
}

// TestKeybindingsExcludedFromMainConfigYAML verifies that the Keybindings
// field is hidden from the main config.yaml via the yaml/mapstructure "-" tags.
// This guards the issue #435 invariant: keybindings live in their own file.
func TestKeybindingsExcludedFromMainConfigYAML(t *testing.T) {
	cfgType := reflect.TypeFor[config.ChatConfig]()
	field, ok := cfgType.FieldByName("Keybindings")
	if !ok {
		t.Fatal("ChatConfig.Keybindings field not found")
	}
	if got := field.Tag.Get("yaml"); got != "-" {
		t.Errorf("Expected yaml tag '-', got %q (Keybindings would leak into config.yaml)", got)
	}
	if got := field.Tag.Get("mapstructure"); got != "-" {
		t.Errorf("Expected mapstructure tag '-', got %q (viper would unmarshal into the field)", got)
	}
}
