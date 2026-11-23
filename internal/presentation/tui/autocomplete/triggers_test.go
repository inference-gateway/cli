package autocomplete_test

import (
	"slices"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"
	ansi "github.com/charmbracelet/x/ansi"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	autocomplete "github.com/inference-gateway/cli/internal/presentation/tui/autocomplete"
)

func newTriggerTestAutocomplete(sources map[string][]string) *autocomplete.Autocomplete {
	files := &agentdomainmocks.FakeFileService{}
	files.ListProjectFilesReturns([]string{"cmd/run.sh"}, nil)

	agents := &agentdomainmocks.FakeToolService{}
	agents.ListMarkdownSubagentsReturns([]agentdomain.SubagentInfo{{Name: "runner", Description: "Runs tests"}})

	issues := &agentdomainmocks.FakeGitHubIssueService{}
	issues.IsAvailableReturns(true)
	issues.ListIssuesReturns([]agentdomain.GitHubIssue{{Number: 12, Title: "Add auth"}}, nil)

	ac := autocomplete.NewAutocomplete(&tuimocks.FakeTheme{}, &tuimocks.FakeShortcutRegistry{})
	ac.SetFileService(files)
	ac.SetToolService(agents)
	ac.SetGitHubIssueService(issues)
	if sources != nil {
		ac.SetTriggerSources(sources)
	}
	return ac
}

// visibleRows walks the open menu row by row, so a table's order is checked
// through what the user would actually insert.
func visibleRows(ac *autocomplete.Autocomplete, n int) []string {
	rows := []string{ac.GetSelectedShortcut()}
	for range n - 1 {
		_, _ = ac.HandleKey(tea.KeyPressMsg{Code: tea.KeyDown})
		rows = append(rows, ac.GetSelectedShortcut())
	}
	return rows
}

func TestTriggerTableBuildsSectionsInOrder(t *testing.T) {
	tests := []struct {
		name    string
		sources map[string][]string
		input   string
		want    []string
	}{
		{"chat at opens files with agents after them", nil, "@", []string{"@cmd/run.sh", "@runner"}},
		{"chat hash opens issues", nil, "#", []string{"#12"}},
		{"a room table puts agents before files", map[string][]string{"@": {"agents", "files"}}, "@", []string{"@runner", "@cmd/run.sh"}},
		{"a menus source order is its section order", map[string][]string{"#": {"files", "issues"}}, "#", []string{"@cmd/run.sh", "#12"}},
		{"a special character opens its menu", map[string][]string{"^": {"issues"}}, "^", []string{"#12"}},
		{"a non-ascii special character opens its menu", map[string][]string{"§": {"files"}}, "§", []string{"@cmd/run.sh"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := newTriggerTestAutocomplete(tt.sources)
			ac.Update(tt.input, len(tt.input))
			if !ac.IsVisible() {
				t.Fatalf("Update(%q) opened nothing, want %v", tt.input, tt.want)
			}
			if got := visibleRows(ac, len(tt.want)); !slices.Equal(got, tt.want) {
				t.Errorf("Update(%q) menu = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestEmptyTriggerTableOpensNothing(t *testing.T) {
	ac := newTriggerTestAutocomplete(map[string][]string{"@": {}})
	ac.Update("@", 1)
	if ac.IsVisible() {
		t.Error("an empty trigger table must open no menu")
	}
}

func TestTriggerLeftOutOfTheTableOpensNothing(t *testing.T) {
	ac := newTriggerTestAutocomplete(map[string][]string{"^": {"issues"}})
	ac.Update("#", 1)
	if ac.IsVisible() {
		t.Error("# must open no menu once the table maps issues to ^ instead")
	}
}

// A multi-byte trigger refines its query after the whole character, and the
// accepted row replaces the trigger and query with the row's own sigil.
func TestNonASCIITriggerSplicesTheRow(t *testing.T) {
	ac := newTriggerTestAutocomplete(map[string][]string{"§": {"files"}})
	input := "see §cmd"
	ac.Update(input, len(input))
	if got := ac.GetSelectedShortcut(); got != "@cmd/run.sh" {
		t.Fatalf("query after § selected %q, want @cmd/run.sh", got)
	}
	_, result := ac.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if !strings.HasPrefix(result, "see @cmd/run.sh") {
		t.Errorf("accepted row = %q, want it to replace §cmd with @cmd/run.sh", result)
	}
}

// The trigger picks only which menu opens: a row from a composed menu keeps
// the sigil the CLI parses, and the filter still finds file rows among them.
func TestComposedMenuRowsKeepTheirSigil(t *testing.T) {
	ac := newTriggerTestAutocomplete(map[string][]string{"@": {"files", "agents", "issues"}})
	ac.Update("@", 1)
	if got := visibleRows(ac, 3); !slices.Equal(got, []string{"@cmd/run.sh", "@runner", "#12"}) {
		t.Errorf("menu = %v, want rows keeping their own sigils", got)
	}

	ac.Update("@cmd/ru", 7)
	if got := ac.GetSelectedShortcut(); got != "@cmd/run.sh" {
		t.Errorf("fuzzy filter on a composed menu lost the file row, got %q", got)
	}
}

func TestTriggerMenuFitsItsWidth(t *testing.T) {
	const width = 80
	ac := newTriggerTestAutocomplete(nil)
	ac.SetWidth(width)
	ac.Update("@", 1)
	for line := range strings.SplitSeq(ac.Render(), "\n") {
		if got := ansi.StringWidth(line); got > width {
			t.Errorf("row %q is %d columns wide, want at most %d", ansi.Strip(line), got, width)
		}
	}
}

func TestComposedMenuKeepsDescriptionsWhileFiltering(t *testing.T) {
	ac := newTriggerTestAutocomplete(map[string][]string{"^": {"agents", "issues"}})
	ac.SetWidth(80)
	ac.Update("^ru", 3)
	if got := ansi.Strip(ac.Render()); !strings.Contains(got, "Runs tests") {
		t.Errorf("filtered menu dropped the agent description:\n%s", got)
	}
}
