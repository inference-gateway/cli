package reset

import (
	"fmt"

	cobra "github.com/spf13/cobra"

	insightscmd "github.com/inference-gateway/cli/cmd/insights"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	container "github.com/inference-gateway/cli/internal/container"
)

// NewCommand constructs the reset command tree. The /reset chat shortcut is a
// vendored YAML shortcut that shells out to these commands, so this is the only
// implementation of the wipe.
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
further prompt - run 'infer reset' first if you want to see the target list.

A chat session running during the wipe keeps the conversation it already has in
memory; start a new one with /new, or restart the chat, to be fully fresh.`,
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

func run(cmd *cobra.Command, state *runtime.State, sub string) error {
	services := container.NewServiceContainer(state.Config())
	w := &wiper{cfg: state.Config(), store: services.GetStorage()}
	found := w.resolve()
	out := cmd.OutOrStdout()

	switch sub {
	case "insights":
		report, err := insightscmd.Generate(cmd, services, state.Config(), "")
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out, report); err != nil {
			return err
		}
		fallthrough
	case "":
		_, err := fmt.Fprintln(out, preview(found))
		return err
	}

	wiped, err := w.wipe(cmd.Context(), found)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, wiped)
	return err
}
