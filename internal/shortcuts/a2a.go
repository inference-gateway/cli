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
	return "/a2a [list|add <name> <url> [--oci IMAGE] [--run] [--model MODEL] [--environment KEY=VALUE ...]|remove <name>]"
}

func (a *A2AShortcut) CanExecute(args []string) bool {
	if len(args) == 0 || (len(args) == 1 && args[0] == "list") {
		return true
	}
	if len(args) >= 3 && args[0] == "add" {
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

func (a *A2AShortcut) executeAdd(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) < 2 {
		return ShortcutResult{
			Output:  "Usage: /a2a add <name> <url> [--oci IMAGE] [--run] [--model MODEL] [--environment KEY=VALUE ...]",
			Success: false,
		}, nil
	}

	name := args[0]
	url := args[1]

	oci := ""
	run := false
	model := ""
	environment := make(map[string]string)

	for i := 2; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--oci" && i+1 < len(args):
			oci = args[i+1]
			i++
		case arg == "--run":
			run = true
		case arg == "--model" && i+1 < len(args):
			model = args[i+1]
			i++
		case arg == "--environment" && i+1 < len(args):
			envPair := args[i+1]
			parts := strings.SplitN(envPair, "=", 2)
			if len(parts) == 2 {
				environment[parts[0]] = parts[1]
			} else {
				return ShortcutResult{
					Output:  fmt.Sprintf("Invalid environment variable format: %s (expected KEY=VALUE)", envPair),
					Success: false,
				}, nil
			}
			i++
		}
	}

	if run && model == "" {
		return ShortcutResult{
			Output:  "--model is required when --run is enabled. Specify a model in the format provider/model (e.g., openai/gpt-4, anthropic/claude-3-5-sonnet)",
			Success: false,
		}, nil
	}

	agent := config.AgentEntry{
		Name:        name,
		URL:         url,
		OCI:         oci,
		Run:         run,
		Model:       model,
		Environment: environment,
	}

	if err := a.agentsConfigService.AddAgent(agent); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to add agent: %v", err),
			Success: false,
		}, nil
	}

	var details strings.Builder
	details.WriteString(fmt.Sprintf("Agent '%s' added successfully!\n", name))
	details.WriteString(fmt.Sprintf("• URL: %s\n", url))
	if oci != "" {
		details.WriteString(fmt.Sprintf("• OCI: %s\n", oci))
	}
	if run {
		details.WriteString("• Run locally: enabled\n")
		if a.agentManager != nil {
			details.WriteString("• Agent is starting...\n")
		}
	}
	if model != "" {
		details.WriteString(fmt.Sprintf("• Model: %s\n", model))
	}
	if len(environment) > 0 {
		details.WriteString(fmt.Sprintf("• Environment variables: %d configured\n", len(environment)))
	}

	return ShortcutResult{
		Output:     details.String(),
		Success:    true,
		SideEffect: SideEffectA2AAgentAdded,
		Data: map[string]any{
			"agent": agent,
			"start": run,
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
