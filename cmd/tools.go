package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cobra "github.com/spf13/cobra"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	services "github.com/inference-gateway/cli/internal/services"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Run and inspect agent tools directly",
	Long: `Run agent tools directly or check whether a bash command is whitelisted.

These use the exact same execution and validation path as the agent, which makes
them useful for debugging tool behavior and configuration.`,
}

var toolsExecuteCmd = &cobra.Command{
	Use:   "execute <tool> [json-args]",
	Short: "Execute any enabled tool directly",
	Long: `Execute any enabled tool directly with JSON arguments.

Available tools: Bash, Read, Grep, Tree, WebFetch, WebSearch

Arguments must be provided as JSON to match how the agent invokes tools.

Examples:
  # Basic tool execution with required parameters
  infer tools execute Bash '{"command":"ls -la"}'
  infer tools execute Tree '{"path":"."}'
  infer tools execute Read '{"file_path":"README.md"}'
  infer tools execute WebFetch '{"url":"https://example.com"}'
  infer tools execute WebSearch '{"query":"golang tutorial"}'

  # Complex parameters (same as the agent uses)
  infer tools execute Tree '{"path":".", "max_depth":2, "max_files":20}'
  infer tools execute Read '{"file_path":"README.md", "start_line":1, "end_line":10}'

  # No arguments for tools that have defaults
  infer tools execute Tree`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		return ExecTool(Cfg, args, format)
	},
}

var toolsValidateCmd = &cobra.Command{
	Use:   "validate <command>",
	Short: "Validate if a bash command is whitelisted",
	Long:  `Check whether a specific bash command would be allowed to execute, without actually running it.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return ValidateTool(Cfg, args[0])
	},
}

func init() {
	toolsExecuteCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

	toolsCmd.AddCommand(toolsExecuteCmd)
	toolsCmd.AddCommand(toolsValidateCmd)

	rootCmd.AddCommand(toolsCmd)
}

// ValidateTool validates if a command is whitelisted for execution
func ValidateTool(cfg *config.Config, command string) error {
	if !cfg.Tools.Enabled {
		fmt.Printf("%s\n", formatting.FormatErrorCLI("Tools are disabled"))
		return nil
	}

	services := container.NewServiceContainer(cfg)
	toolService := services.GetToolService()
	toolArgs := map[string]any{
		"command": command,
	}

	err := toolService.ValidateTool("Bash", toolArgs)
	if err != nil {
		fmt.Printf("%s\n", formatting.FormatErrorCLI(fmt.Sprintf("Command not allowed: %s", command)))
		fmt.Printf("Reason: %s\n", err.Error())
		return nil
	}

	fmt.Printf("%s\n", formatting.FormatSuccess(fmt.Sprintf("Command is whitelisted: %s", command)))
	return nil
}

// ExecTool executes a tool with the given arguments
func ExecTool(cfg *config.Config, args []string, format string) error {
	if !cfg.Tools.Enabled {
		return fmt.Errorf("tools are not enabled")
	}

	serviceContainer := container.NewServiceContainer(cfg)
	toolService := serviceContainer.GetToolService()
	toolRegistry := serviceContainer.GetToolRegistry()

	if len(args) == 0 {
		return fmt.Errorf("tool name is required")
	}

	toolName := canonicalToolName(toolRegistry.ListAvailableTools(), args[0])
	toolArgs := make(map[string]any)

	if len(args) > 1 {
		if err := json.Unmarshal([]byte(args[1]), &toolArgs); err != nil {
			return fmt.Errorf("arguments must be provided as valid JSON. Example: infer tools execute %s '{\"param\":\"value\"}'. Error: %w", toolName, err)
		}
	}

	if format != "" {
		toolArgs["format"] = format
	}

	if !toolService.IsToolEnabled(toolName) {
		return fmt.Errorf("tool %s is not enabled", toolName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	argsJSON, _ := json.Marshal(toolArgs)
	toolCall := sdk.ChatCompletionMessageToolCallFunction{
		Name:      toolName,
		Arguments: string(argsJSON),
	}
	result, err := toolService.ExecuteTool(ctx, toolCall)
	if err != nil {
		return fmt.Errorf("tool execution failed: %w", err)
	}

	styleProvider := styles.NewProvider(serviceContainer.GetThemeService())
	formatterService := services.NewToolFormatterService(toolRegistry, styleProvider)

	fmt.Print(formatterService.FormatToolResultExpanded(result, 80))
	return nil
}

// canonicalToolName resolves a user-supplied tool name to its registered
// (PascalCase) form case-insensitively, so `infer tools execute read` matches
// the `Read` tool. Falls back to the input when there is no match. This
// convenience is CLI-only; the agent always invokes tools by their exact name.
func canonicalToolName(available []string, name string) string {
	for _, registered := range available {
		if strings.EqualFold(registered, name) {
			return registered
		}
	}
	return name
}
