package app

import (
	"fmt"
	"slices"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
)

const (
	readOnlyBadge  = "read-only"
	readWriteBadge = "read-write"
)

// agentPickerSpec titles the dropdown that switches the agent a chat runs as.
var agentPickerSpec = components.PickerSpec{
	Title: "Switch agent",
	Hint:  "the next message runs as it",
	Keys:  "↑/↓ move · enter switch · esc close",
}

// openAgentPicker opens the dropdown under the status row with the agents the
// chat can switch to, the one it runs as selected.
func (app *ChatApplication) openAgentPicker() tea.Cmd {
	app.agentPicker.Open(agentPickerSpec, agentRows(app.toolService.ListMarkdownSubagents()), app.agentService.MainAgent().Name)
	if app.agentPicker.IsOpen() {
		return nil
	}
	return statusCmd("No agents to switch to - add one under ~/.infer/agents")
}

// agentRows lists agents with the default main agent first, each tagged with
// whether it can write.
func agentRows(agents []agentdomain.SubagentInfo) []components.PickerRow {
	sorted := slices.Clone(agents)
	slices.SortStableFunc(sorted, func(a, b agentdomain.SubagentInfo) int {
		return mainAgentFirst(a.Name) - mainAgentFirst(b.Name)
	})
	rows := make([]components.PickerRow, 0, len(sorted))
	for _, agent := range sorted {
		tag, color := readWriteBadge, "warning"
		if agent.ReadOnly {
			tag, color = readOnlyBadge, "success"
		}
		rows = append(rows, components.PickerRow{Value: agent.Name, Label: agent.Name, Tag: tag, TagColor: color, Detail: agent.Description})
	}
	return rows
}

func mainAgentFirst(name string) int {
	if name == agentdomain.DefaultMainAgentName {
		return 0
	}
	return 1
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
