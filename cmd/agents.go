package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/inference-gateway/cli/config"
	"github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Manage A2A agent configurations",
	Long: `Manage A2A agent configurations using a simple agents.yaml file.
This provides an MCP-like experience for configuring agents.`,
}

var agentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		agentsConfig, err := config.LoadAgentsConfig()
		if err != nil {
			return fmt.Errorf("failed to load agents config: %w", err)
		}

		if len(agentsConfig.Agents) == 0 {
			fmt.Println("No agents configured")
			return nil
		}

		fmt.Printf("%-20s %-50s %-30s %-5s\n", "NAME", "URL", "OCI", "RUN")
		fmt.Println("================================================================================")
		for _, agent := range agentsConfig.Agents {
			oci := agent.OCI
			if oci == "" {
				oci = "-"
			}
			runStatus := "false"
			if agent.Run {
				runStatus = "true"
			}
			fmt.Printf("%-20s %-50s %-30s %-5s\n", agent.Name, agent.URL, oci, runStatus)
		}

		return nil
	},
}

var agentsAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a new agent configuration",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		url := args[1]

		oci, _ := cmd.Flags().GetString("oci")
		run, _ := cmd.Flags().GetBool("run")

		agentsConfig, err := config.LoadAgentsConfig()
		if err != nil {
			return fmt.Errorf("failed to load agents config: %w", err)
		}

		agent := config.A2AAgentConfig{
			Name: name,
			URL:  url,
			OCI:  oci,
			Run:  run,
		}

		if err := agentsConfig.AddAgent(agent); err != nil {
			return fmt.Errorf("failed to add agent: %w", err)
		}

		// Save to project-level config
		configPath := ".infer/agents.yaml"
		if err := config.SaveAgentsConfig(agentsConfig, configPath); err != nil {
			return fmt.Errorf("failed to save agents config: %w", err)
		}

		fmt.Printf("Agent '%s' added successfully\n", name)
		return nil
	},
}

var agentsRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an agent configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		agentsConfig, err := config.LoadAgentsConfig()
		if err != nil {
			return fmt.Errorf("failed to load agents config: %w", err)
		}

		if err := agentsConfig.RemoveAgent(name); err != nil {
			return fmt.Errorf("failed to remove agent: %w", err)
		}

		// Determine which config file to update
		configPath := ".infer/agents.yaml"
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get user home directory: %w", err)
			}
			configPath = filepath.Join(homeDir, ".infer", "agents.yaml")
		}

		if err := config.SaveAgentsConfig(agentsConfig, configPath); err != nil {
			return fmt.Errorf("failed to save agents config: %w", err)
		}

		fmt.Printf("Agent '%s' removed successfully\n", name)
		return nil
	},
}

var agentsUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update an existing agent configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		agentsConfig, err := config.LoadAgentsConfig()
		if err != nil {
			return fmt.Errorf("failed to load agents config: %w", err)
		}

		// Get current agent
		currentAgent, err := agentsConfig.GetAgentByName(name)
		if err != nil {
			return fmt.Errorf("agent not found: %w", err)
		}

		// Update fields that were provided
		updatedAgent := *currentAgent

		if cmd.Flags().Changed("url") {
			url, _ := cmd.Flags().GetString("url")
			updatedAgent.URL = url
		}
		if cmd.Flags().Changed("oci") {
			oci, _ := cmd.Flags().GetString("oci")
			updatedAgent.OCI = oci
		}
		if cmd.Flags().Changed("run") {
			run, _ := cmd.Flags().GetBool("run")
			updatedAgent.Run = run
		}

		if err := agentsConfig.UpdateAgent(name, updatedAgent); err != nil {
			return fmt.Errorf("failed to update agent: %w", err)
		}

		// Determine which config file to update
		configPath := ".infer/agents.yaml"
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get user home directory: %w", err)
			}
			configPath = filepath.Join(homeDir, ".infer", "agents.yaml")
		}

		if err := config.SaveAgentsConfig(agentsConfig, configPath); err != nil {
			return fmt.Errorf("failed to save agents config: %w", err)
		}

		fmt.Printf("Agent '%s' updated successfully\n", name)
		return nil
	},
}

func init() {
	// Add flags to add command
	agentsAddCmd.Flags().String("oci", "", "OCI container image reference")
	agentsAddCmd.Flags().Bool("run", false, "Whether the agent should be running locally")

	// Add flags to update command
	agentsUpdateCmd.Flags().String("url", "", "Agent URL")
	agentsUpdateCmd.Flags().String("oci", "", "OCI container image reference")
	agentsUpdateCmd.Flags().Bool("run", false, "Whether the agent should be running locally")

	// Build command hierarchy
	agentsCmd.AddCommand(agentsListCmd)
	agentsCmd.AddCommand(agentsAddCmd)
	agentsCmd.AddCommand(agentsRemoveCmd)
	agentsCmd.AddCommand(agentsUpdateCmd)

	rootCmd.AddCommand(agentsCmd)
}
