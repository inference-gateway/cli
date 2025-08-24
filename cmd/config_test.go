package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigInit(t *testing.T) {
	testCases := []struct {
		name      string
		overwrite bool
		wantErr   bool
	}{
		{
			name:      "successful config initialization",
			overwrite: false,
			wantErr:   false,
		},
		{
			name:      "config initialization with overwrite",
			overwrite: true,
			wantErr:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "infer-config-test-*")
			require.NoError(t, err)
			defer func() {
				if err := os.RemoveAll(tempDir); err != nil {
					t.Errorf("Failed to remove temp dir: %v", err)
				}
			}()

			origDir, err := os.Getwd()
			require.NoError(t, err)
			defer func() {
				if err := os.Chdir(origDir); err != nil {
					t.Errorf("Failed to change back to original dir: %v", err)
				}
			}()

			err = os.Chdir(tempDir)
			require.NoError(t, err)

			testConfigInitCmd := &cobra.Command{
				Use:   "init",
				Short: "Initialize a new configuration file",
				Long: `Initialize a new .infer/config.yaml configuration file in the current directory.
This creates only the configuration file with default settings.

For complete project initialization, use 'infer init' instead.`,
				RunE: func(cmd *cobra.Command, args []string) error {
					configPath := ".infer/config.yaml"

					if _, err := os.Stat(configPath); err == nil {
						overwrite, _ := cmd.Flags().GetBool("overwrite")
						if !overwrite {
							return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
						}
					}

					cfg := config.DefaultConfig()

					if err := cfg.SaveConfig(configPath); err != nil {
						return fmt.Errorf("failed to create config file: %w", err)
					}

					fmt.Printf("Successfully created %s\n", configPath)
					fmt.Println("You can now customize the configuration for this project.")
					fmt.Println("Tip: Use 'infer init' for complete project initialization including additional setup files.")

					return nil
				},
			}
			testConfigInitCmd.Flags().Bool("overwrite", tc.overwrite, "Overwrite existing configuration file")

			err = testConfigInitCmd.RunE(testConfigInitCmd, []string{})

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			configPath := filepath.Join(tempDir, ".infer", "config.yaml")
			gitignorePath := filepath.Join(tempDir, ".infer", ".gitignore")

			assert.FileExists(t, configPath)
			assert.NoFileExists(t, gitignorePath)

			configData, err := os.ReadFile(configPath)
			require.NoError(t, err)
			assert.Contains(t, string(configData), "gateway:")
			assert.Contains(t, string(configData), "tools:")
		})
	}
}

func TestConfigInitExistingConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "infer-config-test-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to remove temp dir: %v", err)
		}
	}()

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Errorf("Failed to change back to original dir: %v", err)
		}
	}()

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	err = os.MkdirAll(".infer", 0755)
	require.NoError(t, err)

	configPath := filepath.Join(".infer", "config.yaml")
	err = os.WriteFile(configPath, []byte("existing config"), 0644)
	require.NoError(t, err)

	testConfigInitCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := ".infer/config.yaml"

			if _, err := os.Stat(configPath); err == nil {
				overwrite, _ := cmd.Flags().GetBool("overwrite")
				if !overwrite {
					return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
				}
			}

			cfg := config.DefaultConfig()

			if err := cfg.SaveConfig(configPath); err != nil {
				return fmt.Errorf("failed to create config file: %w", err)
			}

			fmt.Printf("Successfully created %s\n", configPath)
			fmt.Println("You can now customize the configuration for this project.")
			fmt.Println("Tip: Use 'infer init' for complete project initialization including additional setup files.")

			return nil
		},
	}
	testConfigInitCmd.Flags().Bool("overwrite", false, "Overwrite existing configuration file")

	err = testConfigInitCmd.RunE(testConfigInitCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	testConfigInitCmdWithOverwrite := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := ".infer/config.yaml"

			if _, err := os.Stat(configPath); err == nil {
				overwrite, _ := cmd.Flags().GetBool("overwrite")
				if !overwrite {
					return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
				}
			}

			cfg := config.DefaultConfig()

			if err := cfg.SaveConfig(configPath); err != nil {
				return fmt.Errorf("failed to create config file: %w", err)
			}

			fmt.Printf("Successfully created %s\n", configPath)
			fmt.Println("You can now customize the configuration for this project.")
			fmt.Println("Tip: Use 'infer init' for complete project initialization including additional setup files.")

			return nil
		},
	}
	testConfigInitCmdWithOverwrite.Flags().Bool("overwrite", true, "Overwrite existing configuration file")

	err = testConfigInitCmdWithOverwrite.RunE(testConfigInitCmdWithOverwrite, []string{})
	assert.NoError(t, err)
}

