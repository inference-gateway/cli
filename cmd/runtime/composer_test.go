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
		"triggers the composer.yaml does not set fall back to the default table")

	projComposer := filepath.Join(projectDir, config.ConfigDirName, config.ComposerFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(projComposer), 0o755))
	require.NoError(t, os.WriteFile(projComposer, []byte("triggers:\n  chat:\n    \"@\":\n      - files\n      - agents\n"), 0o644))

	cfg = loadTestConfig(t)
	require.Equal(t, []string{config.ComposerSourceFiles, config.ComposerSourceAgents}, cfg.Composer.TriggerSources(config.ComposerContextChat, "@"),
		"the project composer.yaml layer overrides the home layer's trigger sources")
	require.False(t, cfg.Composer.StatusBar.Enabled,
		"a project layer without status_bar leaves the home layer's status bar in place")
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
		{"unknown trigger", "triggers:\n  chat:\n    \"?\":\n      - files\n", `triggers.chat."?"`},
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

func TestLegacyStatusBarMigratesIntoComposer(t *testing.T) {
	homeDir, _ := splitHomeProjectEnv(t)

	writeTestHomeFile(t, homeDir, config.ConfigFileName, "chat:\n  status_bar:\n    enabled: true\n    indicators:\n      cost: false\n")

	cfg := loadTestConfig(t)
	require.True(t, cfg.Composer.StatusBar.Enabled, "the legacy chat.status_bar block keeps working")
	require.False(t, cfg.Composer.StatusBar.Indicators.Cost, "the legacy block's indicator overrides apply")
	require.Equal(t, config.GetDefaultStatusBarConfig().SubagentLingerSeconds, cfg.Composer.StatusBar.SubagentLingerSeconds,
		"the legacy block inherits the defaults for keys it does not set")
}

func TestComposerStatusBarBeatsLegacyBlock(t *testing.T) {
	homeDir, _ := splitHomeProjectEnv(t)

	writeTestHomeFile(t, homeDir, config.ConfigFileName, "chat:\n  status_bar:\n    enabled: false\n")
	writeTestHomeFile(t, homeDir, config.ComposerFileName, "status_bar:\n  enabled: true\n")

	cfg := loadTestConfig(t)
	require.True(t, cfg.Composer.StatusBar.Enabled, "composer.yaml status_bar wins over the deprecated chat.status_bar block")
}
