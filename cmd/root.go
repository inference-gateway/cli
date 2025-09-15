package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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
	v.SetDefault("gateway", defaults.Gateway)
	v.SetDefault("logging", defaults.Logging)
	v.SetDefault("client", defaults.Client)
	v.SetDefault("tools", defaults.Tools)
	v.SetDefault("agent", defaults.Agent)
	v.SetDefault("a2a", defaults.A2A)

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./.infer")
	v.AddConfigPath("$HOME/.infer")
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if a2aAgents := os.Getenv("INFER_A2A_AGENTS"); a2aAgents != "" {
		agents := strings.Split(a2aAgents, ",")
		for i, agent := range agents {
			agents[i] = strings.TrimSpace(agent)
		}

		v.Set("a2a.agents", agents)
	}

	if err := v.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose")); err != nil {
		fmt.Fprintf(os.Stderr, "Error binding verbose flag: %v\n", err)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
			os.Exit(1)
		}
	}

	verbose := v.GetBool("verbose")
	debug := v.GetBool("logging.debug")
	logDir := v.GetString("logging.dir")

	if logDir == "" {
		configFile := v.ConfigFileUsed()
		if configFile != "" {
			configDir := filepath.Dir(configFile)
			logDir = filepath.Join(configDir, "logs")
		}
	}

	logger.Init(verbose, debug, logDir)
}
