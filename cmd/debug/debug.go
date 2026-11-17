package debug

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentloop "github.com/inference-gateway/cli/internal/agent/loop"
	container "github.com/inference-gateway/cli/internal/container"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	sandbox "github.com/inference-gateway/cli/internal/sandbox"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
	sandboxinfra "github.com/inference-gateway/cli/internal/sandbox/infrastructure"
)

func NewCommand(state *runtime.State, renderer *output.Renderer) *cobra.Command {
	command := &cobra.Command{
		Use:   "debug",
		Short: "Diagnostic commands for inspecting agent internals",
		Long:  "Diagnostic commands that surface internal agent state (e.g. the assembled system prompt) for debugging.",
	}
	agentCommand := &cobra.Command{
		Use:   "agent",
		Short: "Inspect agent configuration and runtime state",
	}
	systemPromptCommand := &cobra.Command{
		Use:   "system_prompt",
		Short: "Print the prompt context a chat session would send to the LLM",
		Long: "Prints the static system prompt (message[0], byte-stable across turns " +
			"for KV-cache prefix reuse), then the volatile context (git, tree, memory, " +
			"date) that is sent each request as a separate hidden <system-reminder> " +
			"user message. With --tokens, prints per-section size stats instead.",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentService := syncedAgentService(cmd.Context(), state.Config())
			full := renderPromptContext(agentService)

			showTokens, _ := cmd.Flags().GetBool("tokens")
			if !showTokens {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), full)
				return err
			}

			tokenizer := conversation.NewTokenizerService(conversation.DefaultTokenizerConfig())
			out := cmd.OutOrStdout()
			if sectioned, ok := agentService.(*agentloop.Agent); ok {
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

	sandboxCommand := &cobra.Command{
		Use:   "sandbox",
		Short: "Show the effective sandbox for the current directory",
		Long: "Prints what the tools may read and write from here: the allowed and denied " +
			"entries of ~/.infer/" + config.SandboxFileName + " with what each covers, the built-in " +
			"carve-outs, the paths the kernel lets an unattended Bash command write in each agent " +
			"mode, and the files it refuses to read.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return renderSandbox(cmd.OutOrStdout(), state.Config(), renderer)
		},
	}

	systemPromptCommand.Flags().Bool("tokens", false, "print size stats (characters, lines, estimated tokens) instead of the prompt")
	agentCommand.AddCommand(systemPromptCommand)
	command.AddCommand(agentCommand, sandboxCommand)
	return command
}

// renderSandbox prints the effective sandbox from the current directory: the
// policy entries, the built-in carve-outs, and what the kernel lets an
// unattended Bash child write and read in each agent mode.
func renderSandbox(w io.Writer, cfg *config.Config, r *output.Renderer) error {
	fmt.Fprintln(w, r.Title("Policy (~/.infer/"+config.SandboxFileName+" over the defaults)"))
	writePolicy(w, cfg, r)
	fmt.Fprintln(w, r.Title("Built-in carve-outs, open although the config dirs ask"))
	writeCarveOuts(w, cfg, r)
	fmt.Fprintln(w, r.Title("Kernel confinement of unattended Bash"))
	writeKernel(w, cfg, r)
	_, err := fmt.Fprintln(w, r.Hint(bashAllowSummary(cfg)))
	return err
}

func writePolicy(w io.Writer, cfg *config.Config, r *output.Renderer) {
	t := r.NewListTable("Entry", "Effect", "Covers")
	fs := cfg.Tools.Sandbox.Filesystem
	for _, entry := range fs.Allowed {
		effect := "read and write"
		if !entry.Access.Allows(sandboxdomain.AccessWrite) {
			effect = "read only"
		}
		t.Row(entry.Path, effect, covers(entry.Path))
	}
	for _, entry := range fs.Denied {
		effect := "blocked, no approval"
		if entry.OnViolation == sandboxdomain.ViolationApproval {
			effect = "asks before read and write"
		}
		if len(entry.Except) > 0 {
			effect += ", except " + strings.Join(entry.Except, ", ")
		}
		t.Row(entry.Path, effect, covers(entry.Path)+", any case")
	}
	fmt.Fprintln(w, t.Render())
}

// covers spells out what an entry reaches: an anchored path as resolved from
// the current directory, a relative pattern at any depth.
func covers(rulePath string) string {
	if !sandbox.IsAnchored(rulePath) {
		return "any depth"
	}
	return absolute(utils.ExpandHome(rulePath))
}

