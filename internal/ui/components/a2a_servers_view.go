package components

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
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
	styleProvider   *styles.Provider
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
func NewA2AServersView(cfg *config.Config, a2aAgentService domain.A2AAgentService, styleProvider *styles.Provider) *A2AServersView {
	return &A2AServersView{
		config:          cfg,
		a2aAgentService: a2aAgentService,
		servers:         []A2AServerInfo{},
		width:           80,
		height:          20,
		styleProvider:   styleProvider,
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
	accentColor := v.styleProvider.GetThemeColor("accent")
	content := v.styleProvider.RenderWithColor("Loading A2A servers...", accentColor)
	return v.styleProvider.RenderCenteredBorderedBox(content, accentColor, v.width, v.height, 2, 4)
}

func (v *A2AServersView) renderError() string {
	errorColor := v.styleProvider.GetThemeColor("error")

	var content strings.Builder
	errorIcon := icons.StyledCrossMark()
	content.WriteString(fmt.Sprintf("%s %s\n\n", errorIcon, v.styleProvider.RenderWithColor("Error loading A2A servers", errorColor)))
	content.WriteString(fmt.Sprintf("%s\n\n", v.styleProvider.RenderDimText(v.error)))

	content.WriteString(fmt.Sprintf("%s\n", v.styleProvider.RenderDimText("Make sure:")))
	content.WriteString("• The Gateway is running and accessible\n")
	content.WriteString("• A2A middleware is exposed (EXPOSE_A2A=true)\n")
	content.WriteString("• Your API key is valid")

	return v.styleProvider.RenderLeftAlignedBorderedBox(content.String(), errorColor, v.width, v.height, 2, 4)
}

func (v *A2AServersView) renderEmpty() string {
	warningColor := v.styleProvider.GetThemeColor("warning")

	var content strings.Builder
	content.WriteString(fmt.Sprintf("%s\n\n", v.styleProvider.RenderWithColor("No A2A agents available", warningColor)))

	content.WriteString(fmt.Sprintf("%s\n\n", v.styleProvider.RenderDimText("Agents are starting or not configured in .infer/agents.yaml")))
	content.WriteString("Available A2A tools:\n")
	content.WriteString("• SubmitTask: Submit tasks to A2A agents\n")
	content.WriteString("• QueryTask: Query task status and results\n")
	content.WriteString("• QueryAgent: Query agent capabilities and information\n")
	content.WriteString("• DownloadArtifacts: Download artifacts from completed tasks")

	return v.styleProvider.RenderLeftAlignedBorderedBox(content.String(), warningColor, v.width, v.height, 2, 4)
}

func (v *A2AServersView) renderServers() string {
	accentColor := v.styleProvider.GetThemeColor("accent")
	successColor := v.styleProvider.GetThemeColor("success")

	var content strings.Builder

	content.WriteString(fmt.Sprintf("%s\n\n", v.styleProvider.RenderBold("Agent Servers")))
	content.WriteString(fmt.Sprintf("%s\n\n", v.styleProvider.RenderWithColor(fmt.Sprintf("Found %d agent card(s)", len(v.servers)), successColor)))

	for i, server := range v.servers {
		content.WriteString(v.renderSingleServer(server))
		if i < len(v.servers)-1 {
			content.WriteString("\n")
		}
	}

	content.WriteString(v.renderConnectionInfo())

	return v.styleProvider.RenderTopAlignedBorderedBox(content.String(), accentColor, v.width, v.height, 1, 2)
}

func (v *A2AServersView) renderSingleServer(server A2AServerInfo) string {
	successIcon := icons.StyledCheckMark()
	accentColor := v.styleProvider.GetThemeColor("accent")

	var content strings.Builder
	content.WriteString(fmt.Sprintf("%s %s\n",
		v.styleProvider.RenderBold(server.Name), successIcon))

	if server.Description != "" {
		content.WriteString(fmt.Sprintf("  %s\n", server.Description))
	}

	if server.DocumentsURL != nil && *server.DocumentsURL != "" {
		content.WriteString(fmt.Sprintf("  %s %s\n", v.styleProvider.RenderWithColor("Docs:", accentColor), *server.DocumentsURL))
	}

	if len(server.InputModes) > 0 {
		content.WriteString(fmt.Sprintf("  %s %s\n",
			v.styleProvider.RenderWithColor("Input:", accentColor), strings.Join(server.InputModes, ", ")))
	}

	if len(server.OutputModes) > 0 {
		content.WriteString(fmt.Sprintf("  %s %s\n",
			v.styleProvider.RenderWithColor("Output:", accentColor), strings.Join(server.OutputModes, ", ")))
	}

	if server.URL != "" {
		content.WriteString(fmt.Sprintf("  %s %s\n",
			v.styleProvider.RenderWithColor("URL:", accentColor), v.styleProvider.RenderDimText(server.URL)))
	}

	return content.String()
}

func (v *A2AServersView) renderConnectionInfo() string {
	var content strings.Builder
	content.WriteString("\n")
	content.WriteString(v.styleProvider.RenderDimText("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━") + "\n")

	content.WriteString("A2A Connection Mode\n")

	content.WriteString("\n")
	content.WriteString(v.styleProvider.RenderDimText("Press ESC to return to chat"))

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
