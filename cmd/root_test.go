package cmd

import (
	"os"
	"testing"

	assert "github.com/stretchr/testify/assert"
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
