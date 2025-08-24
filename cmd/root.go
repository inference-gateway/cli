package cmd

import (
	"fmt"
	"os"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
	cobra "github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "infer",
	Short: "The CLI for the Inference Gateway",
	Long: `A powerful command-line interface for managing and interacting with
the Inference Gateway. This CLI provides tools for configuration,
deployment, monitoring, and management of inference services.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 && !cmd.Flags().Changed("help") {
			fmt.Println("Welcome to the Inference Gateway CLI!")
			fmt.Println("Use 'infer chat' to start interactive chat or --help to see available commands.")
		} else {
			fmt.Println("Welcome to the Inference Gateway CLI!")
			fmt.Println("Use --help to see available commands or 'infer chat' for interactive mode.")
		}
	},
}

func Execute() {
	defer logger.Close()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("config", "c", "", fmt.Sprintf("config file (default is %s)", config.DefaultConfigPath))
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")

	cobra.OnInitialize(initConfig)
}

func initConfig() {
	verbose, _ := rootCmd.PersistentFlags().GetBool("verbose")

	configPath := config.GetConfigPath(false)
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config from %s: %v\n", configPath, err)
		os.Exit(1)
	}

	logger.Init(verbose, cfg)
}
