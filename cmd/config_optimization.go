package cmd

import (
	"fmt"
	"strconv"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/ui"
	"github.com/spf13/cobra"
)

var configOptimizationCmd = &cobra.Command{
	Use:   "optimization",
	Short: "Manage token optimization settings",
	Long:  `Configure token optimization to reduce API costs by intelligently managing conversation history.`,
}

var optimizationEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable token optimization",
	Long:  `Enable token optimization to reduce API costs through intelligent message management.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setOptimizationEnabled(true)
	},
}

var optimizationDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable token optimization",
	Long:  `Disable token optimization (use full conversation history).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setOptimizationEnabled(false)
	},
}

var optimizationStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show token optimization status",
	Long:  `Display current token optimization settings and configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return showOptimizationStatus()
	},
}

var optimizationSetCmd = &cobra.Command{
	Use:   "set [setting] [value]",
	Short: "Set optimization parameters",
	Long: `Configure specific optimization parameters:
  - max-history: Maximum messages to keep in full (default: 10)
  - compact-threshold: Messages older than this are compacted (default: 20)
  - truncate-outputs: Truncate large tool outputs (true/false)
  - skip-confirmations: Skip redundant assistant confirmations (true/false)`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setOptimizationParameter(args[0], args[1])
	},
}

func init() {
	configOptimizationCmd.AddCommand(optimizationEnableCmd)
	configOptimizationCmd.AddCommand(optimizationDisableCmd)
	configOptimizationCmd.AddCommand(optimizationStatusCmd)
	configOptimizationCmd.AddCommand(optimizationSetCmd)
}

func setOptimizationEnabled(enabled bool) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg.Agent.Optimization.Enabled = enabled

	if err := cfg.SaveConfig(""); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	status := ui.FormatSuccess("disabled")
	if enabled {
		status = ui.FormatSuccess("enabled")
	}
	fmt.Printf("✅ Token optimization %s\n", status)

	if enabled {
		fmt.Println("\nOptimization settings:")
		fmt.Printf("  • Max history: %d messages\n", cfg.Agent.Optimization.MaxHistory)
		fmt.Printf("  • Compact threshold: %d messages\n", cfg.Agent.Optimization.CompactThreshold)
		fmt.Printf("  • Truncate large outputs: %v\n", cfg.Agent.Optimization.TruncateLargeOutputs)
		fmt.Printf("  • Skip redundant confirmations: %v\n", cfg.Agent.Optimization.SkipRedundantConfirmations)
	}

	return nil
}

func showOptimizationStatus() error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println("Token Optimization Settings:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━")

	status := ui.FormatError("disabled")
	if cfg.Agent.Optimization.Enabled {
		status = ui.FormatSuccess("enabled")
	}
	fmt.Printf("Status: %s\n", status)

	fmt.Printf("\nParameters:\n")
	fmt.Printf("  • Max history: %d messages\n", cfg.Agent.Optimization.MaxHistory)
	fmt.Printf("  • Compact threshold: %d messages\n", cfg.Agent.Optimization.CompactThreshold)
	fmt.Printf("  • Truncate large outputs: %v\n", cfg.Agent.Optimization.TruncateLargeOutputs)
	fmt.Printf("  • Skip redundant confirmations: %v\n", cfg.Agent.Optimization.SkipRedundantConfirmations)

	if cfg.Agent.Optimization.Enabled {
		fmt.Println("\n💡 Optimization is active. Conversation history will be managed to reduce token usage.")
	} else {
		fmt.Println("\n💡 Optimization is disabled. Full conversation history will be sent with each request.")
	}

	return nil
}

func setOptimizationParameter(param, value string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	switch param {
	case "max-history":
		intVal, err := strconv.Atoi(value)
		if err != nil || intVal < 1 {
			return fmt.Errorf("max-history must be a positive integer")
		}
		cfg.Agent.Optimization.MaxHistory = intVal

	case "compact-threshold":
		intVal, err := strconv.Atoi(value)
		if err != nil || intVal < 1 {
			return fmt.Errorf("compact-threshold must be a positive integer")
		}
		cfg.Agent.Optimization.CompactThreshold = intVal

	case "truncate-outputs":
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("truncate-outputs must be true or false")
		}
		cfg.Agent.Optimization.TruncateLargeOutputs = boolVal

	case "skip-confirmations":
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("skip-confirmations must be true or false")
		}
		cfg.Agent.Optimization.SkipRedundantConfirmations = boolVal

	default:
		return fmt.Errorf("unknown parameter: %s", param)
	}

	if err := cfg.SaveConfig(""); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Set %s to %s\n", param, ui.FormatSuccess(value))
	return nil
}
