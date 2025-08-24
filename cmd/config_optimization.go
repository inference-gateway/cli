package cmd

import (
	"fmt"
	"strconv"

	ui "github.com/inference-gateway/cli/internal/ui"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	cobra "github.com/spf13/cobra"
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
		return setOptimizationEnabled(cmd, true)
	},
}

var optimizationDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable token optimization",
	Long:  `Disable token optimization (use full conversation history).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setOptimizationEnabled(cmd, false)
	},
}

var optimizationStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show token optimization status",
	Long:  `Display current token optimization settings and configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return showOptimizationStatus(cmd)
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
		return setOptimizationParameter(cmd, args[0], args[1])
	},
}

func init() {
	configOptimizationCmd.AddCommand(optimizationEnableCmd)
	configOptimizationCmd.AddCommand(optimizationDisableCmd)
	configOptimizationCmd.AddCommand(optimizationStatusCmd)
	configOptimizationCmd.AddCommand(optimizationSetCmd)
}

func setOptimizationEnabled(_ *cobra.Command, enabled bool) error {
	V.Set("agent.optimization.enabled", enabled)
	if err := V.WriteConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	status := ui.FormatSuccess("disabled")
	if enabled {
		status = ui.FormatSuccess("enabled")
	}
	fmt.Printf("%s Token optimization %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), status)

	if enabled {
		fmt.Println("\nOptimization settings:")
		fmt.Printf("  • Max history: %d messages\n", V.GetInt("agent.optimization.max_history"))
		fmt.Printf("  • Compact threshold: %d messages\n", V.GetInt("agent.optimization.compact_threshold"))
		fmt.Printf("  • Truncate large outputs: %v\n", V.GetBool("agent.optimization.truncate_large_outputs"))
		fmt.Printf("  • Skip redundant confirmations: %v\n", V.GetBool("agent.optimization.skip_redundant_confirmations"))
	}

	return nil
}

func showOptimizationStatus(_ *cobra.Command) error {
	fmt.Println("Token Optimization Settings:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━")

	status := ui.FormatError("disabled")
	if V.GetBool("agent.optimization.enabled") {
		status = ui.FormatSuccess("enabled")
	}
	fmt.Printf("Status: %s\n", status)

	fmt.Printf("\nParameters:\n")
	fmt.Printf("  • Max history: %d messages\n", V.GetInt("agent.optimization.max_history"))
	fmt.Printf("  • Compact threshold: %d messages\n", V.GetInt("agent.optimization.compact_threshold"))
	fmt.Printf("  • Truncate large outputs: %v\n", V.GetBool("agent.optimization.truncate_large_outputs"))
	fmt.Printf("  • Skip redundant confirmations: %v\n", V.GetBool("agent.optimization.skip_redundant_confirmations"))

	if V.GetBool("agent.optimization.enabled") {
		fmt.Println("\n💡 Optimization is active. Conversation history will be managed to reduce token usage.")
	} else {
		fmt.Println("\n💡 Optimization is disabled. Full conversation history will be sent with each request.")
	}

	return nil
}

func setOptimizationParameter(_ *cobra.Command, param, value string) error {
	switch param {
	case "max-history":
		intVal, err := strconv.Atoi(value)
		if err != nil || intVal < 1 {
			return fmt.Errorf("max-history must be a positive integer")
		}
		V.Set("agent.optimization.max_history", intVal)

	case "compact-threshold":
		intVal, err := strconv.Atoi(value)
		if err != nil || intVal < 1 {
			return fmt.Errorf("compact-threshold must be a positive integer")
		}
		V.Set("agent.optimization.compact_threshold", intVal)

	case "truncate-outputs":
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("truncate-outputs must be true or false")
		}
		V.Set("agent.optimization.truncate_large_outputs", boolVal)

	case "skip-confirmations":
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("skip-confirmations must be true or false")
		}
		V.Set("agent.optimization.skip_redundant_confirmations", boolVal)

	default:
		return fmt.Errorf("unknown parameter: %s", param)
	}

	if err := V.WriteConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("%s Set %s to %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), param, ui.FormatSuccess(value))
	return nil
}
