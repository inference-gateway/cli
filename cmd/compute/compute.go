package compute

import (
	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
)

// NewCommand is `infer compute`, managing what sessions and models run on,
// one subcommand per kind of compute.
func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "compute",
		Short: "Manage the clusters, machines and GPUs infer runs on",
		Long: `Manage what infer runs on, kept in ~/.infer/compute.yaml apart from any daemon:
Kubernetes clusters and standalone machines that workspaces run sessions on,
and GPUs rented on demand to serve a model.`,
	}
	command.AddCommand(newGPUsCommand(state, "gpus"))
	return command
}
