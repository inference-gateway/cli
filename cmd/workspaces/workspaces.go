package workspaces

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	daemonhost "github.com/inference-gateway/cli/internal/daemon"
)

// clientKind is what these commands declare in their hello.
const clientKind = "cli"

// clientTimeout bounds one command's wait on the daemon, which probes every
// workspace before it answers.
const clientTimeout = 30 * time.Minute

// NewCommand groups the commands about workspaces, the places daemon.yaml lists
// where sessions run and can be teleported to.
func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "workspaces",
		Short: "List the workspaces sessions can run in and be teleported to",
	}
	command.AddCommand(newListCommand(state))
	return command
}

func newListCommand(state *runtime.State) *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List the workspaces in daemon.yaml and whether each is reachable",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), clientTimeout)
			defer cancel()
			client, err := daemonhost.DialLocal(ctx, state.Config(), clientKind)
			if err != nil {
				return err
			}
			defer client.Close()
			if err := client.Send(map[string]string{"type": "list_workspaces"}); err != nil {
				return err
			}
			frame, err := client.Next(ctx, "workspaces")
			if err != nil {
				return err
			}
			return printWorkspaces(cmd.OutOrStdout(), frame)
		},
	}
}

func printWorkspaces(out io.Writer, frame map[string]any) error {
	raw, _ := json.Marshal(frame["workspaces"])
	var workspaces []agentdomain.WorkspaceStatus
	if err := json.Unmarshal(raw, &workspaces); err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprint(w, "NAME\tCOMPUTE\tKIND\tPROTOCOL\tREACHABLE\tDETAILS\n")
	for _, workspace := range workspaces {
		details := workspace.Error
		if workspace.Reachable {
			parts := make([]string, 0, len(workspace.Capabilities))
			for _, key := range slices.Sorted(maps.Keys(workspace.Capabilities)) {
				parts = append(parts, key+"="+workspace.Capabilities[key])
			}
			details = strings.Join(parts, " ")
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%t\t%s\n", workspace.Name, cmp.Or(workspace.Compute, "-"), workspace.Kind, workspace.Protocol, workspace.Reachable, details)
	}
	return w.Flush()
}
