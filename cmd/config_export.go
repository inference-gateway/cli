package cmd

import (
	"fmt"

	ui "github.com/inference-gateway/cli/internal/ui"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	utils "github.com/inference-gateway/cli/internal/utils"
	cobra "github.com/spf13/cobra"
)

var configExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Manage export command settings",
	Long:  `Configure settings for the /export command including summary model selection.`,
}

var setExportModelCmd = &cobra.Command{
	Use:   "set-model [MODEL_NAME]",
	Short: "Set the model for generating summaries",
	Long: `Set the model to use when generating conversation summaries with /export.
If not set, the current chat model will be used.

Examples:
  infer config export set-model openai/gpt-4-turbo
  infer config export set-model anthropic/claude-4-haiku
  infer config export set-model ""  # Clear to use chat model`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modelName := ""
		if len(args) > 0 {
			modelName = args[0]
		}
		return setExportModel(cmd, modelName)
	},
}

var showExportConfigCmd = &cobra.Command{
	Use:   "show",
	Short: "Show export command configuration",
	Long:  `Display current configuration for the export command.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return showExportConfig(cmd)
	},
}

func init() {
	configExportCmd.AddCommand(setExportModelCmd)
	configExportCmd.AddCommand(showExportConfigCmd)
}

func setExportModel(_ *cobra.Command, modelName string) error {
	V.Set("export.summary_model", modelName)
	if err := utils.WriteViperConfigWithIndent(V, 2); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if modelName == "" {
		fmt.Printf("%s Export will use the current chat model for summaries\n", icons.CheckMarkStyle.Render(icons.CheckMark))
	} else {
		fmt.Printf("%s Set export summary model to %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), ui.FormatSuccess(modelName))
	}

	return nil
}

func showExportConfig(_ *cobra.Command) error {
	fmt.Println("Export Command Configuration:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Output directory: %s\n", V.GetString("export.output_dir"))

	summaryModel := V.GetString("export.summary_model")
	if summaryModel == "" {
		fmt.Printf("Summary model: %s\n", "not configured (uses current chat model)")
	} else {
		fmt.Printf("Summary model: %s\n", ui.FormatSuccess(summaryModel))
	}

	fmt.Println("\n• Use 'infer config export set-model [MODEL]' to change the summary model")

	return nil
}
