package shortcuts

import (
	"context"
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/config"
	sdk "github.com/inference-gateway/sdk"
)

// A2AShortcut lists connected A2A servers
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

func (a *A2AShortcut) GetName() string               { return "a2a" }
func (a *A2AShortcut) GetDescription() string        { return "List connected A2A servers" }
func (a *A2AShortcut) GetUsage() string              { return "/a2a" }
func (a *A2AShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (a *A2AShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	var output strings.Builder
	output.WriteString("## A2A Server Status\n\n")

	gatewayURL := a.config.Gateway.URL
	if gatewayURL == "" {
		output.WriteString("❌ **Gateway URL not configured**\n\n")
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

	output.WriteString(fmt.Sprintf("🌐 **Gateway URL:** %s\n", gatewayURL))

	if a.config.Gateway.Middlewares.A2A {
		output.WriteString("✅ **A2A Middleware:** Enabled (tools execute on Gateway)\n")
	} else {
		output.WriteString("❌ **A2A Middleware:** Disabled (tools execute on client)\n")
	}

	output.WriteString("\n### A2A Server Information\n")
	output.WriteString("A2A (Agent-to-Agent) middleware is configured to handle tool execution on the Gateway.\n")
	output.WriteString("When enabled, A2A tools are centralized for shared access across all clients.\n\n")

	apiKey := a.config.Gateway.APIKey
	if apiKey == "" {
		output.WriteString("⚠️  **API Key:** Not configured - connection may fail\n")
	} else {
		output.WriteString("🔑 **API Key:** Configured\n")
	}

	timeout := a.config.Gateway.Timeout
	output.WriteString(fmt.Sprintf("⏱️  **Connection Timeout:** %d seconds\n", timeout))

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}

// MCPShortcut lists connected MCP servers
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
	output.WriteString("## MCP Server Status\n\n")

	gatewayURL := m.config.Gateway.URL
	if gatewayURL == "" {
		output.WriteString("❌ **Gateway URL not configured**\n\n")
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

	output.WriteString(fmt.Sprintf("🌐 **Gateway URL:** %s\n", gatewayURL))

	if m.config.Gateway.Middlewares.MCP {
		output.WriteString("✅ **MCP Middleware:** Enabled (tools execute on Gateway)\n")
	} else {
		output.WriteString("❌ **MCP Middleware:** Disabled (tools execute on client)\n")
	}

	output.WriteString("\n### MCP Server Information\n")
	output.WriteString("MCP (Model Context Protocol) middleware provides centralized tool and resource management.\n")
	output.WriteString("When enabled, MCP tools are shared across all clients with unified configurations.\n\n")

	apiKey := m.config.Gateway.APIKey
	if apiKey == "" {
		output.WriteString("⚠️  **API Key:** Not configured - connection may fail\n")
	} else {
		output.WriteString("🔑 **API Key:** Configured\n")
	}

	timeout := m.config.Gateway.Timeout
	output.WriteString(fmt.Sprintf("⏱️  **Connection Timeout:** %d seconds\n", timeout))

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}
