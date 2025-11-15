package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	cobra "github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Manage A2A agents",
	Long:  `Manage Agent-to-Agent (A2A) agents configuration. Add, remove, and control local Docker-based agents.`,
}

var agentsAddCmd = &cobra.Command{
	Use:   "add [NAME]",
	Short: "Add a new A2A agent",
	Long: `Add a new Agent-to-Agent agent to the configuration.
Agents can be configured to run locally with Docker or reference external endpoints.

Examples:
  # Add an external agent
  infer agents add my-agent --url https://agent.example.com

  # Add a local Docker-based agent
  infer agents add my-agent --url http://localhost:8080 --oci my-agent:latest --run

  # Add with environment variables
  infer agents add my-agent --url http://localhost:8080 --oci my-agent:latest --run --env API_KEY=secret --env DEBUG=true`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentsAdd,
}

var agentsRemoveCmd = &cobra.Command{
	Use:     "remove [NAME]",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove an A2A agent",
	Long:    `Remove an A2A agent from the configuration. If the agent is running locally, it will be stopped first.`,
	Args:    cobra.ExactArgs(1),
	RunE:    runAgentsRemove,
}

var agentsListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List configured A2A agents",
	Long:    `List all configured A2A agents and their current status.`,
	RunE:    runAgentsList,
}

var agentsStartCmd = &cobra.Command{
	Use:   "start [NAME]",
	Short: "Start a local A2A agent",
	Long:  `Start a local Docker-based A2A agent. The agent must be configured with 'run: true'.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentsStart,
}

var agentsStopCmd = &cobra.Command{
	Use:   "stop [NAME]",
	Short: "Stop a running local A2A agent",
	Long:  `Stop a running local Docker-based A2A agent.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentsStop,
}

