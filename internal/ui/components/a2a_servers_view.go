package components

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
	"github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

// A2AServersView displays connected A2A servers in a dedicated view component
type A2AServersView struct {
	config       *config.Config
	client       sdk.Client
	servers      []A2AServerInfo
	width        int
	height       int
	isLoading    bool
	error        string
	themeService domain.ThemeService
}

// A2AServerInfo represents information about an A2A server
type A2AServerInfo struct {
	ID             string
	Name           string
	Description    string
	DocumentsURL   *string
	InputModes     []string
	OutputModes    []string
	IsConnected    bool
	ConnectionInfo string
}

// NewA2AServersView creates a new A2A servers view
func NewA2AServersView(cfg *config.Config, client sdk.Client, themeService domain.ThemeService) *A2AServersView {
	return &A2AServersView{
		config:       cfg,
		client:       client,
		servers:      []A2AServerInfo{},
		width:        80,
		height:       20,
		themeService: themeService,
	}
}

func (v *A2AServersView) SetWidth(width int) {
	v.width = width
}

func (v *A2AServersView) SetHeight(height int) {
	v.height = height
}

func (v *A2AServersView) LoadServers(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		v.isLoading = true
		v.error = ""

		if v.client == nil {
			return A2AServersLoadedMsg{
				servers: []A2AServerInfo{},
				error:   "SDK client not configured",
			}
		}

		agentsResp, err := v.client.ListAgents(ctx)
		if err != nil {
			return A2AServersLoadedMsg{
				servers: []A2AServerInfo{},
				error:   fmt.Sprintf("Error fetching agents: %v", err),
			}
		}

		var servers []A2AServerInfo
		if agentsResp != nil && len(agentsResp.Data) > 0 {
			for _, agent := range agentsResp.Data {
				server := A2AServerInfo{
					ID:             agent.Id,
					Name:           agent.Name,
					Description:    agent.Description,
					DocumentsURL:   agent.DocumentationUrl,
					InputModes:     agent.DefaultInputModes,
					OutputModes:    agent.DefaultOutputModes,
					IsConnected:    true,
					ConnectionInfo: "Connected via Gateway",
				}
				servers = append(servers, server)
			}
		}

		return A2AServersLoadedMsg{
			servers: servers,
			error:   "",
		}
	}
}

func (v *A2AServersView) Render() string {
	if v.isLoading {
		return v.renderLoading()
	}

	if v.error != "" {
		return v.renderError()
	}

	if len(v.servers) == 0 {
		return v.renderEmpty()
	}

	return v.renderServers()
}

func (v *A2AServersView) renderLoading() string {
	headerColor := v.getHeaderColor()
	content := headerColor + "🔍 Loading A2A servers..." + colors.Reset

	style := lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Align(lipgloss.Center, lipgloss.Center).
		Border(lipgloss.RoundedBorder(), true).
		BorderForeground(lipgloss.Color(v.getAccentColor())).
		Padding(2, 4)

	return style.Render(content)
}

func (v *A2AServersView) renderError() string {
	errorColor := v.getErrorColor()
	dimColor := v.getDimColor()

	var content strings.Builder
	errorIcon := icons.StyledCrossMark()
	content.WriteString(fmt.Sprintf("%s %sError loading A2A servers%s\n\n", errorIcon, errorColor, colors.Reset))
	content.WriteString(fmt.Sprintf("%s%s%s\n\n", dimColor, v.error, colors.Reset))

	content.WriteString(fmt.Sprintf("%sMake sure:%s\n", dimColor, colors.Reset))
	content.WriteString("• The Gateway is running and accessible\n")
	content.WriteString("• A2A middleware is exposed (EXPOSE_A2A=true)\n")
	content.WriteString("• Your API key is valid")

	style := lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Align(lipgloss.Left, lipgloss.Center).
		Border(lipgloss.RoundedBorder(), true).
		BorderForeground(lipgloss.Color(v.getErrorColor())).
		Padding(2, 4)

	return style.Render(content.String())
}

func (v *A2AServersView) renderEmpty() string {
	warningColor := v.getWarningColor()
	dimColor := v.getDimColor()

	var content strings.Builder
	warningIcon := colors.CreateColoredText("⚠️", colors.WarningColor)
	content.WriteString(fmt.Sprintf("%s %sNo A2A servers available%s\n\n", warningIcon, warningColor, colors.Reset))

	content.WriteString(fmt.Sprintf("%sThis could mean:%s\n", dimColor, colors.Reset))
	content.WriteString("• No agents are registered with the Gateway\n")
	content.WriteString("• A2A middleware is not properly configured\n")
	content.WriteString("• Connection issues with the Gateway")

	style := lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Align(lipgloss.Left, lipgloss.Center).
		Border(lipgloss.RoundedBorder(), true).
		BorderForeground(lipgloss.Color(v.getWarningColor())).
		Padding(2, 4)

	return style.Render(content.String())
}

