package cmd

import (
	"fmt"
	"strings"

	config "github.com/inference-gateway/cli/config"
	services "github.com/inference-gateway/cli/internal/services"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	cobra "github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP (Model Context Protocol) server configuration",
	Long:  `Manage MCP servers for extending LLM capabilities with external tools. Add, remove, update, and list configured MCP servers.`,
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured MCP servers",
	Long:  `Display all MCP servers configured in mcp.yaml with their details including status, URL, and tools.`,
	RunE:  listMCPServers,
}

var mcpAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a new MCP server",
	Long: `Add a new MCP server to the configuration.

Example:
  infer mcp add filesystem http://localhost:3000/sse
  infer mcp add database http://localhost:3001/sse --description="Database queries"`,
	Args: cobra.ExactArgs(2),
	RunE: addMCPServer,
}

var mcpRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an MCP server",
	Long:  `Remove an MCP server from the configuration by name.`,
	Args:  cobra.ExactArgs(1),
	RunE:  removeMCPServer,
}

var mcpUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update an existing MCP server",
	Long: `Update an existing MCP server configuration.

Example:
  infer mcp update filesystem --url=http://localhost:3002/sse
  infer mcp update filesystem --description="Updated description"
  infer mcp update filesystem --enabled=false`,
	Args: cobra.ExactArgs(1),
	RunE: updateMCPServer,
}

var mcpEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable an MCP server",
	Long:  `Enable a specific MCP server.`,
	Args:  cobra.ExactArgs(1),
	RunE:  enableMCPServer,
}

var mcpDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable an MCP server",
	Long:  `Disable a specific MCP server.`,
	Args:  cobra.ExactArgs(1),
	RunE:  disableMCPServer,
}

var mcpEnableGlobalCmd = &cobra.Command{
	Use:   "enable-global",
	Short: "Enable MCP globally",
	Long:  `Enable the MCP feature globally. This allows MCP servers to be used.`,
	Args:  cobra.NoArgs,
	RunE:  enableMCPGlobal,
}

var mcpDisableGlobalCmd = &cobra.Command{
	Use:   "disable-global",
	Short: "Disable MCP globally",
	Long:  `Disable the MCP feature globally. This prevents all MCP servers from being used.`,
	Args:  cobra.NoArgs,
	RunE:  disableMCPGlobal,
}

func init() {
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpAddCmd)
	mcpCmd.AddCommand(mcpRemoveCmd)
	mcpCmd.AddCommand(mcpUpdateCmd)
	mcpCmd.AddCommand(mcpEnableCmd)
	mcpCmd.AddCommand(mcpDisableCmd)
	mcpCmd.AddCommand(mcpEnableGlobalCmd)
	mcpCmd.AddCommand(mcpDisableGlobalCmd)

	mcpAddCmd.Flags().String("description", "", "Description of the MCP server")
	mcpAddCmd.Flags().Int("timeout", 0, "Connection timeout in seconds (overrides global)")
	mcpAddCmd.Flags().StringSlice("include", []string{}, "Whitelist specific tools (comma-separated)")
	mcpAddCmd.Flags().StringSlice("exclude", []string{}, "Blacklist specific tools (comma-separated)")
	mcpAddCmd.Flags().Bool("enabled", true, "Enable the server immediately")

	mcpUpdateCmd.Flags().String("url", "", "Update the server URL")
	mcpUpdateCmd.Flags().String("description", "", "Update the description")
	mcpUpdateCmd.Flags().Int("timeout", -1, "Update connection timeout (-1 = no change, 0 = use global)")
	mcpUpdateCmd.Flags().StringSlice("include", []string{}, "Update whitelist (empty = no change)")
	mcpUpdateCmd.Flags().StringSlice("exclude", []string{}, "Update blacklist (empty = no change)")
	mcpUpdateCmd.Flags().Bool("enabled", true, "Enable/disable the server")

	mcpCmd.PersistentFlags().Bool("userspace", false, "Apply to userspace configuration (~/.infer/) instead of project configuration")

	rootCmd.AddCommand(mcpCmd)
}

func getMCPConfigPath(cmd *cobra.Command) string {
	userspace, _ := cmd.Flags().GetBool("userspace")
	if userspace {
		homeDir, _ := cmd.Root().PersistentFlags().GetString("home")
		return homeDir + "/" + config.ConfigDirName + "/" + config.MCPFileName
	}
	return config.DefaultMCPPath
}

