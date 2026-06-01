package cmd

import (
	"fmt"

	container "github.com/inference-gateway/cli/internal/container"
	cobra "github.com/spf13/cobra"
)

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Diagnostic commands for inspecting agent internals",
	Long:  "Diagnostic commands that surface internal agent state (e.g. the assembled system prompt) for debugging.",
}

var debugAgentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Inspect agent configuration and runtime state",
}

var debugAgentSystemPromptCmd = &cobra.Command{
	Use:   "system_prompt",
	Short: "Print the system prompt a chat session would send to the LLM",
	Long: "Builds and prints the exact system prompt (base prompt + custom " +
		"instructions + dynamic context + date) that a fresh `infer chat` " +
		"session would send to the model.",
	RunE: func(cmd *cobra.Command, args []string) error {
		services := container.NewServiceContainer(Cfg)
		_, err := fmt.Fprintln(cmd.OutOrStdout(), services.GetAgentService().BuildSystemPrompt())
		return err
	},
}

func init() {
	debugAgentCmd.AddCommand(debugAgentSystemPromptCmd)
	debugCmd.AddCommand(debugAgentCmd)
	rootCmd.AddCommand(debugCmd)
}
