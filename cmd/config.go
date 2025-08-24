package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	services "github.com/inference-gateway/cli/internal/services"
	ui "github.com/inference-gateway/cli/internal/ui"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	cobra "github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  `Manage the Inference Gateway CLI configuration settings.`,
}

var configAgentSetModelCmd = &cobra.Command{
	Use:   "set-model [MODEL_NAME]",
	Short: "Set the default model for agent sessions",
	Long: `Set the default model for agent sessions. When a default model is configured,
the agent command will use this model directly without requiring selection.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modelName := args[0]
		return setDefaultModel(cmd, modelName)
	},
}

var configAgentSetSystemCmd = &cobra.Command{
	Use:   "set-system [SYSTEM_PROMPT]",
	Short: "Set the system prompt for agent sessions",
	Long: `Set the system prompt that will be included with every agent session.
The system prompt provides context and instructions to the AI model about how to behave.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		systemPrompt := args[0]
		return setSystemPrompt(cmd, systemPrompt)
	},
}

var configAgentSetMaxTurnsCmd = &cobra.Command{
	Use:   "set-max-turns [NUMBER]",
	Short: "Set the maximum number of turns for agent sessions",
	Long: `Set the maximum number of conversation turns for agent sessions.
This limits how long an agent can run to prevent infinite loops or excessive token usage.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		maxTurns := args[0]
		return setMaxTurns(cmd, maxTurns)
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new configuration file",
	Long: `Initialize a new .infer/config.yaml configuration file in the current directory.
This creates only the configuration file with default settings.

For complete project initialization, use 'infer init' instead.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		userspace := GetUserspaceFlag(cmd)
		overwrite, _ := cmd.Flags().GetBool("overwrite")

		var configPath string
		if userspace {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get user home directory: %w", err)
			}
			configPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
		} else {
			configPath = config.DefaultConfigPath
		}

		if _, err := os.Stat(configPath); err == nil {
			if !overwrite {
				return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
			}
		}

		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}

		if err := writeConfigAsYAMLWithIndent(configPath, 2); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}

		var scopeDesc string
		if userspace {
			scopeDesc = "userspace "
		}

		fmt.Printf("Successfully created %sconfiguration: %s\n", scopeDesc, configPath)
		if userspace {
			fmt.Println("This userspace configuration will be used as a fallback for all projects.")
			fmt.Println("Project-level configurations will take precedence when present.")
		} else {
			fmt.Println("You can now customize the configuration for this project.")
		}
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
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := getConfigFromViper()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return ValidateTool(cfg, args[0])
	},
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
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := getConfigFromViper()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		format, _ := cmd.Flags().GetString("format")
		return ExecTool(cfg, args, format)
	},
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

var configToolsSandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Manage sandbox directories",
	Long:  `Manage the sandbox directories where tools are allowed to operate. Tools can only access files within these directories.`,
}

var configToolsSandboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sandbox directories",
	Long:  `Display all directories where tools are allowed to operate.`,
	RunE:  listSandboxDirectories,
}

var configToolsSandboxAddCmd = &cobra.Command{
	Use:   "add <directory>",
	Short: "Add a directory to the sandbox",
	Long:  `Add a directory to the sandbox to allow tools to access it.`,
	Args:  cobra.ExactArgs(1),
	RunE:  addSandboxDirectory,
}

var configToolsSandboxRemoveCmd = &cobra.Command{
	Use:   "remove <directory>",
	Short: "Remove a directory from the sandbox",
	Long:  `Remove a directory from the sandbox to prevent tools from accessing it.`,
	Args:  cobra.ExactArgs(1),
	RunE:  removeSandboxDirectory,
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

var configToolsGithubCmd = &cobra.Command{
	Use:   "github",
	Short: "Manage GitHub tool settings",
	Long:  `Manage GitHub-specific tool execution settings including authentication and repository configuration.`,
}

var configToolsGithubEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable GitHub tool",
	Long:  `Enable the GitHub tool for repository operations.`,
	RunE:  enableGithubTool,
}

var configToolsGithubDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable GitHub tool",
	Long:  `Disable the GitHub tool to prevent repository operations.`,
	RunE:  disableGithubTool,
}

var configToolsGithubStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show GitHub tool status and configuration",
	Long:  `Display current GitHub tool status, authentication, and repository settings.`,
	RunE:  githubStatus,
}

