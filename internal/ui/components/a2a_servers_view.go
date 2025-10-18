package components

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// A2AServersView displays connected A2A servers in a dedicated view component
type A2AServersView struct {
	config          *config.Config
	a2aAgentService domain.A2AAgentService
	servers         []A2AServerInfo
	width           int
	height          int
	isLoading       bool
	error           string
	themeService    domain.ThemeService
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
	URL            string
}

// NewA2AServersView creates a new A2A servers view
func NewA2AServersView(cfg *config.Config, a2aAgentService domain.A2AAgentService, themeService domain.ThemeService) *A2AServersView {
	return &A2AServersView{
		config:          cfg,
		a2aAgentService: a2aAgentService,
		servers:         []A2AServerInfo{},
		width:           80,
		height:          20,
		themeService:    themeService,
	}
}

func (v *A2AServersView) SetWidth(width int) {
	v.width = width
}

func (v *A2AServersView) SetHeight(height int) {
	v.height = height
}

func (v *A2AServersView) LoadServers(ctx context.Context) tea.Cmd {
	v.isLoading = true
	v.error = ""

	return func() tea.Msg {
		if v.a2aAgentService == nil {
			return A2AServersLoadedMsg{
				servers: []A2AServerInfo{},
				error:   "A2A agent service not configured",
			}
		}

		cards, err := v.a2aAgentService.GetAgentCards(ctx)
		if err != nil {
			return A2AServersLoadedMsg{
				servers: []A2AServerInfo{},
				error:   fmt.Sprintf("Failed to fetch agent cards: %v", err),
			}
		}

		var servers []A2AServerInfo
		for _, cached := range cards {
			if cached.Card != nil {
				server := A2AServerInfo{
					ID:             cached.Card.URL,
					Name:           cached.Card.Name,
					Description:    cached.Card.Description,
					DocumentsURL:   cached.Card.DocumentationURL,
					InputModes:     cached.Card.DefaultInputModes,
					OutputModes:    cached.Card.DefaultOutputModes,
					IsConnected:    true,
					ConnectionInfo: "A2A Connection",
					URL:            cached.URL,
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
	content := headerColor + "Loading A2A servers..." + colors.Reset

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
	content.WriteString(fmt.Sprintf("%sNo A2A agent servers configured%s\n\n", warningColor, colors.Reset))

	content.WriteString(fmt.Sprintf("%sConfigure agents in your config or via environment variables.%s\n\n", dimColor, colors.Reset))
	content.WriteString("Available A2A tools:\n")
	content.WriteString("• **Task**: Submit tasks to A2A agents\n")
	content.WriteString("• **Query**: Query A2A agent information")

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

	content.WriteString(fmt.Sprintf("%s## A2A Agent Servers%s\n\n", headerColor, colors.Reset))
	content.WriteString(fmt.Sprintf("%sFound %d agent cards:%s\n\n", successColor, len(v.servers), colors.Reset))

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
	successIcon := icons.StyledCheckMark()
	dimColor := v.getDimColor()
	statusColor := v.getStatusColor()

	var content strings.Builder
	content.WriteString(fmt.Sprintf("**%s** (%s%s%s) %s\n",
		server.Name, dimColor, server.ID, colors.Reset, successIcon))

	if server.Description != "" {
		content.WriteString(fmt.Sprintf("   %s\n", server.Description))
	}

	if server.DocumentsURL != nil && *server.DocumentsURL != "" {
		content.WriteString(fmt.Sprintf("   Docs: %s\n", *server.DocumentsURL))
	}

	if len(server.InputModes) > 0 {
		content.WriteString(fmt.Sprintf("   Input: %s%s%s\n",
			statusColor, strings.Join(server.InputModes, ", "), colors.Reset))
	}

	if len(server.OutputModes) > 0 {
		content.WriteString(fmt.Sprintf("   Output: %s%s%s\n",
			statusColor, strings.Join(server.OutputModes, ", "), colors.Reset))
	}

	if server.URL != "" {
		content.WriteString(fmt.Sprintf("   URL: %s%s%s\n",
			dimColor, server.URL, colors.Reset))
	}

	return content.String()
}

func (v *A2AServersView) renderConnectionInfo() string {
	dimColor := v.getDimColor()

	var content strings.Builder
	content.WriteString("\n")
	content.WriteString(fmt.Sprintf("%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", dimColor, colors.Reset))

	content.WriteString(fmt.Sprintf("A2A Connection Mode%s\n", colors.Reset))

	content.WriteString("\n")
	content.WriteString(fmt.Sprintf("%sPress ESC to return to chat%s", dimColor, colors.Reset))

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