var agentsStatusCmd = &cobra.Command{
	Use:   "status [NAME]",
	Short: "Get the status of an A2A agent",
	Long:  `Get the current status of an A2A agent, including whether it's running and health information.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentsStatus,
}

func init() {
	rootCmd.AddCommand(agentsCmd)

	agentsCmd.AddCommand(agentsAddCmd)
	agentsCmd.AddCommand(agentsRemoveCmd)
	agentsCmd.AddCommand(agentsListCmd)
	agentsCmd.AddCommand(agentsStartCmd)
	agentsCmd.AddCommand(agentsStopCmd)
	agentsCmd.AddCommand(agentsStatusCmd)

	// Add flags for agents add command
	agentsAddCmd.Flags().String("url", "", "URL where the agent is accessible (required)")
	agentsAddCmd.Flags().String("oci", "", "Docker image for local execution")
	agentsAddCmd.Flags().Bool("run", false, "Run the agent locally with Docker")
	agentsAddCmd.Flags().StringSlice("env", []string{}, "Environment variables (format: KEY=VALUE)")
	agentsAddCmd.Flags().String("description", "", "Brief description of the agent")
	agentsAddCmd.Flags().Bool("enabled", true, "Whether the agent is enabled")
	agentsAddCmd.Flags().Int("port", 0, "Port to expose the agent on (Docker)")
	agentsAddCmd.Flags().Int("host-port", 0, "Host port to bind to (Docker)")
	agentsAddCmd.Flags().StringSlice("volume", []string{}, "Volumes to mount (format: host:container)")
	agentsAddCmd.Flags().String("network", "", "Docker network mode")
	agentsAddCmd.Flags().String("restart", "unless-stopped", "Docker restart policy")

	// Mark required flags
	agentsAddCmd.MarkFlagRequired("url")

	// Add flags for agents list command
	agentsListCmd.Flags().Bool("json", false, "Output in JSON format")
	agentsListCmd.Flags().Bool("status", false, "Include status information")
}

func runAgentsAdd(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	// Get flag values
	url, _ := cmd.Flags().GetString("url")
	oci, _ := cmd.Flags().GetString("oci")
	run, _ := cmd.Flags().GetBool("run")
	envVars, _ := cmd.Flags().GetStringSlice("env")
	description, _ := cmd.Flags().GetString("description")
	enabled, _ := cmd.Flags().GetBool("enabled")
	port, _ := cmd.Flags().GetInt("port")
	hostPort, _ := cmd.Flags().GetInt("host-port")
	volumes, _ := cmd.Flags().GetStringSlice("volume")
	network, _ := cmd.Flags().GetString("network")
	restart, _ := cmd.Flags().GetString("restart")

	// Parse environment variables
	environment := make(map[string]string)
	for _, env := range envVars {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid environment variable format: %s (expected KEY=VALUE)", env)
		}
		environment[parts[0]] = parts[1]
	}

	// Create agent definition
	agent := domain.AgentDefinition{
		Name:        agentName,
		URL:         url,
		OCI:         oci,
		Run:         run,
		Environment: environment,
		Description: description,
		Enabled:     enabled,
		Metadata:    make(map[string]string),
	}

	// Add Docker configuration if needed
	if run || port > 0 || len(volumes) > 0 || network != "" || restart != "" {
		agent.Docker = &domain.AgentDockerConfig{
			Port:          port,
			HostPort:      hostPort,
			Volumes:       volumes,
			NetworkMode:   network,
			RestartPolicy: restart,
		}
	}

	// Initialize services
	ctx := context.Background()
	config := &config.Config{}
	if err := V.Unmarshal(config); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	dockerService := services.NewDockerService(ctx)
	agentConfigService := services.NewAgentConfigService(config, dockerService)

	// Add the agent
	if err := agentConfigService.AddAgent(agent); err != nil {
		return fmt.Errorf("failed to add agent: %w", err)
	}

	fmt.Printf("Successfully added agent '%s'\n", agentName)

	// If configured to run locally, offer to start it
	if run {
		fmt.Printf("Agent is configured for local execution. Use 'infer agents start %s' to start it.\n", agentName)
		if !dockerService.IsDockerAvailable() {
			fmt.Printf("Warning: Docker is not available on this system.\n")
		}
	}

	return nil
}

func runAgentsRemove(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	// Initialize services
	ctx := context.Background()
	config := &config.Config{}
	if err := V.Unmarshal(config); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	dockerService := services.NewDockerService(ctx)
	agentConfigService := services.NewAgentConfigService(config, dockerService)

	// Remove the agent
	if err := agentConfigService.RemoveAgent(agentName); err != nil {
		return fmt.Errorf("failed to remove agent: %w", err)
	}

	fmt.Printf("Successfully removed agent '%s'\n", agentName)
	return nil
}

func runAgentsList(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	includeStatus, _ := cmd.Flags().GetBool("status")

	// Initialize services
	ctx := context.Background()
	config := &config.Config{}
	if err := V.Unmarshal(config); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	dockerService := services.NewDockerService(ctx)
	agentConfigService := services.NewAgentConfigService(config, dockerService)

	// Get agents
	agents, err := agentConfigService.ListAgents()
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	if len(agents) == 0 {
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Println("No agents configured.")
		}
		return nil
	}

	if jsonOutput {
		return printAgentsJSON(agents, agentConfigService, includeStatus)
	}

	return printAgentsTable(agents, agentConfigService, includeStatus)
}

func runAgentsStart(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	// Initialize services
	ctx := context.Background()
	config := &config.Config{}
	if err := V.Unmarshal(config); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	dockerService := services.NewDockerService(ctx)
	agentConfigService := services.NewAgentConfigService(config, dockerService)

	// Start the agent
	if err := agentConfigService.StartAgent(agentName); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	fmt.Printf("Successfully started agent '%s'\n", agentName)
	return nil
}

func runAgentsStop(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	// Initialize services
	ctx := context.Background()
	config := &config.Config{}
	if err := V.Unmarshal(config); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	dockerService := services.NewDockerService(ctx)
	agentConfigService := services.NewAgentConfigService(config, dockerService)

	// Stop the agent
	if err := agentConfigService.StopAgent(agentName); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	fmt.Printf("Successfully stopped agent '%s'\n", agentName)
	return nil
}

func runAgentsStatus(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	// Initialize services
	ctx := context.Background()
	config := &config.Config{}
	if err := V.Unmarshal(config); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	dockerService := services.NewDockerService(ctx)
	agentConfigService := services.NewAgentConfigService(config, dockerService)

	// Get agent status
	status, err := agentConfigService.GetAgentStatus(agentName)
	if err != nil {
		return fmt.Errorf("failed to get agent status: %w", err)
	}

	// Print status
	fmt.Printf("Agent: %s\n", status.Name)
	fmt.Printf("URL: %s\n", status.URL)

	if status.Running {
		fmt.Printf("Status: Running\n")
	} else {
		fmt.Printf("Status: Stopped\n")
	}

	if status.Container != "" {
		fmt.Printf("Container: %s\n", status.Container)
	}

	if status.Health != "" && status.Health != "none" {
		fmt.Printf("Health: %s\n", status.Health)
	}

	return nil
}

func printAgentsJSON(agents []domain.AgentDefinition, agentConfigService domain.AgentConfigService, includeStatus bool) error {
	type agentInfo struct {
		domain.AgentDefinition
		Status *domain.AgentStatus `json:"status,omitempty"`
	}

	var agentList []agentInfo
	for _, agent := range agents {
		info := agentInfo{AgentDefinition: agent}

		if includeStatus {
			status, err := agentConfigService.GetAgentStatus(agent.Name)
			if err != nil {
				logger.Warn("Failed to get status for agent", "name", agent.Name, "error", err)
			} else {
				info.Status = &status
			}
		}

		agentList = append(agentList, info)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(agentList)
}

func printAgentsTable(agents []domain.AgentDefinition, agentConfigService domain.AgentConfigService, includeStatus bool) error {
	// Print header
	if includeStatus {
		fmt.Printf("%-20s %-10s %-8s %-50s %s\n", "NAME", "STATUS", "ENABLED", "URL", "DESCRIPTION")
		fmt.Printf("%-20s %-10s %-8s %-50s %s\n", "----", "------", "-------", "---", "-----------")
	} else {
		fmt.Printf("%-20s %-8s %-50s %s\n", "NAME", "ENABLED", "URL", "DESCRIPTION")
		fmt.Printf("%-20s %-8s %-50s %s\n", "----", "-------", "---", "-----------")
	}

	// Print agents
	for _, agent := range agents {
		enabledStr := "false"
		if agent.Enabled {
			enabledStr = "true"
		}

		if includeStatus {
			statusStr := "unknown"
			status, err := agentConfigService.GetAgentStatus(agent.Name)
			if err == nil {
				if status.Running {
					statusStr = "running"
				} else {
					statusStr = "stopped"
				}
			}

			fmt.Printf("%-20s %-10s %-8s %-50s %s\n",
				agent.Name, statusStr, enabledStr, agent.URL, agent.Description)
		} else {
			fmt.Printf("%-20s %-8s %-50s %s\n",
				agent.Name, enabledStr, agent.URL, agent.Description)
		}
	}

	return nil
}
