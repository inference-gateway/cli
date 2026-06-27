package directexec_test

import (
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	directexec "github.com/inference-gateway/cli/internal/services/directexec"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestHandleToolCommand_BlocksToolNotInCurrentMode(t *testing.T) {
	toolSvc := &domainmocks.FakeToolService{}
	toolSvc.IsToolEnabledReturns(true)
	toolSvc.ListToolsForModeReturns([]sdk.ChatCompletionTool{
		{Function: sdk.FunctionObject{Name: "Read"}},
	})

	sm := &domainmocks.FakeStateManager{}
	sm.GetAgentModeReturns(domain.AgentModeStandard)

	svc := directexec.NewService(directexec.Options{ToolService: toolSvc, StateManager: sm})

	// AskUserQuestion is plan-only - not in standard mode's tool list - so !!
	// must refuse it rather than run it.
	cmd := svc.HandleToolCommand(`AskUserQuestion({"questions":[]})`)
	if cmd == nil {
		t.Fatal("expected an error command")
	}
	errEvent, ok := cmd().(domain.ShowErrorEvent)
	if !ok {
		t.Fatalf("expected ShowErrorEvent, got %T", cmd())
	}
	if !strings.Contains(errEvent.Error, "not available") {
		t.Errorf("expected a 'not available' error, got %q", errEvent.Error)
	}
}
