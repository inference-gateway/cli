package directexec_test

import (
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	directexec "github.com/inference-gateway/cli/internal/presentation/tui/directexec"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

func TestHandleToolCommand_BlocksToolNotInCurrentMode(t *testing.T) {
	toolSvc := &agentdomainmocks.FakeToolService{}
	toolSvc.IsToolEnabledReturns(true)
	toolSvc.ListToolsForModeReturns([]sdk.ChatCompletionTool{
		{Function: sdk.FunctionObject{Name: "Read"}},
	})

	sm := statemanager.NewStateManager(false)
	sm.SetAgentMode(agentdomain.AgentModeStandard)

	svc := directexec.NewService(directexec.Options{ToolService: toolSvc, StateManager: sm})

	// AskUserQuestion is plan-only - not in standard mode's tool list - so !!
	// must refuse it rather than run it.
	cmd := svc.HandleToolCommand(`AskUserQuestion({"questions":[]})`)
	if cmd == nil {
		t.Fatal("expected an error command")
	}
	errEvent, ok := cmd().(tui.ShowErrorEvent)
	if !ok {
		t.Fatalf("expected ShowErrorEvent, got %T", cmd())
	}
	if !strings.Contains(errEvent.Error, "not available") {
		t.Errorf("expected a 'not available' error, got %q", errEvent.Error)
	}
}
