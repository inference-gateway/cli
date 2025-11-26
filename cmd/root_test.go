package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBashWhitelistEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name             string
		commandsEnv      string
		patternsEnv      string
		expectedCommands []string
		expectedPatterns []string
	}{
		{
			name:             "Parse comma-separated commands",
			commandsEnv:      "gh,git,npm",
			patternsEnv:      "",
			expectedCommands: []string{"gh", "git", "npm"},
			expectedPatterns: nil,
		},
		{
			name:             "Parse comma-separated patterns",
			commandsEnv:      "",
			patternsEnv:      "^gh .*,^git .*,^npm .*",
			expectedCommands: nil,
			expectedPatterns: []string{"^gh .*", "^git .*", "^npm .*"},
		},
		{
			name:             "Parse both commands and patterns",
			commandsEnv:      "gh,git",
			patternsEnv:      "^gh .*,^git .*",
			expectedCommands: []string{"gh", "git"},
			expectedPatterns: []string{"^gh .*", "^git .*"},
		},
		{
			name:             "Handle whitespace in values",
			commandsEnv:      "gh, git , npm",
			patternsEnv:      "^gh .* , ^git .* ",
			expectedCommands: []string{"gh", "git", "npm"},
			expectedPatterns: []string{"^gh .*", "^git .*"},
		},
		{
			name:             "Handle newline separators",
			commandsEnv:      "gh\ngit\nnpm",
			patternsEnv:      "^gh .*\n^git .*",
			expectedCommands: []string{"gh", "git", "npm"},
			expectedPatterns: []string{"^gh .*", "^git .*"},
		},
		{
			name:             "Handle empty values",
			commandsEnv:      "",
			patternsEnv:      "",
			expectedCommands: nil,
			expectedPatterns: nil,
		},
		{
			name:             "Handle values with extra commas",
			commandsEnv:      "gh,,git,",
			patternsEnv:      ",^gh .*,,^git .*,",
			expectedCommands: []string{"gh", "git"},
			expectedPatterns: []string{"^gh .*", "^git .*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, os.Unsetenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS"))
			assert.NoError(t, os.Unsetenv("INFER_TOOLS_BASH_WHITELIST_PATTERNS"))

			if tt.commandsEnv != "" {
				assert.NoError(t, os.Setenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS", tt.commandsEnv))
			}
			if tt.patternsEnv != "" {
				assert.NoError(t, os.Setenv("INFER_TOOLS_BASH_WHITELIST_PATTERNS", tt.patternsEnv))
			}

			initConfig()

			if tt.expectedCommands != nil {
				commands := V.GetStringSlice("tools.bash.whitelist.commands")
				assert.Equal(t, tt.expectedCommands, commands, "Commands should match expected")
			} else {
				commands := V.GetStringSlice("tools.bash.whitelist.commands")
				assert.NotNil(t, commands, "Commands should not be nil")
			}

			if tt.expectedPatterns != nil {
				patterns := V.GetStringSlice("tools.bash.whitelist.patterns")
				assert.Equal(t, tt.expectedPatterns, patterns, "Patterns should match expected")
			} else {
				patterns := V.GetStringSlice("tools.bash.whitelist.patterns")
				assert.NotNil(t, patterns, "Patterns should not be nil")
			}

			assert.NoError(t, os.Unsetenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS"))
			assert.NoError(t, os.Unsetenv("INFER_TOOLS_BASH_WHITELIST_PATTERNS"))
		})
	}
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
