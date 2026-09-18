package reset

import (
	"fmt"
	"strings"

	cobra "github.com/spf13/cobra"

	insightscmd "github.com/inference-gateway/cli/cmd/insights"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	container "github.com/inference-gateway/cli/internal/container"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
)

// NewCommand constructs the reset command tree. It is the headless face of the
// /reset chat shortcut and reuses it wholesale, so both surfaces wipe exactly
// the same paths.
func NewCommand(state *runtime.State) *cobra.Command {
	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Wipe all local runtime state and start fresh",
		Long: `Preview everything that a reset would delete: the runtime directories of
every project under ~/.infer/projects, the userspace artifacts, plans, tmp,
telemetry, schedules, run and log directories, and the local conversation
store. Nothing is deleted until you run 'infer reset confirm'.

Configuration (config.yaml, shortcuts, skills, projects.yaml) and saved
insights are preserved. Remote conversation stores (postgres, redis, d1) are
left untouched.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, state, "")
		},
	}

	resetCmd.AddCommand(&cobra.Command{
		Use:   "confirm",
		Short: "Delete everything listed in the preview",
		Long: `Perform the wipe. Typing 'confirm' is the confirmation, so there is no
further prompt - run 'infer reset' first if you want to see the target list.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, state, "confirm")
		},
	})

	insightsCmd := &cobra.Command{
		Use:   "insights",
		Short: "Analyze the sessions first, then preview the wipe",
		Long: `Generate and save a session analysis before previewing the wipe; the report
survives the reset. This one needs a model, so it starts the gateway - the
preview and confirm paths do not.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, state, "insights")
		},
	}
	insightscmd.AddModelFlag(insightsCmd)
	resetCmd.AddCommand(insightsCmd)

	return resetCmd
}

// run dispatches sub through the same non-TUI shortcut path headless uses.
func run(cmd *cobra.Command, state *runtime.State, sub string) error {
	services := container.NewServiceContainer(state.Config())
	registry := services.GetShortcutRegistry()

	useCLISurface(registry)
	if sub == "insights" {
		modelFlag, _ := cmd.Flags().GetString(insightscmd.ModelFlag)
		if err := insightscmd.EnsureModel(cmd.Context(), services, state.Config(), modelFlag); err != nil {
			return err
		}
	}

	// ponytail: no services.Shutdown - it flushes telemetry back into the
	// directory just emptied, and cmd/conversations omits it for the same reason.
	out, _, err := shortcuts.Run(cmd.Context(), registry, strings.TrimSpace("/reset "+sub), shortcuts.Deps{})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), out.Text)
	return err
}

// useCLISurface tells the shortcut it is being driven from the command line, so
// its preview gate starts satisfied and its instructions name `infer reset
// confirm` rather than the chat slash command.
func useCLISurface(registry *shortcuts.Registry) {
	shortcut, ok := registry.Get("reset")
	if !ok {
		return
	}
	if resetShortcut, ok := shortcut.(*shortcuts.ResetShortcut); ok {
		resetShortcut.UseCLISurface()
	}
}
