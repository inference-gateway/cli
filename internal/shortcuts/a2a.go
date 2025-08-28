package shortcuts

import (
	"context"
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
	"github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

type A2AShortcut struct {
	config *config.Config
	client sdk.Client
}

func NewA2AShortcut(cfg *config.Config, client sdk.Client) *A2AShortcut {
	return &A2AShortcut{
		config: cfg,
		client: client,
	}
}

func (a *A2AShortcut) GetName() string        { return "a2a" }
func (a *A2AShortcut) GetDescription() string { return "List connected A2A servers" }
func (a *A2AShortcut) GetUsage() string       { return "/a2a [list]" }
func (a *A2AShortcut) CanExecute(args []string) bool {
	return len(args) == 0 || (len(args) == 1 && args[0] == "list")
}

func (a *A2AShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 || (len(args) == 1 && args[0] == "list") {
		return a.listAgents(ctx)
	}

	return a.showStatus(ctx)
}

func (a *A2AShortcut) showStatus(ctx context.Context) (ShortcutResult, error) {
	var output strings.Builder
	header := colors.CreateColoredText("## A2A Server Status\n\n", colors.HeaderColor)
	output.WriteString(header)

	gatewayURL := a.config.Gateway.URL
	if gatewayURL == "" {
		errorIcon := icons.StyledCrossMark()
		output.WriteString(fmt.Sprintf("%s **Gateway URL not configured**\n\n", errorIcon))
		output.WriteString("Configure the gateway URL in your config:\n")
		output.WriteString("```yaml\n")
		output.WriteString("gateway:\n")
		output.WriteString("  url: http://your-gateway-url:8080\n")
		output.WriteString("```\n")

		return ShortcutResult{
			Output:  output.String(),
			Success: false,
		}, nil
	}

	networkLabel := colors.CreateColoredText("🌐", colors.AccentColor)
	output.WriteString(fmt.Sprintf("%s **Gateway URL:** %s\n", networkLabel, gatewayURL))

	if a.config.Gateway.Middlewares.A2A {
		successIcon := icons.StyledCheckMark()
		output.WriteString(fmt.Sprintf("%s **A2A Middleware:** Enabled (tools execute on Gateway)\n", successIcon))
	} else {
		errorIcon := icons.StyledCrossMark()
		output.WriteString(fmt.Sprintf("%s **A2A Middleware:** Disabled (tools execute on client)\n", errorIcon))
	}

	subheader := colors.CreateColoredText("\n### A2A Server Information\n", colors.StatusColor)
	output.WriteString(subheader)
	output.WriteString("A2A (Agent-to-Agent) middleware is configured to handle tool execution on the Gateway.\n")
	output.WriteString("When enabled, A2A tools are centralized for shared access across all clients.\n\n")

	apiKey := a.config.Gateway.APIKey
	keyLabel := colors.CreateColoredText("🔑", colors.WarningColor)
	if apiKey == "" {
		warningIcon := colors.CreateColoredText("⚠️", colors.WarningColor)
		output.WriteString(fmt.Sprintf("%s %s **API Key:** Not configured - connection may fail\n", warningIcon, keyLabel))
	} else {
		successIcon := icons.StyledCheckMark()
		output.WriteString(fmt.Sprintf("%s %s **API Key:** Configured\n", successIcon, keyLabel))
	}

	timeout := a.config.Gateway.Timeout
	timeLabel := colors.CreateColoredText("⏱️", colors.DimColor)
	output.WriteString(fmt.Sprintf("%s **Connection Timeout:** %d seconds\n", timeLabel, timeout))

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}

func (a *A2AShortcut) listAgents(ctx context.Context) (ShortcutResult, error) {
	var output strings.Builder
	header := colors.CreateColoredText("## A2A Agents\n\n", colors.HeaderColor)
	output.WriteString(header)

	if a.client == nil {
		errorIcon := icons.StyledCrossMark()
		output.WriteString(fmt.Sprintf("%s **Error:** SDK client not configured\n", errorIcon))
		return ShortcutResult{
			Output:  output.String(),
			Success: false,
		}, nil
	}

	agentsResp, err := a.client.ListAgents(ctx)
	if err != nil {
		errorIcon := icons.StyledCrossMark()
		output.WriteString(fmt.Sprintf("%s **Error fetching agents:** %v\n\n", errorIcon, err))
		output.WriteString("Make sure:\n")
		output.WriteString("- The Gateway is running and accessible\n")
		output.WriteString("- A2A middleware is exposed (EXPOSE_A2A=true)\n")
		output.WriteString("- Your API key is valid\n")
		return ShortcutResult{
			Output:  output.String(),
			Success: false,
		}, nil
	}

	if agentsResp == nil || len(agentsResp.Data) == 0 {
		warningIcon := colors.CreateColoredText("⚠️", colors.WarningColor)
		output.WriteString(fmt.Sprintf("%s No A2A agents available\n", warningIcon))
		return ShortcutResult{
			Output:  output.String(),
			Success: true,
		}, nil
	}

	output.WriteString(fmt.Sprintf("Found **%d** agents:\n\n", len(agentsResp.Data)))

	for i, agent := range agentsResp.Data {
		agentIcon := colors.CreateColoredText("🤖", colors.AccentColor)
		output.WriteString(fmt.Sprintf("%s **%s** (`%s`)\n", agentIcon, agent.Name, agent.Id))

		if agent.Description != "" {
			output.WriteString(fmt.Sprintf("   %s\n", agent.Description))
		}

		if agent.DocumentationUrl != nil && *agent.DocumentationUrl != "" {
			docsLabel := colors.CreateColoredText("📚", colors.DimColor)
			output.WriteString(fmt.Sprintf("   %s Docs: %s\n", docsLabel, *agent.DocumentationUrl))
		}

		if len(agent.DefaultInputModes) > 0 {
			inputLabel := colors.CreateColoredText("📥", colors.StatusColor)
			output.WriteString(fmt.Sprintf("   %s Input: %s\n", inputLabel, strings.Join(agent.DefaultInputModes, ", ")))
		}

		if len(agent.DefaultOutputModes) > 0 {
			outputLabel := colors.CreateColoredText("📤", colors.StatusColor)
			output.WriteString(fmt.Sprintf("   %s Output: %s\n", outputLabel, strings.Join(agent.DefaultOutputModes, ", ")))
		}

		if i < len(agentsResp.Data)-1 {
			output.WriteString("\n")
		}
	}

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}
