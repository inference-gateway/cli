package cmd

import (
	"os"
	"testing"

	assert "github.com/stretchr/testify/assert"

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

func TestBashWhitelistEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name             string
		commandsEnv      string
		expectedCommands []string
	}{
		{
			name:             "Parse comma-separated commands",
			commandsEnv:      "gh,git,npm",
			expectedCommands: []string{"gh", "git", "npm"},
		},
		{
			name:             "Handle whitespace in values",
			commandsEnv:      "gh, git , npm",
			expectedCommands: []string{"gh", "git", "npm"},
		},
		{
			name:             "Handle newline separators",
			commandsEnv:      "gh\ngit\nnpm",
			expectedCommands: []string{"gh", "git", "npm"},
		},
		{
			name:             "Handle empty values",
			commandsEnv:      "",
			expectedCommands: nil,
		},
		{
			name:             "Handle values with extra commas",
			commandsEnv:      "gh,,git,",
			expectedCommands: []string{"gh", "git"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, os.Unsetenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS"))

			if tt.commandsEnv != "" {
				assert.NoError(t, os.Setenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS", tt.commandsEnv))
			}

			initConfig()

			if tt.expectedCommands != nil {
				commands := V.GetStringSlice("tools.bash.whitelist.commands")
				assert.Equal(t, tt.expectedCommands, commands, "Commands should match expected")
			} else {
				commands := V.GetStringSlice("tools.bash.whitelist.commands")
				assert.NotNil(t, commands, "Commands should not be nil")
			}

			assert.NoError(t, os.Unsetenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS"))
		})
	}
}

func TestBashWhitelistAppendEnvironmentVariables(t *testing.T) {
	whitelistEnv := []string{
		"INFER_TOOLS_BASH_WHITELIST_COMMANDS",
		"INFER_TOOLS_BASH_WHITELIST_COMMANDS_APPEND",
	}
	clear := func() {
		for _, k := range whitelistEnv {
			assert.NoError(t, os.Unsetenv(k))
		}
	}
	// Resolve against a config-free HOME so the base is the built-in default rather
	// than a developer's ~/.infer/config.yaml.
	setup := func(t *testing.T) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		clear()
		t.Cleanup(clear)
	}

	defaults := config.DefaultConfig().Tools.Bash.Whitelist
	const defaultCommand = "^task( |$)" // present in the default command list (now a regex)

	t.Run("append merges onto the built-in default", func(t *testing.T) {
		setup(t)
		assert.NoError(t, os.Setenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS_APPEND", "helm,kubectl"))

		initConfig()

		commands := V.GetStringSlice("tools.bash.whitelist.commands")
		assert.Contains(t, commands, defaultCommand, "default commands must be preserved")
		assert.Subset(t, commands, []string{"helm", "kubectl"}, "appended commands must be present")
	})

	t.Run("append builds on a replace override, not the default", func(t *testing.T) {
		setup(t)
		assert.NoError(t, os.Setenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS", "onlythis"))
		assert.NoError(t, os.Setenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS_APPEND", "extra"))

		initConfig()

		commands := V.GetStringSlice("tools.bash.whitelist.commands")
		assert.Equal(t, []string{"onlythis", "extra"}, commands)
		assert.NotContains(t, commands, defaultCommand, "a replace override discards the default base")
	})

	t.Run("no append leaves the default untouched", func(t *testing.T) {
		setup(t)

		initConfig()

		assert.Equal(t, defaults.Commands, V.GetStringSlice("tools.bash.whitelist.commands"))
	})

	t.Run("whitespace, newline and empty entries are trimmed", func(t *testing.T) {
		setup(t)
		assert.NoError(t, os.Setenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS_APPEND", " helm , ,\nkubectl "))

		initConfig()

		commands := V.GetStringSlice("tools.bash.whitelist.commands")
		assert.Subset(t, commands, []string{"helm", "kubectl"})
		assert.NotContains(t, commands, "", "empty entries must be dropped")
	})
}

func TestBashWhitelistFlags(t *testing.T) {
	whitelistEnv := []string{
		"INFER_TOOLS_BASH_WHITELIST_COMMANDS",
		"INFER_TOOLS_BASH_WHITELIST_COMMANDS_APPEND",
	}
	whitelistFlags := []string{
		"tools-bash-whitelist-commands",
		"tools-bash-whitelist-commands-append",
	}
	reset := func() {
		for _, k := range whitelistEnv {
			assert.NoError(t, os.Unsetenv(k))
		}
		for _, f := range whitelistFlags {
			assert.NoError(t, rootCmd.PersistentFlags().Set(f, ""))
		}
	}
	// Resolve against a config-free HOME so the base is the built-in default, and
	// reset the package-global flags before and after each case so they do not bleed.
	setup := func(t *testing.T) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		reset()
		t.Cleanup(reset)
	}
	setFlag := func(t *testing.T, name, value string) {
		t.Helper()
		assert.NoError(t, rootCmd.PersistentFlags().Set(name, value))
	}

	defaults := config.DefaultConfig().Tools.Bash.Whitelist
	const defaultCommand = "^task( |$)" // present in the default command list (now a regex)

	t.Run("replace flag overrides the default", func(t *testing.T) {
		setup(t)
		setFlag(t, "tools-bash-whitelist-commands", "onlythis,andthis")

		initConfig()

		commands := V.GetStringSlice("tools.bash.whitelist.commands")
		assert.Equal(t, []string{"onlythis", "andthis"}, commands)
		assert.NotContains(t, commands, defaultCommand, "a replace flag discards the default base")
	})

	t.Run("append flag merges onto the default", func(t *testing.T) {
		setup(t)
		setFlag(t, "tools-bash-whitelist-commands-append", "helm,kubectl")

		initConfig()

		commands := V.GetStringSlice("tools.bash.whitelist.commands")
		assert.Contains(t, commands, defaultCommand, "default commands must be preserved")
		assert.Subset(t, commands, []string{"helm", "kubectl"}, "appended commands must be present")
	})

	t.Run("replace and append flags compose", func(t *testing.T) {
		setup(t)
		setFlag(t, "tools-bash-whitelist-commands", "base")
		setFlag(t, "tools-bash-whitelist-commands-append", "extra")

		initConfig()

		assert.Equal(t, []string{"base", "extra"}, V.GetStringSlice("tools.bash.whitelist.commands"))
	})

	t.Run("env var takes precedence over the matching flag", func(t *testing.T) {
		setup(t)
		setFlag(t, "tools-bash-whitelist-commands", "fromflag")
		assert.NoError(t, os.Setenv("INFER_TOOLS_BASH_WHITELIST_COMMANDS", "fromenv"))

		initConfig()

		assert.Equal(t, []string{"fromenv"}, V.GetStringSlice("tools.bash.whitelist.commands"))
	})

	t.Run("no flags leave the default untouched", func(t *testing.T) {
		setup(t)

		initConfig()

		assert.Equal(t, defaults.Commands, V.GetStringSlice("tools.bash.whitelist.commands"))
	})
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
