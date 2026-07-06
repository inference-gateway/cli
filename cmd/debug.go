package cmd

import (
	"context"
	"fmt"

	config "github.com/inference-gateway/cli/config"
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
		_, err := fmt.Fprintln(cmd.OutOrStdout(), renderAgentSystemPrompt(cmd.Context(), Cfg))
		return err
	},
}

// renderAgentSystemPrompt syncs memory in and renders the system prompt a
// fresh session would send. The sync mirrors the headless agent's pre-session
// hook: with the git memory backend on a fresh machine, MEMORY.md only exists
// locally after SyncIn, and without it the render silently omits the
// PERSISTENT MEMORY INDEX section the real agent would receive. Fail-soft
// like the agent: a sync failure must never break the debug render.
func renderAgentSystemPrompt(ctx context.Context, cfg *config.Config) string {
	services := container.NewServiceContainer(cfg)
	_ = services.GetMemoryBackend().SyncIn(ctx)
	return services.GetAgentService().BuildSystemPrompt()
}

func init() {
	debugAgentCmd.AddCommand(debugAgentSystemPromptCmd)
	debugCmd.AddCommand(debugAgentCmd)
	rootCmd.AddCommand(debugCmd)
}
