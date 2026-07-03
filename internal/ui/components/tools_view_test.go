package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

func toolDef(name, description string) sdk.ChatCompletionTool {
	tool := sdk.ChatCompletionTool{}
	tool.Function.Name = name
	if description != "" {
		tool.Function.Description = &description
	}
	return tool
}

// newToolsViewForTest builds a tools view backed by fakes: the tool service
// returns the given tools for any mode and the state manager reports plan
// mode, so the mode propagation is observable.
func newToolsViewForTest(tools []sdk.ChatCompletionTool) (*ToolsViewImpl, *domainmocks.FakeToolService, *domainmocks.FakeStateManager) {
	fakeTheme := &uimocks.FakeTheme{}
	fakeTheme.GetAccentColorReturns("#ff9e64")
	fakeTheme.GetDimColorReturns("#888888")
	themeService := &domainmocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(fakeTheme)

	toolService := &domainmocks.FakeToolService{}
	toolService.ListToolsForModeReturns(tools)

	stateManager := &domainmocks.FakeStateManager{}
	stateManager.GetAgentModeReturns(domain.AgentModePlan)

	view := NewToolsView(toolService, stateManager, styles.NewProvider(themeService))
	return view, toolService, stateManager
}

func TestToolsView_ItemsReflectAvailableTools(t *testing.T) {
	view, toolService, _ := newToolsViewForTest([]sdk.ChatCompletionTool{
		toolDef("Read", "Read a file from the filesystem"),
		toolDef("Bash", ""), // nil description must be safe
	})

	items := view.list.Items()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	first, ok := items[0].(toolItem)
	if !ok {
		t.Fatalf("expected a toolItem, got %T", items[0])
	}
	if first.name != "Read" || first.description != "Read a file from the filesystem" {
		t.Errorf("unexpected first item: %+v", first)
	}
	second := items[1].(toolItem)
	if second.name != "Bash" || second.description != "" {
		t.Errorf("a nil description should map to an empty string, got %+v", second)
	}

	if view.list.Title != "Available Tools (2)" {
		t.Errorf("title = %q, want the tool count", view.list.Title)
	}

	if got := toolService.ListToolsForModeArgsForCall(0); got != domain.AgentModePlan {
		t.Errorf("tools must be listed for the current agent mode, got %v", got)
	}
}

func TestToolsView_EscCancelsEnterDoesNot(t *testing.T) {
	view, _, _ := newToolsViewForTest([]sdk.ChatCompletionTool{toolDef("Read", "")})

	model, _ := view.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view = model.(*ToolsViewImpl)
	if view.IsCancelled() {
		t.Fatal("enter is a no-op in the read-only tools view")
	}

	model, _ = view.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	view = model.(*ToolsViewImpl)
	if !view.IsCancelled() {
		t.Fatal("esc should cancel the tools view")
	}
}

func TestToolsView_ResetRebuildsItems(t *testing.T) {
	view, toolService, _ := newToolsViewForTest([]sdk.ChatCompletionTool{toolDef("Read", "")})

	model, _ := view.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	view = model.(*ToolsViewImpl)

	toolService.ListToolsForModeReturns([]sdk.ChatCompletionTool{
		toolDef("Read", ""),
		toolDef("Write", ""),
		toolDef("Bash", ""),
	})
	view.Reset()

	if view.IsCancelled() {
		t.Error("Reset must clear the cancelled flag")
	}
	if got := len(view.list.Items()); got != 3 {
		t.Errorf("Reset should re-read the tool service, got %d items", got)
	}
	if view.list.Title != "Available Tools (3)" {
		t.Errorf("title = %q, want the refreshed count", view.list.Title)
	}
}
