package keybinding_test

import (
	"reflect"
	"testing"

	config "github.com/inference-gateway/cli/config"
	keybinding "github.com/inference-gateway/cli/internal/ui/keybinding"
)

// TestConfigOverrides tests that keybinding configuration overrides are applied correctly
func TestConfigOverrides(t *testing.T) {
	cfg := config.DefaultConfig()

	cfg.Chat.Keybindings.Enabled = true
	enabled := true
	cfg.Chat.Keybindings.Bindings = map[string]config.KeyBindingEntry{
		"mode_cycle_agent_mode": {
			Keys:    []string{"ctrl+m"},
			Enabled: &enabled,
		},
	}

	registry := keybinding.NewRegistry(cfg)

	action := registry.GetAction("mode_cycle_agent_mode")
	if action == nil {
		t.Fatal("Expected mode_cycle_agent_mode action to exist")
	}

	if len(action.Binding.Keys()) != 1 || action.Binding.Keys()[0] != "ctrl+m" {
		t.Errorf("Expected keys to be [ctrl+m], got %v", action.Binding.Keys())
	}

	if !action.Binding.Enabled() {
		t.Error("Expected action to be enabled")
	}
}

// TestConfigDisableAction tests that actions can be disabled via config
func TestConfigDisableAction(t *testing.T) {
	cfg := config.DefaultConfig()

	disabled := false
	cfg.Chat.Keybindings.Enabled = true
	cfg.Chat.Keybindings.Bindings = map[string]config.KeyBindingEntry{
		"tools_toggle_tool_expansion": {
			Keys:    []string{"ctrl+o"},
			Enabled: &disabled,
		},
	}

	registry := keybinding.NewRegistry(cfg)

	action := registry.GetAction("tools_toggle_tool_expansion")
	if action == nil {
		t.Fatal("Expected tools_toggle_tool_expansion action to exist")
	}

	if action.Binding.Enabled() {
		t.Error("Expected action to be disabled")
	}
}

// TestConfigMultipleKeys tests that multiple keys can be assigned to one action
func TestConfigMultipleKeys(t *testing.T) {
	cfg := config.DefaultConfig()

	enabled := true
	cfg.Chat.Keybindings.Enabled = true
	cfg.Chat.Keybindings.Bindings = map[string]config.KeyBindingEntry{
		"chat_enter_key_handler": {
			Keys:    []string{"ctrl+enter", "enter"},
			Enabled: &enabled,
		},
	}

	registry := keybinding.NewRegistry(cfg)

	action := registry.GetAction("chat_enter_key_handler")
	if action == nil {
		t.Fatal("Expected chat_enter_key_handler action to exist")
	}

	if len(action.Binding.Keys()) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(action.Binding.Keys()))
	}

	expectedKeys := map[string]bool{"ctrl+enter": true, "enter": true}
	for _, key := range action.Binding.Keys() {
		if !expectedKeys[key] {
			t.Errorf("Unexpected key: %s", key)
		}
	}
}

// TestConfigWithoutOverrides tests that registry works without custom bindings
func TestConfigWithoutOverrides(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Chat.Keybindings.Enabled = false

	registry := keybinding.NewRegistry(cfg)

	action := registry.GetAction("global_quit")
	if action == nil {
		t.Fatal("Expected global_quit action to exist even without custom bindings")
	}

	if len(action.Binding.Keys()) == 0 {
		t.Error("Expected default keys to be present")
	}
}

// TestConfigUnknownActionIgnored tests that unknown action IDs are ignored
func TestConfigUnknownActionIgnored(t *testing.T) {
	cfg := config.DefaultConfig()

	enabled := true
	cfg.Chat.Keybindings.Enabled = true
	cfg.Chat.Keybindings.Bindings = map[string]config.KeyBindingEntry{
		"nonexistent_action": {
			Keys:    []string{"ctrl+z"},
			Enabled: &enabled,
		},
	}

	registry := keybinding.NewRegistry(cfg)

	action := registry.GetAction("nonexistent_action")
	if action != nil {
		t.Error("Expected unknown action to be ignored, not registered")
	}
}

// TestConfigNilSafe tests that nil config is handled safely
func TestConfigNilSafe(t *testing.T) {
	registry := keybinding.NewRegistry(nil)

	action := registry.GetAction("global_quit")
	if action == nil {
		t.Fatal("Expected default bindings to work with nil config")
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

// TestConfigNoRuntimeValidation tests that conflicts are allowed at runtime (last wins)
func TestConfigNoRuntimeValidation(t *testing.T) {
	cfg := config.DefaultConfig()

	enabled := true
	cfg.Chat.Keybindings.Enabled = true
	cfg.Chat.Keybindings.Bindings = map[string]config.KeyBindingEntry{
		"mode_cycle_agent_mode": {
			Keys:    []string{"ctrl+c"},
			Enabled: &enabled,
		},
	}

	registry := keybinding.NewRegistry(cfg)

	action := registry.GetAction("mode_cycle_agent_mode")
	if action == nil {
		t.Fatal("Expected mode_cycle_agent_mode action to exist")
	}

	hasConfigKey := false
	for _, key := range action.Binding.Keys() {
		if key == "ctrl+c" {
			hasConfigKey = true
			break
		}
	}

	if !hasConfigKey {
		t.Error("Expected config key to be applied without runtime validation")
	}
}