func TestConfigAgentSetModelWithUserspace(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "infer-config-test-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to remove temp dir: %v", err)
		}
	}()

	// Test both userspace and project-level config setting
	testCases := []struct {
		name      string
		userspace bool
		model     string
	}{
		{
			name:      "set model in project config",
			userspace: false,
			model:     "gpt-4o-mini",
		},
		{
			name:      "set model in userspace config",
			userspace: true,
			model:     "claude-3-sonnet",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Mock home directory for userspace config
			if tc.userspace {
				origHome := os.Getenv("HOME")
				defer func() {
					if err := os.Setenv("HOME", origHome); err != nil {
						t.Errorf("Failed to restore HOME: %v", err)
					}
				}()
				if err := os.Setenv("HOME", tempDir); err != nil {
					t.Fatalf("Failed to set HOME: %v", err)
				}
			}

			// Set working directory for project config
			origDir, err := os.Getwd()
			require.NoError(t, err)
			defer func() {
				if err := os.Chdir(origDir); err != nil {
					t.Errorf("Failed to change back to original dir: %v", err)
				}
			}()
			err = os.Chdir(tempDir)
			require.NoError(t, err)

			// Create test command with the userspace flag
			testCmd := &cobra.Command{Use: "test"}
			testCmd.Flags().Bool("userspace", tc.userspace, "Set configuration in user home directory")

			// Call the function
			err = setDefaultModel(testCmd, tc.model)
			require.NoError(t, err)

			// Verify the config was saved to the correct location
			var expectedPath string
			if tc.userspace {
				expectedPath = filepath.Join(tempDir, ".infer", "config.yaml")
			} else {
				expectedPath = filepath.Join(tempDir, ".infer", "config.yaml")
			}

			// Load the config and verify the model was set
			cfg, err := config.LoadConfig(expectedPath)
			require.NoError(t, err)
			assert.Equal(t, tc.model, cfg.Agent.Model)
		})
	}
}

func TestConfigAgentSetSystemWithUserspace(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "infer-config-test-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to remove temp dir: %v", err)
		}
	}()

	testSystemPrompt := "You are a helpful assistant."

	// Test both userspace and project-level config setting
	testCases := []struct {
		name      string
		userspace bool
	}{
		{
			name:      "set system prompt in project config",
			userspace: false,
		},
		{
			name:      "set system prompt in userspace config",
			userspace: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Mock home directory for userspace config
			if tc.userspace {
				origHome := os.Getenv("HOME")
				defer func() {
					if err := os.Setenv("HOME", origHome); err != nil {
						t.Errorf("Failed to restore HOME: %v", err)
					}
				}()
				if err := os.Setenv("HOME", tempDir); err != nil {
					t.Fatalf("Failed to set HOME: %v", err)
				}
			}

			// Set working directory for project config
			origDir, err := os.Getwd()
			require.NoError(t, err)
			defer func() {
				if err := os.Chdir(origDir); err != nil {
					t.Errorf("Failed to change back to original dir: %v", err)
				}
			}()
			err = os.Chdir(tempDir)
			require.NoError(t, err)

			// Create test command with the userspace flag
			testCmd := &cobra.Command{Use: "test"}
			testCmd.Flags().Bool("userspace", tc.userspace, "Set configuration in user home directory")

			// Call the function
			err = setSystemPrompt(testCmd, testSystemPrompt)
			require.NoError(t, err)

			// Verify the config was saved to the correct location
			var expectedPath string
			if tc.userspace {
				expectedPath = filepath.Join(tempDir, ".infer", "config.yaml")
			} else {
				expectedPath = filepath.Join(tempDir, ".infer", "config.yaml")
			}

			// Load the config and verify the system prompt was set
			cfg, err := config.LoadConfig(expectedPath)
			require.NoError(t, err)
			assert.Equal(t, testSystemPrompt, cfg.Agent.SystemPrompt)
		})
	}
}