var configToolsGithubSetTokenCmd = &cobra.Command{
	Use:   "set-token <token>",
	Short: "Set GitHub authentication token",
	Long:  `Set the GitHub personal access token for API authentication.`,
	Args:  cobra.ExactArgs(1),
	RunE:  setGithubToken,
}

var configToolsGithubSetOwnerCmd = &cobra.Command{
	Use:   "set-owner <owner>",
	Short: "Set default GitHub repository owner",
	Long:  `Set the default GitHub repository owner/organization name.`,
	Args:  cobra.ExactArgs(1),
	RunE:  setGithubOwner,
}

var configToolsGithubSetRepoCmd = &cobra.Command{
	Use:   "set-repo <repo>",
	Short: "Set default GitHub repository name",
	Long:  `Set the default GitHub repository name (optional, can be overridden per operation).`,
	Args:  cobra.ExactArgs(1),
	RunE:  setGithubRepo,
}

var configToolsWebFetchCmd = &cobra.Command{
	Use:   "web-fetch",
	Short: "Manage web fetch tool settings",
	Long: `Manage the web fetch tool that allows LLMs to retrieve content from whitelisted URLs.
The web fetch tool supports GitHub integration and URL pattern matching for secure content retrieval.`,
}

var configToolsWebFetchEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable the web fetch tool",
	Long:  `Enable the web fetch tool to allow LLMs to retrieve content from whitelisted sources.`,
	RunE:  enableFetch,
}

var configToolsWebFetchDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the web fetch tool",
	Long:  `Disable the web fetch tool to prevent LLMs from retrieving any external content.`,
	RunE:  disableFetch,
}

var configToolsWebFetchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List whitelisted domains",
	Long:  `Display all whitelisted domains that can be fetched by LLMs.`,
	RunE:  listFetchDomains,
}

var configToolsWebFetchAddDomainCmd = &cobra.Command{
	Use:   "add-domain <domain>",
	Short: "Add a domain to the whitelist",
	Long:  `Add a domain to the whitelist of allowed web fetch sources (e.g., github.com, example.org).`,
	Args:  cobra.ExactArgs(1),
	RunE:  addFetchDomain,
}

var configToolsWebFetchRemoveDomainCmd = &cobra.Command{
	Use:   "remove-domain <domain>",
	Short: "Remove a domain from the whitelist",
	Long:  `Remove a domain from the whitelist of allowed web fetch sources.`,
	Args:  cobra.ExactArgs(1),
	RunE:  removeFetchDomain,
}

var configToolsWebFetchCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage web fetch cache settings",
	Long:  `Manage caching settings for fetched content to improve performance.`,
}

var configToolsWebFetchCacheStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show cache status and statistics",
	Long:  `Display current cache status, statistics, and configuration.`,
	RunE:  fetchCacheStatus,
}

var configToolsWebFetchCacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the web fetch cache",
	Long:  `Clear all cached content to free up memory and force fresh fetches.`,
	RunE:  fetchCacheClear,
}

// getConfigFromViper creates a config object from current Viper settings
func getConfigFromViper() (*config.Config, error) {
	cfg := &config.Config{}
	if err := V.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from Viper: %w", err)
	}
	return cfg, nil
}

// GetUserspaceFlag checks for --userspace flag on the current command or parent commands
func GetUserspaceFlag(cmd *cobra.Command) bool {
	if userspace, err := cmd.Flags().GetBool("userspace"); err == nil && userspace {
		return true
	}

	parent := cmd.Parent()
	for parent != nil {
		if userspace, err := parent.Flags().GetBool("userspace"); err == nil && userspace {
			return true
		}
		parent = parent.Parent()
	}

	return false
}

