package compute

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	computedomain "github.com/inference-gateway/cli/internal/compute/domain"
	computek8s "github.com/inference-gateway/cli/internal/compute/infrastructure/kubernetes"
)

// clusterCheckTimeout bounds one cluster's status check, so one unreachable
// cluster never holds up the report on the others.
const clusterCheckTimeout = 10 * time.Second

func newClustersCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "clusters",
		Short: "Manage the Kubernetes clusters workspaces run sessions in",
	}

	addCommand := &cobra.Command{
		Use:   "add <name>",
		Short: "Register a Kubernetes cluster in compute.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return clusterAdd(cmd, args[0])
		},
	}
	addCommand.Flags().String("namespace", "", "Namespace every workspace on the cluster shares (required)")
	addCommand.Flags().String("context", "", "Kube context of the cluster, the current one when empty")
	addCommand.Flags().String("kubeconfig", "", "Kubeconfig file, the default one when empty")
	addCommand.Flags().String("mode", "", "Where the gateway runs: standard (default) or all-in-one")
	addCommand.Flags().StringSlice("gateway-env", nil, "Variables the cluster's gateway gets, such as ANTHROPIC_API_KEY")
	_ = addCommand.MarkFlagRequired("namespace")

	command.AddCommand(addCommand,
		&cobra.Command{
			Use:   "list",
			Short: "List the clusters in compute.yaml",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return clusterList(cmd, state.Config().Compute.Clusters)
			},
		},
		&cobra.Command{
			Use:   "status [name]",
			Short: "Check that each cluster is reachable and runs the operator",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return clusterStatus(cmd, state.Config().Compute.Clusters, firstArg(args))
			},
		},
		&cobra.Command{
			Use:   "remove <name>",
			Short: "Remove a cluster no workspace runs on",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := usedBy(state.Config().Daemon, args[0]); err != nil {
					return err
				}
				path, err := editCompute(func(cfg *config.ComputeConfig) error { return cfg.RemoveCluster(args[0]) })
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed cluster %s from %s\n", args[0], path)
				return nil
			},
		},
	)
	return command
}

func clusterAdd(cmd *cobra.Command, name string) error {
	flags := cmd.Flags()
	namespace, _ := flags.GetString("namespace")
	kubeContext, _ := flags.GetString("context")
	kubeconfig, _ := flags.GetString("kubeconfig")
	mode, _ := flags.GetString("mode")
	gatewayEnv, _ := flags.GetStringSlice("gateway-env")
	entry := config.ClusterEntry{Name: name, Kubeconfig: kubeconfig, Context: kubeContext, Namespace: namespace, Mode: config.WorkspaceMode(mode), GatewayEnv: gatewayEnv}
	path, err := editCompute(func(cfg *config.ComputeConfig) error { return cfg.AddCluster(entry) })
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Added cluster %s to %s\n", name, path)
	return nil
}

func clusterList(cmd *cobra.Command, clusters []config.ClusterEntry) error {
	if len(clusters) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No clusters in compute.yaml.")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tCONTEXT\tNAMESPACE\tMODE\tGATEWAY_ENV")
	for _, cluster := range clusters {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", cluster.Name, dash(cluster.Context), cluster.Namespace, dash(string(cluster.Mode)), dash(strings.Join(cluster.GatewayEnv, ",")))
	}
	return w.Flush()
}

func clusterStatus(cmd *cobra.Command, clusters []config.ClusterEntry, name string) error {
	clusters, err := only(clusters, name, func(c config.ClusterEntry) string { return c.Name }, "cluster")
	if err != nil {
		return err
	}
	statuses := checkAll(cmd.Context(), clusters, func(ctx context.Context, cluster config.ClusterEntry) computedomain.ClusterStatus {
		ctx, cancel := context.WithTimeout(ctx, clusterCheckTimeout)
		defer cancel()
		return computek8s.Status(ctx, cluster)
	})
	return printClusterStatuses(cmd, statuses)
}

func printClusterStatuses(cmd *cobra.Command, statuses []computedomain.ClusterStatus) error {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tNAMESPACE\tREACHABLE\tKUBERNETES\tSESSIONS\tDETAILS")
	for _, status := range statuses {
		sessions := "-"
		if status.Reachable {
			sessions = fmt.Sprint(status.Sessions)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\t%s\n", status.Name, status.Namespace, status.Reachable, dash(status.KubernetesVersion), sessions, status.Error)
	}
	return w.Flush()
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
