package app

import (
	"fmt"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// openAgentPicker opens the dropdown under the status row with the agents the
// chat can switch to, the one it runs as selected.
func (app *ChatApplication) openAgentPicker() tea.Cmd {
	app.agentPicker.Open(app.toolService.ListMarkdownSubagents(), app.agentService.MainAgent().Name)
	if app.agentPicker.IsOpen() {
		return nil
	}
	return statusCmd("No agents to switch to - add one under ~/.infer/agents")
}

// handleAgentPickerKeys interprets keys while the agent dropdown is open, which
// holds every key until it closes.
func (app *ChatApplication) handleAgentPickerKeys(keyMsg tea.KeyPressMsg) []tea.Cmd {
	gk := guardKeys
	switch {
	case key.Matches(keyMsg, gk.navUp):
		app.agentPicker.Move(-1)
	case key.Matches(keyMsg, gk.navDown):
		app.agentPicker.Move(1)
	case key.Matches(keyMsg, gk.cancel):
		app.agentPicker.Close()
	case key.Matches(keyMsg, gk.confirm):
		name := app.agentPicker.Selected()
		app.agentPicker.Close()
		return []tea.Cmd{app.switchMainAgent(name)}
	}
	return nil
}

// switchMainAgent switches the chat to the named agent from its next message.
// The agent service resolves the name, here or on the daemon.
func (app *ChatApplication) switchMainAgent(name string) tea.Cmd {
	if err := app.agentService.SelectMainAgent(agentdomain.MainAgent{Name: name}); err != nil {
		return errorCmd(fmt.Sprintf("Could not switch to agent %s: %v", name, err), false)
	}
	return statusCmd(fmt.Sprintf("Switched to agent %s - it answers your next message", name))
}
