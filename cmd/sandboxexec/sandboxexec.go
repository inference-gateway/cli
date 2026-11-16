package sandboxexec

import (
	cobra "github.com/spf13/cobra"

	sandboxinfra "github.com/inference-gateway/cli/internal/sandbox/infrastructure"
)

// NewCommand is the hidden re-exec step of the kernel sandbox: it confines this
// process to the given write paths and then becomes the program after "--",
// which inherits the confinement. It skips the root's config loading, since the
// parent already decided the policy.
func NewCommand() *cobra.Command {
	var writes []string
	command := &cobra.Command{
		Use:               sandboxinfra.ReexecCommand + " [--write dir]... -- program [args...]",
		Hidden:            true,
		Args:              cobra.MinimumNArgs(1),
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(_ *cobra.Command, args []string) error {
			if err := sandboxinfra.Restrict(writes); err != nil {
				return err
			}
			return execProgram(args)
		},
	}
	command.Flags().StringArrayVar(&writes, "write", nil, "directory or file the program may write")
	return command
}
