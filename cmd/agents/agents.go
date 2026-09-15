package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	agentapp "github.com/inference-gateway/cli/internal/agent/application"
	containerruntime "github.com/inference-gateway/cli/internal/platform/container"
)

type command struct {
	state    *runtime.State
	renderer *output.Renderer
}

// NewCommand constructs the agents command tree.
func NewCommand(state *runtime.State, renderer *output.Renderer) *cobra.Command {
	cmd := &command{state: state, renderer: renderer}
	agentsCmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage A2A agent configurations",
		Long: `Manage Agent-to-Agent (A2A) configurations stored in agents.yaml.
This allows you to configure remote or local agents that can be used for delegation.`,
	}

	agentsAddCmd := cmd.newAddCommand()
	agentsUpdateCmd := cmd.newUpdateCommand()
	agentsListCmd := cmd.newListCommand()
	agentsRemoveCmd := cmd.newRemoveCommand()
	agentsShowCmd := cmd.newShowCommand()
	agentsInitCmd := cmd.newInitCommand()
	agentsStatusCmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Probe configured A2A agents and report readiness",
		Long:  `Fetch each configured agent's card once (or only the named agent) and report which agents are reachable.`,
		Args:  cobra.MaximumNArgs(1),
		RunE:  cmd.agentsStatus,
	}

	agentsStartCmd := &cobra.Command{
		Use:   "start [name]",
		Short: "Start run:true agents as detached containers",
		Long:  `Start every run:true agent (or only the named one) as a detached container that outlives this command. Chat and headless sessions reuse it instead of starting their own.`,
		Args:  cobra.MaximumNArgs(1),
		RunE:  cmd.startAgents,
	}
	agentsStopCmd := &cobra.Command{
		Use:   "stop [name]",
		Short: "Stop detached agent containers",
		Args:  cobra.MaximumNArgs(1),
		RunE:  cmd.stopAgents,
	}

	agentsCmd.AddCommand(agentsAddCmd, agentsUpdateCmd, agentsListCmd, agentsRemoveCmd, agentsShowCmd, agentsInitCmd, agentsStatusCmd, agentsStartCmd, agentsStopCmd)

	agentsAddCmd.Flags().String("oci", "", "OCI image reference for local execution")
	agentsAddCmd.Flags().String("tag", "", "Image tag for the agent's default image (browser-agent: chromium, firefox, webkit, lightpanda)")
	agentsAddCmd.Flags().String("artifacts-url", "", "Artifacts server URL")
	agentsAddCmd.Flags().Bool("run", false, "Run this agent locally with Docker")
	agentsAddCmd.Flags().String("model", "", "Model to use for the agent (format: provider/model)")
	agentsAddCmd.Flags().StringSlice("environment", []string{}, "Environment variables (KEY=VALUE)")

	agentsUpdateCmd.Flags().String("url", "", "Agent URL")
	agentsUpdateCmd.Flags().String("artifacts-url", "", "Artifacts server URL")
	agentsUpdateCmd.Flags().String("oci", "", "OCI image reference for local execution")
	agentsUpdateCmd.Flags().String("tag", "", "Image tag for the agent's default image (browser-agent: chromium, firefox, webkit, lightpanda)")
	agentsUpdateCmd.Flags().Bool("run", false, "Run this agent locally with Docker")
	agentsUpdateCmd.Flags().String("model", "", "Model to use for the agent (format: provider/model)")
	agentsUpdateCmd.Flags().StringSlice("environment", []string{}, "Environment variables (KEY=VALUE)")

	agentsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	agentsShowCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	agentsStatusCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

	agentsCmd.PersistentFlags().Bool("project", false, "Apply to the project configuration (./.infer/) instead of the userspace baseline (~/.infer/)")

	return agentsCmd
}

