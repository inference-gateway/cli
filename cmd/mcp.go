package cmd

import (
	"fmt"
	"net/url"
	"strings"

	glamour "github.com/charmbracelet/glamour"
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
	Use:   "add <name> [url]",
	Short: "Add a new MCP server",
	Long: `Add a new MCP server to the configuration.

Examples:
  # Add external/manual server
  infer mcp add filesystem http://localhost:3000/sse

  # Add auto-starting container
  infer mcp add demo --run --oci=mcp-demo-server:latest --port=3000`,
	Args: cobra.RangeArgs(1, 2),
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
	mcpAddCmd.Flags().Bool("run", false, "Auto-start this server in a container")
	mcpAddCmd.Flags().String("oci", "", "OCI image to use (required if --run is true)")
	mcpAddCmd.Flags().Int("port", 0, "Container port to expose")
	mcpAddCmd.Flags().Int("startup-timeout", 60, "Startup timeout in seconds")

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

	var md strings.Builder
	md.WriteString("**MCP CONFIGURATION**\n\n")

	globalStatus := icons.CheckMark
	if !cfg.Enabled {
		globalStatus = icons.CrossMark
	}

	md.WriteString(fmt.Sprintf("**Global Status:** %s %s  \n", globalStatus, enabledText(cfg.Enabled)))
	md.WriteString(fmt.Sprintf("**Connection Timeout:** %ds  \n", cfg.ConnectionTimeout))
	md.WriteString(fmt.Sprintf("**Discovery Timeout:** %ds  \n", cfg.DiscoveryTimeout))
	md.WriteString(fmt.Sprintf("**Liveness Probes:** %s", enabledText(cfg.LivenessProbeEnabled)))
	if cfg.LivenessProbeEnabled {
		md.WriteString(fmt.Sprintf(" (Interval: %ds)", cfg.LivenessProbeInterval))
	}
	md.WriteString("\n")
	md.WriteString(fmt.Sprintf("**Config Path:** `%s`\n\n", configPath))

	md.WriteString(fmt.Sprintf("**Servers:** %d total\n\n", len(cfg.Servers)))

	md.WriteString("| Enabled | Name | URL | Description | Timeout | Auto |\n")
	md.WriteString("|---------|------|-----|-------------|---------|------|\n")

	for _, server := range cfg.Servers {
		status := icons.CheckMark
		if !server.Enabled {
			status = icons.CrossMark
		}

		name := server.Name
		url := server.GetURL()
		description := server.Description
		if description == "" {
			description = "-"
		}

		timeoutStr := "-"
		if server.Timeout > 0 {
			timeoutStr = fmt.Sprintf("%ds", server.Timeout)
		}

		autoStart := "-"
		if server.Run {
			autoStart = icons.CheckMark
			if server.OCI != "" {
				ociParts := strings.Split(server.OCI, "/")
				ociShort := ociParts[len(ociParts)-1]
				autoStart = fmt.Sprintf("%s %s", icons.CheckMark, ociShort)
			}
		}

		md.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
			status, name, url, description, timeoutStr, autoStart))
	}

	md.WriteString("\n")

	hasFilters := false
	for _, server := range cfg.Servers {
		if len(server.IncludeTools) > 0 || len(server.ExcludeTools) > 0 {
			hasFilters = true
			break
		}
	}

	if hasFilters {
		md.WriteString("### Tool Filters\n\n")
		for _, server := range cfg.Servers {
			if len(server.IncludeTools) > 0 {
				md.WriteString(fmt.Sprintf("**%s** - Include: `%s`  \n", server.Name, strings.Join(server.IncludeTools, ", ")))
			}
			if len(server.ExcludeTools) > 0 {
				md.WriteString(fmt.Sprintf("**%s** - Exclude: `%s`  \n", server.Name, strings.Join(server.ExcludeTools, ", ")))
			}
		}
		md.WriteString("\n")
	}

	md.WriteString(fmt.Sprintf("\n%s = enabled, %s = disabled\n",
		icons.CheckMark,
		icons.CrossMark))

	rendered, err := renderMarkdown(md.String())
	if err != nil {
		fmt.Print(md.String())
		return nil
	}

	fmt.Print(rendered)
	return nil
}

func renderMarkdown(markdown string) (string, error) {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0),
	)
	if err != nil {
		return "", err
	}

	return r.Render(markdown)
}

func addMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]

	description, _ := cmd.Flags().GetString("description")
	timeout, _ := cmd.Flags().GetInt("timeout")
	include, _ := cmd.Flags().GetStringSlice("include")
	exclude, _ := cmd.Flags().GetStringSlice("exclude")
	enabled, _ := cmd.Flags().GetBool("enabled")
	run, _ := cmd.Flags().GetBool("run")
	oci, _ := cmd.Flags().GetString("oci")
	containerPort, _ := cmd.Flags().GetInt("port")
	startupTimeout, _ := cmd.Flags().GetInt("startup-timeout")

	server := config.MCPServerEntry{
		Name:           name,
		Enabled:        enabled,
		Description:    description,
		Timeout:        timeout,
		IncludeTools:   include,
		ExcludeTools:   exclude,
		Run:            run,
		OCI:            oci,
		StartupTimeout: startupTimeout,
	}

	if len(args) > 1 {
		urlStr := args[1]
		scheme, host, port, path := parseURL(urlStr)
		server.Scheme = scheme
		server.Host = host
		server.Ports = []string{port}
		server.Path = path
	} else {
		server.Scheme = "http"
		server.Host = "localhost"
		server.Path = "/mcp"
	}

	if containerPort > 0 {
		server.Port = containerPort
		server.Ports = nil
	} else if run {
		basePort := 3000

		configPath := getMCPConfigPath(cmd)
		mcpConfigService := services.NewMCPConfigService(configPath)
		existingConfig, _ := mcpConfigService.Load()

		for _, existing := range existingConfig.Servers {
			if existing.Port > basePort {
				basePort = existing.Port
			}
			if primaryPort := existing.GetPrimaryPort(); primaryPort > basePort {
				basePort = primaryPort
			}
		}

		server.Port = basePort + 1
		server.Ports = nil
	}

	configPath := getMCPConfigPath(cmd)
	mcpConfigService := services.NewMCPConfigService(configPath)

	if err := mcpConfigService.AddServer(server); err != nil {
		return fmt.Errorf("failed to add MCP server: %w", err)
	}

	fmt.Printf("%s MCP server added: %s\n",
		icons.CheckMarkStyle.Render(icons.CheckMark),
		name)
	fmt.Printf("  URL: %s\n", server.GetURL())
	if description != "" {
		fmt.Printf("  Description: %s\n", description)
	}
	if run {
		fmt.Printf("  Auto-start: %s (OCI: %s)\n", enabledText(true), oci)
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
		urlStr, _ := cmd.Flags().GetString("url")
		scheme, host, port, path := parseURL(urlStr)
		existing.Scheme = scheme
		existing.Host = host
		existing.Ports = []string{port}
		existing.Path = path
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

// parseURL parses a URL string into its components (scheme, host, port, path)
func parseURL(urlStr string) (scheme, host, port, path string) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "http", "localhost", "8080", "/mcp"
	}

	scheme = u.Scheme
	if scheme == "" {
		scheme = "http"
	}

	host = u.Hostname()
	if host == "" {
		host = "localhost"
	}

	port = u.Port()
	if port == "" {
		port = "8080"
	}

	path = u.Path
	if path == "" {
		path = "/mcp"
	}

	return scheme, host, port, path
}
