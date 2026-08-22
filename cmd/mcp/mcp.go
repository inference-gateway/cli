package mcp

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
)

type command struct {
	renderer *output.Renderer
}

// NewCommand constructs the MCP command tree.
func NewCommand(renderer *output.Renderer) *cobra.Command {
	c := &command{renderer: renderer}
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP (Model Context Protocol) server configuration",
		Long:  `Manage MCP servers for extending LLM capabilities with external tools. Add, remove, update, and list configured MCP servers.`,
	}
	mcpListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured MCP servers",
		Long:  `Display all MCP servers configured in mcp.yaml with their details including status, URL, and tools.`,
		RunE:  c.listMCPServers,
	}
	mcpAddCmd := &cobra.Command{
		Use:   "add <name> [url]",
		Short: "Add a new MCP server",
		Long: `Add a new MCP server to the configuration.

Examples:
  # Add external/manual server
  infer mcp add filesystem http://localhost:3000/sse

  # Add auto-starting container
  infer mcp add demo --run --oci=mcp-demo-server:latest --port=3000`,
		Args: cobra.RangeArgs(1, 2),
		RunE: c.addMCPServer,
	}
	mcpRemoveCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an MCP server",
		Long:  `Remove an MCP server from the configuration by name.`,
		Args:  cobra.ExactArgs(1),
		RunE:  c.removeMCPServer,
	}
	mcpUpdateCmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an existing MCP server",
		Long: `Update an existing MCP server configuration.

Example:
  infer mcp update filesystem --url=http://localhost:3002/sse
  infer mcp update filesystem --description="Updated description"
  infer mcp update filesystem --enabled=false`,
		Args: cobra.ExactArgs(1),
		RunE: c.updateMCPServer,
	}
	mcpEnableCmd := &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable an MCP server",
		Long:  `Enable a specific MCP server.`,
		Args:  cobra.ExactArgs(1),
		RunE:  c.enableMCPServer,
	}
	mcpDisableCmd := &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable an MCP server",
		Long:  `Disable a specific MCP server.`,
		Args:  cobra.ExactArgs(1),
		RunE:  c.disableMCPServer,
	}
	mcpEnableGlobalCmd := &cobra.Command{
		Use:   "enable-global",
		Short: "Enable MCP globally",
		Long:  `Enable the MCP feature globally. This allows MCP servers to be used.`,
		Args:  cobra.NoArgs,
		RunE:  c.enableMCPGlobal,
	}
	mcpDisableGlobalCmd := &cobra.Command{
		Use:   "disable-global",
		Short: "Disable MCP globally",
		Long:  `Disable the MCP feature globally. This prevents all MCP servers from being used.`,
		Args:  cobra.NoArgs,
		RunE:  c.disableMCPGlobal,
	}

	mcpCmd.AddCommand(mcpListCmd, mcpAddCmd, mcpRemoveCmd, mcpUpdateCmd, mcpEnableCmd, mcpDisableCmd, mcpEnableGlobalCmd, mcpDisableGlobalCmd)

	mcpAddCmd.Flags().String("description", "", "Description of the MCP server")
	mcpAddCmd.Flags().Int("timeout", 0, "Connection timeout in seconds (overrides global)")
	mcpAddCmd.Flags().StringSlice("include", []string{}, "Filter for allowed tools (comma-separated)")
	mcpAddCmd.Flags().StringSlice("exclude", []string{}, "Filter for excluded tools (comma-separated)")
	mcpAddCmd.Flags().Bool("enabled", true, "Enable the server immediately")
	mcpAddCmd.Flags().Bool("run", false, "Auto-start this server in a container")
	mcpAddCmd.Flags().String("oci", "", "OCI image to use (required if --run is true)")
	mcpAddCmd.Flags().Int("port", 0, "Container port to expose")
	mcpAddCmd.Flags().Int("startup-timeout", 60, "Startup timeout in seconds")

	mcpUpdateCmd.Flags().String("url", "", "Update the server URL")
	mcpUpdateCmd.Flags().String("description", "", "Update the description")
	mcpUpdateCmd.Flags().Int("timeout", -1, "Update connection timeout (-1 = no change, 0 = use global)")
	mcpUpdateCmd.Flags().StringSlice("include", []string{}, "Update filter for allowed tools (empty = no change)")
	mcpUpdateCmd.Flags().StringSlice("exclude", []string{}, "Update filter for excluded tools (empty = no change)")
	mcpUpdateCmd.Flags().Bool("enabled", true, "Enable/disable the server")

	mcpCmd.PersistentFlags().Bool("project", false, "Apply to the project configuration (./.infer/) instead of the userspace baseline (~/.infer/)")

	return mcpCmd
}