func (c *command) newAddCommand() *cobra.Command { //nolint:gocognit
	return &cobra.Command{
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

  # Pin the image tag of a known agent (browser-agent ships one tag per browser engine)
  infer agents add browser-agent --tag lightpanda

  # Add a remote agent
  infer agents add code-reviewer https://agent.example.com

  # Add a local agent with OCI image
  infer agents add test-runner https://localhost:8081 --oci ghcr.io/org/test-runner:latest --run

  # Add agent with specific model
  infer agents add code-reviewer https://agent.example.com --run --model deepseek/deepseek-v4-pro

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

			taggedOCI, err := resolveTagFlag(cmd, name)
			if err != nil {
				return err
			}
			if taggedOCI != "" {
				oci = taggedOCI
			}

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

			//nolint:nestif // Agent metadata defaults and explicit flag overrides form one precedence block.
			if defaults != nil {
				if url == "" {
					url = defaults.URL
				}
				if !cmd.Flags().Changed("artifacts-url") && defaults.ArtifactsURL != "" {
					artifactsURL = defaults.ArtifactsURL
				}
				if oci == "" && defaults.OCI != "" {
					oci = defaults.OCI
				}
				if !cmd.Flags().Changed("run") {
					run = defaults.Run
				}
				if !cmd.Flags().Changed("model") && defaults.Model != "" {
					model = defaults.Model
				}
				if !cmd.Flags().Changed("environment") && defaults.Environment != nil {
					environment = defaults.Environment
				} else if cmd.Flags().Changed("environment") && defaults.Environment != nil {
					merged := make(map[string]string)
					for k, v := range defaults.Environment {
						merged[k] = v
					}
					for k, v := range environment {
						merged[k] = v
					}
					environment = merged
				}
			}

			return c.addAgent(cmd, name, url, artifactsURL, oci, run, model, environment)
		},
	}
}

func (c *command) newUpdateCommand() *cobra.Command {
	return &cobra.Command{
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

  # Switch to another image tag (browser-agent ships one tag per browser engine)
  infer agents update browser-agent --tag lightpanda

  # Add environment variables (replaces existing ones)
  infer agents update analyzer --environment CUSTOM_ENV=value --environment DEBUG=true`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if !cmd.Flags().Changed("url") && !cmd.Flags().Changed("artifacts-url") &&
				!cmd.Flags().Changed("oci") && !cmd.Flags().Changed("tag") &&
				!cmd.Flags().Changed("run") && !cmd.Flags().Changed("model") &&
				!cmd.Flags().Changed("environment") {
				return fmt.Errorf("at least one flag must be provided to update the agent")
			}

			url, _ := cmd.Flags().GetString("url")
			artifactsURL, _ := cmd.Flags().GetString("artifacts-url")
			oci, _ := cmd.Flags().GetString("oci")
			run, _ := cmd.Flags().GetBool("run")
			model, _ := cmd.Flags().GetString("model")
			envVars, _ := cmd.Flags().GetStringSlice("environment")

			taggedOCI, err := resolveTagFlag(cmd, name)
			if err != nil {
				return err
			}
			if taggedOCI != "" {
				oci = taggedOCI
			}

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

			return c.updateAgent(cmd, name, url, artifactsURL, oci, run, model, environment)
		},
	}
}

func (c *command) newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured A2A agents",
		Long:  `List all Agent-to-Agent (A2A) agents configured in agents.yaml.`,
		RunE:  c.listAgents,
	}
}

func (c *command) newRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an A2A agent configuration",
		Long:  `Remove an Agent-to-Agent (A2A) agent from the agents.yaml configuration.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.removeAgent(cmd, args[0])
		},
	}
}

