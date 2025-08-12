package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/spf13/cobra"
)

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Manage and execute tools for LLM interaction",
	Long: `Manage the tool execution system that allows LLMs to execute whitelisted bash commands securely.
Tools must be explicitly enabled and commands must be whitelisted for execution.`,
}

var toolsEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable tool execution for LLMs",
	Long:  `Enable the tool execution system, allowing LLMs to execute whitelisted bash commands.`,
	RunE:  enableTools,
}

var toolsDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable tool execution for LLMs",
	Long:  `Disable the tool execution system, preventing LLMs from executing any commands.`,
	RunE:  disableTools,
}

var toolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List whitelisted commands and patterns",
	Long:  `Display all whitelisted commands and regex patterns that can be executed by LLMs.`,
	RunE:  listTools,
}

var toolsValidateCmd = &cobra.Command{
	Use:   "validate <command>",
	Short: "Validate if a command is whitelisted",
	Long:  `Check if a specific command would be allowed to execute without actually running it.`,
	Args:  cobra.ExactArgs(1),
	RunE:  validateTool,
}

var toolsExecCmd = &cobra.Command{
	Use:   "exec <command>",
	Short: "Execute a whitelisted command directly",
	Long:  `Execute a command directly if it passes whitelist validation.`,
	Args:  cobra.ExactArgs(1),
	RunE:  execTool,
}

var toolsSafetyCmd = &cobra.Command{
	Use:   "safety",
	Short: "Manage safety approval settings",
	Long:  `Manage safety approval prompts that are shown before executing commands.`,
}

var toolsSafetyEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable safety approval prompts",
	Long:  `Enable safety approval prompts that ask for confirmation before executing commands.`,
	RunE:  enableSafety,
}

var toolsSafetyDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable safety approval prompts",
	Long:  `Disable safety approval prompts, allowing commands to execute immediately.`,
	RunE:  disableSafety,
}

var toolsSafetyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current safety approval status",
	Long:  `Display whether safety approval prompts are currently enabled or disabled.`,
	RunE:  safetyStatus,
}

func init() {
	rootCmd.AddCommand(toolsCmd)

	toolsCmd.AddCommand(toolsEnableCmd)
	toolsCmd.AddCommand(toolsDisableCmd)
	toolsCmd.AddCommand(toolsListCmd)
	toolsCmd.AddCommand(toolsValidateCmd)
	toolsCmd.AddCommand(toolsExecCmd)
	toolsCmd.AddCommand(toolsSafetyCmd)

	toolsSafetyCmd.AddCommand(toolsSafetyEnableCmd)
	toolsSafetyCmd.AddCommand(toolsSafetyDisableCmd)
	toolsSafetyCmd.AddCommand(toolsSafetyStatusCmd)

	toolsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	toolsExecCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
}

func enableTools(cmd *cobra.Command, args []string) error {
	cfg, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Enabled = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("✅ Tools enabled successfully\n")
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("Whitelisted commands: %d\n", len(cfg.Tools.Whitelist.Commands))
	fmt.Printf("Whitelisted patterns: %d\n", len(cfg.Tools.Whitelist.Patterns))
	return nil
}

func disableTools(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Enabled = false
	})
	if err != nil {
		return err
	}

	fmt.Printf("❌ Tools disabled successfully\n")
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	return nil
}

func listTools(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	format, _ := cmd.Flags().GetString("format")

	if format == "json" {
		data := map[string]interface{}{
			"enabled":  cfg.Tools.Enabled,
			"commands": cfg.Tools.Whitelist.Commands,
			"patterns": cfg.Tools.Whitelist.Patterns,
			"safety": map[string]bool{
				"require_approval": cfg.Tools.Safety.RequireApproval,
			},
		}
		jsonOutput, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(jsonOutput))
		return nil
	}

	fmt.Printf("Tools Status: ")
	if cfg.Tools.Enabled {
		fmt.Printf("✅ Enabled\n")
	} else {
		fmt.Printf("❌ Disabled\n")
	}

	fmt.Printf("\nWhitelisted Commands (%d):\n", len(cfg.Tools.Whitelist.Commands))
	for _, cmd := range cfg.Tools.Whitelist.Commands {
		fmt.Printf("  • %s\n", cmd)
	}

	fmt.Printf("\nWhitelisted Patterns (%d):\n", len(cfg.Tools.Whitelist.Patterns))
	for _, pattern := range cfg.Tools.Whitelist.Patterns {
		fmt.Printf("  • %s\n", pattern)
	}

	fmt.Printf("\nSafety Settings:\n")
	if cfg.Tools.Safety.RequireApproval {
		fmt.Printf("  • Approval required: ✅ Enabled\n")
	} else {
		fmt.Printf("  • Approval required: ❌ Disabled\n")
	}

	return nil
}

func validateTool(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !cfg.Tools.Enabled {
		fmt.Printf("❌ Tools are disabled\n")
		return nil
	}

	services := container.NewServiceContainer(cfg)
	toolService := services.GetToolService()

	command := args[0]
	toolArgs := map[string]interface{}{
		"command": command,
	}

	err = toolService.ValidateTool("Bash", toolArgs)
	if err != nil {
		fmt.Printf("❌ Command not allowed: %s\n", command)
		fmt.Printf("Reason: %s\n", err.Error())
		return nil
	}

	fmt.Printf("✅ Command is whitelisted: %s\n", command)
	return nil
}

func execTool(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !cfg.Tools.Enabled {
		return fmt.Errorf("tools are not enabled")
	}

	services := container.NewServiceContainer(cfg)
	toolService := services.GetToolService()

	command := args[0]
	format, _ := cmd.Flags().GetString("format")

	toolArgs := map[string]interface{}{
		"command": command,
		"format":  format,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	result, err := toolService.ExecuteTool(ctx, "Bash", toolArgs)
	if err != nil {
		return fmt.Errorf("command execution failed: %w", err)
	}

	fmt.Print(result)
	return nil
}

func enableSafety(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Safety.RequireApproval = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("✅ Safety approval enabled\n")
	fmt.Printf("Commands will require approval before execution\n")
	return nil
}

func disableSafety(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Safety.RequireApproval = false
	})
	if err != nil {
		return err
	}

	fmt.Printf("⚠️  Safety approval disabled\n")
	fmt.Printf("Commands will execute immediately without approval\n")
	return nil
}

func safetyStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Printf("Safety Approval Status: ")
	if cfg.Tools.Safety.RequireApproval {
		fmt.Printf("✅ Enabled\n")
		fmt.Printf("Commands require approval before execution\n")
	} else {
		fmt.Printf("❌ Disabled\n")
		fmt.Printf("Commands execute immediately without approval\n")
	}

	return nil
}

func loadAndUpdateConfig(updateFn func(*config.Config)) (*config.Config, error) {
	configPath := getConfigPath()
	cfg, err := config.LoadConfig("")
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	updateFn(cfg)

	err = cfg.SaveConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return cfg, nil
}

func getConfigPath() string {
	configPath := ".infer/config.yaml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = ".infer.yaml"
	}
	return configPath
}