// getMCPConfigPath returns the mcp.yaml path to write. Writes target the
// userspace baseline (~/.infer/) by default; --project targets the project
// .infer/mcp.yaml instead.
func getMCPConfigPath(cmd *cobra.Command) string {
	if runtime.ProjectFlag(cmd) {
		return config.DefaultMCPPath
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return config.DefaultMCPPath
	}
	return filepath.Join(homeDir, config.ConfigDirName, config.MCPFileName)
}

func (c *command) listMCPServers(cmd *cobra.Command, _ []string) error {
	configPath := getMCPConfigPath(cmd)

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	if len(cfg.Servers) == 0 {
		fmt.Println("No MCP servers configured.")
		fmt.Println()
		fmt.Printf("To add a server: infer mcp add <name> <url>\n")
		return nil
	}

	fmt.Println(c.renderer.Title("MCP Configuration"))
	fmt.Println()

	fmt.Println(c.renderer.Field("Global Status", fmt.Sprintf("%s %s", c.renderer.StatusIcon(cfg.Enabled), enabledText(cfg.Enabled))))
	fmt.Println(c.renderer.Field("Connection Timeout", fmt.Sprintf("%ds", cfg.ConnectionTimeout)))
	fmt.Println(c.renderer.Field("Discovery Timeout", fmt.Sprintf("%ds", cfg.DiscoveryTimeout)))
	liveness := enabledText(cfg.LivenessProbeEnabled)
	if cfg.LivenessProbeEnabled {
		liveness += fmt.Sprintf(" (interval: %ds)", cfg.LivenessProbeInterval)
	}
	fmt.Println(c.renderer.Field("Liveness Probes", liveness))
	fmt.Println(c.renderer.Field("Config Path", configPath))
	fmt.Println()
	fmt.Println(c.renderer.Hint(fmt.Sprintf("%d server(s) configured", len(cfg.Servers))))
	fmt.Println()

	serversTable := c.renderer.NewListTable("Enabled", "Name", "URL", "Description", "Timeout", "Auto")
	for _, server := range cfg.Servers {
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
			autoStart = c.renderer.StatusIcon(true)
			if server.OCI != "" {
				ociParts := strings.Split(server.OCI, "/")
				autoStart = fmt.Sprintf("%s %s", c.renderer.StatusIcon(true), ociParts[len(ociParts)-1])
			}
		}

		serversTable.Row(c.renderer.StatusIcon(server.Enabled), server.Name, server.GetURL(), description, timeoutStr, autoStart)
	}
	fmt.Println(serversTable.Render())

	c.printMCPToolFilters(cfg.Servers)

	fmt.Println()
	fmt.Println(c.renderer.StatusLegend())
	return nil
}

// printMCPToolFilters prints the per-server include/exclude tool filters when
// any server defines them.
func (c *command) printMCPToolFilters(servers []config.MCPServerEntry) {
	hasFilters := false
	for _, server := range servers {
		if len(server.IncludeTools) > 0 || len(server.ExcludeTools) > 0 {
			hasFilters = true
			break
		}
	}
	if !hasFilters {
		return
	}

	fmt.Println()
	fmt.Println(c.renderer.Title("Tool Filters"))
	for _, server := range servers {
		if len(server.IncludeTools) > 0 {
			fmt.Println(c.renderer.Field(server.Name+" include", strings.Join(server.IncludeTools, ", ")))
		}
		if len(server.ExcludeTools) > 0 {
			fmt.Println(c.renderer.Field(server.Name+" exclude", strings.Join(server.ExcludeTools, ", ")))
		}
	}
}

