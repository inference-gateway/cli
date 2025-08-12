package cmd

import (
	"fmt"

	"github.com/inference-gateway/cli/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  `Manage the Inference Gateway CLI configuration settings.`,
}

var setModelCmd = &cobra.Command{
	Use:   "set-model [MODEL_NAME]",
	Short: "Set the default model for chat sessions",
	Long: `Set the default model for chat sessions. When a default model is configured,
the chat command will skip the model selection view and use the configured model directly.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modelName := args[0]
		return setDefaultModel(modelName)
	},
}

func setDefaultModel(modelName string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg.Chat.DefaultModel = modelName

	if err := cfg.SaveConfig(""); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Default model set to: %s\n", modelName)
	fmt.Println("The chat command will now use this model by default and skip model selection.")
	return nil
}

func init() {
	configCmd.AddCommand(setModelCmd)
	rootCmd.AddCommand(configCmd)
}