func (v *A2AServersView) renderServers() string {
	headerColor := v.getHeaderColor()
	successColor := v.getSuccessColor()

	var content strings.Builder

	content.WriteString(fmt.Sprintf("%s## A2A Connected Servers%s\n\n", headerColor, colors.Reset))
	content.WriteString(fmt.Sprintf("%sFound %d connected agents:%s\n\n", successColor, len(v.servers), colors.Reset))

	for i, server := range v.servers {
		content.WriteString(v.renderSingleServer(server))
		if i < len(v.servers)-1 {
			content.WriteString("\n")
		}
	}

	content.WriteString(v.renderConnectionInfo())

	style := lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		Align(lipgloss.Left, lipgloss.Top).
		Border(lipgloss.RoundedBorder(), true).
		BorderForeground(lipgloss.Color(v.getAccentColor())).
		Padding(1, 2)

	return style.Render(content.String())
}

func (v *A2AServersView) renderSingleServer(server A2AServerInfo) string {
	agentIcon := colors.CreateColoredText("🤖", colors.AccentColor)
	successIcon := icons.StyledCheckMark()
	dimColor := v.getDimColor()
	statusColor := v.getStatusColor()

	var content strings.Builder
	content.WriteString(fmt.Sprintf("%s **%s** (%s%s%s) %s\n",
		agentIcon, server.Name, dimColor, server.ID, colors.Reset, successIcon))

	if server.Description != "" {
		content.WriteString(fmt.Sprintf("   %s\n", server.Description))
	}

	if server.DocumentsURL != nil && *server.DocumentsURL != "" {
		docsIcon := colors.CreateColoredText("📚", colors.DimColor)
		content.WriteString(fmt.Sprintf("   %s Docs: %s\n", docsIcon, *server.DocumentsURL))
	}

	if len(server.InputModes) > 0 {
		inputIcon := colors.CreateColoredText("📥", colors.StatusColor)
		content.WriteString(fmt.Sprintf("   %s Input: %s%s%s\n",
			inputIcon, statusColor, strings.Join(server.InputModes, ", "), colors.Reset))
	}

	if len(server.OutputModes) > 0 {
		outputIcon := colors.CreateColoredText("📤", colors.StatusColor)
		content.WriteString(fmt.Sprintf("   %s Output: %s%s%s\n",
			outputIcon, statusColor, strings.Join(server.OutputModes, ", "), colors.Reset))
	}

	return content.String()
}

func (v *A2AServersView) renderConnectionInfo() string {
	dimColor := v.getDimColor()
	accentColor := v.getAccentColor()

	var content strings.Builder
	content.WriteString("\n")
	content.WriteString(fmt.Sprintf("%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", dimColor, colors.Reset))

	gatewayURL := v.config.Gateway.URL
	networkIcon := colors.CreateColoredText("🌐", colors.AccentColor)
	content.WriteString(fmt.Sprintf("%s Gateway: %s%s%s\n", networkIcon, accentColor, gatewayURL, colors.Reset))

	if v.config.Gateway.Middlewares.A2A {
		successIcon := icons.StyledCheckMark()
		content.WriteString(fmt.Sprintf("%s A2A Middleware: Enabled\n", successIcon))
	} else {
		errorIcon := icons.StyledCrossMark()
		content.WriteString(fmt.Sprintf("%s A2A Middleware: Disabled\n", errorIcon))
	}

	return content.String()
}

// Bubble Tea interface
func (v *A2AServersView) Init() tea.Cmd {
	return nil
}

func (v *A2AServersView) View() string {
	return v.Render()
}

func (v *A2AServersView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case A2AServersLoadedMsg:
		v.isLoading = false
		v.servers = msg.servers
		v.error = msg.error
	case tea.WindowSizeMsg:
		v.SetWidth(msg.Width)
		v.SetHeight(msg.Height)
	}
	return v, nil
}

// A2AServersLoadedMsg represents the result of loading A2A servers
type A2AServersLoadedMsg struct {
	servers []A2AServerInfo
	error   string
}

// Helper methods to get theme colors with fallbacks
func (v *A2AServersView) getHeaderColor() string {
	if v.themeService != nil {
		return v.themeService.GetCurrentTheme().GetAccentColor()
	}
	return colors.HeaderColor.ANSI
}

func (v *A2AServersView) getSuccessColor() string {
	if v.themeService != nil {
		return v.themeService.GetCurrentTheme().GetStatusColor()
	}
	return colors.SuccessColor.ANSI
}

func (v *A2AServersView) getErrorColor() string {
	if v.themeService != nil {
		return v.themeService.GetCurrentTheme().GetErrorColor()
	}
	return colors.ErrorColor.ANSI
}

func (v *A2AServersView) getWarningColor() string {
	if v.themeService != nil {
		return v.themeService.GetCurrentTheme().GetErrorColor()
	}
	return colors.WarningColor.ANSI
}

func (v *A2AServersView) getAccentColor() string {
	if v.themeService != nil {
		return v.themeService.GetCurrentTheme().GetAccentColor()
	}
	return colors.AccentColor.ANSI
}

func (v *A2AServersView) getDimColor() string {
	if v.themeService != nil {
		return v.themeService.GetCurrentTheme().GetDimColor()
	}
	return colors.DimColor.ANSI
}

func (v *A2AServersView) getStatusColor() string {
	if v.themeService != nil {
		return v.themeService.GetCurrentTheme().GetStatusColor()
	}
	return colors.StatusColor.ANSI
}