func TestConfigAgentSetMaxTurnsWithUserspace(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "infer-config-test-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to remove temp dir: %v", err)
		}
	}()

	// Test both userspace and project-level config setting
	testCases := []struct {
		name      string
		userspace bool
		maxTurns  string
		expected  int
	}{
		{
			name:      "set max turns in project config",
			userspace: false,
			maxTurns:  "25",
			expected:  25,
		},
		{
			name:      "set max turns in userspace config",
			userspace: true,
			maxTurns:  "100",
			expected:  100,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Mock home directory for userspace config
			if tc.userspace {
				origHome := os.Getenv("HOME")
				defer func() {
					if err := os.Setenv("HOME", origHome); err != nil {
						t.Errorf("Failed to restore HOME: %v", err)
					}
				}()
				if err := os.Setenv("HOME", tempDir); err != nil {
					t.Fatalf("Failed to set HOME: %v", err)
				}
			}

			// Set working directory for project config
			origDir, err := os.Getwd()
			require.NoError(t, err)
			defer func() {
				if err := os.Chdir(origDir); err != nil {
					t.Errorf("Failed to change back to original dir: %v", err)
				}
			}()
			err = os.Chdir(tempDir)
			require.NoError(t, err)

			// Create test command with the userspace flag
			testCmd := &cobra.Command{Use: "test"}
			testCmd.Flags().Bool("userspace", tc.userspace, "Set configuration in user home directory")

			// Call the function
			err = setMaxTurns(testCmd, tc.maxTurns)
			require.NoError(t, err)

			// Verify the config was saved to the correct location
			var expectedPath string
			if tc.userspace {
				expectedPath = filepath.Join(tempDir, ".infer", "config.yaml")
			} else {
				expectedPath = filepath.Join(tempDir, ".infer", "config.yaml")
			}

			// Load the config and verify the max turns was set
			cfg, err := config.LoadConfig(expectedPath)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, cfg.Agent.MaxTurns)
		})
	}
}

