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
	Short: "Print the prompt context a chat session would send to the LLM",
	Long: "Prints the static system prompt (message[0], byte-stable across turns " +
		"for KV-cache prefix reuse), then the volatile context (git, tree, memory, " +
		"date) that is sent each request as a separate hidden <system-reminder> " +
		"user message. With --tokens, prints per-section size stats instead.",
	RunE: func(cmd *cobra.Command, args []string) error {
		agentService := syncedAgentService(cmd.Context(), Cfg)
		full := renderPromptContext(agentService)

		showTokens, _ := cmd.Flags().GetBool("tokens")
		if !showTokens {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), full)
			return err
		}

		tokenizer := services.NewTokenizerService(services.DefaultTokenizerConfig())
		out := cmd.OutOrStdout()
		if sectioned, ok := agentService.(interface{ SystemPromptSections() []agent.PromptSection }); ok {
			inTail := false
			for _, section := range sectioned.SystemPromptSections() {
				if section.Volatile && !inTail {
					inTail = true
					_, _ = fmt.Fprintln(out, "\nvolatile tail (per-request hidden message):")
				}
				text := strings.TrimSpace(section.Text)
				_, _ = fmt.Fprintf(out, "%-22s %7d tokens  (%d chars)\n",
					section.Name, tokenizer.EstimateTokenCount(text), utf8.RuneCountInString(text))
			}
			_, _ = fmt.Fprintln(out)
		}
		_, err := fmt.Fprintf(out,
			"Characters: %d\nLines: %d\nEstimated tokens: %d\n",
			utf8.RuneCountInString(full),
			strings.Count(full, "\n")+1,
			tokenizer.EstimateTokenCount(full),
		)
		return err
	},
}

// volatileTailDivider separates the static system prompt from the volatile
// tail in debug output, naming what the tail actually is on the wire.
const volatileTailDivider = "--- volatile context: sent each request as a separate hidden " +
	"<system-reminder> user message; NOT part of the system prompt ---"

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

// renderAgentSystemPrompt renders the full prompt context a fresh session
// would send: the static system prompt, then the volatile tail below a
// divider marking it as a separate hidden per-request message.
func renderAgentSystemPrompt(ctx context.Context, cfg *config.Config) string {
	return renderPromptContext(syncedAgentService(ctx, cfg))
}

// renderPromptContext assembles the debug view from the exact runtime
// builders, so what this prints is byte-for-byte what goes on the wire.
func renderPromptContext(agentService domain.AgentService) string {
	prompt := agentService.BuildSystemPrompt()
	tailer, ok := agentService.(interface{ VolatileTailText() (string, bool) })
	if !ok {
		return prompt
	}
	tail, ok := tailer.VolatileTailText()
	if !ok {
		return prompt
	}
	return prompt + "\n\n" + volatileTailDivider + "\n\n" + tail
}

func init() {
	debugAgentSystemPromptCmd.Flags().Bool("tokens", false, "print size stats (characters, lines, estimated tokens) instead of the prompt")
	debugAgentCmd.AddCommand(debugAgentSystemPromptCmd)
	debugCmd.AddCommand(debugAgentCmd)
	rootCmd.AddCommand(debugCmd)
}
