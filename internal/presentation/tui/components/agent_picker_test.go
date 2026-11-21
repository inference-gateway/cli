package components

import (
	"strings"
	"testing"

	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	lipgloss "charm.land/lipgloss/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

func newTestAgentPicker() *AgentPicker {
	theme := &tuimocks.FakeTheme{}
	theme.GetAccentColorReturns("#ff9e64")
	theme.GetDimColorReturns("#888888")
	themeService := &tuimocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(theme)
	return NewAgentPicker(styles.NewProvider(themeService))
}

var pickerAgents = []agentdomain.SubagentInfo{
	{Name: "explore", Description: "Read-only search agent.", ReadOnly: true},
	{Name: "main", Description: "The default main agent."},
	{Name: "general-purpose", Description: strings.Repeat("A long description. ", 20)},
}

func TestAgentPickerListsMainFirstAndOpensOnTheCurrentAgent(t *testing.T) {
	p := newTestAgentPicker()
	p.Open(pickerAgents, "explore")

	if !p.IsOpen() || p.Selected() != "explore" {
		t.Fatalf("opened on %q, want the current agent explore", p.Selected())
	}
	p.Move(-1)
	if p.Selected() != "main" {
		t.Fatalf("the row above explore is %q, want main listed first", p.Selected())
	}
	p.Move(-1)
	if p.Selected() != "general-purpose" {
		t.Fatalf("moving up from the top must wrap to the last agent, got %q", p.Selected())
	}
	p.Close()
	if p.IsOpen() || p.Render() != "" {
		t.Fatal("a closed picker renders nothing")
	}
}

func TestAgentPickerRendersWithinItsWidth(t *testing.T) {
	p := newTestAgentPicker()
	p.Open(pickerAgents, "main")
	p.SetWidth(300)

	out := p.Render()
	for _, want := range []string{"Switch agent", "read-only", "read-write", "enter switch"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q", want)
		}
	}
	if width := lipgloss.Width(out); width > agentPickerMaxInner+agentPickerChrome {
		t.Fatalf("render is %d wide, want the description truncated to at most %d", width, agentPickerMaxInner+agentPickerChrome)
	}

	p.SetWidth(60)
	if width := lipgloss.Width(p.Render()); width > 60 {
		t.Fatalf("render is %d wide in a 60 column row", width)
	}
}

func TestAgentPickerWithNoAgentsStaysClosed(t *testing.T) {
	p := newTestAgentPicker()
	p.Open(nil, "main")
	if p.IsOpen() || p.Selected() != "" {
		t.Fatal("with no agents to list the picker must not open")
	}
}