func setDefaultModel(_ *cobra.Command, modelName string) error {
	V.Set("agent.model", modelName)
	if err := V.WriteConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("%s Default model set to: %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), modelName)
	fmt.Printf("Configuration saved to %s\n", V.ConfigFileUsed())
	fmt.Println("The agent command will now use this model by default.")
	return nil
}

func setSystemPrompt(_ *cobra.Command, systemPrompt string) error {
	V.Set("agent.system_prompt", systemPrompt)
	if err := V.WriteConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("%s System prompt set successfully\n", icons.CheckMarkStyle.Render(icons.CheckMark))
	fmt.Printf("Configuration saved to %s\n", V.ConfigFileUsed())
	fmt.Printf("System prompt: %s\n", systemPrompt)
	fmt.Println("This prompt will be included with every agent session.")
	return nil
}

func setMaxTurns(_ *cobra.Command, maxTurnsStr string) error {
	maxTurns, err := strconv.Atoi(maxTurnsStr)
	if err != nil {
		return fmt.Errorf("invalid max turns value '%s': must be a positive integer", maxTurnsStr)
	}

	if maxTurns < 1 {
		return fmt.Errorf("max turns must be at least 1, got %d", maxTurns)
	}

	V.Set("agent.max_turns", maxTurns)
	if err := V.WriteConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("%s Maximum turns set to: %d\n", icons.CheckMarkStyle.Render(icons.CheckMark), maxTurns)
	fmt.Printf("Configuration saved to %s\n", V.ConfigFileUsed())
	fmt.Println("Agent sessions will now be limited to this number of conversation turns.")
	return nil
}

var configAgentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Configure agent command settings",
	Long:  "Configure settings specific to the agent command",
}

var configAgentVerboseToolsCmd = &cobra.Command{
	Use:   "verbose-tools [enable|disable]",
	Short: "Enable or disable verbose tool output in agent logs",
	Long:  "Control whether the agent command shows full tool details or just tool names in the output",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var statusMsg string
		switch args[0] {
		case "enable":
			V.Set("agent.verbose_tools", true)
			statusMsg = "Verbose tools output enabled for agent command"
		case "disable":
			V.Set("agent.verbose_tools", false)
			statusMsg = "Verbose tools output disabled for agent command (will show tool names only)"
		default:
			return fmt.Errorf("invalid argument: %s. Use 'enable' or 'disable'", args[0])
		}

		if err := V.WriteConfig(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("%s %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), statusMsg)
		fmt.Printf("Configuration saved to %s\n", V.ConfigFileUsed())

		return nil
	},
}

func init() {
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configToolsCmd)
	configCmd.AddCommand(configOptimizationCmd)
	configCmd.AddCommand(configCompactCmd)
	configCmd.AddCommand(configAgentCmd)

	configToolsCmd.AddCommand(configToolsEnableCmd)
	configToolsCmd.AddCommand(configToolsDisableCmd)
	configToolsCmd.AddCommand(configToolsListCmd)
	configToolsCmd.AddCommand(configToolsValidateCmd)
	configToolsCmd.AddCommand(configToolsExecCmd)
	configToolsCmd.AddCommand(configToolsSafetyCmd)
	configToolsCmd.AddCommand(configToolsSandboxCmd)
	configToolsCmd.AddCommand(configToolsBashCmd)
	configToolsCmd.AddCommand(configToolsGrepCmd)
	configToolsCmd.AddCommand(configToolsGithubCmd)
	configToolsCmd.AddCommand(configToolsWebSearchCmd)
	configToolsCmd.AddCommand(configToolsWebFetchCmd)

	configToolsSafetyCmd.AddCommand(configToolsSafetyEnableCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetyDisableCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetyStatusCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetySetCmd)
	configToolsSafetyCmd.AddCommand(configToolsSafetyUnsetCmd)

	configToolsSandboxCmd.AddCommand(configToolsSandboxListCmd)
	configToolsSandboxCmd.AddCommand(configToolsSandboxAddCmd)
	configToolsSandboxCmd.AddCommand(configToolsSandboxRemoveCmd)

	configToolsBashCmd.AddCommand(configToolsBashEnableCmd)
	configToolsBashCmd.AddCommand(configToolsBashDisableCmd)

	configToolsWebSearchCmd.AddCommand(configToolsWebSearchEnableCmd)
	configToolsWebSearchCmd.AddCommand(configToolsWebSearchDisableCmd)

	configToolsGrepCmd.AddCommand(configToolsGrepEnableCmd)
	configToolsGrepCmd.AddCommand(configToolsGrepDisableCmd)
	configToolsGrepCmd.AddCommand(configToolsGrepBackendCmd)
	configToolsGrepCmd.AddCommand(configToolsGrepStatusCmd)

	configToolsGithubCmd.AddCommand(configToolsGithubEnableCmd)
	configToolsGithubCmd.AddCommand(configToolsGithubDisableCmd)
	configToolsGithubCmd.AddCommand(configToolsGithubStatusCmd)
	configToolsGithubCmd.AddCommand(configToolsGithubSetTokenCmd)
	configToolsGithubCmd.AddCommand(configToolsGithubSetOwnerCmd)
	configToolsGithubCmd.AddCommand(configToolsGithubSetRepoCmd)

	configToolsWebFetchCmd.AddCommand(configToolsWebFetchEnableCmd)
	configToolsWebFetchCmd.AddCommand(configToolsWebFetchDisableCmd)
	configToolsWebFetchCmd.AddCommand(configToolsWebFetchListCmd)
	configToolsWebFetchCmd.AddCommand(configToolsWebFetchAddDomainCmd)
	configToolsWebFetchCmd.AddCommand(configToolsWebFetchRemoveDomainCmd)
	configToolsWebFetchCmd.AddCommand(configToolsWebFetchCacheCmd)

	configToolsWebFetchCacheCmd.AddCommand(configToolsWebFetchCacheStatusCmd)
	configToolsWebFetchCacheCmd.AddCommand(configToolsWebFetchCacheClearCmd)

	configInitCmd.Flags().Bool("overwrite", false, "Overwrite existing configuration file")
	configToolsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	configToolsExecCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	configToolsWebFetchListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

	configAgentCmd.AddCommand(configAgentSetModelCmd)
	configAgentCmd.AddCommand(configAgentSetSystemCmd)
	configAgentCmd.AddCommand(configAgentSetMaxTurnsCmd)
	configAgentCmd.AddCommand(configAgentVerboseToolsCmd)

	configCmd.PersistentFlags().Bool("userspace", false, "Apply to userspace configuration (~/.infer/) instead of project configuration")

	rootCmd.AddCommand(configCmd)
}

