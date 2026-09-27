package components

import (
	"testing"

	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// fakeSubagentCatalog is a canned SubagentCatalog for view tests.
type fakeSubagentCatalog struct {
	infos []agentdomain.SubagentInfo
}

func (f *fakeSubagentCatalog) ListMarkdownSubagents() []agentdomain.SubagentInfo {
	return f.infos
}

// newSubagentsViewForTest builds a subagents view over the given presets with
// a mock theme service.
func newSubagentsViewForTest(infos []agentdomain.SubagentInfo) *SubagentsView {
	return NewSubagentsView(&fakeSubagentCatalog{infos: infos}, newSubagentsTestStyleProvider())
}

// newSubagentsTestStyleProvider returns a style provider backed by a fake theme.
func newSubagentsTestStyleProvider() *styles.Provider {
	fakeTheme := &tuimocks.FakeTheme{}
	fakeTheme.GetAccentColorReturns("#ff9e64")
	fakeTheme.GetDimColorReturns("#888888")
	fakeTheme.GetStatusColorReturns("#e0af68")
	fakeTheme.GetErrorColorReturns("#f7768e")
	themeService := &tuimocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(fakeTheme)
	return styles.NewProvider(themeService)
}

func TestSubagentsView_ItemsSortedWithCapabilities(t *testing.T) {
	view := newSubagentsViewForTest([]agentdomain.SubagentInfo{
		{Name: "docs-drafter", Description: "Drafts documentation", Tools: []string{"Read", "Write", "WebFetch"}, ReadOnly: false, Source: "user"},
		{Name: "explorer", Description: "Read-only code explorer", Tools: []string{"Grep", "Read"}, ReadOnly: true, Source: "project"},
	})

	items := view.list.Items()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	first := items[0].(subagentItem)
	if first.info.Name != "docs-drafter" || first.info.ReadOnly {
		t.Errorf("items must be sorted by name, got %+v", first)
	}
	second := items[1].(subagentItem)
	if second.info.Name != "explorer" || !second.info.ReadOnly || second.info.Source != "project" {
		t.Errorf("unexpected second item: %+v", second)
	}

	if view.list.Title != "Subagents (2)" {
		t.Errorf("title = %q, want the preset count", view.list.Title)
	}

	caps := subagentCapabilities(second.info)
	if caps != "tools: Grep, Read | model: inherit | project" {
		t.Errorf("capabilities = %q, want the allowlist, inherit model and source", caps)
	}
}

func TestSubagentsView_InheritedToolsAndModelShown(t *testing.T) {
	caps := subagentCapabilities(agentdomain.SubagentInfo{Name: "runner", Description: "Runs tests", Source: "project"})

	if caps != "tools: all tools (inherited) | model: inherit | project" {
		t.Errorf("capabilities = %q, want the inherited placeholders", caps)
	}
}

func TestSubagentsView_NilCatalogIsSafe(t *testing.T) {
	view := NewSubagentsView(nil, newSubagentsTestStyleProvider())

	if got := len(view.list.Items()); got != 0 {
		t.Fatalf("expected no items without a catalog, got %d", got)
	}
	if view.list.Title != "Subagents (0)" {
		t.Errorf("title = %q, want an empty summary", view.list.Title)
	}
}

func TestSubagentsView_EscCancelsEnterDoesNot(t *testing.T) {
	view := newSubagentsViewForTest([]agentdomain.SubagentInfo{
		{Name: "explorer", Description: "Read-only explorer"},
	})

	model, _ := view.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view = model.(*SubagentsView)
	if view.IsCancelled() {
		t.Fatal("enter is a no-op in the read-only subagents view")
	}

	model, _ = view.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	view = model.(*SubagentsView)
	if !view.IsCancelled() {
		t.Fatal("esc should cancel the subagents view")
	}
}
