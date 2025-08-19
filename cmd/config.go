package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/ui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  `Manage the Inference Gateway CLI configuration settings.`,
}

var setModelCmd = &cobra.Command{
	Use:   "set-model [MODEL_NAME]",
	Short: "Set the default model for chat sessions",
	Long: `Set the default model for chat sessions. When a default model is configured,
the chat command will skip the model selection view and use the configured model directly.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modelName := args[0]
		return setDefaultModel(modelName)
	},
}

var setSystemCmd = &cobra.Command{
	Use:   "set-system [SYSTEM_PROMPT]",
	Short: "Set the system prompt for chat sessions",
	Long: `Set the system prompt that will be included with every chat session.
The system prompt provides context and instructions to the AI model about how to behave.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		systemPrompt := args[0]
		return setSystemPrompt(systemPrompt)
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new configuration file",
	Long: `Initialize a new .infer/config.yaml configuration file in the current directory.
This creates only the configuration file with default settings.

For complete project initialization, use 'infer init' instead.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := ".infer/config.yaml"

		if _, err := os.Stat(configPath); err == nil {
			overwrite, _ := cmd.Flags().GetBool("overwrite")
			if !overwrite {
				return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
			}
		}

		cfg := config.DefaultConfig()

		if err := cfg.SaveConfig(configPath); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}

		fmt.Printf("Successfully created %s\n", configPath)
		fmt.Println("You can now customize the configuration for this project.")
		fmt.Println("Tip: Use 'infer init' for complete project initialization including additional setup files.")

		return nil
	},
}

var configToolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Manage tool execution settings",
	Long: `Manage the tool execution system that allows LLMs to execute whitelisted bash commands securely.
Tools must be explicitly enabled and commands must be whitelisted for execution.`,
}

var configToolsEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable tool execution for LLMs",
	Long:  `Enable the tool execution system, allowing LLMs to execute whitelisted bash commands.`,
	RunE:  enableTools,
}

var configToolsDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable tool execution for LLMs",
	Long:  `Disable the tool execution system, preventing LLMs from executing any commands.`,
	RunE:  disableTools,
}

var configToolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List whitelisted commands and patterns",
	Long:  `Display all whitelisted commands and regex patterns that can be executed by LLMs.`,
	RunE:  listTools,
}

var configToolsValidateCmd = &cobra.Command{
	Use:   "validate <command>",
	Short: "Validate if a command is whitelisted",
	Long:  `Check if a specific command would be allowed to execute without actually running it.`,
	Args:  cobra.ExactArgs(1),
	RunE:  validateTool,
}

var configToolsExecCmd = &cobra.Command{
	Use:   "exec <tool> [json-args]",
	Short: "Execute any enabled tool directly",
	Long: `Execute any enabled tool directly with JSON arguments.

Available tools: Bash, Read, Grep, Tree, WebFetch, WebSearch

Arguments must be provided as JSON to ensure consistency with LLM tool execution.

Examples:
  # Basic tool execution with required parameters
  infer config tools exec Bash '{"command":"ls -la"}'
  infer config tools exec Tree '{"path":"."}'
  infer config tools exec Read '{"file_path":"README.md"}'
  infer config tools exec WebFetch '{"url":"https://example.com"}'
  infer config tools exec WebSearch '{"query":"golang tutorial"}'

  # Complex parameters (same as LLMs use)
  infer config tools exec Tree '{"path":".", "max_depth":2, "max_files":20}'
  infer config tools exec Read '{"file_path":"README.md", "start_line":1, "end_line":10}'

  # No arguments for tools that have defaults
  infer config tools exec Tree

This uses the exact same argument parsing as LLMs, ensuring consistency.`,
	Args: cobra.MinimumNArgs(1),
	RunE: execTool,
}

var configToolsSafetyCmd = &cobra.Command{
	Use:   "safety",
	Short: "Manage safety approval settings",
	Long:  `Manage safety approval prompts that are shown before executing commands.`,
}

var configToolsSafetyEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable safety approval prompts",
	Long:  `Enable safety approval prompts that ask for confirmation before executing commands.`,
	RunE:  enableSafety,
}

var configToolsSafetyDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable safety approval prompts",
	Long:  `Disable safety approval prompts, allowing commands to execute immediately.`,
	RunE:  disableSafety,
}

var configToolsSafetyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current safety approval status",
	Long:  `Display whether safety approval prompts are currently enabled or disabled.`,
	RunE:  safetyStatus,
}

var configToolsSafetySetCmd = &cobra.Command{
	Use:   "set <tool> <enabled|disabled>",
	Short: "Set tool-specific approval requirement",
	Long:  `Set whether approval is required for a specific tool. Valid tools: Bash, Read, Grep, WebFetch, WebSearch.`,
	Args:  cobra.ExactArgs(2),
	RunE:  setToolApproval,
}

var configToolsSafetyUnsetCmd = &cobra.Command{
	Use:   "unset <tool>",
	Short: "Remove tool-specific approval setting",
	Long:  `Remove tool-specific approval setting, falling back to global setting. Valid tools: Bash, Read, Grep, WebFetch, WebSearch.`,
	Args:  cobra.ExactArgs(1),
	RunE:  unsetToolApproval,
}

var configToolsExcludePathCmd = &cobra.Command{
	Use:   "exclude-path",
	Short: "Manage excluded paths",
	Long:  `Manage paths that are excluded from tool access for security purposes.`,
}

var configToolsExcludePathListCmd = &cobra.Command{
	Use:   "list",
	Short: "List excluded paths",
	Long:  `Display all paths that are excluded from tool access.`,
	RunE:  listExcludedPaths,
}

var configToolsExcludePathAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add a path to the exclusion list",
	Long:  `Add a path to the exclusion list to prevent tools from accessing it.`,
	Args:  cobra.ExactArgs(1),
	RunE:  addExcludedPath,
}

var configToolsExcludePathRemoveCmd = &cobra.Command{
	Use:   "remove <path>",
	Short: "Remove a path from the exclusion list",
	Long:  `Remove a path from the exclusion list to allow tools to access it again.`,
	Args:  cobra.ExactArgs(1),
	RunE:  removeExcludedPath,
}

var configToolsBashCmd = &cobra.Command{
	Use:   "bash",
	Short: "Manage bash tool settings",
	Long:  `Manage bash-specific tool execution settings.`,
}

var configToolsBashEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable bash tool execution",
	Long:  `Enable the bash tool for executing whitelisted bash commands.`,
	RunE:  enableBashTool,
}

var configToolsBashDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable bash tool execution",
	Long:  `Disable the bash tool to prevent execution of bash commands.`,
	RunE:  disableBashTool,
}

var configToolsWebSearchCmd = &cobra.Command{
	Use:   "web-search",
	Short: "Manage web search tool settings",
	Long:  `Manage web search-specific tool execution settings.`,
}

var configToolsWebSearchEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable web search tool",
	Long:  `Enable the web search tool for LLM searches.`,
	RunE:  enableWebSearchTool,
}

var configToolsWebSearchDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable web search tool",
	Long:  `Disable the web search tool to prevent web searches.`,
	RunE:  disableWebSearchTool,
}

var configToolsGrepCmd = &cobra.Command{
	Use:   "grep",
	Short: "Manage grep tool settings",
	Long:  `Manage grep-specific tool execution settings including backend configuration.`,
}

var configToolsGrepEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable grep tool",
	Long:  `Enable the grep tool for searching file contents.`,
	RunE:  enableGrepTool,
}

var configToolsGrepDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable grep tool",
	Long:  `Disable the grep tool to prevent file content searches.`,
	RunE:  disableGrepTool,
}

var configToolsGrepBackendCmd = &cobra.Command{
	Use:   "set-backend <backend>",
	Short: "Set the grep backend implementation",
	Long: `Set the backend implementation for grep operations.

Available backends:
  auto     - Automatically choose ripgrep if available, fall back to Go implementation (default)
  ripgrep  - Use native ripgrep binary (fastest, requires rg to be installed)
  rg       - Alias for ripgrep
  go       - Use Go-based implementation (portable, no external dependencies)
  native   - Alias for go

The 'auto' backend provides the best experience by using ripgrep when available
for maximum performance, and gracefully falling back to the Go implementation
when ripgrep is not installed.`,
	Args: cobra.ExactArgs(1),
	RunE: setGrepBackend,
}

var configToolsGrepStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show grep tool status and backend information",
	Long:  `Display current grep tool status, configured backend, and detection results.`,
	RunE:  grepStatus,
}

var configFetchCmd = &cobra.Command{
	Use:   "web-fetch",
	Short: "Manage web fetch tool settings",
	Long: `Manage the web fetch tool that allows LLMs to retrieve content from whitelisted URLs.
The web fetch tool supports GitHub integration and URL pattern matching for secure content retrieval.`,
}

var configFetchEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable the web fetch tool",
	Long:  `Enable the web fetch tool to allow LLMs to retrieve content from whitelisted sources.`,
	RunE:  enableFetch,
}

var configFetchDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the web fetch tool",
	Long:  `Disable the web fetch tool to prevent LLMs from retrieving any external content.`,
	RunE:  disableFetch,
}

var configFetchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List whitelisted domains",
	Long:  `Display all whitelisted domains that can be fetched by LLMs.`,
	RunE:  listFetchDomains,
}

var configFetchAddDomainCmd = &cobra.Command{
	Use:   "add-domain <domain>",
	Short: "Add a domain to the whitelist",
	Long:  `Add a domain to the whitelist of allowed web fetch sources (e.g., github.com, example.org).`,
	Args:  cobra.ExactArgs(1),
	RunE:  addFetchDomain,
}

var configFetchRemoveDomainCmd = &cobra.Command{
	Use:   "remove-domain <domain>",
	Short: "Remove a domain from the whitelist",
	Long:  `Remove a domain from the whitelist of allowed web fetch sources.`,
	Args:  cobra.ExactArgs(1),
	RunE:  removeFetchDomain,
}

var configFetchGitHubCmd = &cobra.Command{
	Use:   "github",
	Short: "Manage GitHub integration settings",
	Long:  `Manage GitHub-specific web fetch settings including API access and optimization features.`,
}

var configFetchGitHubEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable GitHub integration",
	Long:  `Enable GitHub API integration for optimized fetching of GitHub issues and pull requests.`,
	RunE:  enableGitHubFetch,
}

var configFetchGitHubDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable GitHub integration",
	Long:  `Disable GitHub API integration, falling back to regular HTTP fetching.`,
	RunE:  disableGitHubFetch,
}

var configFetchGitHubTokenCmd = &cobra.Command{
	Use:   "set-token <token>",
	Short: "Set GitHub API token",
	Long:  `Set the GitHub API token for authenticated requests to increase rate limits.`,
	Args:  cobra.ExactArgs(1),
	RunE:  setGitHubToken,
}

var configFetchCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage web fetch cache settings",
	Long:  `Manage caching settings for fetched content to improve performance.`,
}

var configFetchCacheStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show cache status and statistics",
	Long:  `Display current cache status, statistics, and configuration.`,
	RunE:  fetchCacheStatus,
}

var configFetchCacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the web fetch cache",
	Long:  `Clear all cached content to free up memory and force fresh fetches.`,
	RunE:  fetchCacheClear,
}

func setDefaultModel(modelName string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg.Chat.DefaultModel = modelName

	if err := cfg.SaveConfig(""); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Default model set to: %s\n", modelName)
	fmt.Println("The chat command will now use this model by default and skip model selection.")
	return nil
}

func setSystemPrompt(systemPrompt string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cfg.Chat.SystemPrompt = systemPrompt

	if err := cfg.SaveConfig(""); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ System prompt set successfully\n")
	fmt.Printf("System prompt: %s\n", systemPrompt)
	fmt.Println("This prompt will be included with every chat session.")
	return nil
}

func init() {
	configCmd.AddCommand(setModelCmd)
	configCmd.AddCommand(setSystemCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configToolsCmd)
	configCmd.AddCommand(configFetchCmd)

	configToolsCmd.AddCommand(configToolsEnableCmd)
	configToolsCmd.AddCommand(configToolsDisableCmd)
	configToolsCmd.AddCommand(configToolsListCmd)
	configToolsCmd.AddCommand(configToolsValidateCmd)
	configToolsCmd.AddCommand(configToolsExecCmd)
	configToolsCmd.AddCommand(configToolsSafetyCmd)
	configToolsCmd.AddCommand(configToolsExcludePathCmd)
	configToolsCmd.AddCommand(configToolsBashCmd)
	configToolsCmd.AddCommand(configToolsWebSearchCmd)
	configToolsCmd.AddCommand(configToolsGrepCmd)

	configToolsSafetyCmd.AddCommand(configToolsSafetyEnableCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetyDisableCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetyStatusCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetySetCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetyUnsetCmd)

	configToolsExcludePathCmd.AddCommand(configToolsExcludePathListCmd)
	configToolsExcludePathCmd.AddCommand(configToolsExcludePathAddCmd)
	configToolsExcludePathCmd.AddCommand(configToolsExcludePathRemoveCmd)

	configToolsBashCmd.AddCommand(configToolsBashEnableCmd)
	configToolsBashCmd.AddCommand(configToolsBashDisableCmd)

	configToolsWebSearchCmd.AddCommand(configToolsWebSearchEnableCmd)
	configToolsWebSearchCmd.AddCommand(configToolsWebSearchDisableCmd)

	configToolsGrepCmd.AddCommand(configToolsGrepEnableCmd)
	configToolsGrepCmd.AddCommand(configToolsGrepDisableCmd)
	configToolsGrepCmd.AddCommand(configToolsGrepBackendCmd)
	configToolsGrepCmd.AddCommand(configToolsGrepStatusCmd)

	configFetchCmd.AddCommand(configFetchEnableCmd)
	configFetchCmd.AddCommand(configFetchDisableCmd)
	configFetchCmd.AddCommand(configFetchListCmd)
	configFetchCmd.AddCommand(configFetchAddDomainCmd)
	configFetchCmd.AddCommand(configFetchRemoveDomainCmd)
	configFetchCmd.AddCommand(configFetchGitHubCmd)
	configFetchCmd.AddCommand(configFetchCacheCmd)

	configFetchGitHubCmd.AddCommand(configFetchGitHubEnableCmd)
	configFetchGitHubCmd.AddCommand(configFetchGitHubDisableCmd)
	configFetchGitHubCmd.AddCommand(configFetchGitHubTokenCmd)

	configFetchCacheCmd.AddCommand(configFetchCacheStatusCmd)
	configFetchCacheCmd.AddCommand(configFetchCacheClearCmd)

	configInitCmd.Flags().Bool("overwrite", false, "Overwrite existing configuration file")
	configToolsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	configToolsExecCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	configFetchListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

	rootCmd.AddCommand(configCmd)
}

func enableTools(cmd *cobra.Command, args []string) error {
	cfg, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Enabled = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Tools enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("Whitelisted commands: %d\n", len(cfg.Tools.Bash.Whitelist.Commands))
	fmt.Printf("Whitelisted patterns: %d\n", len(cfg.Tools.Bash.Whitelist.Patterns))
	return nil
}

func disableTools(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Enabled = false
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Tools disabled successfully"))
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
			"enabled": cfg.Tools.Enabled,
			"bash": map[string]bool{
				"enabled": cfg.Tools.Bash.Enabled,
			},
			"web_fetch": map[string]interface{}{
				"enabled":             cfg.Tools.WebFetch.Enabled,
				"whitelisted_domains": cfg.Tools.WebFetch.WhitelistedDomains,
				"github": map[string]interface{}{
					"enabled":   cfg.Tools.WebFetch.GitHub.Enabled,
					"base_url":  cfg.Tools.WebFetch.GitHub.BaseURL,
					"has_token": cfg.Tools.WebFetch.GitHub.Token != "",
				},
			},
			"web_search": map[string]interface{}{
				"enabled":        cfg.Tools.WebSearch.Enabled,
				"default_engine": cfg.Tools.WebSearch.DefaultEngine,
				"max_results":    cfg.Tools.WebSearch.MaxResults,
				"engines":        cfg.Tools.WebSearch.Engines,
				"timeout":        cfg.Tools.WebSearch.Timeout,
			},
			"commands":      cfg.Tools.Bash.Whitelist.Commands,
			"patterns":      cfg.Tools.Bash.Whitelist.Patterns,
			"exclude_paths": cfg.Tools.ExcludePaths,
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
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("\nIndividual Tools:\n")
	fmt.Printf("  Bash: ")
	if cfg.Tools.Bash.Enabled {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("  WebFetch: ")
	if cfg.Tools.WebFetch.Enabled {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("  WebSearch: ")
	if cfg.Tools.WebSearch.Enabled {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("\nWhitelisted Commands (%d):\n", len(cfg.Tools.Bash.Whitelist.Commands))
	for _, cmd := range cfg.Tools.Bash.Whitelist.Commands {
		fmt.Printf("  • %s\n", cmd)
	}

	fmt.Printf("\nWhitelisted Patterns (%d):\n", len(cfg.Tools.Bash.Whitelist.Patterns))
	for _, pattern := range cfg.Tools.Bash.Whitelist.Patterns {
		fmt.Printf("  • %s\n", pattern)
	}

	fmt.Printf("\nExcluded Paths (%d):\n", len(cfg.Tools.ExcludePaths))
	if len(cfg.Tools.ExcludePaths) == 0 {
		fmt.Printf("  • None\n")
	} else {
		for _, path := range cfg.Tools.ExcludePaths {
			fmt.Printf("  • %s\n", path)
		}
	}

	fmt.Printf("\nSafety Settings:\n")
	if cfg.Tools.Safety.RequireApproval {
		fmt.Printf("  • Approval required: %s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("  • Approval required: %s\n", ui.FormatErrorCLI("Disabled"))
	}

	return nil
}

func validateTool(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !cfg.Tools.Enabled {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Tools are disabled"))
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
		fmt.Printf("%s\n", ui.FormatErrorCLI(fmt.Sprintf("Command not allowed: %s", command)))
		fmt.Printf("Reason: %s\n", err.Error())
		return nil
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Command is whitelisted: %s", command)))
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

	if len(args) == 0 {
		return fmt.Errorf("tool name is required")
	}

	toolName := args[0]
	toolArgs := make(map[string]interface{})

	if len(args) > 1 {
		if err := json.Unmarshal([]byte(args[1]), &toolArgs); err != nil {
			return fmt.Errorf("arguments must be provided as valid JSON. Example: infer config tools exec %s '{\"param\":\"value\"}'. Error: %w", toolName, err)
		}
	}

	if format, _ := cmd.Flags().GetString("format"); format != "" {
		toolArgs["format"] = format
	}

	if !toolService.IsToolEnabled(toolName) {
		return fmt.Errorf("tool %s is not enabled", toolName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	result, err := toolService.ExecuteTool(ctx, toolName, toolArgs)
	if err != nil {
		return fmt.Errorf("tool execution failed: %w", err)
	}

	fmt.Print(ui.FormatToolResultExpanded(result))
	return nil
}

func enableSafety(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Safety.RequireApproval = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Safety approval enabled"))
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

	fmt.Printf("%s\n", ui.FormatWarning("Safety approval disabled"))
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
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
		fmt.Printf("Commands require approval before execution\n")
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
		fmt.Printf("Commands execute immediately without approval\n")
	}

	fmt.Printf("\nTool-specific Approval Settings:\n")
	tools := []struct {
		name    string
		setting *bool
	}{
		{"Bash", cfg.Tools.Bash.RequireApproval},
		{"Read", cfg.Tools.Read.RequireApproval},
		{"Grep", cfg.Tools.Grep.RequireApproval},
		{"WebFetch", cfg.Tools.WebFetch.RequireApproval},
		{"WebSearch", cfg.Tools.WebSearch.RequireApproval},
	}

	for _, tool := range tools {
		fmt.Printf("  %s: ", tool.name)
		if tool.setting == nil {
			fmt.Printf("using global setting (%s)\n",
				func() string {
					if cfg.Tools.Safety.RequireApproval {
						return "enabled"
					}
					return "disabled"
				}())
		} else if *tool.setting {
			fmt.Printf("%s\n", ui.FormatSuccess("enabled"))
		} else {
			fmt.Printf("%s\n", ui.FormatErrorCLI("disabled"))
		}
	}

	return nil
}

func setToolApproval(cmd *cobra.Command, args []string) error {
	toolName := args[0]
	setting := args[1]

	var enabled bool
	switch strings.ToLower(setting) {
	case "enabled", "enable", "true":
		enabled = true
	case "disabled", "disable", "false":
		enabled = false
	default:
		return fmt.Errorf("invalid setting '%s', must be 'enabled' or 'disabled'", setting)
	}

	validTools := []string{"bash", "read", "grep", "webfetch", "websearch"}
	toolLower := strings.ToLower(toolName)
	isValid := false
	for _, valid := range validTools {
		if toolLower == valid {
			isValid = true
			break
		}
	}
	if !isValid {
		return fmt.Errorf("invalid tool '%s', must be one of: %s", toolName, strings.Join(validTools, ", "))
	}

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		switch toolLower {
		case "bash":
			c.Tools.Bash.RequireApproval = &enabled
		case "read":
			c.Tools.Read.RequireApproval = &enabled
		case "grep":
			c.Tools.Grep.RequireApproval = &enabled
		case "webfetch":
			c.Tools.WebFetch.RequireApproval = &enabled
		case "websearch":
			c.Tools.WebSearch.RequireApproval = &enabled
		}
	})
	if err != nil {
		return err
	}

	status := "enabled"
	if !enabled {
		status = "disabled"
	}
	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Tool-specific approval for %s %s", toolName, status)))
	return nil
}

func unsetToolApproval(cmd *cobra.Command, args []string) error {
	toolName := args[0]

	validTools := []string{"bash", "read", "grep", "webfetch", "websearch"}
	toolLower := strings.ToLower(toolName)
	isValid := false
	for _, valid := range validTools {
		if toolLower == valid {
			isValid = true
			break
		}
	}
	if !isValid {
		return fmt.Errorf("invalid tool '%s', must be one of: %s", toolName, strings.Join(validTools, ", "))
	}

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		switch toolLower {
		case "bash":
			c.Tools.Bash.RequireApproval = nil
		case "read":
			c.Tools.Read.RequireApproval = nil
		case "grep":
			c.Tools.Grep.RequireApproval = nil
		case "webfetch":
			c.Tools.WebFetch.RequireApproval = nil
		case "websearch":
			c.Tools.WebSearch.RequireApproval = nil
		}
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Tool-specific approval setting removed for %s (using global setting)", toolName)))
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

func listExcludedPaths(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(cfg.Tools.ExcludePaths) == 0 {
		fmt.Println("No paths are currently excluded.")
		return nil
	}

	fmt.Printf("Excluded Paths (%d):\n", len(cfg.Tools.ExcludePaths))
	for _, path := range cfg.Tools.ExcludePaths {
		fmt.Printf("  • %s\n", path)
	}

	return nil
}

func addExcludedPath(cmd *cobra.Command, args []string) error {
	pathToAdd := args[0]

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		for _, existingPath := range c.Tools.ExcludePaths {
			if existingPath == pathToAdd {
				return
			}
		}
		c.Tools.ExcludePaths = append(c.Tools.ExcludePaths, pathToAdd)
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Added '%s' to excluded paths", pathToAdd)))
	fmt.Printf("Tools will no longer be able to access this path\n")
	return nil
}

func removeExcludedPath(cmd *cobra.Command, args []string) error {
	pathToRemove := args[0]
	var found bool

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		for i, existingPath := range c.Tools.ExcludePaths {
			if existingPath == pathToRemove {
				c.Tools.ExcludePaths = append(c.Tools.ExcludePaths[:i], c.Tools.ExcludePaths[i+1:]...)
				found = true
				return
			}
		}
	})
	if err != nil {
		return err
	}

	if !found {
		fmt.Printf("%s\n", ui.FormatWarning(fmt.Sprintf("Path '%s' was not in the excluded paths list", pathToRemove)))
		return nil
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Removed '%s' from excluded paths", pathToRemove)))
	fmt.Printf("Tools can now access this path again\n")
	return nil
}

func enableFetch(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.WebFetch.Enabled = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Web fetch tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Println("You can now configure whitelisted sources with 'infer config web-fetch add-domain <domain>'")
	return nil
}

func disableFetch(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.WebFetch.Enabled = false
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Web fetch tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	return nil
}

func listFetchDomains(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	format, _ := cmd.Flags().GetString("format")

	if format == "json" {
		data := map[string]interface{}{
			"enabled":             cfg.Tools.WebFetch.Enabled,
			"whitelisted_domains": cfg.Tools.WebFetch.WhitelistedDomains,
			"github": map[string]interface{}{
				"enabled":   cfg.Tools.WebFetch.GitHub.Enabled,
				"base_url":  cfg.Tools.WebFetch.GitHub.BaseURL,
				"has_token": cfg.Tools.WebFetch.GitHub.Token != "",
			},
			"safety": map[string]interface{}{
				"max_size":       cfg.Tools.WebFetch.Safety.MaxSize,
				"timeout":        cfg.Tools.WebFetch.Safety.Timeout,
				"allow_redirect": cfg.Tools.WebFetch.Safety.AllowRedirect,
			},
			"cache": map[string]interface{}{
				"enabled":  cfg.Tools.WebFetch.Cache.Enabled,
				"ttl":      cfg.Tools.WebFetch.Cache.TTL,
				"max_size": cfg.Tools.WebFetch.Cache.MaxSize,
			},
		}
		jsonOutput, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(jsonOutput))
		return nil
	}

	fmt.Printf("Web Fetch Tool Status: ")
	if cfg.Tools.WebFetch.Enabled {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("\nWhitelisted Domains (%d):\n", len(cfg.Tools.WebFetch.WhitelistedDomains))
	if len(cfg.Tools.WebFetch.WhitelistedDomains) == 0 {
		fmt.Printf("  • None configured\n")
	} else {
		for _, domain := range cfg.Tools.WebFetch.WhitelistedDomains {
			fmt.Printf("  • %s\n", domain)
		}
	}

	fmt.Printf("\nGitHub Integration:\n")
	if cfg.Tools.WebFetch.GitHub.Enabled {
		fmt.Printf("  • Status: %s\n", ui.FormatSuccess("Enabled"))
		fmt.Printf("  • Base URL: %s\n", cfg.Tools.WebFetch.GitHub.BaseURL)
		if cfg.Tools.WebFetch.GitHub.Token != "" {
			fmt.Printf("  • Token: %s\n", ui.FormatSuccess("Configured"))
		} else {
			fmt.Printf("  • Token: %s\n", ui.FormatWarning("Not configured"))
		}
	} else {
		fmt.Printf("  • Status: %s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("\nSafety Settings:\n")
	fmt.Printf("  • Max size: %d bytes (%.1f MB)\n", cfg.Tools.WebFetch.Safety.MaxSize, float64(cfg.Tools.WebFetch.Safety.MaxSize)/(1024*1024))
	fmt.Printf("  • Timeout: %d seconds\n", cfg.Tools.WebFetch.Safety.Timeout)
	fmt.Printf("  • Allow redirects: %t\n", cfg.Tools.WebFetch.Safety.AllowRedirect)

	fmt.Printf("\nCache Settings:\n")
	if cfg.Tools.WebFetch.Cache.Enabled {
		fmt.Printf("  • Status: %s\n", ui.FormatSuccess("Enabled"))
		fmt.Printf("  • TTL: %d seconds\n", cfg.Tools.WebFetch.Cache.TTL)
		fmt.Printf("  • Max size: %d bytes (%.1f MB)\n", cfg.Tools.WebFetch.Cache.MaxSize, float64(cfg.Tools.WebFetch.Cache.MaxSize)/(1024*1024))
	} else {
		fmt.Printf("  • Status: %s\n", ui.FormatErrorCLI("Disabled"))
	}

	return nil
}

func addFetchDomain(cmd *cobra.Command, args []string) error {
	domainToAdd := args[0]

	if strings.Contains(domainToAdd, "://") {
		return fmt.Errorf("please provide just the domain (e.g., 'github.com'), not a full URL")
	}

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		for _, existingDomain := range c.Tools.WebFetch.WhitelistedDomains {
			if existingDomain == domainToAdd {
				return
			}
		}
		c.Tools.WebFetch.WhitelistedDomains = append(c.Tools.WebFetch.WhitelistedDomains, domainToAdd)
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Added '%s' to whitelisted domains", domainToAdd)))
	fmt.Printf("LLMs can now web fetch content from this domain and its subdomains\n")
	return nil
}

func removeFetchDomain(cmd *cobra.Command, args []string) error {
	domainToRemove := args[0]
	var found bool

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		for i, existingDomain := range c.Tools.WebFetch.WhitelistedDomains {
			if existingDomain == domainToRemove {
				c.Tools.WebFetch.WhitelistedDomains = append(c.Tools.WebFetch.WhitelistedDomains[:i], c.Tools.WebFetch.WhitelistedDomains[i+1:]...)
				found = true
				return
			}
		}
	})
	if err != nil {
		return err
	}

	if !found {
		fmt.Printf("%s\n", ui.FormatWarning(fmt.Sprintf("Domain '%s' was not in the whitelist", domainToRemove)))
		return nil
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Removed '%s' from whitelisted domains", domainToRemove)))
	fmt.Printf("LLMs can no longer web fetch content from this domain\n")
	return nil
}

func enableGitHubFetch(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.WebFetch.GitHub.Enabled = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("GitHub integration enabled"))
	fmt.Printf("LLMs can now use optimized GitHub fetching with 'github:owner/repo#123' syntax\n")
	fmt.Printf("Set a GitHub token with 'infer config web-fetch github set-token <token>' for higher rate limits\n")
	return nil
}

func disableGitHubFetch(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.WebFetch.GitHub.Enabled = false
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("GitHub integration disabled"))
	fmt.Printf("GitHub URLs will now be fetched using regular HTTP requests\n")
	return nil
}

func setGitHubToken(cmd *cobra.Command, args []string) error {
	token := args[0]

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.WebFetch.GitHub.Token = token
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("GitHub token configured successfully"))
	fmt.Printf("GitHub API requests will now use authentication for higher rate limits\n")
	return nil
}

func fetchCacheStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Printf("Cache Status: ")
	if cfg.Tools.WebFetch.Cache.Enabled {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("\nCache Statistics:\n")
	fmt.Printf("  • Web fetch functionality has been refactored to tools package\n")
	fmt.Printf("  • Cache statistics are currently unavailable\n")

	return nil
}

func fetchCacheClear(cmd *cobra.Command, args []string) error {
	fmt.Printf("%s\n", ui.FormatErrorCLI("Web fetch cache clear is currently unavailable"))
	fmt.Printf("Web fetch functionality has been refactored to tools package\n")
	return nil
}

func enableBashTool(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Bash.Enabled = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Bash tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("LLMs can now execute whitelisted bash commands\n")
	return nil
}

func disableBashTool(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Bash.Enabled = false
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Bash tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("LLMs can no longer execute bash commands\n")
	return nil
}

func enableWebSearchTool(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.WebSearch.Enabled = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Web search tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("LLMs can now perform web searches using %s\n", "DuckDuckGo and Google")
	return nil
}

func disableWebSearchTool(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.WebSearch.Enabled = false
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Web search tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("LLMs can no longer perform web searches\n")
	return nil
}

func enableGrepTool(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Grep.Enabled = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Grep tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("LLMs can now search file contents using grep\n")
	return nil
}

func disableGrepTool(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Grep.Enabled = false
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Grep tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("LLMs can no longer search file contents\n")
	return nil
}

func setGrepBackend(cmd *cobra.Command, args []string) error {
	backend := args[0]

	validBackends := map[string]string{
		"auto":    "auto",
		"ripgrep": "ripgrep",
		"rg":      "ripgrep",
		"go":      "go",
		"native":  "go",
	}

	normalizedBackend, valid := validBackends[strings.ToLower(backend)]
	if !valid {
		return fmt.Errorf("invalid backend '%s', must be one of: auto, ripgrep, rg, go, native", backend)
	}

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Tools.Grep.Backend = normalizedBackend
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Grep backend set to: %s", normalizedBackend)))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())

	switch normalizedBackend {
	case "ripgrep":
		fmt.Printf("Note: Ensure 'rg' is installed and available in PATH\n")
	case "auto":
		fmt.Printf("Backend will auto-detect: ripgrep if available, otherwise Go implementation\n")
	default:
		fmt.Printf("Using portable Go implementation (no external dependencies)\n")
	}

	return nil
}

func grepStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Printf("Grep Tool Status: ")
	if cfg.Tools.Grep.Enabled {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	backend := cfg.Tools.Grep.Backend
	if backend == "" {
		backend = "auto"
	}

	fmt.Printf("Configured Backend: %s\n", backend)

	fmt.Printf("\nBackend Detection:\n")
	if rgPath, err := exec.LookPath("rg"); err == nil {
		fmt.Printf("  • ripgrep (rg): %s (%s)\n", ui.FormatSuccess("Available"), rgPath)
	} else {
		fmt.Printf("  • ripgrep (rg): %s\n", ui.FormatErrorCLI("Not found"))
	}
	fmt.Printf("  • Go implementation: %s\n", ui.FormatSuccess("Available"))

	fmt.Printf("\nActive Backend: ")
	switch backend {
	case "ripgrep":
		if _, err := exec.LookPath("rg"); err == nil {
			fmt.Printf("%s\n", ui.FormatSuccess("ripgrep"))
		} else {
			fmt.Printf("%s (fallback to Go - ripgrep not found)\n", ui.FormatWarning("Go implementation"))
		}
	case "go":
		fmt.Printf("%s\n", ui.FormatSuccess("Go implementation"))
	case "auto":
		if _, err := exec.LookPath("rg"); err == nil {
			fmt.Printf("%s\n", ui.FormatSuccess("ripgrep (auto-detected)"))
		} else {
			fmt.Printf("%s\n", ui.FormatSuccess("Go implementation (auto-fallback)"))
		}
	default:
		if _, err := exec.LookPath("rg"); err == nil {
			fmt.Printf("%s\n", ui.FormatSuccess("ripgrep (auto-detected)"))
		} else {
			fmt.Printf("%s\n", ui.FormatSuccess("Go implementation (auto-fallback)"))
		}
	}

	return nil
}
