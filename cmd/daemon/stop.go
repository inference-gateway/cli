package daemon

import (
	"fmt"

	cobra "github.com/spf13/cobra"

	daemonhost "github.com/inference-gateway/cli/internal/daemon"
)

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon, which shuts down everything it runs first",
		Long: `Stop the daemon this user runs, the one whose pid ~/.infer/run/daemon.pid
holds. It shuts down like on Ctrl+C: every session worker ends its turn and
cleans up, every scheduled, heartbeat or A2A run shuts down on SIGTERM, and
the daemon exits. One still running after a minute is killed.

Stopping no daemon is not an error. The next 'infer chat --daemon' starts one.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pid, err := daemonhost.Stop(cmd.Context())
			if err != nil {
				return fmt.Errorf("stopping the infer daemon (pid %d): %w", pid, err)
			}
			if pid == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no infer daemon is running")
				return nil
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "stopped the infer daemon (pid %d)\n", pid)
			return nil
		},
	}
}