func listMCPServers(cmd *cobra.Command, args []string) error {
	configPath := getMCPConfigPath(cmd)
	mcpConfigService := services.NewMCPConfigService(configPath)

	cfg, err := mcpConfigService.Load()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	if len(cfg.Servers) == 0 {
		fmt.Println("No MCP servers configured.")
		fmt.Println()
		fmt.Printf("To add a server: infer mcp add <name> <url>\n")
		return nil
	}

	fmt.Printf("MCP CONFIGURATION\n")
	fmt.Printf("═════════════════\n\n")

	globalStatus := icons.CheckMarkStyle.Render(icons.CheckMark)
	if !cfg.Enabled {
		globalStatus = icons.CrossMarkStyle.Render(icons.CrossMark)
	}
	fmt.Printf("Global Status:      %s %s\n", globalStatus, enabledText(cfg.Enabled))
	fmt.Printf("Connection Timeout: %ds\n", cfg.ConnectionTimeout)
	fmt.Printf("Discovery Timeout:  %ds\n", cfg.DiscoveryTimeout)
	fmt.Printf("Liveness Probes:    %s\n", enabledText(cfg.LivenessProbeEnabled))
	if cfg.LivenessProbeEnabled {
		fmt.Printf("Probe Interval:     %ds\n", cfg.LivenessProbeInterval)
	}
	fmt.Printf("Config Path:        %s\n", configPath)
	fmt.Println()

	fmt.Printf("SERVERS (%d total)\n", len(cfg.Servers))
	fmt.Printf("═════════════════\n\n")

	for i, server := range cfg.Servers {
		if i > 0 {
			fmt.Println()
		}

		status := icons.CheckMarkStyle.Render(icons.CheckMark)
		if !server.Enabled {
			status = icons.CrossMarkStyle.Render(icons.CrossMark)
		}

		fmt.Printf("%s %s\n", status, server.Name)
		fmt.Printf("  URL: %s\n", server.URL)

		if server.Description != "" {
			fmt.Printf("  Description: %s\n", server.Description)
		}

		if server.Timeout > 0 {
			fmt.Printf("  Timeout: %ds\n", server.Timeout)
		}

		if len(server.IncludeTools) > 0 {
			fmt.Printf("  Include Tools: %s\n", strings.Join(server.IncludeTools, ", "))
		}

		if len(server.ExcludeTools) > 0 {
			fmt.Printf("  Exclude Tools: %s\n", strings.Join(server.ExcludeTools, ", "))
		}
	}

	fmt.Println()
	fmt.Printf("\n%s = enabled, %s = disabled\n",
		icons.CheckMarkStyle.Render(icons.CheckMark),
		icons.CrossMarkStyle.Render(icons.CrossMark))

	return nil
}

func addMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]
	url := args[1]

	description, _ := cmd.Flags().GetString("description")
	timeout, _ := cmd.Flags().GetInt("timeout")
	include, _ := cmd.Flags().GetStringSlice("include")
	exclude, _ := cmd.Flags().GetStringSlice("exclude")
	enabled, _ := cmd.Flags().GetBool("enabled")

	server := config.MCPServerEntry{
		Name:         name,
		URL:          url,
		Enabled:      enabled,
		Description:  description,
		Timeout:      timeout,
		IncludeTools: include,
		ExcludeTools: exclude,
	}

	configPath := getMCPConfigPath(cmd)
	mcpConfigService := services.NewMCPConfigService(configPath)

	if err := mcpConfigService.AddServer(server); err != nil {
		return fmt.Errorf("failed to add MCP server: %w", err)
	}

	fmt.Printf("%s MCP server added: %s\n",
		icons.CheckMarkStyle.Render(icons.CheckMark),
		name)
	fmt.Printf("  URL: %s\n", url)
	if description != "" {
		fmt.Printf("  Description: %s\n", description)
	}
	fmt.Printf("  Status: %s\n", enabledText(enabled))
	fmt.Printf("\nConfiguration saved to %s\n", configPath)
	fmt.Printf("\n⚠️  Note: If using chat mode, restart the chat session to connect to the new MCP server.\n")
	// TODO: Implement hot-reload for MCP configuration changes without requiring chat restart

	return nil
}

func removeMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getMCPConfigPath(cmd)
	mcpConfigService := services.NewMCPConfigService(configPath)

	if err := mcpConfigService.RemoveServer(name); err != nil {
		return fmt.Errorf("failed to remove MCP server: %w", err)
	}

	fmt.Printf("%s MCP server removed: %s\n",
		icons.CheckMarkStyle.Render(icons.CheckMark),
		name)
	fmt.Printf("Configuration saved to %s\n", configPath)

	return nil
}

func updateMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getMCPConfigPath(cmd)
	mcpConfigService := services.NewMCPConfigService(configPath)

	existing, err := mcpConfigService.GetServer(name)
	if err != nil {
		return fmt.Errorf("failed to get MCP server: %w", err)
	}

	if cmd.Flags().Changed("url") {
		url, _ := cmd.Flags().GetString("url")
		existing.URL = url
	}

	if cmd.Flags().Changed("description") {
		description, _ := cmd.Flags().GetString("description")
		existing.Description = description
	}

	if cmd.Flags().Changed("timeout") {
		timeout, _ := cmd.Flags().GetInt("timeout")
		if timeout >= 0 {
			existing.Timeout = timeout
		}
	}

	if cmd.Flags().Changed("include") {
		include, _ := cmd.Flags().GetStringSlice("include")
		existing.IncludeTools = include
	}

	if cmd.Flags().Changed("exclude") {
		exclude, _ := cmd.Flags().GetStringSlice("exclude")
		existing.ExcludeTools = exclude
	}

	if cmd.Flags().Changed("enabled") {
		enabled, _ := cmd.Flags().GetBool("enabled")
		existing.Enabled = enabled
	}

	if err := mcpConfigService.UpdateServer(*existing); err != nil {
		return fmt.Errorf("failed to update MCP server: %w", err)
	}

	fmt.Printf("%s MCP server updated: %s\n",
		icons.CheckMarkStyle.Render(icons.CheckMark),
		name)
	fmt.Printf("Configuration saved to %s\n", configPath)

	return nil
}

func enableMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getMCPConfigPath(cmd)
	mcpConfigService := services.NewMCPConfigService(configPath)

	server, err := mcpConfigService.GetServer(name)
	if err != nil {
		return fmt.Errorf("failed to get MCP server: %w", err)
	}

	server.Enabled = true

	if err := mcpConfigService.UpdateServer(*server); err != nil {
		return fmt.Errorf("failed to enable MCP server: %w", err)
	}

	fmt.Printf("%s MCP server enabled: %s\n",
		icons.CheckMarkStyle.Render(icons.CheckMark),
		name)
	fmt.Printf("Configuration saved to %s\n", configPath)
	fmt.Printf("\n⚠️  Note: If using chat mode, restart the chat session to apply changes.\n")

	return nil
}

func disableMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getMCPConfigPath(cmd)
	mcpConfigService := services.NewMCPConfigService(configPath)

	server, err := mcpConfigService.GetServer(name)
	if err != nil {
		return fmt.Errorf("failed to get MCP server: %w", err)
	}

	server.Enabled = false

	if err := mcpConfigService.UpdateServer(*server); err != nil {
		return fmt.Errorf("failed to disable MCP server: %w", err)
	}

	fmt.Printf("%s MCP server disabled: %s\n",
		icons.CrossMarkStyle.Render(icons.CrossMark),
		name)
	fmt.Printf("Configuration saved to %s\n", configPath)
	fmt.Printf("\n⚠️  Note: If using chat mode, restart the chat session to apply changes.\n")

	return nil
}

func enableMCPGlobal(cmd *cobra.Command, args []string) error {
	configPath := getMCPConfigPath(cmd)
	mcpConfigService := services.NewMCPConfigService(configPath)

	cfg, err := mcpConfigService.Load()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	cfg.Enabled = true

	if err := mcpConfigService.Save(cfg); err != nil {
		return fmt.Errorf("failed to enable MCP globally: %w", err)
	}

	fmt.Printf("%s MCP enabled globally\n",
		icons.CheckMarkStyle.Render(icons.CheckMark))
	fmt.Printf("Configuration saved to %s\n", configPath)
	fmt.Printf("\n⚠️  Note: If using chat mode, restart the chat session to apply changes.\n")

	return nil
}

func disableMCPGlobal(cmd *cobra.Command, args []string) error {
	configPath := getMCPConfigPath(cmd)
	mcpConfigService := services.NewMCPConfigService(configPath)

	cfg, err := mcpConfigService.Load()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	cfg.Enabled = false

	if err := mcpConfigService.Save(cfg); err != nil {
		return fmt.Errorf("failed to disable MCP globally: %w", err)
	}

	fmt.Printf("%s MCP disabled globally\n",
		icons.CrossMarkStyle.Render(icons.CrossMark))
	fmt.Printf("Configuration saved to %s\n", configPath)
	fmt.Printf("\n⚠️  Note: If using chat mode, restart the chat session to apply changes.\n")

	return nil
}

func enabledText(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
