package cmd

import (
	"fmt"

	cobra "github.com/spf13/cobra"
)

var version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Long:  `Display version information for the Inference Gateway CLI.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("infer %s\n", version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
