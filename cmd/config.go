package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

func init() {
	configCmd.AddCommand(setModelCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configToolsCmd)

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

	configInitCmd.Flags().Bool("overwrite", false, "Overwrite existing configuration file")
	configToolsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	configToolsExecCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

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
