package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version = "0.1.0"
	commit  = "dev"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Long:  `Display version information for the Inference Gateway CLI.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("infer version %s\n", version)
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("built at: %s\n", date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
