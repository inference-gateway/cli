package cmd

import (
	"fmt"

	ui "github.com/inference-gateway/cli/internal/ui"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	utils "github.com/inference-gateway/cli/internal/utils"
	cobra "github.com/spf13/cobra"
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
		return setCompactModel(cmd, modelName)
	},
}

var showCompactConfigCmd = &cobra.Command{
	Use:   "show",
	Short: "Show compact command configuration",
	Long:  `Display current configuration for the compact command.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return showCompactConfig(cmd)
	},
}

func init() {
	configCompactCmd.AddCommand(setCompactModelCmd)
	configCompactCmd.AddCommand(showCompactConfigCmd)
}

func setCompactModel(_ *cobra.Command, modelName string) error {
	V.Set("compact.summary_model", modelName)
	if err := utils.WriteViperConfigWithIndent(V, 2); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if modelName == "" {
		fmt.Printf("%s Compact will use the current chat model for summaries\n", icons.CheckMarkStyle.Render(icons.CheckMark))
	} else {
		fmt.Printf("%s Set compact summary model to %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), ui.FormatSuccess(modelName))
	}

	return nil
}

func showCompactConfig(_ *cobra.Command) error {
	fmt.Println("Compact Command Configuration:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Output directory: %s\n", V.GetString("compact.output_dir"))

	summaryModel := V.GetString("compact.summary_model")
	if summaryModel == "" {
		fmt.Printf("Summary model: %s\n", "not configured (uses current chat model)")
	} else {
		fmt.Printf("Summary model: %s\n", ui.FormatSuccess(summaryModel))
	}

	fmt.Println("\n• Use 'infer config compact set-model [MODEL]' to change the summary model")

	return nil
}
