package cmd

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	agent "github.com/inference-gateway/cli/internal/agent"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
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
		"session would send to the model. With --tokens, prints size stats " +
		"(characters, lines, estimated tokens) instead of the prompt itself.",
	RunE: func(cmd *cobra.Command, args []string) error {
		agentService := syncedAgentService(cmd.Context(), Cfg)
		prompt := agentService.BuildSystemPrompt()

		showTokens, _ := cmd.Flags().GetBool("tokens")
		if !showTokens {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), prompt)
			return err
		}

		tokenizer := services.NewTokenizerService(services.DefaultTokenizerConfig())
		out := cmd.OutOrStdout()
		if sectioned, ok := agentService.(interface{ SystemPromptSections() []agent.PromptSection }); ok {
			for _, section := range sectioned.SystemPromptSections() {
				text := strings.TrimSpace(section.Text)
				_, _ = fmt.Fprintf(out, "%-22s %7d tokens  (%d chars)\n",
					section.Name, tokenizer.EstimateTokenCount(text), utf8.RuneCountInString(text))
			}
			_, _ = fmt.Fprintln(out)
		}
		_, err := fmt.Fprintf(out,
			"Characters: %d\nLines: %d\nEstimated tokens: %d\n",
			utf8.RuneCountInString(prompt),
			strings.Count(prompt, "\n")+1,
			tokenizer.EstimateTokenCount(prompt),
		)
		return err
	},
}

// syncedAgentService builds the service container and syncs memory in before
// returning the agent service. The sync mirrors the headless agent's
// pre-session hook: with the git memory backend on a fresh machine, MEMORY.md
// only exists locally after SyncIn, and without it the rendered prompt
// silently omits the PERSISTENT MEMORY INDEX section the real agent would
// receive. Fail-soft like the agent: a sync failure must never break the
// debug render.
func syncedAgentService(ctx context.Context, cfg *config.Config) domain.AgentService {
	services := container.NewServiceContainer(cfg)
	_ = services.GetMemoryBackend().SyncIn(ctx)
	return services.GetAgentService()
}

// renderAgentSystemPrompt renders the system prompt a fresh session would send.
func renderAgentSystemPrompt(ctx context.Context, cfg *config.Config) string {
	return syncedAgentService(ctx, cfg).BuildSystemPrompt()
}

func init() {
	debugAgentSystemPromptCmd.Flags().Bool("tokens", false, "print size stats (characters, lines, estimated tokens) instead of the prompt")
	debugAgentCmd.AddCommand(debugAgentSystemPromptCmd)
	debugCmd.AddCommand(debugAgentCmd)
	rootCmd.AddCommand(debugCmd)
}
