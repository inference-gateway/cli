package runtime

import (
	"os"
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// setRemindersFileFlag sets the global --reminders-file persistent flag for the
// duration of a test. pflag.FlagSet.Set does not set Changed itself, so we set
// it explicitly so remindersFileOverride picks up the value.
func setRemindersFileFlag(t *testing.T, path string) {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("reminders-file")
	require.NotNil(t, f, "reminders-file flag is not registered")
	require.NoError(t, rootCmd.PersistentFlags().Set("reminders-file", path))
	f.Changed = true
	t.Cleanup(func() {
		_ = rootCmd.PersistentFlags().Set("reminders-file", "")
		f.Changed = false
	})
}

func writeRemindersFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "custom.yaml")
	require.NoError(t, os.WriteFile(path, []byte("enabled: true\nreminders:\n  - name: "+name+"\n    text: hi\n"), 0o644))
	return path
}

func reminderNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Reminders.Reminders))
	for _, r := range cfg.Reminders.Reminders {
		names = append(names, r.Name)
	}
	return names
}

func TestRemindersInlineEnv(t *testing.T) {
	withHermeticEnv(t)
	t.Setenv("INFER_REMINDERS_CONFIG", "enabled: true\nreminders:\n  - name: from-env\n    hook: post_tool\n    trigger: on_failure\n    text: boom\n")

	cfg := loadTestConfig(t)
	assert.True(t, cfg.Reminders.Enabled)
	assert.Equal(t, []string{"from-env"}, reminderNames(cfg))
	assert.Equal(t, config.ReminderTriggerOnFailure, cfg.Reminders.Reminders[0].Trigger)
}

func TestRemindersInlineEnvMalformedErrors(t *testing.T) {
	withHermeticEnv(t)
	t.Setenv("INFER_REMINDERS_CONFIG", "enabled: true\nreminders: [unterminated")

	_, _, err := loadConfig(rootCmd)
	require.ErrorContains(t, err, "INFER_REMINDERS_CONFIG")
}

func TestRemindersFileFlag(t *testing.T) {
	withHermeticEnv(t)
	setRemindersFileFlag(t, writeRemindersFile(t, "flagged"))

	cfg := loadTestConfig(t)
	assert.Equal(t, []string{"flagged"}, reminderNames(cfg))
}

// INFER_REMINDERS_CONFIG beats --reminders-file, one of the two documented
// env-over-flag exceptions.
func TestRemindersEnvBeatsFlag(t *testing.T) {
	withHermeticEnv(t)
	setRemindersFileFlag(t, writeRemindersFile(t, "flagged"))
	t.Setenv("INFER_REMINDERS_CONFIG", "enabled: true\nreminders:\n  - name: from-env\n    text: boom\n")

	cfg := loadTestConfig(t)
	assert.Equal(t, []string{"from-env"}, reminderNames(cfg))
}

// merge: true adds the supplied entries on top of the built-in defaults by
// name instead of replacing them.
func TestRemindersMergeTrue(t *testing.T) {
	withHermeticEnv(t)
	t.Setenv("INFER_MEMORY_ENABLED", "true")
	t.Setenv("INFER_REMINDERS_CONFIG", "enabled: true\nmerge: true\nreminders:\n  - name: my-custom\n    text: custom nudge\n    hook: pre_stream\n    trigger: interval\n    interval: 5\n")

	cfg := loadTestConfig(t)
	names := reminderNames(cfg)
	assert.Len(t, names, len(config.DefaultRemindersConfig().Reminders)+1)
	assert.Contains(t, names, "my-custom")
	assert.Contains(t, names, "todo-hygiene")
}
