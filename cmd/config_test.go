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
			// Create temporary directory
			tempDir, err := os.MkdirTemp("", "infer-config-test-*")
			require.NoError(t, err)
			defer func() {
				if err := os.RemoveAll(tempDir); err != nil {
					t.Errorf("Failed to remove temp dir: %v", err)
				}
			}()

			// Change to temp directory
			origDir, err := os.Getwd()
			require.NoError(t, err)
			defer func() {
				if err := os.Chdir(origDir); err != nil {
					t.Errorf("Failed to change back to original dir: %v", err)
				}
			}()

			err = os.Chdir(tempDir)
			require.NoError(t, err)

			// Create a fresh command for this test
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

			// Run config initialization
			err = testConfigInitCmd.RunE(testConfigInitCmd, []string{})

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Check that only config file was created (not gitignore)
			configPath := filepath.Join(tempDir, ".infer", "config.yaml")
			gitignorePath := filepath.Join(tempDir, ".infer", ".gitignore")

			assert.FileExists(t, configPath)
			assert.NoFileExists(t, gitignorePath) // Should NOT exist for config init

			// Check config file content
			configData, err := os.ReadFile(configPath)
			require.NoError(t, err)
			assert.Contains(t, string(configData), "gateway:")
			assert.Contains(t, string(configData), "tools:")
		})
	}
}

func TestConfigInitExistingConfig(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "infer-config-test-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Errorf("Failed to remove temp dir: %v", err)
		}
	}()

	// Change to temp directory
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Errorf("Failed to change back to original dir: %v", err)
		}
	}()

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	// Create .infer directory and config file
	err = os.MkdirAll(".infer", 0755)
	require.NoError(t, err)

	configPath := filepath.Join(".infer", "config.yaml")
	err = os.WriteFile(configPath, []byte("existing config"), 0644)
	require.NoError(t, err)

	// Create a fresh command without overwrite flag
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

	// Try config init without overwrite - should fail
	err = testConfigInitCmd.RunE(testConfigInitCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// Create a new command with overwrite enabled
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

	// Try with overwrite - should succeed
	err = testConfigInitCmdWithOverwrite.RunE(testConfigInitCmdWithOverwrite, []string{})
	assert.NoError(t, err)
}
