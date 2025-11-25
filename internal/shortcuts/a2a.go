package shortcuts

import (
	"context"
	"fmt"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

type A2AShortcut struct {
	config              *config.Config
	a2aAgentService     domain.A2AAgentService
	agentsConfigService AgentsConfigService
	agentManager        domain.AgentManager
}

func NewA2AShortcut(cfg *config.Config, a2aAgentService domain.A2AAgentService, agentsConfigService AgentsConfigService, agentManager domain.AgentManager) *A2AShortcut {
	return &A2AShortcut{
		config:              cfg,
		a2aAgentService:     a2aAgentService,
		agentsConfigService: agentsConfigService,
		agentManager:        agentManager,
	}
}

func (a *A2AShortcut) GetName() string { return "a2a" }
func (a *A2AShortcut) GetDescription() string {
	return "Manage A2A agent servers (list, add, remove)"
}
func (a *A2AShortcut) GetUsage() string {
	return "/a2a [list|add <name> [url] [--oci IMAGE] [--artifacts-url URL] [--run] [--model MODEL] [--environment KEY=VALUE ...]|remove <name>]"
}

func (a *A2AShortcut) CanExecute(args []string) bool {
	if len(args) == 0 || (len(args) == 1 && args[0] == "list") {
		return true
	}
	if len(args) >= 2 && args[0] == "add" {
		return true
	}
	if len(args) == 2 && args[0] == "remove" {
		return true
	}
	return false
}

func (a *A2AShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 || (len(args) == 1 && args[0] == "list") {
		return ShortcutResult{
			Output:     "Opening A2A agent servers view...",
			Success:    true,
			SideEffect: SideEffectShowA2AServers,
		}, nil
	}

	if args[0] == "add" {
		return a.executeAdd(ctx, args[1:])
	}

	if args[0] == "remove" {
		return a.executeRemove(ctx, args[1:])
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("Unknown subcommand: %s. Use '/a2a list', '/a2a add <name> <url>', or '/a2a remove <name>'", args[0]),
		Success: false,
	}, nil
}

// agentAddConfig holds the configuration for adding an agent
type agentAddConfig struct {
	name         string
	url          string
	oci          string
	artifactsURL string
	run          bool
	runSet       bool
	model        string
	modelSet     bool
	environment  map[string]string
}

// parseAgentAddArgs parses the arguments for adding an agent
func parseAgentAddArgs(args []string) (*agentAddConfig, int, error) {
	cfg := &agentAddConfig{
		name:        args[0],
		environment: make(map[string]string),
	}

	argsStartIdx := 1
	if len(args) > 1 && !strings.HasPrefix(args[1], "--") {
		cfg.url = args[1]
		argsStartIdx = 2
	}

	return cfg, argsStartIdx, nil
}

// parseAgentFlags parses command-line flags for agent configuration
func parseAgentFlags(cfg *agentAddConfig, args []string, startIdx int) error {
	for i := startIdx; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--oci" && i+1 < len(args):
			cfg.oci = args[i+1]
			i++
		case arg == "--artifacts-url" && i+1 < len(args):
			cfg.artifactsURL = args[i+1]
			i++
		case arg == "--run":
			cfg.run = true
			cfg.runSet = true
		case arg == "--model" && i+1 < len(args):
			cfg.model = args[i+1]
			cfg.modelSet = true
			i++
		case arg == "--environment" && i+1 < len(args):
			envPair := args[i+1]
			parts := strings.SplitN(envPair, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid environment variable format: %s (expected KEY=VALUE)", envPair)
			}
			cfg.environment[parts[0]] = parts[1]
			i++
		}
	}
	return nil
}

// applyAgentDefaults applies default values from known agent configuration
func applyAgentDefaults(cfg *agentAddConfig, defaults *config.AgentDefaults) {
	if defaults == nil {
		return
	}

	if cfg.url == "" {
		cfg.url = defaults.URL
	}
	if cfg.artifactsURL == "" && defaults.ArtifactsURL != "" {
		cfg.artifactsURL = defaults.ArtifactsURL
	}
	if cfg.oci == "" && defaults.OCI != "" {
		cfg.oci = defaults.OCI
	}
	if !cfg.runSet {
		cfg.run = defaults.Run
	}
	if !cfg.modelSet && defaults.Model != "" {
		cfg.model = defaults.Model
	}
}