func (c *command) newShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show details of a specific A2A agent",
		Long:  `Show detailed configuration of a specific Agent-to-Agent (A2A) agent.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.showAgent(cmd, args[0])
		},
	}
}

func (c *command) newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize the agents.yaml configuration file",
		Long:  `Initialize a new agents.yaml configuration file with default settings.`,
		RunE:  c.initAgents,
	}
}

// agentsConfigPath returns the agents.yaml path for the current command. Writes
// target the userspace baseline (~/.infer/) by default; --project targets the
// project .infer/agents.yaml instead.
func agentsConfigPath(cmd *cobra.Command) (string, error) {
	if runtime.ProjectFlag(cmd) {
		return config.DefaultAgentsPath, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, config.ConfigDirName, config.AgentsFileName), nil
}

// ExternalAgent represents an agent configured via INFER_A2A_AGENTS
type ExternalAgent struct {
	Name string
	URL  string
}

// extractExternalAgents extracts agent names and URLs from INFER_A2A_AGENTS
func extractExternalAgents(cfg *config.Config) []ExternalAgent {
	if len(cfg.A2A.Agents) == 0 {
		return nil
	}

	externalAgents := make([]ExternalAgent, 0, len(cfg.A2A.Agents))
	for _, agentURL := range cfg.A2A.Agents {
		name := extractAgentNameFromURL(agentURL)
		externalAgents = append(externalAgents, ExternalAgent{
			Name: name,
			URL:  agentURL,
		})
	}

	return externalAgents
}

// extractAgentNameFromURL extracts a display name from an agent URL
func extractAgentNameFromURL(url string) string {
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")

	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return url
	}

	hostPort := parts[0]
	host := strings.Split(hostPort, ":")[0]
	return host
}

// requiresModel reports whether the named agent needs a model when run locally.
// Known agents consult their metadata; unknown agents are presumed LLM-backed.
func requiresModel(name string, run bool) bool {
	return config.AgentRequiresModel(name, run)
}

// resolveTagFlag turns --tag into a full OCI reference against the agent's
// default image, or returns an empty string when the flag was not used.
func resolveTagFlag(cmd *cobra.Command, name string) (string, error) {
	if !cmd.Flags().Changed("tag") {
		return "", nil
	}
	if cmd.Flags().Changed("oci") {
		return "", fmt.Errorf("cannot combine --tag with --oci: --tag only replaces the tag of the agent's default image")
	}
	tag, _ := cmd.Flags().GetString("tag")
	return config.ResolveOCITag(name, tag)
}

func (c *command) addAgent(cmd *cobra.Command, name, url, artifactsURL, oci string, run bool, model string, environment map[string]string) error {
	if model == "" && requiresModel(name, run) {
		return fmt.Errorf("--model is required when --run is enabled. Specify a model in the format provider/model (e.g., openai/gpt-5, anthropic/claude-4-5-sonnet)")
	}

	path, err := agentsConfigPath(cmd)
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

	cfg, err := config.LoadAgents(path)
	if err != nil {
		return err
	}
	if err := cfg.CreateEntry(agent); err != nil {
		return err
	}

	fmt.Printf("%s Agent '%s' added successfully\n", c.renderer.StatusIcon(true), name)
	fmt.Printf("  URL: %s\n", url)
	if artifactsURL != "" {
		fmt.Printf("  Artifacts URL: %s\n", artifactsURL)
	}
	if oci != "" {
		fmt.Printf("  OCI: %s\n", oci)
	}
	if run {
		fmt.Printf("  Run locally: enabled\n")
	}
	if model != "" {
		fmt.Printf("  Model: %s\n", model)
	}
	if len(environment) > 0 {
		fmt.Printf("  Environment variables: %d configured\n", len(environment))
	}

	return nil
}

func (c *command) updateAgent(cmd *cobra.Command, name, url, artifactsURL, oci string, run bool, model string, environment map[string]string) error {
	path, err := agentsConfigPath(cmd)
	if err != nil {
		return err
	}

	cfg, err := config.LoadAgents(path)
	if err != nil {
		return err
	}
	existing, err := cfg.ReadEntry(name)
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
	if cmd.Flags().Changed("oci") || cmd.Flags().Changed("tag") {
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

	if agent.Model == "" && requiresModel(name, agent.Run) {
		return fmt.Errorf("--model is required when --run is enabled. Specify a model in the format provider/model (e.g., openai/gpt-5, anthropic/claude-4-5-sonnet)")
	}

	if err := cfg.UpdateEntry(agent); err != nil {
		return err
	}

	fmt.Printf("%s Agent '%s' updated successfully\n", c.renderer.StatusIcon(true), name)
	fmt.Printf("  URL: %s\n", agent.URL)
	if agent.ArtifactsURL != "" {
		fmt.Printf("  Artifacts URL: %s\n", agent.ArtifactsURL)
	}
	if agent.OCI != "" {
		fmt.Printf("  OCI: %s\n", agent.OCI)
	}
	if agent.Run {
		fmt.Printf("  Run locally: enabled\n")
	}
	if agent.Model != "" {
		fmt.Printf("  Model: %s\n", agent.Model)
	}
	if len(agent.Environment) > 0 {
		fmt.Printf("  Environment variables: %d configured\n", len(agent.Environment))
	}

	return nil
}

func (c *command) removeAgent(cmd *cobra.Command, name string) error {
	path, err := agentsConfigPath(cmd)
	if err != nil {
		return err
	}

	cfg, err := config.LoadAgents(path)
	if err != nil {
		return err
	}
	if err := cfg.DeleteEntry(name); err != nil {
		return err
	}

	fmt.Printf("%s Agent '%s' removed successfully\n", c.renderer.StatusIcon(true), name)
	return nil
}

func (c *command) listAgents(cmd *cobra.Command, _ []string) error {
	path, err := agentsConfigPath(cmd)
	if err != nil {
		return err
	}

	cfg, err := config.LoadAgents(path)
	if err != nil {
		return err
	}
	localAgents := cfg.ListEntries()

	externalAgents := extractExternalAgents(c.state.Config())

	totalAgents := len(localAgents) + len(externalAgents)

	format, _ := cmd.Flags().GetString("format")

	if format == "json" {
		combinedOutput := map[string]any{
			"local":    localAgents,
			"external": externalAgents,
			"total":    totalAgents,
		}
		output, err := json.MarshalIndent(combinedOutput, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal agents: %w", err)
		}
		fmt.Println(string(output))
		return nil
	}

	if totalAgents == 0 {
		fmt.Println("No agents configured.")
		fmt.Println("Use 'infer agents add <name> <url>' to add an agent or set INFER_A2A_AGENTS environment variable.")
		return nil
	}

	fmt.Println(c.renderer.Title(fmt.Sprintf("Configured A2A Agents (%d)", totalAgents)))
	fmt.Println(c.renderer.Hint(fmt.Sprintf("%d local, %d external", len(localAgents), len(externalAgents))))
	fmt.Println()

	agentsTable := c.renderer.NewListTable("Source", "Name", "URL", "OCI Image", "Local", "Model", "Env")
	for _, agent := range localAgents {
		oci := "-"
		if agent.OCI != "" {
			ociParts := strings.Split(agent.OCI, "/")
			oci = ociParts[len(ociParts)-1]
		}

		runLocally := "-"
		if agent.Run {
			runLocally = c.renderer.StatusIcon(true)
		}

		model := "-"
		if agent.Model != "" {
			model = agent.Model
		}

		envStr := "-"
		if len(agent.Environment) > 0 {
			envStr = fmt.Sprintf("%d", len(agent.Environment))
		}

		agentsTable.Row("yaml", agent.Name, agent.URL, oci, runLocally, model, envStr)
	}

	for _, agent := range externalAgents {
		agentsTable.Row("env", agent.Name, agent.URL, "-", "-", "-", "-")
	}
	fmt.Println(agentsTable.Render())

	fmt.Println()
	return nil
}

func (c *command) agentsStatus(cmd *cobra.Command, args []string) error {
	path, err := agentsConfigPath(cmd)
	if err != nil {
		return err
	}
	cfg, err := config.LoadAgents(path)
	if err != nil {
		return err
	}

	local := cfg.ListEntries()
	external := []string{}
	for _, agent := range extractExternalAgents(c.state.Config()) {
		external = append(external, agent.URL)
	}

	if len(args) == 1 {
		name := args[0]
		local = filterAgents(local, name)
		external = nil
		for _, agent := range extractExternalAgents(c.state.Config()) {
			if agent.Name == name {
				external = append(external, agent.URL)
			}
		}
		if len(local)+len(external) == 0 {
			return fmt.Errorf("agent %q not found", name)
		}
	}

	report := agentapp.ProbeAgents(context.Background(), local, external)

	format, _ := cmd.Flags().GetString("format")
	if format == "json" {
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal agents status: %w", err)
		}
		fmt.Println(string(out))
		return nil
	}

	fmt.Println(c.renderer.Title(fmt.Sprintf("A2A: %d/%d", report.ReadyAgents, report.TotalAgents)))
	if len(report.Agents) == 0 {
		fmt.Println("No agents configured.")
		return nil
	}
	fmt.Println()
	statusTable := c.renderer.NewListTable("Ready", "Name", "URL", "Error")
	for _, agent := range report.Agents {
		errText := agent.Error
		if errText == "" {
			errText = "-"
		}
		statusTable.Row(c.renderer.StatusIcon(agent.State == "Ready"), agent.Name, agent.URL, errText)
	}
	fmt.Println(statusTable.Render())
	return nil
}

func (c *command) startAgents(cmd *cobra.Command, args []string) error {
	cfg, agents, err := c.runnableAgents(cmd, args)
	if err != nil {
		return err
	}
	rt, err := c.sharedRuntime()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := rt.EnsureNetwork(ctx); err != nil {
		return fmt.Errorf("failed to create container network: %w", err)
	}
	manager := agentapp.NewAgentManager(containerruntime.SharedSessionID, c.state.Config(), cfg, rt, nil)
	var failed error
	for _, agent := range agents {
		if err := manager.StartAgent(ctx, agent); err != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", agent.Name, err))
			fmt.Printf("%s %s\n", c.renderer.StatusIcon(false), agent.Name)
			continue
		}
		fmt.Printf("%s %s %s\n", c.renderer.StatusIcon(true), agent.Name, agent.URL)
	}
	return failed
}

func (c *command) stopAgents(cmd *cobra.Command, args []string) error {
	cfg, agents, err := c.runnableAgents(cmd, args)
	if err != nil {
		return err
	}
	rt, err := c.sharedRuntime()
	if err != nil {
		return err
	}
	manager := agentapp.NewAgentManager(containerruntime.SharedSessionID, c.state.Config(), cfg, rt, nil)
	for _, agent := range agents {
		if err := manager.StopAgentByName(context.Background(), agent.Name); err != nil {
			return fmt.Errorf("%s: %w", agent.Name, err)
		}
		fmt.Printf("%s %s stopped\n", c.renderer.StatusIcon(true), agent.Name)
	}
	return nil
}

// runnableAgents returns the run:true agents, narrowed to args[0] when given.
func (c *command) runnableAgents(cmd *cobra.Command, args []string) (*config.AgentsConfig, []config.AgentEntry, error) {
	path, err := agentsConfigPath(cmd)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := config.LoadAgents(path)
	if err != nil {
		return nil, nil, err
	}
	var agents []config.AgentEntry
	for _, agent := range cfg.ListEntries() {
		if len(args) == 1 && agent.Name != args[0] {
			continue
		}
		if !agent.Run {
			if len(args) == 1 {
				return nil, nil, fmt.Errorf("agent %q is not a run: true agent", args[0])
			}
			continue
		}
		agents = append(agents, agent)
	}
	if len(args) == 1 && len(agents) == 0 {
		return nil, nil, fmt.Errorf("agent %q not found", args[0])
	}
	return cfg, agents, nil
}

func (c *command) sharedRuntime() (containerruntime.ContainerRuntime, error) {
	rt, err := containerruntime.NewContainerRuntime(containerruntime.SharedSessionID, containerruntime.RuntimeType(c.state.Config().ContainerRuntime.Type))
	if err != nil {
		return nil, err
	}
	if rt == nil {
		return nil, fmt.Errorf("no container runtime configured (set container_runtime.type to docker or podman)")
	}
	return rt, nil
}

func filterAgents(agents []config.AgentEntry, name string) []config.AgentEntry {
	for _, agent := range agents {
		if agent.Name == name {
			return []config.AgentEntry{agent}
		}
	}
	return nil
}

func (c *command) showAgent(cmd *cobra.Command, name string) error {
	path, err := agentsConfigPath(cmd)
	if err != nil {
		return err
	}

	cfg, err := config.LoadAgents(path)
	if err != nil {
		return err
	}
	agent, err := cfg.ReadEntry(name)
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

	fmt.Println(c.renderer.Title(fmt.Sprintf("Agent: %s", agent.Name)))
	fmt.Println()

	fmt.Println(c.renderer.Field("URL", agent.URL))

	if agent.ArtifactsURL != "" {
		fmt.Println(c.renderer.Field("Artifacts URL", agent.ArtifactsURL))
	}

	if agent.OCI != "" {
		fmt.Println(c.renderer.Field("OCI", agent.OCI))
	}

	runLocally := c.renderer.StatusIcon(false)
	if agent.Run {
		runLocally = c.renderer.StatusIcon(true)
	}
	fmt.Println(c.renderer.Field("Run Locally", runLocally))

	if agent.Model != "" {
		fmt.Println(c.renderer.Field("Model", agent.Model))
	}

	if len(agent.Environment) > 0 {
		fmt.Println()
		fmt.Println(c.renderer.Title("Environment Variables"))
		fmt.Println()
		envTable := c.renderer.NewListTable("Variable", "Value")
		for key, value := range agent.Environment {
			envTable.Row(key, value)
		}
		fmt.Println(envTable.Render())
	}

	return nil
}

func (c *command) initAgents(cmd *cobra.Command, _ []string) error {
	path, err := agentsConfigPath(cmd)
	if err != nil {
		return err
	}

	if err := config.SaveAgents(path, config.DefaultAgentsConfig()); err != nil {
		return err
	}

	scopeDesc := "userspace "
	if runtime.ProjectFlag(cmd) {
		scopeDesc = "project "
	}

	fmt.Printf("%s %sagents.yaml initialized successfully\n", c.renderer.StatusIcon(true), scopeDesc)
	return nil
}
