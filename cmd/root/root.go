package root

import (
	"context"
	"errors"
	"fmt"
	"os"

	fang "charm.land/fang/v2"
	colorprofile "github.com/charmbracelet/colorprofile"
	cobra "github.com/spf13/cobra"

	agents "github.com/inference-gateway/cli/cmd/agents"
	chat "github.com/inference-gateway/cli/cmd/chat"
	configcmd "github.com/inference-gateway/cli/cmd/config"
	conversations "github.com/inference-gateway/cli/cmd/conversations"
	conversationtitle "github.com/inference-gateway/cli/cmd/conversationtitle"
	daemon "github.com/inference-gateway/cli/cmd/daemon"
	debug "github.com/inference-gateway/cli/cmd/debug"
	envcmd "github.com/inference-gateway/cli/cmd/env"
	export "github.com/inference-gateway/cli/cmd/export"
	gpu "github.com/inference-gateway/cli/cmd/gpu"
	headless "github.com/inference-gateway/cli/cmd/headless"
	initcmd "github.com/inference-gateway/cli/cmd/init"
	keybindings "github.com/inference-gateway/cli/cmd/keybindings"
	mcp "github.com/inference-gateway/cli/cmd/mcp"
	migrate "github.com/inference-gateway/cli/cmd/migrate"
	output "github.com/inference-gateway/cli/cmd/output"
	plans "github.com/inference-gateway/cli/cmd/plans"
	plugins "github.com/inference-gateway/cli/cmd/plugins"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	skills "github.com/inference-gateway/cli/cmd/skills"
	stats "github.com/inference-gateway/cli/cmd/stats"
	status "github.com/inference-gateway/cli/cmd/status"
	tools "github.com/inference-gateway/cli/cmd/tools"
	traces "github.com/inference-gateway/cli/cmd/traces"
	version "github.com/inference-gateway/cli/cmd/version"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// ExitCodeMaxTurns distinguishes agent turn exhaustion from generic failure.
const ExitCodeMaxTurns = 2

func NewCommand() *cobra.Command {
	state := runtime.NewState()
	renderer := output.NewRenderer()
	command := &cobra.Command{
		Use:   "infer",
		Short: "The CLI for the Inference Gateway",
		Long: `A powerful command-line interface for managing and interacting with
the Inference Gateway. This CLI provides tools for configuration,
deployment, monitoring, and management of inference services.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := state.Initialize(cmd.Root()); err != nil {
				return err
			}
			if ownsStdout(cmd) {
				state.DisableStdoutLogging()
			}
			noColors, _ := cmd.Flags().GetBool("no-colors")
			if noColors || colorprofile.Detect(os.Stdout, os.Environ()) < colorprofile.ANSI {
				renderer.DisableColors()
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Welcome to the Inference Gateway CLI!")
			fmt.Println("Use 'infer chat' to start interactive chat or --help to see available commands.")
			return nil
		},
	}
	command.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	command.PersistentFlags().Bool("no-colors", false,
		"disable ANSI colors in command output (colors are also auto-disabled when stdout is not a terminal or NO_COLOR is set)")
	command.PersistentFlags().String("tools-bash-allow-append", "",
		"comma/newline-separated commands added to the bash allow-list in every mode (standard, plan, auto); INFER_TOOLS_BASH_ALLOW_APPEND takes precedence")
	command.PersistentFlags().String("reminders-file", "",
		"path to a reminders YAML file, overriding project .infer/ and ~/.infer reminders.yaml (INFER_REMINDERS_CONFIG inline YAML takes precedence)")

	command.AddCommand(
		agents.NewCommand(state, renderer),
		chat.NewCommand(state),
		configcmd.NewCommand(state),
		conversationtitle.NewCommand(state),
		conversations.NewCommand(state, renderer),
		daemon.NewCommand(state),
		debug.NewCommand(state),
		envcmd.NewCommand(),
		export.NewCommand(state),
		gpu.NewCommand(state),
		headless.NewCommand(state),
		initcmd.NewCommand(state),
		keybindings.NewCommand(state),
		mcp.NewCommand(renderer),
		migrate.NewCommand(state),
		plans.NewCommand(state, renderer),
		plugins.NewCommand(state, renderer),
		skills.NewCommand(state, renderer),
		stats.NewCommand(state, renderer),
		status.NewCommand(state),
		tools.NewCommand(state),
		traces.NewCommand(renderer),
		version.NewCommand(),
	)
	return command
}

func Execute() {
	defer logger.Close()
	if err := fang.Execute(context.Background(), NewCommand(), fang.WithVersion(version.Value())); err != nil {
		if errors.Is(err, agentdomain.ErrMaxTurnsReached) {
			os.Exit(ExitCodeMaxTurns)
		}
		os.Exit(1)
	}
}

func ownsStdout(command *cobra.Command) bool {
	return command.Annotations[output.TUICommandAnnotation] == "true"
}
