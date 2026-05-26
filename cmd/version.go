package cmd

import (
	"fmt"

	cobra "github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Long:  `Display version information for the Inference Gateway CLI.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("infer %s\n", version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
