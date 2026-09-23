package insights

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// ModelFlag names the flag that picks the model the analysis runs on.
const ModelFlag = "model"

// NewCommand constructs the insights command. It is the headless face of the
// /insights chat shortcut and reuses it wholesale.
func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "insights [since]",
		Short: "Analyze past sessions for repeatable workflows and recurring tool failures",
		Long: `Distill saved sessions into a markdown report - which workflows repeat often
enough to deserve a skill, and which tool calls keep failing the same way - and
save it to ~/.infer/insights/.

Counts and error strings are computed locally; the model only interprets them.
Needs conversation storage enabled.

Examples:
  infer insights           # every saved session
  infer insights 7d        # only the last 7 days
  infer insights 24h --model deepseek/deepseek-chat`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			since := ""
			if len(args) == 1 {
				since = args[0]
			}
			return run(cmd, state, since)
		},
	}
	AddModelFlag(command)
	return command
}

// AddModelFlag declares --model on any command that generates an insights
// report, so `infer reset insights` spells it the same way.
func AddModelFlag(command *cobra.Command) {
	command.Flags().StringP(ModelFlag, "m", "", "Model to run the analysis on (defaults to agent.model)")
}

func run(cmd *cobra.Command, state *runtime.State, since string) error {
	services := container.NewServiceContainer(state.Config())
	report, err := Generate(cmd, services, state.Config(), since)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), report)
	return err
}

// Generate resolves the model, runs the analysis and returns the saved report,
// so `infer reset insights` produces exactly what `infer insights` does.
func Generate(cmd *cobra.Command, services *container.ServiceContainer, cfg *config.Config, since string) (string, error) {
	window, err := telemetry.ParseSince(since)
	if err != nil {
		return "", err
	}

	modelFlag, _ := cmd.Flags().GetString(ModelFlag)
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Checking the gateway and model...")
	if err := EnsureModel(cmd.Context(), services, cfg, modelFlag); err != nil {
		return "", err
	}

	markdown, path, err := services.GetInsightsGenerator().Generate(cmd.Context(), window, cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("failed to generate insights: %w", err)
	}
	return markdown + "\nSaved to " + path, nil
}

// EnsureModel starts the gateway and selects the model the analysis runs on. A
// one-shot command has no chat session to inherit a selection from, so without
// this the generator asks the gateway for an empty model.
func EnsureModel(ctx context.Context, services *container.ServiceContainer, cfg *config.Config, modelFlag string) error {
	model := cmp.Or(modelFlag, cfg.Agent.Model)
	if model == "" {
		return fmt.Errorf("no model specified; use --%s or set agent.model in config", ModelFlag)
	}

	if err := services.GetGatewaySupervisor().EnsureStarted(); err != nil {
		return fmt.Errorf("failed to start inference gateway: %w", err)
	}

	listCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Gateway.Timeout)*time.Second)
	defer cancel()
	available, err := services.GetModelService().ListModels(listCtx)
	if err != nil {
		return fmt.Errorf("inference gateway not available: %w", err)
	}
	if !slices.Contains(available, model) {
		return fmt.Errorf("model %q not available. Available: %v", model, available)
	}
	return services.GetModelService().SelectModel(model)
}
