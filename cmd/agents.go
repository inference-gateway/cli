package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	services "github.com/inference-gateway/cli/internal/services"
	ui "github.com/inference-gateway/cli/internal/ui"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	cobra "github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Manage A2A agent configurations",
	Long: `Manage Agent-to-Agent (A2A) configurations stored in agents.yaml.
This allows you to configure remote or local agents that can be used for delegation.`,
}

var agentsAddCmd = &cobra.Command{
	Use:   "add <name> [url]",
	Short: "Add a new A2A agent configuration",
	Long: `Add a new Agent-to-Agent (A2A) agent to the agents.yaml configuration.

For known agents (browser-agent, mock-agent, google-calendar-agent, documentation-agent, n8n-agent),
you can simply provide the name and sensible defaults will be used. You can override any default
with flags.

Examples:
  # Add a known agent with defaults
  infer agents add browser-agent

  # Add a known agent and override the model
  infer agents add browser-agent --model anthropic/claude-4-5-sonnet

  # Add a remote agent
  infer agents add code-reviewer https://agent.example.com

  # Add a local agent with OCI image
  infer agents add test-runner https://localhost:8081 --oci ghcr.io/org/test-runner:latest --run

  # Add agent with specific model
  infer agents add code-reviewer https://agent.example.com --run --model deepseek/deepseek-chat

  # Add agent with custom environment variables
  infer agents add analyzer https://agent.example.com --run --environment CUSTOM_ENV=value --environment A2A_DEBUG=true --environment A2A_PORT=8443`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		var url string
		if len(args) > 1 {
			url = args[1]
		}

		defaults := config.GetAgentDefaults(name)

		if url == "" && defaults == nil {
			return fmt.Errorf("URL is required for unknown agent '%s'. Known agents: %v", name, config.ListKnownAgents())
		}

		oci, _ := cmd.Flags().GetString("oci")
		artifactsURL, _ := cmd.Flags().GetString("artifacts-url")
		run, _ := cmd.Flags().GetBool("run")
		model, _ := cmd.Flags().GetString("model")
		envVars, _ := cmd.Flags().GetStringSlice("environment")

		var environment map[string]string
		if len(envVars) > 0 {
			environment = make(map[string]string)
			for _, env := range envVars {
				parts := strings.SplitN(env, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid environment variable format: %s (expected KEY=VALUE)", env)
				}
				environment[parts[0]] = parts[1]
			}
		}

		if defaults != nil {
			if url == "" {
				url = defaults.URL
			}
			if !cmd.Flags().Changed("artifacts-url") && defaults.ArtifactsURL != "" {
				artifactsURL = defaults.ArtifactsURL
			}
			if !cmd.Flags().Changed("oci") && defaults.OCI != "" {
				oci = defaults.OCI
			}
			if !cmd.Flags().Changed("run") {
				run = defaults.Run
			}
			if !cmd.Flags().Changed("model") && defaults.Model != "" {
				model = defaults.Model
			}
		}

		return addAgent(cmd, name, url, artifactsURL, oci, run, model, environment)
	},
}

var agentsUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update an existing A2A agent configuration",
	Long: `Update an existing Agent-to-Agent (A2A) agent in the agents.yaml configuration.
At least one flag must be provided to update the agent.

Examples:
  # Update agent URL
  infer agents update code-reviewer --run=false --url https://new-agent.example.com

  # Update agent model
  infer agents update code-reviewer --model openai/gpt-4

  # Update multiple properties
  infer agents update test-runner --oci ghcr.io/org/test-runner:v2 --model anthropic/claude-4-5-sonnet

  # Add environment variables (replaces existing ones)
  infer agents update analyzer --environment CUSTOM_ENV=value --environment DEBUG=true`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if !cmd.Flags().Changed("url") && !cmd.Flags().Changed("artifacts-url") &&
			!cmd.Flags().Changed("oci") && !cmd.Flags().Changed("run") &&
			!cmd.Flags().Changed("model") && !cmd.Flags().Changed("environment") {
			return fmt.Errorf("at least one flag must be provided to update the agent")
		}

		url, _ := cmd.Flags().GetString("url")
		artifactsURL, _ := cmd.Flags().GetString("artifacts-url")
		oci, _ := cmd.Flags().GetString("oci")
		run, _ := cmd.Flags().GetBool("run")
		model, _ := cmd.Flags().GetString("model")
		envVars, _ := cmd.Flags().GetStringSlice("environment")

		var environment map[string]string
		if cmd.Flags().Changed("environment") {
			environment = make(map[string]string)
			for _, env := range envVars {
				parts := strings.SplitN(env, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid environment variable format: %s (expected KEY=VALUE)", env)
				}
				environment[parts[0]] = parts[1]
			}
		}

		return updateAgent(cmd, name, url, artifactsURL, oci, run, model, environment)
	},
}

var agentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured A2A agents",
	Long:  `List all Agent-to-Agent (A2A) agents configured in agents.yaml.`,
	RunE:  listAgents,
}

var agentsRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an A2A agent configuration",
	Long:  `Remove an Agent-to-Agent (A2A) agent from the agents.yaml configuration.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return removeAgent(cmd, args[0])
	},
}

var agentsShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show details of a specific A2A agent",
	Long:  `Show detailed configuration of a specific Agent-to-Agent (A2A) agent.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return showAgent(cmd, args[0])
	},
}

var agentsInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the agents.yaml configuration file",
	Long:  `Initialize a new agents.yaml configuration file with default settings.`,
	RunE:  initAgents,
}

func getAgentsConfigService(cmd *cobra.Command) (*services.AgentsConfigService, error) {
	userspace := GetUserspaceFlag(cmd)

	var agentsPath string
	if userspace {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		agentsPath = filepath.Join(homeDir, config.ConfigDirName, config.AgentsFileName)
	} else {
		agentsPath = config.DefaultAgentsPath
	}

	return services.NewAgentsConfigService(agentsPath), nil
}

func addAgent(cmd *cobra.Command, name, url, artifactsURL, oci string, run bool, model string, environment map[string]string) error {
	if run && model == "" {
		return fmt.Errorf("--model is required when --run is enabled. Specify a model in the format provider/model (e.g., openai/gpt-5, anthropic/claude-4-5-sonnet)")
	}

	svc, err := getAgentsConfigService(cmd)
	if err != nil {
		return err
	}

	agent := config.AgentEntry{
		Name:         name,
		URL:          url,
		ArtifactsURL: artifactsURL,
		OCI:          oci,
		Run:          run,
		Model:        model,
		Environment:  environment,
	}

	if err := svc.AddAgent(agent); err != nil {
		return err
	}

	fmt.Printf("%s Agent '%s' added successfully\n", icons.CheckMarkStyle.Render(icons.CheckMark), name)
	fmt.Printf("  URL: %s\n", url)
	if artifactsURL != "" {
		fmt.Printf("  Artifacts URL: %s\n", artifactsURL)
	}
	if oci != "" {
		fmt.Printf("  OCI: %s\n", oci)
	}
	if run {
		fmt.Printf("  Run locally: %s\n", ui.FormatEnabled())
	}
	if model != "" {
		fmt.Printf("  Model: %s\n", model)
	}
	if len(environment) > 0 {
		fmt.Printf("  Environment variables: %d configured\n", len(environment))
	}

	return nil
}

func updateAgent(cmd *cobra.Command, name, url, artifactsURL, oci string, run bool, model string, environment map[string]string) error {
	svc, err := getAgentsConfigService(cmd)
	if err != nil {
		return err
	}

	existing, err := svc.GetAgent(name)
	if err != nil {
		return err
	}

	agent := *existing
	if cmd.Flags().Changed("url") {
		agent.URL = url
	}
	if cmd.Flags().Changed("artifacts-url") {
		agent.ArtifactsURL = artifactsURL
	}
	if cmd.Flags().Changed("oci") {
		agent.OCI = oci
	}
	if cmd.Flags().Changed("run") {
		agent.Run = run
	}
	if cmd.Flags().Changed("model") {
		agent.Model = model
	}
	if cmd.Flags().Changed("environment") {
		agent.Environment = environment
	}

	if agent.Run && agent.Model == "" {
		return fmt.Errorf("--model is required when --run is enabled. Specify a model in the format provider/model (e.g., openai/gpt-5, anthropic/claude-4-5-sonnet)")
	}

	if err := svc.UpdateAgent(agent); err != nil {
		return err
	}

	fmt.Printf("%s Agent '%s' updated successfully\n", icons.CheckMarkStyle.Render(icons.CheckMark), name)
	fmt.Printf("  URL: %s\n", agent.URL)
	if agent.ArtifactsURL != "" {
		fmt.Printf("  Artifacts URL: %s\n", agent.ArtifactsURL)
	}
	if agent.OCI != "" {
		fmt.Printf("  OCI: %s\n", agent.OCI)
	}
	if agent.Run {
		fmt.Printf("  Run locally: %s\n", ui.FormatEnabled())
	}
	if agent.Model != "" {
		fmt.Printf("  Model: %s\n", agent.Model)
	}
	if len(agent.Environment) > 0 {
		fmt.Printf("  Environment variables: %d configured\n", len(agent.Environment))
	}

	return nil
}

func removeAgent(cmd *cobra.Command, name string) error {
	svc, err := getAgentsConfigService(cmd)
	if err != nil {
		return err
	}

	if err := svc.RemoveAgent(name); err != nil {
		return err
	}

	fmt.Printf("%s Agent '%s' removed successfully\n", icons.CheckMarkStyle.Render(icons.CheckMark), name)
	return nil
}

