package cmd

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// setRemindersFileFlag sets the global --reminders-file persistent flag for the
// duration of a test and resets it afterwards so tests stay isolated.
func setRemindersFileFlag(t *testing.T, path string) {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("reminders-file")
	if f == nil {
		t.Fatal("reminders-file flag is not registered")
	}
	if err := rootCmd.PersistentFlags().Set("reminders-file", path); err != nil {
		t.Fatalf("set reminders-file: %v", err)
	}
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
