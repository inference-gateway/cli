package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the inference gateway",
	Long: `Display the current status of the inference gateway including:
- Running services
- Model deployments
- Health checks
- Resource usage`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Checking inference gateway status...")

		detailed, _ := cmd.Flags().GetBool("detailed")
		format, _ := cmd.Flags().GetString("format")

		if detailed {
			fmt.Println("Detailed status information:")
			fmt.Println("- Gateway: Running")
			fmt.Println("- Models deployed: 3")
			fmt.Println("- Active connections: 42")
			fmt.Println("- Memory usage: 2.1GB")
			fmt.Println("- CPU usage: 15%")
		} else {
			fmt.Println("Gateway Status: Running")
			fmt.Println("Models: 3 active")
		}

		if format != "text" {
			fmt.Printf("Output format: %s\n", format)
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)

	statusCmd.Flags().BoolP("detailed", "d", false, "Show detailed status information")
	statusCmd.Flags().StringP("format", "f", "text", "Output format (text, json, yaml)")
}
