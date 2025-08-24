package cmd

import (
	"fmt"
	"os"
	"strings"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"
)

// Global Viper instance for commands to use
var V *viper.Viper

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
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")

	cobra.OnInitialize(initConfig)
}

func initConfig() {
	V = viper.New()
	v := V

	defaults := config.DefaultConfig()
	v.SetDefault("gateway.url", defaults.Gateway.URL)
	v.SetDefault("gateway.api_key", defaults.Gateway.APIKey)
	v.SetDefault("gateway.timeout", defaults.Gateway.Timeout)
	v.SetDefault("logging.debug", defaults.Logging.Debug)
	v.SetDefault("logging.dir", defaults.Logging.Dir)
	v.SetDefault("client.timeout", defaults.Client.Timeout)
	v.SetDefault("client.retry.enabled", defaults.Client.Retry.Enabled)
	v.SetDefault("client.retry.max_attempts", defaults.Client.Retry.MaxAttempts)
	v.SetDefault("tools.enabled", defaults.Tools.Enabled)
	v.SetDefault("agent.model", defaults.Agent.Model)
	v.SetDefault("agent.system_prompt", defaults.Agent.SystemPrompt)
	v.SetDefault("agent.max_turns", defaults.Agent.MaxTurns)
	v.SetDefault("agent.max_tokens", defaults.Agent.MaxTokens)
	v.SetDefault("agent.verbose_tools", defaults.Agent.VerboseTools)

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./.infer")
	v.AddConfigPath("$HOME/.infer")
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose")); err != nil {
		fmt.Fprintf(os.Stderr, "Error binding verbose flag: %v\n", err)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
			os.Exit(1)
		}
	}

	cfg := &config.Config{
		Logging: config.LoggingConfig{
			Debug: v.GetBool("logging.debug"),
			Dir:   v.GetString("logging.dir"),
		},
	}

	verbose := v.GetBool("verbose")
	logger.Init(verbose, cfg)
}
