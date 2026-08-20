package directexec_test

import (
	ui "github.com/inference-gateway/cli/internal/ui"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	services "github.com/inference-gateway/cli/internal/services"
	directexec "github.com/inference-gateway/cli/internal/services/directexec"
	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
)

func TestHandleToolCommand_BlocksToolNotInCurrentMode(t *testing.T) {
	toolSvc := &agentdomainmocks.FakeToolService{}
	toolSvc.IsToolEnabledReturns(true)
	toolSvc.ListToolsForModeReturns([]sdk.ChatCompletionTool{
		{Function: sdk.FunctionObject{Name: "Read"}},
	})

	sm := services.NewStateManager(false)
	sm.SetAgentMode(agentdomain.AgentModeStandard)

	svc := directexec.NewService(directexec.Options{ToolService: toolSvc, StateManager: sm})

	// AskUserQuestion is plan-only - not in standard mode's tool list - so !!
	// must refuse it rather than run it.
	cmd := svc.HandleToolCommand(`AskUserQuestion({"questions":[]})`)
	if cmd == nil {
		t.Fatal("expected an error command")
	}
	errEvent, ok := cmd().(ui.ShowErrorEvent)
	if !ok {
		t.Fatalf("expected ShowErrorEvent, got %T", cmd())
	}
	if !strings.Contains(errEvent.Error, "not available") {
		t.Errorf("expected a 'not available' error, got %q", errEvent.Error)
	}
}
