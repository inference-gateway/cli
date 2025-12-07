package cmd

import (
	"fmt"
	"strings"

	config "github.com/inference-gateway/cli/config"
	services "github.com/inference-gateway/cli/internal/services"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	cobra "github.com/spf13/cobra"
)

var a2aCmd = &cobra.Command{
	Use:   "a2a",
	Short: "Manage A2A (Agent-to-Agent) configuration",
	Long:  `Manage A2A agents for task delegation. Add, remove, enable, disable, and list configured A2A agents.`,
}

var a2aListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured A2A agents",
	Long:  `Display all A2A agents configured in agents.yaml with their details including status, URL, model, and auto-start settings.`,
	RunE:  listA2AAgents,
}

var a2aAddCmd = &cobra.Command{
	Use:   "add <name> [url]",
	Short: "Add a new A2A agent",
	Long: `Add a new A2A agent to the configuration.

Examples:
  # Add external agent
  infer a2a add researcher https://api.example.com/a2a --model anthropic/claude-4-5-sonnet

  # Add auto-starting agent with OCI
  infer a2a add coder --run --oci=my-agent:latest --model openai/gpt-4

  # Add agent with environment variables
  infer a2a add analyst --model anthropic/claude-4-5-sonnet --environment API_KEY=xxx --environment DEBUG=true`,
	Args: cobra.RangeArgs(1, 2),
	RunE: addA2AAgent,
}

var a2aRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an A2A agent",
	Long:  `Remove an A2A agent from the configuration by name.`,
	Args:  cobra.ExactArgs(1),
	RunE:  removeA2AAgent,
}

var a2aEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable an A2A agent",
	Long:  `Enable a specific A2A agent. Note: You may need to restart your chat session for changes to take effect.`,
	Args:  cobra.ExactArgs(1),
	RunE:  enableA2AAgent,
}

var a2aDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable an A2A agent",
	Long:  `Disable a specific A2A agent. Note: You may need to restart your chat session for changes to take effect.`,
	Args:  cobra.ExactArgs(1),
	RunE:  disableA2AAgent,
}

func init() {
	a2aCmd.AddCommand(a2aListCmd)
	a2aCmd.AddCommand(a2aAddCmd)
	a2aCmd.AddCommand(a2aRemoveCmd)
	a2aCmd.AddCommand(a2aEnableCmd)
	a2aCmd.AddCommand(a2aDisableCmd)

	a2aAddCmd.Flags().String("oci", "", "OCI image for auto-start")
	a2aAddCmd.Flags().Bool("run", false, "Auto-start this agent")
	a2aAddCmd.Flags().String("model", "", "Model specification (e.g., anthropic/claude-4-5-sonnet)")
	a2aAddCmd.Flags().String("artifacts-url", "", "Artifacts download URL")
	a2aAddCmd.Flags().StringSlice("environment", []string{}, "Environment variables in KEY=VALUE format (can be specified multiple times)")
	a2aAddCmd.Flags().Bool("enabled", true, "Enable the agent immediately")

	a2aCmd.PersistentFlags().Bool("userspace", false, "Apply to userspace configuration (~/.infer/) instead of project configuration")

	rootCmd.AddCommand(a2aCmd)
}

func getA2AConfigPath(cmd *cobra.Command) string {
	userspace, _ := cmd.Flags().GetBool("userspace")
	if userspace {
		homeDir, _ := cmd.Root().PersistentFlags().GetString("home")
		return homeDir + "/" + config.ConfigDirName + "/" + config.AgentsFileName
	}
	return config.DefaultAgentsPath
}

func listA2AAgents(cmd *cobra.Command, args []string) error {
	configPath := getA2AConfigPath(cmd)
	agentsConfigService := services.NewAgentsConfigService(configPath)

	agents, err := agentsConfigService.ListAgents()
	if err != nil {
		return fmt.Errorf("failed to load A2A agents: %w", err)
	}

	if len(agents) == 0 {
		fmt.Println("No A2A agents configured.")
		fmt.Println()
		fmt.Printf("To add an agent: infer a2a add <name> <url> --model <model>\n")
		fmt.Printf("Example: infer a2a add researcher https://api.example.com/a2a --model anthropic/claude-4-5-sonnet\n")
		return nil
	}

	fmt.Printf("A2A AGENTS (%d total)\n", len(agents))
	fmt.Println("═══════════════════")
	fmt.Println()

	for _, agent := range agents {
		statusIcon := icons.CheckMark
		status := "enabled"
		if !agent.Enabled {
			statusIcon = icons.CrossMark
			status = "disabled"
		}

		fmt.Printf("%s %s (%s)\n", statusIcon, agent.Name, status)
		fmt.Printf("  URL: %s\n", agent.URL)

		if agent.Model != "" {
			fmt.Printf("  Model: %s\n", agent.Model)
		}

		if agent.Run {
			fmt.Printf("  Auto-start: yes\n")
			if agent.OCI != "" {
				fmt.Printf("  OCI: %s\n", agent.OCI)
			}
		}

		if agent.ArtifactsURL != "" {
			fmt.Printf("  Artifacts URL: %s\n", agent.ArtifactsURL)
		}

		if len(agent.Environment) > 0 {
			fmt.Printf("  Environment: %d variable(s)\n", len(agent.Environment))
		}

		fmt.Println()
	}

	return nil
}

