package cmd

import (
	"os"
	"slices"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// TestMain redirects the logger to a throwaway directory for the whole package.
// Many tests here call initConfig(), which calls logger.Init; config.DefaultLogsPath
// is relative (".infer/logs"), so without an override the logger would create
// .infer/logs/ under the package working directory. No test asserts on logging.dir,
// and tests that clear INFER_* env vars (e.g. root_defaults_test) chdir into their
// own temp dir, so this override is safe and self-cleaning.
func TestMain(m *testing.M) {
	logDir, err := os.MkdirTemp("", "infer-cmd-test-logs")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("INFER_LOGGING_DIR", logDir); err != nil {
		panic(err)
	}

	code := m.Run()

	_ = os.RemoveAll(logDir)
	_ = os.Unsetenv("INFER_LOGGING_DIR")
	os.Exit(code)
}

func TestA2AAgentsEnvironmentVariable(t *testing.T) {
	tests := []struct {
		name           string
		agentsEnv      string
		expectedAgents []string
	}{
		{
			name:           "Parse comma-separated agents",
			agentsEnv:      "agent1,agent2,agent3",
			expectedAgents: []string{"agent1", "agent2", "agent3"},
		},
		{
			name:           "Handle whitespace",
			agentsEnv:      "agent1, agent2 , agent3",
			expectedAgents: []string{"agent1", "agent2", "agent3"},
		},
		{
			name:           "Handle newline separators",
			agentsEnv:      "agent1\nagent2\nagent3",
			expectedAgents: []string{"agent1", "agent2", "agent3"},
		},
		{
			name:           "Handle extra commas",
			agentsEnv:      "agent1,,agent2,",
			expectedAgents: []string{"agent1", "agent2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, os.Unsetenv("INFER_A2A_AGENTS"))

			if tt.agentsEnv != "" {
				assert.NoError(t, os.Setenv("INFER_A2A_AGENTS", tt.agentsEnv))
			}

			initConfig()

			if tt.expectedAgents != nil {
				agents := V.GetStringSlice("a2a.agents")
				assert.Equal(t, tt.expectedAgents, agents, "Agents should match expected")
			}

			assert.NoError(t, os.Unsetenv("INFER_A2A_AGENTS"))
		})
	}
}

// bashAllowAppendEnv / bashAllowAppendFlag are the override knobs reintroduced so
// CI (and infer-action) can add a few commands to the allow-list baseline without
// rewriting tools.bash.mode.all.allow or shipping ".*".
const (
	bashAllowAppendEnv  = "INFER_TOOLS_BASH_MODE_ALL_ALLOW_APPEND"
	bashAllowAppendFlag = "tools-bash-mode-all-allow-append"
)

// setBashAllowAppendFlag sets the persistent append flag and resets it after the
// test so its value can't leak into other tests sharing the global rootCmd.
func setBashAllowAppendFlag(t *testing.T, value string) {
	t.Helper()
	require.NoError(t, rootCmd.PersistentFlags().Set(bashAllowAppendFlag, value))
	t.Cleanup(func() { _ = rootCmd.PersistentFlags().Set(bashAllowAppendFlag, "") })
}

func TestBashAllowAppendOverride(t *testing.T) {
	defaultAll := config.DefaultConfig().Tools.Bash.Mode.All.Allow

	tests := []struct {
		name         string
		appendEnv    string
		appendFlag   string
		wantAppended []string
	}{
		{
			name:         "env appends onto the mode.all baseline",
			appendEnv:    "docker ps,kubectl get pods",
			wantAppended: []string{"docker ps", "kubectl get pods"},
		},
		{
			name:         "env trims whitespace, newlines and empty entries",
			appendEnv:    "docker ps\n kubectl get pods ,,helm list",
			wantAppended: []string{"docker ps", "kubectl get pods", "helm list"},
		},
		{
			name:         "flag appends onto the mode.all baseline",
			appendFlag:   "docker ps",
			wantAppended: []string{"docker ps"},
		},
		{
			name:         "env takes precedence over the flag",
			appendEnv:    "docker ps",
			appendFlag:   "should-be-ignored",
			wantAppended: []string{"docker ps"},
		},
		{
			name:         "no override leaves the baseline untouched",
			wantAppended: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withHermeticEnv(t)
			if tt.appendEnv != "" {
				t.Setenv(bashAllowAppendEnv, tt.appendEnv)
			}
			if tt.appendFlag != "" {
				setBashAllowAppendFlag(t, tt.appendFlag)
			}

			initConfig()

			want := append(slices.Clone(defaultAll), tt.wantAppended...)
			assert.Equal(t, want, Cfg.Tools.Bash.Mode.All.Allow)
		})
	}
}

// TestBashAllowAppendReachesMatcher proves the appended command is honored by the
// single matcher (IsBashCommandAllowed), while an off-list command stays denied.
// Only standard and plan exercise the append: mode.auto defaults to ".*" (allow-all),
// so it would pass any command regardless of the baseline.
func TestBashAllowAppendReachesMatcher(t *testing.T) {
	withHermeticEnv(t)
	t.Setenv(bashAllowAppendEnv, "docker ps")

	initConfig()

	for _, mode := range []string{"standard", "plan"} {
		assert.True(t, Cfg.IsBashCommandAllowed("docker ps", mode),
			"appended command should be allowed in %s mode via the mode.all baseline", mode)
	}
	assert.False(t, Cfg.IsBashCommandAllowed("docker rm -f box", "standard"),
		"an off-list command must stay denied")
}
