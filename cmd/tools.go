package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal"
)

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Manage and execute tools",
	Long:  "Manage tool configuration and execute whitelisted commands securely.",
}

var toolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List whitelisted commands and patterns",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Printf("Tools enabled: %t\n\n", cfg.Tools.Enabled)
		fmt.Println("Whitelisted commands:")
		for _, command := range cfg.Tools.Whitelist.Commands {
			fmt.Printf("  - %s\n", command)
		}

		fmt.Println("\nWhitelisted patterns:")
		for _, pattern := range cfg.Tools.Whitelist.Patterns {
			fmt.Printf("  - %s\n", pattern)
		}

		return nil
	},
}

var toolsEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable tool execution",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		cfg.Tools.Enabled = true

		if err := cfg.SaveConfig(configPath); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Println("Tools enabled successfully")
		return nil
	},
}

var toolsDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable tool execution",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		cfg.Tools.Enabled = false

		if err := cfg.SaveConfig(configPath); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Println("Tools disabled successfully")
		return nil
	},
}

var toolsExecCmd = &cobra.Command{
	Use:   "exec <command>",
	Short: "Execute a whitelisted command",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		toolEngine := internal.NewToolEngine(cfg)
		command := args[0]
		if len(args) > 1 {
			command = fmt.Sprintf("%s %s", args[0], args[1])
		}

		result, err := toolEngine.ExecuteBash(command)
		if err != nil {
			return fmt.Errorf("execution failed: %w", err)
		}

		outputFormat, _ := cmd.Flags().GetString("format")
		switch outputFormat {
		case "json":
			output, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(output))
		default:
			fmt.Printf("Command: %s\n", result.Command)
			fmt.Printf("Exit Code: %d\n", result.ExitCode)
			fmt.Printf("Duration: %s\n", result.Duration)
			if result.Error != "" {
				fmt.Printf("Error: %s\n", result.Error)
			}
			fmt.Printf("Output:\n%s\n", result.Output)
		}

		if result.ExitCode != 0 {
			os.Exit(result.ExitCode)
		}

		return nil
	},
}

var toolsValidateCmd = &cobra.Command{
	Use:   "validate <command>",
	Short: "Validate if a command is whitelisted",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		toolEngine := internal.NewToolEngine(cfg)
		command := args[0]

		if err := toolEngine.ValidateCommand(command); err != nil {
			fmt.Printf("❌ Command not allowed: %s\n", err)
			os.Exit(1)
		} else {
			fmt.Printf("✅ Command allowed: %s\n", command)
		}

		return nil
	},
}

var toolsLLMCmd = &cobra.Command{
	Use:   "llm",
	Short: "LLM tool interface",
	Long:  "Interface for LLMs to interact with available tools.",
}

var toolsLLMListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available tools for LLMs",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		manager := internal.NewLLMToolsManager(cfg)
		tools := manager.GetAvailableTools()

		outputFormat, _ := cmd.Flags().GetString("format")
		switch outputFormat {
		case "json":
			output, _ := json.MarshalIndent(tools, "", "  ")
			fmt.Println(string(output))
		default:
			if len(tools) == 0 {
				fmt.Println("No tools available (tools may be disabled)")
				return nil
			}
			fmt.Printf("Available tools (%d):\n\n", len(tools))
			for _, tool := range tools {
				fmt.Printf("Name: %s\n", tool.Name)
				fmt.Printf("Description: %s\n", tool.Description)
				fmt.Printf("Parameters: %+v\n\n", tool.Parameters)
			}
		}

		return nil
	},
}

var toolsLLMInvokeCmd = &cobra.Command{
	Use:   "invoke <tool_name> <parameters_json>",
	Short: "Invoke a tool with JSON parameters",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		toolName := args[0]
		parametersJSON := args[1]

		var parameters map[string]interface{}
		if err := json.Unmarshal([]byte(parametersJSON), &parameters); err != nil {
			return fmt.Errorf("failed to parse parameters JSON: %w", err)
		}

		manager := internal.NewLLMToolsManager(cfg)
		result, err := manager.InvokeTool(toolName, parameters)
		if err != nil {
			return fmt.Errorf("tool invocation failed: %w", err)
		}

		fmt.Println(result)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(toolsCmd)

	toolsCmd.AddCommand(toolsListCmd)
	toolsCmd.AddCommand(toolsEnableCmd)
	toolsCmd.AddCommand(toolsDisableCmd)
	toolsCmd.AddCommand(toolsExecCmd)
	toolsCmd.AddCommand(toolsValidateCmd)
	toolsCmd.AddCommand(toolsLLMCmd)

	toolsLLMCmd.AddCommand(toolsLLMListCmd)
	toolsLLMCmd.AddCommand(toolsLLMInvokeCmd)

	toolsExecCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	toolsLLMListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
}