func enableTools(cmd *cobra.Command, args []string) error {
	V.Set("tools.enabled", true)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Tools enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	return nil
}

func disableTools(cmd *cobra.Command, args []string) error {
	V.Set("tools.enabled", false)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Tools disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	return nil
}

func listTools(cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("format")

	if format == "json" {
		data := map[string]any{
			"enabled": V.GetBool("tools.enabled"),
			"bash": map[string]bool{
				"enabled": V.GetBool("tools.bash.enabled"),
			},
			"web_fetch": map[string]any{
				"enabled":             V.GetBool("tools.web_fetch.enabled"),
				"whitelisted_domains": V.GetStringSlice("tools.web_fetch.whitelisted_domains"),
			},
			"web_search": map[string]any{
				"enabled":        V.GetBool("tools.web_search.enabled"),
				"default_engine": V.GetString("tools.web_search.default_engine"),
				"max_results":    V.GetInt("tools.web_search.max_results"),
				"engines":        V.GetStringSlice("tools.web_search.engines"),
				"timeout":        V.GetInt("tools.web_search.timeout"),
			},
			"commands": V.GetStringSlice("tools.bash.whitelist.commands"),
			"patterns": V.GetStringSlice("tools.bash.whitelist.patterns"),
			"sandbox": map[string]any{
				"directories": V.GetStringSlice("tools.sandbox.directories"),
			},
			"safety": map[string]bool{
				"require_approval": V.GetBool("tools.safety.require_approval"),
			},
		}
		jsonOutput, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(jsonOutput))
		return nil
	}

	fmt.Printf("TOOLS STATUS\n")
	fmt.Printf("────────────\n")
	if V.GetBool("tools.enabled") {
		fmt.Printf("Overall: %s\n", ui.FormatEnabled())
	} else {
		fmt.Printf("Overall: %s\n", ui.FormatDisabled())
	}

	fmt.Printf("\nINDIVIDUAL TOOLS\n")
	fmt.Printf("────────────────\n")

	fmt.Printf("File Operations:\n")
	fmt.Printf("  Read      : %s\n", formatToolStatus(V.GetBool("tools.read.enabled")))
	fmt.Printf("  Write     : %s\n", formatToolStatus(V.GetBool("tools.write.enabled")))
	fmt.Printf("  Edit      : %s\n", formatToolStatus(V.GetBool("tools.edit.enabled")))
	fmt.Printf("  Delete    : %s\n", formatToolStatus(V.GetBool("tools.delete.enabled")))

	fmt.Printf("\nSearch & Analysis:\n")
	fmt.Printf("  Grep      : %s\n", formatToolStatus(V.GetBool("tools.grep.enabled")))
	fmt.Printf("  Tree      : %s\n", formatToolStatus(V.GetBool("tools.tree.enabled")))

	fmt.Printf("\nSystem Operations:\n")
	fmt.Printf("  Bash      : %s\n", formatToolStatus(V.GetBool("tools.bash.enabled")))

	fmt.Printf("\nWeb Operations:\n")
	fmt.Printf("  WebFetch  : %s\n", formatToolStatus(V.GetBool("tools.web_fetch.enabled")))
	fmt.Printf("  WebSearch : %s\n", formatToolStatus(V.GetBool("tools.web_search.enabled")))

	fmt.Printf("\nExternal Services:\n")
	fmt.Printf("  GitHub    : %s\n", formatToolStatus(V.GetBool("tools.github.enabled")))

	fmt.Printf("\nTask Management:\n")
	fmt.Printf("  TodoWrite : %s\n", formatToolStatus(V.GetBool("tools.todo_write.enabled")))

	fmt.Printf("\nBASH CONFIGURATION\n")
	fmt.Printf("──────────────────\n")
	commands := V.GetStringSlice("tools.bash.whitelist.commands")
	fmt.Printf("Whitelisted Commands (%d):\n", len(commands))
	if len(commands) == 0 {
		fmt.Printf("  None configured\n")
	} else {
		for _, cmd := range commands {
			fmt.Printf("  %s\n", cmd)
		}
	}

	patterns := V.GetStringSlice("tools.bash.whitelist.patterns")
	fmt.Printf("\nWhitelisted Patterns (%d):\n", len(patterns))
	if len(patterns) == 0 {
		fmt.Printf("  None configured\n")
	} else {
		for _, pattern := range patterns {
			fmt.Printf("  %s\n", pattern)
		}
	}

	fmt.Printf("\nSANDBOX CONFIGURATION\n")
	fmt.Printf("─────────────────────\n")
	directories := V.GetStringSlice("tools.sandbox.directories")
	fmt.Printf("Directories (%d):\n", len(directories))
	if len(directories) == 0 {
		fmt.Printf("  None configured\n")
	} else {
		for _, dir := range directories {
			fmt.Printf("  %s\n", dir)
		}
	}

	fmt.Printf("\nSAFETY CONFIGURATION\n")
	fmt.Printf("────────────────────\n")
	fmt.Printf("Approval Required: %s\n", formatToolStatus(V.GetBool("tools.safety.require_approval")))

	return nil
}

