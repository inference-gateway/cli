package headless

import (
	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	presentation "github.com/inference-gateway/cli/internal/presentation/headless"
)

func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "headless [task description]",
		Short: "Execute a task using an autonomous agent in headless mode (non-interactive)",
		Long: `Execute a task in headless (non-interactive) mode. The CLI works
iteratively until the task is considered complete.

Examples:
  infer headless "fix issue #42"
  infer headless --model openai/gpt-4 "implement feature"
  infer headless --mode auto-with-judge "fix issue #42"
  infer headless --files screenshot.png "analyze this"
  infer headless --session-id abc-123 "continue working"

Exit Codes:
  0  task completed
  1  task failed
  2  max turns exhausted`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := presentation.Options{Task: args[0]}
			opts.Model, _ = cmd.Flags().GetString("model")
			opts.Files, _ = cmd.Flags().GetStringSlice("files")
			opts.NoSave, _ = cmd.Flags().GetBool("no-save")
			opts.SessionID, _ = cmd.Flags().GetString("session-id")
			opts.RequireApproval, _ = cmd.Flags().GetBool("require-approval")
			opts.Heartbeat, _ = cmd.Flags().GetBool("heartbeat")
			opts.Remote, _ = cmd.Flags().GetBool("remote")
			opts.ResultFile, _ = cmd.Flags().GetString("result-file")
			opts.Format, _ = cmd.Flags().GetString("format")
			opts.Mode, _ = cmd.Flags().GetString("mode")
			return presentation.Run(state.Config(), opts)
		},
	}

	command.Flags().StringP("model", "m", "", "Model to use (e.g. openai/gpt-4)")
	command.Flags().StringSliceP("files", "f", []string{}, "Files or images to include")
	command.Flags().Bool("no-save", false, "Disable saving conversation to database")
	command.Flags().String("session-id", "", "Resume an existing session by conversation ID")
	command.Flags().Bool("require-approval", false, "Enable IPC tool approval via stdin/stdout")
	command.Flags().Bool("heartbeat", false, "Use heartbeat system prompt")
	command.Flags().Bool("remote", false, "Use remote-control system prompt")
	command.Flags().String("result-file", "", "Write final result JSON to this path")
	command.Flags().String("format", "json", "Output format: json, json-pretty, ag-ui, text")
	command.Flags().String("mode", "", "Agent mode: standard, plan, auto, auto-with-judge (env: INFER_AGENT_MODE)")
	return command
}
