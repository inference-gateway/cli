package compute

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	browserinfra "github.com/inference-gateway/cli/internal/browser/infrastructure"
)

func newMachinesCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "machines",
		Short: "Manage the standalone machines workspaces run sessions on",
	}

	addCommand := &cobra.Command{
		Use:   "add <name>",
		Short: "Register a standalone infer daemon in compute.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return machineAdd(cmd, args[0])
		},
	}
	addCommand.Flags().String("address", "", "host:port of the machine's daemon binding (required)")
	addCommand.Flags().String("token", "", "Binding token, best written as a ${VAR} reference")
	addCommand.Flags().String("ca-file", "", "CA that signed the machine's certificate, the system roots when empty")
	addCommand.Flags().String("cert-file", "", "Client certificate to present when the machine requires one")
	addCommand.Flags().String("key-file", "", "Key of the client certificate")
	_ = addCommand.MarkFlagRequired("address")

	command.AddCommand(addCommand,
		&cobra.Command{
			Use:   "list",
			Short: "List the machines in compute.yaml",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return machineList(cmd, state.Config().Compute.Machines)
			},
		},
		&cobra.Command{
			Use:   "status [name]",
			Short: "Check that each machine's daemon answers",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return machineStatus(cmd, state.Config(), firstArg(args))
			},
		},
		&cobra.Command{
			Use:   "remove <name>",
			Short: "Remove a machine no workspace runs on",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := usedBy(state.Config().Daemon, args[0]); err != nil {
					return err
				}
				path, err := editCompute(func(cfg *config.ComputeConfig) error { return cfg.RemoveMachine(args[0]) })
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed machine %s from %s\n", args[0], path)
				return nil
			},
		},
	)
	return command
}

func machineAdd(cmd *cobra.Command, name string) error {
	flags := cmd.Flags()
	address, _ := flags.GetString("address")
	token, _ := flags.GetString("token")
	caFile, _ := flags.GetString("ca-file")
	certFile, _ := flags.GetString("cert-file")
	keyFile, _ := flags.GetString("key-file")
	entry := config.MachineEntry{Name: name, Address: address, Token: token, TLS: config.PeerTLSConfig{CAFile: caFile, CertFile: certFile, KeyFile: keyFile}}
	path, err := editCompute(func(cfg *config.ComputeConfig) error { return cfg.AddMachine(entry) })
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Added machine %s to %s\n", name, path)
	return nil
}

// machineList never prints a token, which may be a resolved secret.
func machineList(cmd *cobra.Command, machines []config.MachineEntry) error {
	if len(machines) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No machines in compute.yaml.")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tADDRESS\tCA_FILE\tCLIENT_CERT")
	for _, machine := range machines {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", machine.Name, machine.Address, dash(machine.TLS.CAFile), dash(machine.TLS.CertFile))
	}
	return w.Flush()
}

// machineStatus probes each machine the way the daemon probes a daemon-kind
// workspace: dialled over TLS with this daemon's identity, answered by a hello.
func machineStatus(cmd *cobra.Command, cfg *config.Config, name string) error {
	machines, err := only(cfg.Compute.Machines, name, func(m config.MachineEntry) string { return m.Name }, "machine")
	if err != nil {
		return err
	}
	identity := cfg.Daemon.Identity()
	tlsProvider := agentinfra.NewTLSProvider(identity, nil)
	dial := agentinfra.PeerDialer{Me: identity, Handshake: browserinfra.BindingHandshake}
	statuses := checkAll(cmd.Context(), machines, func(ctx context.Context, machine config.MachineEntry) agentdomain.WorkspaceStatus {
		entry := config.WorkspaceEntry{Name: machine.Name, Kind: config.WorkspaceKindDaemon, Address: machine.Address, Token: machine.Token, TLS: machine.TLS}
		workspace, err := agentinfra.NewDaemonWorkspace(entry, tlsProvider, dial)
		if err != nil {
			return agentdomain.WorkspaceStatus{Name: machine.Name, Error: err.Error()}
		}
		return workspace.Probe(ctx)
	})
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tREACHABLE\tDETAILS")
	for _, status := range statuses {
		details := status.Error
		if status.Reachable {
			parts := make([]string, 0, len(status.Capabilities))
			for _, key := range slices.Sorted(maps.Keys(status.Capabilities)) {
				parts = append(parts, key+"="+status.Capabilities[key])
			}
			details = strings.Join(parts, " ")
		}
		_, _ = fmt.Fprintf(w, "%s\t%t\t%s\n", status.Name, status.Reachable, details)
	}
	return w.Flush()
}
