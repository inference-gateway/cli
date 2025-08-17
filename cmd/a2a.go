package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/spf13/cobra"
)

var a2aCmd = &cobra.Command{
	Use:   "a2a",
	Short: "List A2A (Agent-to-Agent) connections",
	Long: `Display the current A2A agents connected to the inference gateway including:
- Agent IDs and names
- Connection status (unknown, available, degraded)
- Endpoint information
- Version details`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.Debug("Starting a2a command")
		fmt.Println("Fetching A2A agents...")

		configPath, _ := cmd.Flags().GetString("config")
		format, _ := cmd.Flags().GetString("format")

		logger.Debug("A2A command flags", "config_path", configPath, "format", format)

		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			logger.Error("Failed to load config in a2a command", "error", err)
			return fmt.Errorf("failed to load config: %w", err)
		}

		logger.Debug("Fetching A2A agents from gateway", "gateway_url", cfg.Gateway.URL)
		agents, err := fetchA2AAgents(cfg)
		if err != nil {
			logger.Warn("Gateway unreachable", "error", err)
			fmt.Printf("Gateway Status: Unreachable (%v)\n", err)
			fmt.Println("A2A Agents: Unable to connect")
			return nil
		}

		agentCount := len(agents)
		logger.Debug("Successfully fetched A2A agents", "count", agentCount)

		if format == "json" {
			return displayA2AAgentsJSON(agents)
		}

		return displayA2AAgentsText(agents)
	},
}

// fetchA2AAgents retrieves the list of available A2A agents from the gateway
func fetchA2AAgents(cfg *config.Config) ([]domain.A2AAgent, error) {
	logger.Debug("Creating service container")
	services := container.NewServiceContainer(cfg)

	timeout := time.Duration(cfg.Gateway.Timeout) * time.Second
	logger.Debug("Creating request context", "timeout", timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logger.Debug("Calling ListAgents API")
	agents, err := services.GetA2AService().ListAgents(ctx)
	if err != nil {
		logger.Error("ListAgents API call failed", "error", err)
		return nil, err
	}

	logger.Debug("ListAgents API call succeeded", "agents", agents)
	return agents, nil
}

// displayA2AAgentsText displays the agents in human-readable text format
func displayA2AAgentsText(agents []domain.A2AAgent) error {
	fmt.Printf("A2A Agents: %d connected\n\n", len(agents))

	if len(agents) == 0 {
		fmt.Println("No A2A agents are currently connected.")
		return nil
	}

	for i, agent := range agents {
		fmt.Printf("Agent %d:\n", i+1)
		fmt.Printf("  ID:       %s\n", agent.ID)
		fmt.Printf("  Name:     %s\n", agent.Name)
		fmt.Printf("  Status:   %s\n", formatAgentStatus(agent.Status))
		if agent.Endpoint != "" {
			fmt.Printf("  Endpoint: %s\n", agent.Endpoint)
		}
		if agent.Version != "" {
			fmt.Printf("  Version:  %s\n", agent.Version)
		}
		if i < len(agents)-1 {
			fmt.Println()
		}
	}

	return nil
}

// displayA2AAgentsJSON displays the agents in JSON format
func displayA2AAgentsJSON(agents []domain.A2AAgent) error {
	fmt.Printf("{\n")
	fmt.Printf("  \"total\": %d,\n", len(agents))
	fmt.Printf("  \"agents\": [\n")

	for i, agent := range agents {
		fmt.Printf("    {\n")
		fmt.Printf("      \"id\": \"%s\",\n", agent.ID)
		fmt.Printf("      \"name\": \"%s\",\n", agent.Name)
		fmt.Printf("      \"status\": \"%s\"", agent.Status)
		if agent.Endpoint != "" {
			fmt.Printf(",\n      \"endpoint\": \"%s\"", agent.Endpoint)
		}
		if agent.Version != "" {
			fmt.Printf(",\n      \"version\": \"%s\"", agent.Version)
		}
		fmt.Printf("\n    }")
		if i < len(agents)-1 {
			fmt.Printf(",")
		}
		fmt.Printf("\n")
	}

	fmt.Printf("  ]\n")
	fmt.Printf("}\n")
	return nil
}

// formatAgentStatus returns a colored/formatted version of the agent status
func formatAgentStatus(status domain.A2AAgentStatus) string {
	switch status {
	case domain.A2AAgentStatusAvailable:
		return "✓ Available"
	case domain.A2AAgentStatusDegraded:
		return "⚠ Degraded"
	case domain.A2AAgentStatusUnknown:
		return "? Unknown"
	default:
		return string(status)
	}
}

func init() {
	rootCmd.AddCommand(a2aCmd)

	a2aCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
}
