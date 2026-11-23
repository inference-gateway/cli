package runtime

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

func writeTestHomeFile(t *testing.T, homeDir string, name string, content string) string {
	t.Helper()
	path := filepath.Join(homeDir, config.ConfigDirName, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestComposerSidecarLayers(t *testing.T) {
	homeDir, projectDir := splitHomeProjectEnv(t)

	writeTestHomeFile(t, homeDir, config.ComposerFileName, "triggers:\n  chat:\n    \"@\":\n      - agents\nstatus_bar:\n  enabled: false\n  indicators:\n    cost: false\n")

	cfg := loadTestConfig(t)
	require.Equal(t, []string{config.ComposerSourceAgents}, cfg.Composer.TriggerSources(config.ComposerContextChat, "@"),
		"the composer.yaml home layer sets the chat @ sources")
	require.False(t, cfg.Composer.StatusBar.Enabled, "the composer.yaml home layer sets the status bar")
	require.False(t, cfg.Composer.StatusBar.Indicators.Cost, "a partial status_bar block sets the indicator it names")
	require.True(t, cfg.Composer.StatusBar.Indicators.Model, "a partial status_bar block keeps the indicator defaults it does not set")
	require.Equal(t, []string{config.ComposerSourceAgents, config.ComposerSourceFiles}, cfg.Composer.TriggerSources(config.ComposerContextRoom, "@"),
		"a context the composer.yaml does not set keeps the default table")

	projComposer := filepath.Join(projectDir, config.ConfigDirName, config.ComposerFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(projComposer), 0o755))
	require.NoError(t, os.WriteFile(projComposer, []byte("triggers:\n  chat:\n    \"@\":\n      - files\n      - agents\n"), 0o644))

	cfg = loadTestConfig(t)
	require.Equal(t, []string{config.ComposerSourceFiles, config.ComposerSourceAgents}, cfg.Composer.TriggerSources(config.ComposerContextChat, "@"),
		"the project composer.yaml layer overrides the home layer's trigger sources")
	require.False(t, cfg.Composer.StatusBar.Enabled,
		"a project layer without status_bar leaves the home layer's status bar in place")
}

func TestComposerContextReplacesItsDefaultTable(t *testing.T) {
	homeDir, _ := splitHomeProjectEnv(t)

	writeTestHomeFile(t, homeDir, config.ComposerFileName, "triggers:\n  chat:\n    \"^\":\n      - issues\n    \"@\":\n      - files\n      - agents\n")

	cfg := loadTestConfig(t)
	require.Equal(t, []string{config.ComposerSourceIssues}, cfg.Composer.TriggerSources(config.ComposerContextChat, "^"),
		"a special character the file maps opens its sources")
	require.Nil(t, cfg.Composer.TriggerSources(config.ComposerContextChat, "#"),
		"a default trigger the file's chat table leaves out opens nothing in chat")
	require.Equal(t, []string{config.ComposerSourceIssues}, cfg.Composer.TriggerSources(config.ComposerContextRoom, "#"),
		"the room context the file does not set keeps its default #")
}

func TestComposerReservedAndUnknownKeysFailTheLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"reserved slash trigger", "triggers:\n  chat:\n    \"/\":\n      - files\n", `triggers.chat."/"`},
		{"reserved single bang trigger", "triggers:\n  chat:\n    \"!\":\n      - files\n", `triggers.chat."!"`},
		{"reserved double bang trigger", "triggers:\n  chat:\n    \"!!\":\n      - files\n", `triggers.chat."!!"`},
		{"letter trigger", "triggers:\n  chat:\n    \"a\":\n      - files\n", `triggers.chat."a"`},
		{"unknown context", "triggers:\n  meeting:\n    \"@\":\n      - files\n", "triggers.meeting"},
		{"unknown source", "triggers:\n  chat:\n    \"@\":\n      - repos\n", `triggers.chat."@"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir, _ := splitHomeProjectEnv(t)
			composer := writeTestHomeFile(t, homeDir, config.ComposerFileName, tt.yaml)

			_, _, err := loadConfig(rootCmd)
			require.Error(t, err, "an invalid composer.yaml must fail the load")
			require.Contains(t, err.Error(), tt.wantErr, "the error must name the composer key")
			require.Contains(t, err.Error(), composer, "the error must name the composer.yaml file")
		})
	}
}