// applyDefaultIfEmpty returns the default value if the current value is empty
func applyDefaultIfEmpty(current, defaultValue string) string {
	if current == "" && defaultValue != "" {
		return defaultValue
	}
	return current
}

func addA2AAgent(cmd *cobra.Command, args []string) error {
	name := args[0]
	url := ""
	if len(args) > 1 {
		url = args[1]
	}

	// Get flags
	oci, _ := cmd.Flags().GetString("oci")
	run, _ := cmd.Flags().GetBool("run")
	model, _ := cmd.Flags().GetString("model")
	artifactsURL, _ := cmd.Flags().GetString("artifacts-url")
	envVars, _ := cmd.Flags().GetStringSlice("environment")
	enabled, _ := cmd.Flags().GetBool("enabled")

	environment := make(map[string]string)
	for _, envVar := range envVars {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid environment variable format: %s (expected KEY=VALUE)", envVar)
		}
		environment[parts[0]] = parts[1]
	}

	defaults := config.GetAgentDefaults(name)
	if defaults != nil {
		url = applyDefaultIfEmpty(url, defaults.URL)
		artifactsURL = applyDefaultIfEmpty(artifactsURL, defaults.ArtifactsURL)
		oci = applyDefaultIfEmpty(oci, defaults.OCI)
		model = applyDefaultIfEmpty(model, defaults.Model)

		if !cmd.Flags().Changed("run") {
			run = defaults.Run
		}
	}

	if url == "" {
		knownAgents := config.ListKnownAgents()
		return fmt.Errorf("URL is required for unknown agent '%s'. Known agents: %v", name, knownAgents)
	}

	if run && model == "" {
		return fmt.Errorf("--model is required when --run is enabled. Specify a model in the format provider/model (e.g., openai/gpt-4, anthropic/claude-4-5-sonnet)")
	}

	agent := config.AgentEntry{
		Name:         name,
		URL:          url,
		ArtifactsURL: artifactsURL,
		OCI:          oci,
		Run:          run,
		Model:        model,
		Environment:  environment,
		Enabled:      enabled,
	}

	configPath := getA2AConfigPath(cmd)
	agentsConfigService := services.NewAgentsConfigService(configPath)

	if err := agentsConfigService.AddAgent(agent); err != nil {
		return fmt.Errorf("failed to add agent: %w", err)
	}

	fmt.Printf("%s Agent '%s' added successfully!\n", icons.CheckMark, name)
	fmt.Printf("  URL: %s\n", url)
	if model != "" {
		fmt.Printf("  Model: %s\n", model)
	}
	if run {
		fmt.Printf("  Auto-start: enabled\n")
		if oci != "" {
			fmt.Printf("  OCI: %s\n", oci)
		}
	}
	if artifactsURL != "" {
		fmt.Printf("  Artifacts URL: %s\n", artifactsURL)
	}
	if len(environment) > 0 {
		fmt.Printf("  Environment: %d variable(s) configured\n", len(environment))
	}
	fmt.Println()
	fmt.Println("Note: Restart your chat session for changes to take effect.")

	return nil
}

func removeA2AAgent(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getA2AConfigPath(cmd)
	agentsConfigService := services.NewAgentsConfigService(configPath)

	agent, err := agentsConfigService.GetAgent(name)
	if err != nil {
		return fmt.Errorf("failed to find agent: %w", err)
	}

	if err := agentsConfigService.RemoveAgent(name); err != nil {
		return fmt.Errorf("failed to remove agent: %w", err)
	}

	fmt.Printf("%s Agent '%s' removed successfully!\n", icons.CheckMark, name)
	if agent.Run {
		fmt.Println()
		fmt.Println("Note: If the agent was running, you may need to manually stop its container.")
		fmt.Println("Restart your chat session for changes to take effect.")
	}

	return nil
}

func enableA2AAgent(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getA2AConfigPath(cmd)
	agentsConfigService := services.NewAgentsConfigService(configPath)

	agent, err := agentsConfigService.GetAgent(name)
	if err != nil {
		return fmt.Errorf("failed to find agent: %w", err)
	}

	agent.Enabled = true
	if err := agentsConfigService.UpdateAgent(*agent); err != nil {
		return fmt.Errorf("failed to enable agent: %w", err)
	}

	fmt.Printf("%s Agent '%s' enabled successfully!\n", icons.CheckMark, name)
	fmt.Println()
	fmt.Println("Note: Restart your chat session for changes to take effect.")

	return nil
}

func disableA2AAgent(cmd *cobra.Command, args []string) error {
	name := args[0]

	configPath := getA2AConfigPath(cmd)
	agentsConfigService := services.NewAgentsConfigService(configPath)

	agent, err := agentsConfigService.GetAgent(name)
	if err != nil {
		return fmt.Errorf("failed to find agent: %w", err)
	}

	agent.Enabled = false
	if err := agentsConfigService.UpdateAgent(*agent); err != nil {
		return fmt.Errorf("failed to disable agent: %w", err)
	}

	fmt.Printf("%s Agent '%s' disabled successfully!\n", icons.CheckMark, name)
	fmt.Println()
	fmt.Println("Note: Restart your chat session for changes to take effect.")

	return nil
}
