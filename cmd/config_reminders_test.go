package cmd

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// setRemindersFileFlag sets the global --reminders-file persistent flag for the
// duration of a test and resets it afterwards so tests stay isolated.
// pflag.FlagSet.Set does not set Changed itself, so we set it explicitly so
// remindersFileOverride (which checks Changed) picks up the value.
func setRemindersFileFlag(t *testing.T, path string) {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("reminders-file")
	if f == nil {
		t.Fatal("reminders-file flag is not registered")
	}
	if err := rootCmd.PersistentFlags().Set("reminders-file", path); err != nil {
		t.Fatalf("set reminders-file: %v", err)
	}
	f.Changed = true
	t.Cleanup(func() {
		_ = rootCmd.PersistentFlags().Set("reminders-file", "")
		f.Changed = false
	})
}

func TestResolveRemindersConfig_InlineEnv(t *testing.T) {
	t.Setenv("INFER_REMINDERS_CONFIG", `enabled: true
reminders:
  - name: from-env
    hook: post_tool
    trigger: on_failure
    text: boom
`)
	cfg, err := resolveRemindersConfig()
	if err != nil {
		t.Fatalf("resolveRemindersConfig: %v", err)
	}
	if !cfg.Enabled || len(cfg.Reminders) != 1 || cfg.Reminders[0].Name != "from-env" {
		t.Fatalf("inline INFER_REMINDERS_CONFIG not honored: %+v", cfg)
	}
}

func TestResolveRemindersConfig_EnvMalformedErrors(t *testing.T) {
	t.Setenv("INFER_REMINDERS_CONFIG", "enabled: true\nreminders: [unterminated")
	if _, err := resolveRemindersConfig(); err == nil {
		t.Error("malformed INFER_REMINDERS_CONFIG should surface an error")
	}
}

func TestResolveRemindersConfig_FileFlag(t *testing.T) {
	t.Setenv("INFER_REMINDERS_CONFIG", "")
	path := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(path, []byte("enabled: true\nreminders:\n  - name: flagged\n    text: hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setRemindersFileFlag(t, path)

	cfg, err := resolveRemindersConfig()
	if err != nil {
		t.Fatalf("resolveRemindersConfig: %v", err)
	}
	if len(cfg.Reminders) != 1 || cfg.Reminders[0].Name != "flagged" {
		t.Fatalf("--reminders-file not honored: %+v", cfg)
	}
}

// Env wins over the flag, matching the documented flags < env layering.
func TestResolveRemindersConfig_EnvBeatsFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(path, []byte("enabled: true\nreminders:\n  - name: flagged\n    text: hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setRemindersFileFlag(t, path)
	t.Setenv("INFER_REMINDERS_CONFIG", "enabled: true\nreminders:\n  - name: from-env\n    text: boom\n")

	cfg, err := resolveRemindersConfig()
	if err != nil {
		t.Fatalf("resolveRemindersConfig: %v", err)
	}
	if len(cfg.Reminders) != 1 || cfg.Reminders[0].Name != "from-env" {
		t.Fatalf("env should win over --reminders-file: %+v", cfg)
	}
}

func TestApplyRemindersEnvOverrides_Enabled(t *testing.T) {
	cfg := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	t.Setenv("INFER_REMINDERS_ENABLED", "false")
	applyRemindersEnvOverrides(cfg)
	if cfg.Reminders.Enabled {
		t.Error("INFER_REMINDERS_ENABLED=false should disable reminders")
	}
}

// Merge:true via INFER_REMINDERS_CONFIG should add custom entries on top of
// built-in defaults instead of replacing them. This guards the cfg.Merge
// branch at resolveRemindersConfig (cmd/config.go:344).
func TestResolveRemindersConfig_MergeTrue(t *testing.T) {
	t.Setenv("INFER_REMINDERS_CONFIG", `enabled: true
merge: true
reminders:
  - name: my-custom
    text: custom nudge
    hook: pre_stream
    trigger: interval
    interval: 5
`)
	cfg, err := resolveRemindersConfig()
	if err != nil {
		t.Fatalf("resolveRemindersConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Error("merged config should be enabled")
	}
	wantLen := len(config.DefaultRemindersConfig().Reminders) + 1
	if len(cfg.Reminders) != wantLen {
		t.Fatalf("expected %d reminders (defaults + 1 custom), got %d:\n%+v", wantLen, len(cfg.Reminders), cfg.Reminders)
	}
	found := false
	for _, r := range cfg.Reminders {
		if r.Name == "my-custom" {
			found = true
			if r.Interval != 5 {
				t.Errorf("custom reminder interval = %d, want 5", r.Interval)
			}
			break
		}
	}
	if !found {
		t.Error("custom reminder 'my-custom' not found in merged result")
	}
	hasTodo := false
	for _, r := range cfg.Reminders {
		if r.Name == "todo-hygiene" {
			hasTodo = true
			break
		}
	}
	if !hasTodo {
		t.Error("merged result should include built-in todo-hygiene reminder")
	}
}
