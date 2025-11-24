package components

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// AgentReadinessView displays the agent readiness status indicator
type AgentReadinessView struct {
	stateManager  domain.StateManager
	styleProvider *styles.Provider
	width         int
}

// NewAgentReadinessView creates a new agent readiness view
func NewAgentReadinessView(stateManager domain.StateManager, styleProvider *styles.Provider) *AgentReadinessView {
	return &AgentReadinessView{
		stateManager:  stateManager,
		styleProvider: styleProvider,
	}
}

// SetWidth sets the width of the view
func (v *AgentReadinessView) SetWidth(width int) {
	v.width = width
}

// SetHeight sets the height of the view (not used for this component)
func (v *AgentReadinessView) SetHeight(height int) {
	// Not used
}

// Render renders the agent readiness indicator
func (v *AgentReadinessView) Render() string {
	readiness := v.stateManager.GetAgentReadiness()
	if readiness == nil {
		return ""
	}

	// If all agents are ready, don't show the indicator
	if readiness.ReadyAgents >= readiness.TotalAgents {
		return ""
	}

	// Build the status message
	statusMsg := fmt.Sprintf("Agents: %d/%d ready", readiness.ReadyAgents, readiness.TotalAgents)

	// Add details about agents that are still starting
	var details []string
	for _, agent := range readiness.Agents {
		if agent.State != domain.AgentStateReady {
			detail := fmt.Sprintf("%s (%s)", agent.Name, agent.State.DisplayName())
			details = append(details, detail)
		}
	}

	if len(details) > 0 {
		statusMsg += " • "
		for i, detail := range details {
			if i > 0 {
				statusMsg += ", "
			}
			statusMsg += detail
			// Limit to 3 agents shown
			if i >= 2 && len(details) > 3 {
				statusMsg += fmt.Sprintf(" +%d more", len(details)-3)
				break
			}
		}
	}

	// Style the message
	color := v.styleProvider.GetThemeColor("status")
	styledMsg := v.styleProvider.RenderWithColor(fmt.Sprintf("⚡ %s", statusMsg), color)

	return styledMsg
}

// View renders the view (Bubble Tea interface)
func (v *AgentReadinessView) View() string {
	return v.Render()
}

// Init initializes the view (Bubble Tea interface)
func (v *AgentReadinessView) Init() tea.Cmd {
	return nil
}

// Update handles messages (Bubble Tea interface)
func (v *AgentReadinessView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.SetWidth(msg.Width)
	case domain.AgentStatusUpdateEvent:
		// Agent status updated, view will re-render automatically
	case domain.AgentReadyEvent:
		// Agent became ready, view will re-render automatically
	}

	return v, nil
}
