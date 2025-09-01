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

type MCPShortcut struct {
	config *config.Config
	client sdk.Client
}

func NewMCPShortcut(cfg *config.Config, client sdk.Client) *MCPShortcut {
	return &MCPShortcut{
		config: cfg,
		client: client,
	}
}

func (m *MCPShortcut) GetName() string               { return "mcp" }
func (m *MCPShortcut) GetDescription() string        { return "List connected MCP servers" }
func (m *MCPShortcut) GetUsage() string              { return "/mcp" }
func (m *MCPShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (m *MCPShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	var output strings.Builder
	header := colors.CreateColoredText("## MCP Server Status\n\n", colors.HeaderColor)
	output.WriteString(header)

	gatewayURL := m.config.Gateway.URL
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

	if m.config.Gateway.Middlewares.MCP {
		successIcon := icons.StyledCheckMark()
		output.WriteString(fmt.Sprintf("%s **MCP Middleware:** Enabled (tools execute on Gateway)\n", successIcon))
	} else {
		errorIcon := icons.StyledCrossMark()
		output.WriteString(fmt.Sprintf("%s **MCP Middleware:** Disabled (tools execute on client)\n", errorIcon))
	}

	subheader := colors.CreateColoredText("\n### MCP Server Information\n", colors.StatusColor)
	output.WriteString(subheader)
	output.WriteString("MCP (Model Context Protocol) middleware provides centralized tool and resource management.\n")
	output.WriteString("When enabled, MCP tools are shared across all clients with unified configurations.\n\n")

	apiKey := m.config.Gateway.APIKey
	keyLabel := colors.CreateColoredText("🔑", colors.WarningColor)
	if apiKey == "" {
		warningIcon := colors.CreateColoredText("⚠️", colors.WarningColor)
		output.WriteString(fmt.Sprintf("%s %s **API Key:** Not configured - connection may fail\n", warningIcon, keyLabel))
	} else {
		successIcon := icons.StyledCheckMark()
		output.WriteString(fmt.Sprintf("%s %s **API Key:** Configured\n", successIcon, keyLabel))
	}

	timeout := m.config.Gateway.Timeout
	timeLabel := colors.CreateColoredText("⏱️", colors.DimColor)
	output.WriteString(fmt.Sprintf("%s **Connection Timeout:** %d seconds\n", timeLabel, timeout))

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}