func (c *command) addMCPServer(cmd *cobra.Command, args []string) error {
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
		existingConfig, _ := config.LoadMCP(configPath)

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

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}
	if err := cfg.CreateEntry(server); err != nil {
		return fmt.Errorf("failed to add MCP server: %w", err)
	}

	fmt.Printf("%s MCP server added: %s\n",
		c.renderer.StatusIcon(true),
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

func (c *command) removeMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getMCPConfigPath(cmd)

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}
	if err := cfg.DeleteEntry(name); err != nil {
		return fmt.Errorf("failed to remove MCP server: %w", err)
	}

	fmt.Printf("%s MCP server removed: %s\n",
		c.renderer.StatusIcon(true),
		name)
	fmt.Printf("Configuration saved to %s\n", configPath)

	return nil
}

func (c *command) updateMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getMCPConfigPath(cmd)

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}
	existing, err := cfg.ReadEntry(name)
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

	if err := cfg.UpdateEntry(*existing); err != nil {
		return fmt.Errorf("failed to update MCP server: %w", err)
	}

	fmt.Printf("%s MCP server updated: %s\n",
		c.renderer.StatusIcon(true),
		name)
	fmt.Printf("Configuration saved to %s\n", configPath)

	return nil
}

func (c *command) enableMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getMCPConfigPath(cmd)

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}
	server, err := cfg.ReadEntry(name)
	if err != nil {
		return fmt.Errorf("failed to get MCP server: %w", err)
	}

	server.Enabled = true

	if err := cfg.UpdateEntry(*server); err != nil {
		return fmt.Errorf("failed to enable MCP server: %w", err)
	}

	fmt.Printf("%s MCP server enabled: %s\n",
		c.renderer.StatusIcon(true),
		name)
	fmt.Printf("Configuration saved to %s\n", configPath)
	fmt.Printf("\n⚠️  Note: If using chat mode, restart the chat session to apply changes.\n")

	return nil
}

func (c *command) disableMCPServer(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getMCPConfigPath(cmd)

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}
	server, err := cfg.ReadEntry(name)
	if err != nil {
		return fmt.Errorf("failed to get MCP server: %w", err)
	}

	server.Enabled = false

	if err := cfg.UpdateEntry(*server); err != nil {
		return fmt.Errorf("failed to disable MCP server: %w", err)
	}

	fmt.Printf("%s MCP server disabled: %s\n",
		c.renderer.StatusIcon(false),
		name)
	fmt.Printf("Configuration saved to %s\n", configPath)
	fmt.Printf("\n⚠️  Note: If using chat mode, restart the chat session to apply changes.\n")

	return nil
}

func (c *command) enableMCPGlobal(cmd *cobra.Command, _ []string) error {
	configPath := getMCPConfigPath(cmd)

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	cfg.Enabled = true

	if err := config.SaveMCP(configPath, cfg); err != nil {
		return fmt.Errorf("failed to enable MCP globally: %w", err)
	}

	fmt.Printf("%s MCP enabled globally\n", c.renderer.StatusIcon(true))
	fmt.Printf("Configuration saved to %s\n", configPath)
	fmt.Printf("\n⚠️  Note: If using chat mode, restart the chat session to apply changes.\n")

	return nil
}

func (c *command) disableMCPGlobal(cmd *cobra.Command, _ []string) error {
	configPath := getMCPConfigPath(cmd)

	cfg, err := config.LoadMCP(configPath)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	cfg.Enabled = false

	if err := config.SaveMCP(configPath, cfg); err != nil {
		return fmt.Errorf("failed to disable MCP globally: %w", err)
	}

	fmt.Printf("%s MCP disabled globally\n", c.renderer.StatusIcon(false))
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
