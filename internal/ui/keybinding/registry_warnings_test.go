package keybinding_test

import (
	"testing"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
	observer "go.uber.org/zap/zaptest/observer"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
	keybinding "github.com/inference-gateway/cli/internal/ui/keybinding"
)

// TestApplyConfigOverrides_UnknownActionWarnings guards two regressions at once:
//
//   - Bug B: a stock `infer init` config must NOT warn about the namespace-path
//     actions (chat_focus_attachments, diff_viewer_*, explorer_*) that the registry
//     never registers but components consume via config.ResolveNamespaceBindings.
//   - Bug A: the warning must be a structured log carrying an "action" field, never
//     the printf misuse that made zap emit a second "Ignored key without a value."
//     error line.
//
// The subtests mutate the package-global sugared logger, so they must not run in
// parallel.
func TestApplyConfigOverrides_UnknownActionWarnings(t *testing.T) {
	enabled := true
	tests := []struct {
		name      string
		bindings  map[string]config.KeyBindingEntry
		wantWarns int
		wantField string
	}{
		{
			name:      "fresh default config is silent",
			bindings:  config.GetDefaultKeybindings(),
			wantWarns: 0,
		},
		{
			name:      "namespace-path diff_viewer action not flagged",
			bindings:  map[string]config.KeyBindingEntry{"diff_viewer_nav_up": {Keys: []string{"up"}, Enabled: &enabled}},
			wantWarns: 0,
		},
		{
			name:      "chat focus-attachments not flagged",
			bindings:  map[string]config.KeyBindingEntry{"chat_focus_attachments": {Keys: []string{"ctrl+g"}, Enabled: &enabled}},
			wantWarns: 0,
		},
		{
			name:      "genuine typo warns once",
			bindings:  map[string]config.KeyBindingEntry{"totally_bogus_action": {Keys: []string{"ctrl+x"}, Enabled: &enabled}},
			wantWarns: 1,
			wantField: "totally_bogus_action",
		},
		{
			name:      "namespace typo still warns once",
			bindings:  map[string]config.KeyBindingEntry{"diff_viewer_bogus": {Keys: []string{"ctrl+x"}, Enabled: &enabled}},
			wantWarns: 1,
			wantField: "diff_viewer_bogus",
		},
		{
			name:      "stale renamed action warns once",
			bindings:  map[string]config.KeyBindingEntry{"plan_approval_plan_approval_accept_and_auto_approve": {Keys: []string{"a"}, Enabled: &enabled}},
			wantWarns: 1,
			wantField: "plan_approval_plan_approval_accept_and_auto_approve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.WarnLevel)
			prev := logger.GetGlobalLogger()
			logger.SetGlobalLogger(zap.New(core))
			defer logger.SetGlobalLogger(prev)

			cfg := &config.Config{}
			cfg.Chat.Keybindings = config.KeybindingsConfig{Enabled: true, Bindings: tt.bindings}

			_ = keybinding.NewRegistry(cfg)

			unknown := logs.FilterMessage("unknown keybinding action in config, ignoring").All()
			if len(unknown) != tt.wantWarns {
				t.Fatalf("unknown-keybinding warns = %d, want %d (all logs: %v)", len(unknown), tt.wantWarns, logs.All())
			}

			if n := logs.FilterMessageSnippet("Ignored key").Len(); n != 0 {
				t.Fatalf("got %d spurious 'Ignored key without a value.' lines (Bug A regression)", n)
			}

			if tt.wantField != "" {
				if got := unknown[0].ContextMap()["action"]; got != tt.wantField {
					t.Fatalf("warn action field = %v, want %q", got, tt.wantField)
				}
			}
		})
	}
}