func absolute(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func writeCarveOuts(w io.Writer, cfg *config.Config, r *output.Renderer) {
	t := r.NewListTable("Path", "Access")
	for _, dir := range sandbox.WritableCarveOuts(cfg) {
		t.Row(absolute(dir), "read and write")
	}
	for _, dir := range sandbox.ReadOnlyCarveOuts() {
		t.Row(dir, "read only")
	}
	fmt.Fprintln(w, t.Render())
}

// writeKernel tabulates the paths a confined Bash child may write per mode,
// then the files the kernel refuses to read. Plan mode differs from the rest,
// since the project gives way to the scratchpad there.
func writeKernel(w io.Writer, cfg *config.Config, r *output.Renderer) {
	if !sandboxinfra.Enforced(cfg) {
		fmt.Fprintln(w, r.Hint("off: filesystem.enforce is false or this platform or kernel has no kernel sandbox (no Landlock), so the policy above is the only guard"))
		return
	}
	standard := sandbox.WritePaths(cfg, agentdomain.AgentModeStandard)
	plan := sandbox.WritePaths(cfg, agentdomain.AgentModePlan)
	paths := slices.Clone(standard)
	for _, path := range plan {
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	t := r.NewListTable("Path", "standard, auto", "plan")
	for _, path := range paths {
		t.Row(path, writeOrRead(writableUnder(standard, path)), writeOrRead(writableUnder(plan, path)))
	}
	t.Row("everything else", "read and execute", "read and execute")
	fmt.Fprintln(w, r.Field("Kernel", sandboxinfra.KernelName()))
	fmt.Fprintln(w, t.Render())

	denied := sandboxinfra.KernelDenied(cfg)
	if len(denied) == 0 {
		fmt.Fprintln(w, r.Hint("reads stay with the policy above: this kernel only grants"))
		return
	}
	names := make([]string, 0, len(denied))
	for _, entry := range denied {
		names = append(names, entry.Path)
	}
	fmt.Fprintln(w, r.Field("Refused to read, rename or delete", strings.Join(names, ", ")))
}

// writableUnder reports whether path is one of the write dirs or inside one,
// so a listed subdirectory of a writable parent shows as writable too.
func writableUnder(dirs []string, path string) bool {
	return slices.ContainsFunc(dirs, func(dir string) bool {
		return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
	})
}

func writeOrRead(writable bool) string {
	if writable {
		return "write"
	}
	return "read"
}

// bashAllowSummary counts the allow-list each mode runs unattended, which is
// what the kernel confines. Auto reads "every command" when its list lets an
// arbitrary one through.
func bashAllowSummary(cfg *config.Config) string {
	auto := fmt.Sprintf("%d in auto", len(sandbox.BashAllowedCommands(cfg, agentdomain.AgentModeAutoAccept)))
	if sandbox.IsBashCommandAllowed(cfg, "infer-debug-sandbox-probe", agentdomain.AgentModeAutoAccept) {
		auto = "every command in auto"
	}
	return fmt.Sprintf("Bash allow-list: %d patterns in standard, %d in plan, %s. infer tools validate <command> checks one.",
		len(sandbox.BashAllowedCommands(cfg, agentdomain.AgentModeStandard)),
		len(sandbox.BashAllowedCommands(cfg, agentdomain.AgentModePlan)), auto)
}

// volatileTailDivider separates the static system prompt from the volatile
// tail in debug output, naming what the tail actually is on the wire.
const volatileTailDivider = "--- volatile context: sent each request as a separate hidden " +
	"<system-reminder> user message; NOT part of the system prompt ---"

// syncedAgentService builds the service container and syncs memory in before
// returning the agent service, mirroring the headless agent's pre-session hook:
// with the git memory backend on a fresh machine, MEMORY.md only exists locally
// after SyncIn - without it the prompt silently omits the PERSISTENT MEMORY
// INDEX section. Fail-soft: a sync failure never breaks the debug render.
func syncedAgentService(ctx context.Context, cfg *config.Config) agentdomain.AgentService {
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
func renderPromptContext(agentService agentdomain.AgentService) string {
	prompt := agentService.BuildSystemPrompt()
	tailer, ok := agentService.(*agentloop.Agent)
	if !ok {
		return prompt
	}
	tail, ok := tailer.VolatileTailText()
	if !ok {
		return prompt
	}
	return prompt + "\n\n" + volatileTailDivider + "\n\n" + tail
}