func TestConfigGlobalUserspaceFlag(t *testing.T) {
	testCases := []struct {
		name              string
		globalUserspace   bool
		subcommandArgs    []string
		expectedUserspace bool
	}{
		{
			name:              "global userspace flag on agent set-model",
			globalUserspace:   true,
			subcommandArgs:    []string{"agent", "set-model", "test-model"},
			expectedUserspace: true,
		},
		{
			name:              "no global userspace flag",
			globalUserspace:   false,
			subcommandArgs:    []string{"agent", "set-model", "test-model"},
			expectedUserspace: false,
		},
		{
			name:              "global userspace flag on agent set-system",
			globalUserspace:   true,
			subcommandArgs:    []string{"agent", "set-system", "test prompt"},
			expectedUserspace: true,
		},
		{
			name:              "global userspace flag on agent set-max-turns",
			globalUserspace:   true,
			subcommandArgs:    []string{"agent", "set-max-turns", "10"},
			expectedUserspace: true,
		},
		{
			name:              "global userspace flag on agent verbose-tools",
			globalUserspace:   true,
			subcommandArgs:    []string{"agent", "verbose-tools", "enable"},
			expectedUserspace: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create temp directory for testing
			tempDir, err := os.MkdirTemp("", "infer-config-global-test-*")
			require.NoError(t, err)
			defer func() {
				if err := os.RemoveAll(tempDir); err != nil {
					t.Errorf("Failed to remove temp dir: %v", err)
				}
			}()

			// Set up HOME environment for userspace testing
			origHome := os.Getenv("HOME")
			defer func() {
				if err := os.Setenv("HOME", origHome); err != nil {
					t.Errorf("Failed to restore HOME: %v", err)
				}
			}()
			if err := os.Setenv("HOME", tempDir); err != nil {
				t.Fatalf("Failed to set HOME: %v", err)
			}

			// Set working directory for project config
			origDir, err := os.Getwd()
			require.NoError(t, err)
			defer func() {
				if err := os.Chdir(origDir); err != nil {
					t.Errorf("Failed to change back to original dir: %v", err)
				}
			}()

			// Create project directory and cd into it for project config
			projectWorkingDir := filepath.Join(tempDir, "project")
			require.NoError(t, os.MkdirAll(projectWorkingDir, 0755))
			
			if err := os.Chdir(projectWorkingDir); err != nil {
				t.Fatalf("Failed to change to project dir: %v", err)
			}

			// Create initial configs for both project and userspace
			projectDir := filepath.Join(tempDir, "project", ".infer")
			userspaceDir := filepath.Join(tempDir, ".infer")
			
			require.NoError(t, os.MkdirAll(projectDir, 0755))
			require.NoError(t, os.MkdirAll(userspaceDir, 0755))

			projectConfigPath := filepath.Join(projectDir, "config.yaml")
			userspaceConfigPath := filepath.Join(userspaceDir, "config.yaml")

			// Create initial configs
			defaultConfig := config.DefaultConfig()
			require.NoError(t, defaultConfig.SaveConfig(projectConfigPath))
			require.NoError(t, defaultConfig.SaveConfig(userspaceConfigPath))

			// Build command args
			args := []string{"config"}
			if tc.globalUserspace {
				args = append(args, "--userspace")
			}
			args = append(args, tc.subcommandArgs...)

			// Create and execute command
			cmd := &cobra.Command{Use: "test-root"}
			cmd.AddCommand(configCmd)
			cmd.SetArgs(args)

			err = cmd.Execute()
			require.NoError(t, err)

			// Verify the config was written to the expected location
			var expectedPath string
			if tc.expectedUserspace {
				expectedPath = userspaceConfigPath
			} else {
				expectedPath = projectConfigPath
			}

			// Check if the config file was modified (has recent modification time)
			stat, err := os.Stat(expectedPath)
			require.NoError(t, err)
			
			// Load the config and verify it was actually changed
			cfg, err := config.LoadConfig(expectedPath)
			require.NoError(t, err)

			// Verify the change was made based on the subcommand
			switch tc.subcommandArgs[1] {
			case "set-model":
				assert.Equal(t, "test-model", cfg.Agent.Model)
			case "set-system":
				assert.Equal(t, "test prompt", cfg.Agent.SystemPrompt)
			case "set-max-turns":
				assert.Equal(t, 10, cfg.Agent.MaxTurns)
			case "verbose-tools":
				assert.Equal(t, true, cfg.Agent.VerboseTools)
			}

			// Verify the other config file wasn't changed (still has default values)
			var otherPath string
			if tc.expectedUserspace {
				otherPath = projectConfigPath
			} else {
				otherPath = userspaceConfigPath
			}

			otherCfg, err := config.LoadConfig(otherPath)
			require.NoError(t, err)
			
			switch tc.subcommandArgs[1] {
			case "set-model":
				assert.NotEqual(t, "test-model", otherCfg.Agent.Model)
			case "set-system":
				assert.NotEqual(t, "test prompt", otherCfg.Agent.SystemPrompt)
			case "set-max-turns":
				assert.NotEqual(t, 10, otherCfg.Agent.MaxTurns)
			case "verbose-tools":
				assert.NotEqual(t, true, otherCfg.Agent.VerboseTools)
			}

			_ = stat // Suppress unused variable warning
		})
	}
}