// formatAgentAddResult formats the success message for adding an agent
func (a *A2AShortcut) formatAgentAddResult(agent config.AgentEntry) string {
	var details strings.Builder
	details.WriteString(fmt.Sprintf("Agent '%s' added successfully!\n", agent.Name))
	details.WriteString(fmt.Sprintf("• URL: %s\n", agent.URL))
	if agent.ArtifactsURL != "" {
		details.WriteString(fmt.Sprintf("• Artifacts URL: %s\n", agent.ArtifactsURL))
	}
	if agent.OCI != "" {
		details.WriteString(fmt.Sprintf("• OCI: %s\n", agent.OCI))
	}
	if agent.Run {
		details.WriteString("• Run locally: enabled\n")
		if a.agentManager != nil {
			details.WriteString("• Agent is starting...\n")
		}
	}
	if agent.Model != "" {
		details.WriteString(fmt.Sprintf("• Model: %s\n", agent.Model))
	}
	if len(agent.Environment) > 0 {
		details.WriteString(fmt.Sprintf("• Environment variables: %d configured\n", len(agent.Environment)))
	}
	return details.String()
}

func (a *A2AShortcut) executeAdd(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) < 1 {
		return ShortcutResult{
			Output:  "Usage: /a2a add <name> [url] [--oci IMAGE] [--artifacts-url URL] [--run] [--model MODEL] [--environment KEY=VALUE ...]",
			Success: false,
		}, nil
	}

	cfg, argsStartIdx, err := parseAgentAddArgs(args)
	if err != nil {
		return ShortcutResult{
			Output:  err.Error(),
			Success: false,
		}, nil
	}

	defaults := config.GetAgentDefaults(cfg.name)

	if cfg.url == "" && defaults == nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("URL is required for unknown agent '%s'. Known agents: %v", cfg.name, config.ListKnownAgents()),
			Success: false,
		}, nil
	}

	if err := parseAgentFlags(cfg, args, argsStartIdx); err != nil {
		return ShortcutResult{
			Output:  err.Error(),
			Success: false,
		}, nil
	}

	applyAgentDefaults(cfg, defaults)

	if cfg.run && cfg.model == "" {
		return ShortcutResult{
			Output:  "--model is required when --run is enabled. Specify a model in the format provider/model (e.g., openai/gpt-4, anthropic/claude-4-5-sonnet)",
			Success: false,
		}, nil
	}

	agent := config.AgentEntry{
		Name:         cfg.name,
		URL:          cfg.url,
		ArtifactsURL: cfg.artifactsURL,
		OCI:          cfg.oci,
		Run:          cfg.run,
		Model:        cfg.model,
		Environment:  cfg.environment,
	}

	if err := a.agentsConfigService.AddAgent(agent); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to add agent: %v", err),
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:     a.formatAgentAddResult(agent),
		Success:    true,
		SideEffect: SideEffectA2AAgentAdded,
		Data: map[string]any{
			"agent": agent,
			"start": cfg.run,
		},
	}, nil
}

func (a *A2AShortcut) executeRemove(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) < 1 {
		return ShortcutResult{
			Output:  "Usage: /a2a remove <name>",
			Success: false,
		}, nil
	}

	name := args[0]

	agent, err := a.agentsConfigService.GetAgent(name)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to find agent: %v", err),
			Success: false,
		}, nil
	}

	if agent.Run && a.agentManager != nil {
		if err := a.agentManager.StopAgent(ctx, name); err != nil {
			return ShortcutResult{
				Output:  fmt.Sprintf("Failed to stop running agent: %v", err),
				Success: false,
			}, nil
		}
	}

	if err := a.agentsConfigService.RemoveAgent(name); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to remove agent from config: %v", err),
			Success: false,
		}, nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Agent '%s' removed successfully!", name))

	if agent.Run {
		output.WriteString("\nAgent container stopped.")
	}

	return ShortcutResult{
		Output:     output.String(),
		Success:    true,
		SideEffect: SideEffectA2AAgentRemoved,
		Data:       name,
	}, nil
}

func (a *A2AShortcut) GetA2AAgentService() domain.A2AAgentService {
	return a.a2aAgentService
}
