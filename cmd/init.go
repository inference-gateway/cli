package cmd

import (
	"fmt"
	"os"

	"github.com/inference-gateway/cli/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new project with Inference Gateway CLI",
	Long: `Initialize a new project directory with Inference Gateway CLI configuration.
This creates the .infer directory with configuration file and additional setup files like .gitignore.

This is the recommended command to start working with Inference Gateway CLI in a new project.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return initializeProject(cmd)
	},
}

func init() {
	initCmd.Flags().Bool("overwrite", false, "Overwrite existing files if they already exist")
	rootCmd.AddCommand(initCmd)
}

func initializeProject(cmd *cobra.Command) error {
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