func listAgents(cmd *cobra.Command, args []string) error {
	svc, err := getAgentsConfigService(cmd)
	if err != nil {
		return err
	}

	agents, err := svc.ListAgents()
	if err != nil {
		return err
	}

	format, _ := cmd.Flags().GetString("format")

	if format == "json" {
		output, err := json.MarshalIndent(agents, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal agents: %w", err)
		}
		fmt.Println(string(output))
		return nil
	}

	if len(agents) == 0 {
		fmt.Println("No agents configured.")
		fmt.Println("Use 'infer agents add <name> <url>' to add an agent.")
		return nil
	}

	fmt.Printf("Configured A2A Agents (%d):\n\n", len(agents))
	for i, agent := range agents {
		fmt.Printf("%d. %s\n", i+1, agent.Name)
		fmt.Printf("   URL: %s\n", agent.URL)
		if agent.OCI != "" {
			fmt.Printf("   OCI: %s\n", agent.OCI)
		}
		if agent.Run {
			fmt.Printf("   Run locally: %s\n", ui.FormatEnabled())
		}
		if agent.Model != "" {
			fmt.Printf("   Model: %s\n", agent.Model)
		}
		if len(agent.Environment) > 0 {
			fmt.Printf("   Environment: %d variables configured\n", len(agent.Environment))
		}
		fmt.Println()
	}

	return nil
}

func showAgent(cmd *cobra.Command, name string) error {
	svc, err := getAgentsConfigService(cmd)
	if err != nil {
		return err
	}

	agent, err := svc.GetAgent(name)
	if err != nil {
		return err
	}

	format, _ := cmd.Flags().GetString("format")

	if format == "json" {
		output, err := json.MarshalIndent(agent, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal agent: %w", err)
		}
		fmt.Println(string(output))
		return nil
	}

	fmt.Printf("Agent: %s\n", agent.Name)
	fmt.Printf("URL: %s\n", agent.URL)
	if agent.OCI != "" {
		fmt.Printf("OCI: %s\n", agent.OCI)
	}
	fmt.Printf("Run locally: %v\n", agent.Run)
	if agent.Model != "" {
		fmt.Printf("Model: %s\n", agent.Model)
	}
	if len(agent.Environment) > 0 {
		fmt.Println("Environment variables:")
		for key, value := range agent.Environment {
			fmt.Printf("  %s=%s\n", key, value)
		}
	}

	return nil
}

func initAgents(cmd *cobra.Command, args []string) error {
	svc, err := getAgentsConfigService(cmd)
	if err != nil {
		return err
	}

	cfg := config.DefaultAgentsConfig()
	if err := svc.Save(cfg); err != nil {
		return err
	}

	userspace := GetUserspaceFlag(cmd)
	var scopeDesc string
	if userspace {
		scopeDesc = "userspace "
	}

	fmt.Printf("%s %sagents.yaml initialized successfully\n", icons.CheckMarkStyle.Render(icons.CheckMark), scopeDesc)
	return nil
}

func init() {
	agentsCmd.AddCommand(agentsAddCmd)
	agentsCmd.AddCommand(agentsUpdateCmd)
	agentsCmd.AddCommand(agentsListCmd)
	agentsCmd.AddCommand(agentsRemoveCmd)
	agentsCmd.AddCommand(agentsShowCmd)
	agentsCmd.AddCommand(agentsInitCmd)

	agentsAddCmd.Flags().String("oci", "", "OCI image reference for local execution")
	agentsAddCmd.Flags().String("artifacts-url", "", "Artifacts server URL")
	agentsAddCmd.Flags().Bool("run", false, "Run this agent locally with Docker")
	agentsAddCmd.Flags().String("model", "", "Model to use for the agent (format: provider/model)")
	agentsAddCmd.Flags().StringSlice("environment", []string{}, "Environment variables (KEY=VALUE)")

	agentsUpdateCmd.Flags().String("url", "", "Agent URL")
	agentsUpdateCmd.Flags().String("artifacts-url", "", "Artifacts server URL")
	agentsUpdateCmd.Flags().String("oci", "", "OCI image reference for local execution")
	agentsUpdateCmd.Flags().Bool("run", false, "Run this agent locally with Docker")
	agentsUpdateCmd.Flags().String("model", "", "Model to use for the agent (format: provider/model)")
	agentsUpdateCmd.Flags().StringSlice("environment", []string{}, "Environment variables (KEY=VALUE)")

	agentsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	agentsShowCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

	agentsCmd.PersistentFlags().Bool("userspace", false, "Use userspace configuration (~/.infer/) instead of project configuration")

	rootCmd.AddCommand(agentsCmd)
}
