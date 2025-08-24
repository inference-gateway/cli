package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	cobra "github.com/spf13/cobra"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
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
				Long: `Initialize a new {{config.DefaultConfigPath}} configuration file in the current directory.
This creates only the configuration file with default settings.

For complete project initialization, use 'infer init' instead.`,
				RunE: func(cmd *cobra.Command, args []string) error {
					configPath := config.DefaultConfigPath

					if _, err := os.Stat(configPath); err == nil {
						overwrite, _ := cmd.Flags().GetBool("overwrite")
						if !overwrite {
							return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
						}
					}

					cfg := config.DefaultConfig()
					cfg.Path = configPath

					if err := cfg.Save(); err != nil {
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
			configPath := config.DefaultConfigPath

			if _, err := os.Stat(configPath); err == nil {
				overwrite, _ := cmd.Flags().GetBool("overwrite")
				if !overwrite {
					return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
				}
			}

			cfg := config.DefaultConfig()
			cfg.Path = configPath

			if err := cfg.Save(); err != nil {
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
			configPath := config.DefaultConfigPath

			if _, err := os.Stat(configPath); err == nil {
				overwrite, _ := cmd.Flags().GetBool("overwrite")
				if !overwrite {
					return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
				}
			}

			cfg := config.DefaultConfig()
			cfg.Path = configPath

			if err := cfg.Save(); err != nil {
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
