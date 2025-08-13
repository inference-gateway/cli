package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
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
	Short: "Initialize a new project configuration",
	Long: `Initialize a new .infer/config.yaml configuration file in the current directory.
This creates a local project configuration with default settings.`,
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
	Use:   "exec <command>",
	Short: "Execute a whitelisted command directly",
	Long:  `Execute a command directly if it passes whitelist validation.`,
	Args:  cobra.ExactArgs(1),
	RunE:  execTool,
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

var configFetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Manage fetch tool settings",
	Long: `Manage the fetch tool that allows LLMs to retrieve content from whitelisted URLs.
The fetch tool supports GitHub integration and URL pattern matching for secure content retrieval.`,
}

var configFetchEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable the fetch tool",
	Long:  `Enable the fetch tool to allow LLMs to retrieve content from whitelisted sources.`,
	RunE:  enableFetch,
}

var configFetchDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the fetch tool",
	Long:  `Disable the fetch tool to prevent LLMs from retrieving any external content.`,
	RunE:  disableFetch,
}

var configFetchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List whitelisted sources and patterns",
	Long:  `Display all whitelisted URLs and patterns that can be fetched by LLMs.`,
	RunE:  listFetchSources,
}

var configFetchAddSourceCmd = &cobra.Command{
	Use:   "add-source <url>",
	Short: "Add a URL to the whitelist",
	Long:  `Add a URL or URL prefix to the whitelist of allowed fetch sources.`,
	Args:  cobra.ExactArgs(1),
	RunE:  addFetchSource,
}

var configFetchRemoveSourceCmd = &cobra.Command{
	Use:   "remove-source <url>",
	Short: "Remove a URL from the whitelist",
	Long:  `Remove a URL or URL prefix from the whitelist of allowed fetch sources.`,
	Args:  cobra.ExactArgs(1),
	RunE:  removeFetchSource,
}

var configFetchAddPatternCmd = &cobra.Command{
	Use:   "add-pattern <pattern>",
	Short: "Add a URL pattern to the whitelist",
	Long:  `Add a regex pattern to match URLs that are allowed for fetching.`,
	Args:  cobra.ExactArgs(1),
	RunE:  addFetchPattern,
}

var configFetchRemovePatternCmd = &cobra.Command{
	Use:   "remove-pattern <pattern>",
	Short: "Remove a URL pattern from the whitelist",
	Long:  `Remove a regex pattern from the whitelist of allowed fetch patterns.`,
	Args:  cobra.ExactArgs(1),
	RunE:  removeFetchPattern,
}

