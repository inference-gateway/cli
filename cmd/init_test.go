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

func testInitializeProject(cmd *cobra.Command) error {
	overwrite, _ := cmd.Flags().GetBool("overwrite")

	configPath := ".infer/config.yaml"
	gitignorePath := ".infer/.gitignore"

	if !overwrite {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
		}
		if _, err := os.Stat(gitignorePath); err == nil {
			return fmt.Errorf(".gitignore file %s already exists (use --overwrite to replace)", gitignorePath)
		}
	}

	cfg := config.DefaultConfig()

	if err := cfg.SaveConfig(configPath); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	gitignoreContent := `# Ignore log files and history files
*.log
history
chat_export_*
`

	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
		return fmt.Errorf("failed to create .gitignore file: %w", err)
	}

	fmt.Printf("✅ Successfully initialized Inference Gateway CLI project\n")
	fmt.Printf("   Created: %s\n", configPath)
	fmt.Printf("   Created: %s\n", gitignorePath)
	fmt.Println("")
	fmt.Println("You can now customize the configuration for this project:")
	fmt.Println("  • Set default model: infer config set-model <model-name>")
	fmt.Println("  • Configure tools: infer config tools --help")
	fmt.Println("  • Start chatting: infer chat")

	return nil
}

func TestInitializeProject(t *testing.T) {
	testCases := []struct {
		name      string
		overwrite bool
		wantErr   bool
	}{
		{
			name:      "successful initialization",
			overwrite: false,
			wantErr:   false,
		},
		{
			name:      "initialization with overwrite",
			overwrite: true,
			wantErr:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "infer-test-*")
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

			testInitCmd := &cobra.Command{
				Use:   "init",
				Short: "Initialize a new project with Inference Gateway CLI",
				RunE: func(cmd *cobra.Command, args []string) error {
					return testInitializeProject(cmd)
				},
			}
			testInitCmd.Flags().Bool("overwrite", tc.overwrite, "Overwrite existing files if they already exist")

			err = testInitCmd.RunE(testInitCmd, []string{})

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			configPath := filepath.Join(tempDir, ".infer", "config.yaml")
			gitignorePath := filepath.Join(tempDir, ".infer", ".gitignore")

			assert.FileExists(t, configPath)
			assert.FileExists(t, gitignorePath)

			configData, err := os.ReadFile(configPath)
			require.NoError(t, err)
			assert.Contains(t, string(configData), "gateway:")
			assert.Contains(t, string(configData), "tools:")

			gitignoreData, err := os.ReadFile(gitignorePath)
			require.NoError(t, err)
			assert.Contains(t, string(gitignoreData), "*.log")
			assert.Contains(t, string(gitignoreData), "history")
			assert.Contains(t, string(gitignoreData), "chat_export_*")
		})
	}
}

func TestInitializeProjectExistingFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "infer-test-*")
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

	testInitCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new project with Inference Gateway CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return testInitializeProject(cmd)
		},
	}
	testInitCmd.Flags().Bool("overwrite", false, "Overwrite existing files if they already exist")

	err = testInitCmd.RunE(testInitCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	testInitCmdWithOverwrite := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new project with Inference Gateway CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return testInitializeProject(cmd)
		},
	}
	testInitCmdWithOverwrite.Flags().Bool("overwrite", true, "Overwrite existing files if they already exist")

	err = testInitCmdWithOverwrite.RunE(testInitCmdWithOverwrite, []string{})
	assert.NoError(t, err)
}
