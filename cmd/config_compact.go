package cmd

import (
	"fmt"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/ui"
	"github.com/spf13/cobra"
)

var configCompactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Manage compact command settings",
	Long:  `Configure settings for the /compact command including summary model selection.`,
}

var setCompactModelCmd = &cobra.Command{
	Use:   "set-model [MODEL_NAME]",
	Short: "Set the model for generating summaries",
	Long: `Set the model to use when generating conversation summaries with /compact.
If not set, the current chat model will be used.

Examples:
  infer config compact set-model openai/gpt-4-turbo
  infer config compact set-model anthropic/claude-3-haiku
  infer config compact set-model ""  # Clear to use chat model`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modelName := ""
		if len(args) > 0 {
			modelName = args[0]
		}
		return setCompactModel(modelName)
	},
}

var showCompactConfigCmd = &cobra.Command{
	Use:   "show",
	Short: "Show compact command configuration",
	Long:  `Display current configuration for the compact command.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return showCompactConfig()
	},
}

func init() {
	configCompactCmd.AddCommand(setCompactModelCmd)
	configCompactCmd.AddCommand(showCompactConfigCmd)
}

func setCompactModel(modelName string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg.Compact.SummaryModel = modelName

	if err := cfg.SaveConfig(""); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if modelName == "" {
		fmt.Println("✅ Compact will use the current chat model for summaries")
	} else {
		fmt.Printf("✅ Set compact summary model to %s\n", ui.FormatSuccess(modelName))
	}

	return nil
}

func showCompactConfig() error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println("Compact Command Configuration:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Output directory: %s\n", cfg.Compact.OutputDir)

	if cfg.Compact.SummaryModel == "" {
		fmt.Printf("Summary model: %s\n", "(uses current chat model)")
	} else {
		fmt.Printf("Summary model: %s\n", ui.FormatSuccess(cfg.Compact.SummaryModel))
	}

	fmt.Println("\n💡 Use 'infer config compact set-model [MODEL]' to change the summary model")

	return nil
}
