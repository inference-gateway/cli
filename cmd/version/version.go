package version

import (
	"fmt"

	cobra "github.com/spf13/cobra"

	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

var version = "dev"

// Stamp the build version onto telemetry's resource (service.version) once, so
// exported metrics carry which CLI version produced them.
func init() { telemetry.Version = version }

func Value() string { return version }

func NewCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Long:  `Display version information for the Inference Gateway CLI.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("infer %s\n", version)
			return nil
		},
	}
}
