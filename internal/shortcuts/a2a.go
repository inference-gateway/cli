package shortcuts

import (
	"context"
	"fmt"
	"strings"

	config "github.com/inference-gateway/cli/config"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
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
func (a *A2AShortcut) GetClient() sdk.Client  { return a.client }
func (a *A2AShortcut) CanExecute(args []string) bool {
	return len(args) == 0 || (len(args) == 1 && args[0] == "list")
}

func (a *A2AShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 1 && args[0] == "list" {
		return ShortcutResult{
			Output:     "Opening A2A servers view...",
			Success:    true,
			SideEffect: SideEffectShowA2AServers,
		}, nil
	}

	if len(args) == 0 {
		return ShortcutResult{
			Output:     "Opening A2A servers view...",
			Success:    true,
			SideEffect: SideEffectShowA2AServers,
		}, nil
	}

	return a.showStatus(ctx)
}

func (a *A2AShortcut) showStatus(_ context.Context) (ShortcutResult, error) {
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