// ValidateTool validates if a command is whitelisted for execution
func ValidateTool(cfg *config.Config, command string) error {
	if !cfg.Tools.Enabled {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Tools are disabled"))
		return nil
	}

	services := container.NewServiceContainer(cfg)
	toolService := services.GetToolService()
	toolArgs := map[string]any{
		"command": command,
	}

	err := toolService.ValidateTool("Bash", toolArgs)
	if err != nil {
		fmt.Printf("%s\n", ui.FormatErrorCLI(fmt.Sprintf("Command not allowed: %s", command)))
		fmt.Printf("Reason: %s\n", err.Error())
		return nil
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Command is whitelisted: %s", command)))
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

	toolName := args[0]
	toolArgs := make(map[string]any)

	if len(args) > 1 {
		if err := json.Unmarshal([]byte(args[1]), &toolArgs); err != nil {
			return fmt.Errorf("arguments must be provided as valid JSON. Example: infer config tools exec %s '{\"param\":\"value\"}'. Error: %w", toolName, err)
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

	result, err := toolService.ExecuteTool(ctx, toolName, toolArgs)
	if err != nil {
		return fmt.Errorf("tool execution failed: %w", err)
	}

	formatterService := services.NewToolFormatterService(toolRegistry)

	fmt.Print(formatterService.FormatToolResultExpanded(result, 80))
	return nil
}

func enableSafety(cmd *cobra.Command, args []string) error {
	V.Set("tools.safety.require_approval", true)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Safety approval enabled"))
	fmt.Printf("Commands will require approval before execution\n")
	return nil
}

func disableSafety(cmd *cobra.Command, args []string) error {
	V.Set("tools.safety.require_approval", false)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatWarning("Safety approval disabled"))
	fmt.Printf("Commands will execute immediately without approval\n")
	return nil
}

func safetyStatus(cmd *cobra.Command, args []string) error {
	globalApproval := V.GetBool("tools.safety.require_approval")

	fmt.Printf("Safety Approval Status: ")
	if globalApproval {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
		fmt.Printf("Commands require approval before execution\n")
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
		fmt.Printf("Commands execute immediately without approval\n")
	}

	fmt.Printf("\nTool-specific Approval Settings:\n")
	tools := []struct {
		name string
		key  string
	}{
		{"Bash", "tools.bash.require_approval"},
		{"Read", "tools.read.require_approval"},
		{"Grep", "tools.grep.require_approval"},
		{"WebFetch", "tools.web_fetch.require_approval"},
		{"WebSearch", "tools.web_search.require_approval"},
	}

	for _, tool := range tools {
		fmt.Printf("  %s: ", tool.name)
		if !V.IsSet(tool.key) {
			status := "disabled"
			if globalApproval {
				status = "enabled"
			}
			fmt.Printf("using global setting (%s)\n", status)
			continue
		}

		if V.GetBool(tool.key) {
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

	var err error
	switch toolLower {
	case "bash":
		V.Set("tools.bash.require_approval", enabled)
	case "read":
		V.Set("tools.read.require_approval", enabled)
	case "grep":
		V.Set("tools.grep.require_approval", enabled)
	case "webfetch":
		V.Set("tools.web_fetch.require_approval", enabled)
	case "websearch":
		V.Set("tools.web_search.require_approval", enabled)
	}
	err = V.WriteConfig()
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

	var err error
	switch toolLower {
	case "bash":
		V.Set("tools.bash.require_approval", nil)
	case "read":
		V.Set("tools.read.require_approval", nil)
	case "grep":
		V.Set("tools.grep.require_approval", nil)
	case "webfetch":
		V.Set("tools.web_fetch.require_approval", nil)
	case "websearch":
		V.Set("tools.web_search.require_approval", nil)
	}
	err = V.WriteConfig()
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Tool-specific approval setting removed for %s (using global setting)", toolName)))
	return nil
}

func getConfigPath() string {
	configPath := config.DefaultConfigPath
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = ".infer.yaml"
	}
	return configPath
}

func listSandboxDirectories(cmd *cobra.Command, args []string) error {
	cfg, err := getConfigFromViper()
	if err != nil {
		return err
	}

	if len(cfg.Tools.Sandbox.Directories) == 0 {
		fmt.Println("No sandbox directories are currently configured.")
		return nil
	}

	fmt.Printf("Sandbox Directories (%d):\n", len(cfg.Tools.Sandbox.Directories))
	for _, dir := range cfg.Tools.Sandbox.Directories {
		fmt.Printf("  • %s\n", dir)
	}

	return nil
}

func addSandboxDirectory(cmd *cobra.Command, args []string) error {
	dirToAdd := args[0]

	// Get existing directories and check for duplicates
	existingDirs := V.GetStringSlice("tools.sandbox.directories")
	for _, existingDir := range existingDirs {
		if existingDir == dirToAdd {
			return nil // Already exists, no error
		}
	}

	// Add the new directory
	updatedDirs := append(existingDirs, dirToAdd)
	V.Set("tools.sandbox.directories", updatedDirs)
	err := V.WriteConfig()
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Added '%s' to sandbox directories", dirToAdd)))
	fmt.Printf("Tools can now access files within this directory\n")
	return nil
}

func removeSandboxDirectory(cmd *cobra.Command, args []string) error {
	dirToRemove := args[0]
	var found bool

	existingDirs := V.GetStringSlice("tools.sandbox.directories")
	for i, existingDir := range existingDirs {
		if existingDir == dirToRemove {
			updatedDirs := append(existingDirs[:i], existingDirs[i+1:]...)
			V.Set("tools.sandbox.directories", updatedDirs)
			if err := V.WriteConfig(); err != nil {
				return err
			}
			found = true
			break
		}
	}

	if !found {
		fmt.Printf("%s\n", ui.FormatWarning(fmt.Sprintf("Directory '%s' was not in the sandbox directories list", dirToRemove)))
		return nil
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Removed '%s' from sandbox directories", dirToRemove)))
	fmt.Printf("Tools can no longer access this directory\n")
	return nil
}

func enableFetch(cmd *cobra.Command, args []string) error {
	V.Set("tools.web_fetch.enabled", true)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Web fetch tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	fmt.Println("You can now configure whitelisted sources with 'infer config tools web-fetch add-domain <domain>'")
	return nil
}

func disableFetch(cmd *cobra.Command, args []string) error {
	V.Set("tools.web_fetch.enabled", false)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Web fetch tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	return nil
}

func listFetchDomains(cmd *cobra.Command, args []string) error {
	cfg, err := getConfigFromViper()
	if err != nil {
		return err
	}

	format, _ := cmd.Flags().GetString("format")

	if format == "json" {
		data := map[string]any{
			"enabled":             cfg.Tools.WebFetch.Enabled,
			"whitelisted_domains": cfg.Tools.WebFetch.WhitelistedDomains,
			"safety": map[string]any{
				"max_size":       cfg.Tools.WebFetch.Safety.MaxSize,
				"timeout":        cfg.Tools.WebFetch.Safety.Timeout,
				"allow_redirect": cfg.Tools.WebFetch.Safety.AllowRedirect,
			},
			"cache": map[string]any{
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

	// Get existing domains and check for duplicates
	existingDomains := V.GetStringSlice("tools.web_fetch.whitelisted_domains")
	for _, existingDomain := range existingDomains {
		if existingDomain == domainToAdd {
			return nil // Already exists, no error
		}
	}

	// Add the new domain
	updatedDomains := append(existingDomains, domainToAdd)
	V.Set("tools.web_fetch.whitelisted_domains", updatedDomains)
	err := V.WriteConfig()
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

	// Get existing domains and find the one to remove
	existingDomains := V.GetStringSlice("tools.web_fetch.whitelisted_domains")
	for i, existingDomain := range existingDomains {
		if existingDomain == domainToRemove {
			// Remove the domain from the slice
			updatedDomains := append(existingDomains[:i], existingDomains[i+1:]...)
			V.Set("tools.web_fetch.whitelisted_domains", updatedDomains)
			if err := V.WriteConfig(); err != nil {
				return err
			}
			found = true
			break
		}
	}

	if !found {
		fmt.Printf("%s\n", ui.FormatWarning(fmt.Sprintf("Domain '%s' was not in the whitelist", domainToRemove)))
		return nil
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("Removed '%s' from whitelisted domains", domainToRemove)))
	fmt.Printf("LLMs can no longer web fetch content from this domain\n")
	return nil
}

func fetchCacheStatus(cmd *cobra.Command, args []string) error {
	cfg, err := getConfigFromViper()
	if err != nil {
		return err
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
	V.Set("tools.bash.enabled", true)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Bash tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	fmt.Printf("LLMs can now execute whitelisted bash commands\n")
	return nil
}

func disableBashTool(cmd *cobra.Command, args []string) error {
	V.Set("tools.bash.enabled", false)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Bash tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	fmt.Printf("LLMs can no longer execute bash commands\n")
	return nil
}

func enableWebSearchTool(cmd *cobra.Command, args []string) error {
	V.Set("tools.web_search.enabled", true)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Web search tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	fmt.Printf("LLMs can now perform web searches using %s\n", "DuckDuckGo and Google")
	return nil
}

func disableWebSearchTool(cmd *cobra.Command, args []string) error {
	V.Set("tools.web_search.enabled", false)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Web search tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	fmt.Printf("LLMs can no longer perform web searches\n")
	return nil
}

func enableGrepTool(cmd *cobra.Command, args []string) error {
	V.Set("tools.grep.enabled", true)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("Grep tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	fmt.Printf("LLMs can now search file contents using grep\n")
	return nil
}

func disableGrepTool(cmd *cobra.Command, args []string) error {
	V.Set("tools.grep.enabled", false)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("Grep tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
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

	V.Set("tools.grep.backend", normalizedBackend)
	err := V.WriteConfig()
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
	cfg, err := getConfigFromViper()
	if err != nil {
		return err
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

// formatToolStatus formats a tool's enabled/disabled status
func enableGithubTool(cmd *cobra.Command, args []string) error {
	V.Set("tools.github.enabled", true)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("GitHub tool enabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	fmt.Printf("LLMs can now perform GitHub operations\n")
	return nil
}

func disableGithubTool(cmd *cobra.Command, args []string) error {
	V.Set("tools.github.enabled", false)
	if err := V.WriteConfig(); err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatErrorCLI("GitHub tool disabled successfully"))
	fmt.Printf("Configuration saved to: %s\n", V.ConfigFileUsed())
	fmt.Printf("LLMs can no longer perform GitHub operations\n")
	return nil
}

func githubStatus(cmd *cobra.Command, args []string) error {
	fmt.Printf("GitHub Tool Status: ")
	if V.GetBool("tools.github.enabled") {
		fmt.Printf("%s\n", ui.FormatSuccess("Enabled"))
	} else {
		fmt.Printf("%s\n", ui.FormatErrorCLI("Disabled"))
	}

	fmt.Printf("\nConfiguration:\n")
	fmt.Printf("  • Base URL: %s\n", V.GetString("tools.github.base_url"))
	fmt.Printf("  • Owner: ")
	if owner := V.GetString("tools.github.owner"); owner != "" {
		fmt.Printf("%s\n", owner)
	} else {
		fmt.Printf("(not set)\n")
	}

	fmt.Printf("  • Repository: ")
	if repo := V.GetString("tools.github.repo"); repo != "" {
		fmt.Printf("%s\n", repo)
	} else {
		fmt.Printf("(not set)\n")
	}

	fmt.Printf("  • Token: ")
	token := V.GetString("tools.github.token")
	resolvedToken := config.ResolveEnvironmentVariables(token)
	if resolvedToken != "" && resolvedToken != "%GITHUB_TOKEN%" {
		fmt.Printf("%s configured\n", icons.CheckMarkStyle.Render(icons.CheckMark))
	} else if token == "%GITHUB_TOKEN%" {
		fmt.Printf("%s environment variable GITHUB_TOKEN not set\n", icons.CrossMarkStyle.Render(icons.CrossMark))
	} else {
		fmt.Printf("%s not configured\n", icons.CrossMarkStyle.Render(icons.CrossMark))
	}

	fmt.Printf("\nSafety Settings:\n")
	maxSize := V.GetInt("tools.github.safety.max_size")
	fmt.Printf("  • Max size: %d bytes (%.1f MB)\n", maxSize, float64(maxSize)/(1024*1024))
	fmt.Printf("  • Timeout: %d seconds\n", V.GetInt("tools.github.safety.timeout"))

	fmt.Printf("  • Require approval: ")
	if V.IsSet("tools.github.require_approval") {
		status := "disabled"
		color := ui.FormatErrorCLI
		if V.GetBool("tools.github.require_approval") {
			status = "enabled"
			color = ui.FormatSuccess
		}
		fmt.Printf("%s\n", color(status))
	} else {
		status := "disabled"
		if V.GetBool("tools.safety.require_approval") {
			status = "enabled"
		}
		fmt.Printf("using global setting (%s)\n", status)
	}

	return nil
}

func setGithubToken(cmd *cobra.Command, args []string) error {
	token := args[0]

	V.Set("tools.github.token", token)
	err := V.WriteConfig()
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess("GitHub token set successfully"))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("Note: For security, consider using environment variables (%%GITHUB_TOKEN%%)\n")
	return nil
}

func setGithubOwner(cmd *cobra.Command, args []string) error {
	owner := args[0]

	V.Set("tools.github.owner", owner)
	err := V.WriteConfig()
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("GitHub default owner set to: %s", owner)))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	return nil
}

func setGithubRepo(cmd *cobra.Command, args []string) error {
	repo := args[0]

	V.Set("tools.github.repo", repo)
	err := V.WriteConfig()
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", ui.FormatSuccess(fmt.Sprintf("GitHub default repository set to: %s", repo)))
	fmt.Printf("Configuration saved to: %s\n", getConfigPath())
	fmt.Printf("Note: This can be overridden per operation\n")
	return nil
}

// formatToolStatus formats a tool's enabled/disabled status
func formatToolStatus(enabled bool) string {
	if enabled {
		return ui.FormatEnabled()
	}
	return ui.FormatDisabled()
}
