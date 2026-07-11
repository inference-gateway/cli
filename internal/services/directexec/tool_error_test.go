package directexec_test

import (
	"errors"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	directexec "github.com/inference-gateway/cli/internal/services/directexec"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// TestHandleToolCommand_ErrorStopsSpinner guards the stuck "running" spinner
// fix: when a !! tool execution returns a hard error, the async goroutine must
// still emit a terminal failed progress event (so the ToolCallRenderer stops
// the timer) alongside the error banner - without wiping the error banner with
// a status-clearing event.
func TestHandleToolCommand_ErrorStopsSpinner(t *testing.T) {
	toolSvc := &domainmocks.FakeToolService{}
	toolSvc.IsToolEnabledReturns(true)
	toolSvc.ListToolsForModeReturns([]sdk.ChatCompletionTool{
		{Function: sdk.FunctionObject{Name: "CloseSubagent"}},
	})
	toolSvc.ExecuteToolDirectReturns(nil, errors.New("boom"))

	sm := services.NewStateManager(false)
	sm.SetAgentMode(domain.AgentModeStandard)

	svc := directexec.NewService(directexec.Options{
		ToolService:      toolSvc,
		StateManager:     sm,
		ConversationRepo: &domainmocks.FakeConversationRepository{},
		Listener:         &domainmocks.FakeChatEventListener{},
	})

	if cmd := svc.HandleToolCommand(`CloseSubagent({"subagent_id":""})`); cmd == nil {
		t.Fatal("expected a command from HandleToolCommand")
	}

	events := drainToolEvents(t, svc)

	var sawError, sawFailedProgress bool
	for _, ev := range events {
		switch e := ev.(type) {
		case domain.ShowErrorEvent:
			sawError = true
		case domain.ToolExecutionProgressEvent:
			if e.Status == "failed" {
				sawFailedProgress = true
			}
		}
	}

	if !sawError {
		t.Error("error path must surface the failure via a ShowErrorEvent")
	}
	if !sawFailedProgress {
		t.Error("error path must emit a ToolExecutionProgressEvent with Status \"failed\" to stop the running timer")
	}
}

// drainToolEvents reads the per-invocation tool event channel until the async
// goroutine closes it. The goroutine buffers (cap 100) and closes after a short
// delay, so this never blocks indefinitely; a guard timeout fails loudly if it
// ever does.
func drainToolEvents(t *testing.T, svc *directexec.Service) []interface{} {
	t.Helper()

	ch := svc.PendingToolChannel()
	if ch == nil {
		t.Fatal("expected a pending tool event channel after HandleToolCommand")
	}

	var events []interface{}
	timeout := time.After(5 * time.Second)
	for {
		select {
		case msg, open := <-ch:
			if !open {
				return events
			}
			events = append(events, msg)
		case <-timeout:
			t.Fatal("timed out draining tool events (channel never closed)")
			return events
		}
	}
}
