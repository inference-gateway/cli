package gatewaycmd

import (
	"fmt"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	gatewaydomain "github.com/inference-gateway/cli/internal/gateway/domain"
	gatewayinfra "github.com/inference-gateway/cli/internal/gateway/infrastructure"
)

const versionFlag = "version"

func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "gateway",
		Short: "Manage the gateway binary the CLI runs from ~/.infer/bin",
		Long: `Install the Inference Gateway binary the CLI starts when gateway.run is
set. The CLI is the single owner of this binary on a local machine: it picks
the release gateway.version asks for, the latest by default, and replaces the
cached binary only when it is another release. Desktop installs through this
command instead of downloading on its own.`,
	}

	installCommand := &cobra.Command{
		Use:          "install",
		Short:        "Install the gateway release gateway.version asks for",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			gatewayConfig := state.Config().Gateway
			if cmd.Flags().Changed(versionFlag) {
				gatewayConfig.Version, _ = cmd.Flags().GetString(versionFlag)
			}
			if err := gatewayConfig.ValidateVersion(); err != nil {
				return err
			}

			pinnedTag, _ := gatewayConfig.PinnedRelease()
			installer := gatewayinfra.NewBinaryInstaller(cmd.OutOrStdout())
			path, err := installer.Ensure(cmd.Context(), gatewaydomain.WantedRelease{PinnedTag: pinnedTag})
			if err != nil {
				return fmt.Errorf("installing the gateway binary: %w", err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "inference-gateway %s %s\n", installer.Probe(cmd.Context()).Version, path)
			return err
		},
	}
	installCommand.Flags().String(versionFlag, "", "install this release (e.g. 0.65.0 or latest) instead of gateway.version")

	command.AddCommand(installCommand)
	return command
}