var configFetchGitHubCmd = &cobra.Command{
	Use:   "github",
	Short: "Manage GitHub integration settings",
	Long:  `Manage GitHub-specific fetch settings including API access and optimization features.`,
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
	Short: "Manage fetch cache settings",
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
	Short: "Clear the fetch cache",
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

	configToolsSafetyCmd.AddCommand(configToolsSafetyEnableCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetyDisableCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetyStatusCmd)

	configToolsExcludePathCmd.AddCommand(configToolsExcludePathListCmd)
	configToolsExcludePathCmd.AddCommand(configToolsExcludePathAddCmd)
	configToolsExcludePathCmd.AddCommand(configToolsExcludePathRemoveCmd)

	configFetchCmd.AddCommand(configFetchEnableCmd)
	configFetchCmd.AddCommand(configFetchDisableCmd)
	configFetchCmd.AddCommand(configFetchListCmd)
	configFetchCmd.AddCommand(configFetchAddSourceCmd)
	configFetchCmd.AddCommand(configFetchRemoveSourceCmd)
	configFetchCmd.AddCommand(configFetchAddPatternCmd)
	configFetchCmd.AddCommand(configFetchRemovePatternCmd)
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
			"enabled":       cfg.Tools.Enabled,
			"commands":      cfg.Tools.Whitelist.Commands,
			"patterns":      cfg.Tools.Whitelist.Patterns,
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

	fmt.Printf("\nWhitelisted Commands (%d):\n", len(cfg.Tools.Whitelist.Commands))
	for _, cmd := range cfg.Tools.Whitelist.Commands {
		fmt.Printf("  • %s\n", cmd)
	}

	fmt.Printf("\nWhitelisted Patterns (%d):\n", len(cfg.Tools.Whitelist.Patterns))
	for _, pattern := range cfg.Tools.Whitelist.Patterns {
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
		c.Fetch.Enabled = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Fetch tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Println("You can now configure whitelisted sources with 'infer config fetch add-source <url>'")
	return nil
}

func disableFetch(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Fetch.Enabled = false
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Fetch tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	return nil
}

func listFetchSources(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	format, _ := cmd.Flags().GetString("format")

	if format == "json" {
		data := map[string]interface{}{
			"enabled":          cfg.Fetch.Enabled,
			"whitelisted_urls": cfg.Fetch.WhitelistedURLs,
			"url_patterns":     cfg.Fetch.URLPatterns,
			"github": map[string]interface{}{
				"enabled":   cfg.Fetch.GitHub.Enabled,
				"base_url":  cfg.Fetch.GitHub.BaseURL,
				"has_token": cfg.Fetch.GitHub.Token != "",
			},
			"safety": map[string]interface{}{
				"max_size":       cfg.Fetch.Safety.MaxSize,
				"timeout":        cfg.Fetch.Safety.Timeout,
				"allow_redirect": cfg.Fetch.Safety.AllowRedirect,
			},
			"cache": map[string]interface{}{
				"enabled":  cfg.Fetch.Cache.Enabled,
				"ttl":      cfg.Fetch.Cache.TTL,
				"max_size": cfg.Fetch.Cache.MaxSize,
			},
		}
		jsonOutput, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(jsonOutput))
		return nil
	}

	fmt.Printf("Fetch Tool Status: ")
	if cfg.Fetch.Enabled {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("\nWhitelisted URLs (%d):\n", len(cfg.Fetch.WhitelistedURLs))
	if len(cfg.Fetch.WhitelistedURLs) == 0 {
		fmt.Printf("  • None configured\n")
	} else {
		for _, url := range cfg.Fetch.WhitelistedURLs {
			fmt.Printf("  • %s\n", url)
		}
	}

	fmt.Printf("\nURL Patterns (%d):\n", len(cfg.Fetch.URLPatterns))
	if len(cfg.Fetch.URLPatterns) == 0 {
		fmt.Printf("  • None configured\n")
	} else {
		for _, pattern := range cfg.Fetch.URLPatterns {
			fmt.Printf("  • %s\n", pattern)
		}
	}

	fmt.Printf("\nGitHub Integration:\n")
	if cfg.Fetch.GitHub.Enabled {
		fmt.Printf("  • Status: %s\n", ui.FormatSuccess("Enabled"))
		fmt.Printf("  • Base URL: %s\n", cfg.Fetch.GitHub.BaseURL)
		if cfg.Fetch.GitHub.Token != "" {
			fmt.Printf("  • Token: %s\n", ui.FormatSuccess("Configured"))
		} else {
			fmt.Printf("  • Token: %s\n", ui.FormatWarning("Not configured"))
		}
	} else {
		fmt.Printf("  • Status: %s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("\nSafety Settings:\n")
	fmt.Printf("  • Max size: %d bytes (%.1f MB)\n", cfg.Fetch.Safety.MaxSize, float64(cfg.Fetch.Safety.MaxSize)/(1024*1024))
	fmt.Printf("  • Timeout: %d seconds\n", cfg.Fetch.Safety.Timeout)
	fmt.Printf("  • Allow redirects: %t\n", cfg.Fetch.Safety.AllowRedirect)

	fmt.Printf("\nCache Settings:\n")
	if cfg.Fetch.Cache.Enabled {
		fmt.Printf("  • Status: %s\n", ui.FormatSuccess("Enabled"))
		fmt.Printf("  • TTL: %d seconds\n", cfg.Fetch.Cache.TTL)
		fmt.Printf("  • Max size: %d bytes (%.1f MB)\n", cfg.Fetch.Cache.MaxSize, float64(cfg.Fetch.Cache.MaxSize)/(1024*1024))
	} else {
		fmt.Printf("  • Status: %s\n", ui.FormatErrorCLI("Disabled"))
	}

	return nil
}

func addFetchSource(cmd *cobra.Command, args []string) error {
	urlToAdd := args[0]

	if _, err := url.Parse(urlToAdd); err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		for _, existingURL := range c.Fetch.WhitelistedURLs {
			if existingURL == urlToAdd {
				return
			}
		}
		c.Fetch.WhitelistedURLs = append(c.Fetch.WhitelistedURLs, urlToAdd)
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Added '%s' to whitelisted URLs", urlToAdd)))
	fmt.Printf("LLMs can now fetch content from this URL\n")
	return nil
}

func removeFetchSource(cmd *cobra.Command, args []string) error {
	urlToRemove := args[0]
	var found bool

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		for i, existingURL := range c.Fetch.WhitelistedURLs {
			if existingURL == urlToRemove {
				c.Fetch.WhitelistedURLs = append(c.Fetch.WhitelistedURLs[:i], c.Fetch.WhitelistedURLs[i+1:]...)
				found = true
				return
			}
		}
	})
	if err != nil {
		return err
	}

	if !found {
		fmt.Printf("%s\n", ui.FormatWarning(fmt.Sprintf("URL '%s' was not in the whitelist", urlToRemove)))
		return nil
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Removed '%s' from whitelisted URLs", urlToRemove)))
	fmt.Printf("LLMs can no longer fetch content from this URL\n")
	return nil
}

func addFetchPattern(cmd *cobra.Command, args []string) error {
	patternToAdd := args[0]

	if _, err := regexp.Compile(patternToAdd); err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		for _, existingPattern := range c.Fetch.URLPatterns {
			if existingPattern == patternToAdd {
				return
			}
		}
		c.Fetch.URLPatterns = append(c.Fetch.URLPatterns, patternToAdd)
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Added pattern '%s' to URL patterns", patternToAdd)))
	fmt.Printf("LLMs can now fetch content from URLs matching this pattern\n")
	return nil
}

func removeFetchPattern(cmd *cobra.Command, args []string) error {
	patternToRemove := args[0]
	var found bool

	_, err := loadAndUpdateConfig(func(c *config.Config) {
		for i, existingPattern := range c.Fetch.URLPatterns {
			if existingPattern == patternToRemove {
				c.Fetch.URLPatterns = append(c.Fetch.URLPatterns[:i], c.Fetch.URLPatterns[i+1:]...)
				found = true
				return
			}
		}
	})
	if err != nil {
		return err
	}

	if !found {
		fmt.Printf("%s\n", ui.FormatWarning(fmt.Sprintf("Pattern '%s' was not in the URL patterns", patternToRemove)))
		return nil
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Removed pattern '%s' from URL patterns", patternToRemove)))
	fmt.Printf("LLMs can no longer fetch content from URLs matching this pattern\n")
	return nil
}

func enableGitHubFetch(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Fetch.GitHub.Enabled = true
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("GitHub integration enabled"))
	fmt.Printf("LLMs can now use optimized GitHub fetching with 'github:owner/repo#123' syntax\n")
	fmt.Printf("Set a GitHub token with 'infer config fetch github set-token <token>' for higher rate limits\n")
	return nil
}

func disableGitHubFetch(cmd *cobra.Command, args []string) error {
	_, err := loadAndUpdateConfig(func(c *config.Config) {
		c.Fetch.GitHub.Enabled = false
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
		c.Fetch.GitHub.Token = token
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

	services := container.NewServiceContainer(cfg)
	fetchService := services.GetFetchService()
	stats := fetchService.GetCacheStats()

	fmt.Printf("Cache Status: ")
	if cfg.Fetch.Cache.Enabled {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("\nCache Statistics:\n")
	fmt.Printf("  • Entries: %d\n", stats["entries"])
	fmt.Printf("  • Total size: %d bytes (%.1f KB)\n", stats["total_size"], float64(stats["total_size"].(int64))/1024)
	fmt.Printf("  • Max size: %d bytes (%.1f MB)\n", stats["max_size"], float64(stats["max_size"].(int64))/(1024*1024))
	fmt.Printf("  • TTL: %d seconds\n", stats["ttl"])

	return nil
}

func fetchCacheClear(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	services := container.NewServiceContainer(cfg)
	fetchService := services.GetFetchService()
	fetchService.ClearCache()

	fmt.Printf("%s\n", ui.FormatSuccess("Fetch cache cleared successfully"))
	fmt.Printf("All cached content has been removed\n")
	return nil
}
