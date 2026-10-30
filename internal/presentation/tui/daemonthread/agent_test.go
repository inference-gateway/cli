package daemonthread

import (
	"slices"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	models "github.com/inference-gateway/cli/internal/platform/models"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

var (
	_ agentdomain.AgentService    = (*Agent)(nil)
	_ convdomain.ModelService     = (*Models)(nil)
	_ storage.ConversationStorage = (*Storage)(nil)
	_ tui.SessionTeleporter       = (*Thread)(nil)
	_ agui.Handler                = (*Thread)(nil)
)

func userEntry(text string) convdomain.ConversationEntry {
	return convdomain.ConversationEntry{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(text)}, Time: time.Now()}
}

// agentOver is an agent on thread whose mirror is a repository over the
// thread's own storage, the way the daemon chat container wires it.
func agentOver(thread *Thread) (*Agent, *conversation.PersistentConversationRepository) {
	repo := conversation.NewPersistentConversationRepository(nil, nil, NewStorage(thread))
	repo.SetConversationID("conv-1")
	models := &convmocks.FakeModelService{}
	models.GetCurrentModelReturns("m1")
	return NewAgent(thread, repo, &convmocks.FakeMessageQueue{}, &agentdomainmocks.FakeAgentModeState{}, models), repo
}

func collect(t *testing.T, events <-chan agentdomain.ChatEvent, onEvent func(agentdomain.ChatEvent)) []agentdomain.ChatEvent {
	t.Helper()
	var got []agentdomain.ChatEvent
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return got
			}
			got = append(got, event)
			if onEvent != nil {
				onEvent(event)
			}
		case <-timeout:
			t.Fatalf("the run never ended, got %d events", len(got))
		}
	}
}

func TestAgentRunsATurnOnTheWorkerAndReconcilesTheConversation(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)
	agent, repo := agentOver(thread)
	call := sdk.ChatCompletionMessageToolCall{ID: "tc1", Type: "function", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"ls"}`}}
	stored := []convdomain.ConversationEntry{userEntry("list files"), userEntry("stored by the worker")}
	d.setExport(stored)
	d.setRuns(func(conn *agui.Conn, _ map[string]any) {
		for _, line := range encodeRun(t,
			agentdomain.ChatChunkEvent{Content: "Listing"},
			agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{call}},
			agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolCallID: "tc1", ToolName: "Bash", Success: true}}},
			agentdomain.ChatChunkEvent{Content: "done"},
			agentdomain.ChatCompleteEvent{},
		) {
			send(conn, line)
		}
	})
	if err := repo.AddMessage(userEntry("list files")); err != nil {
		t.Fatal(err)
	}

	events, err := agent.RunWithStream(t.Context(), &agentdomain.AgentRequest{RequestID: "req-1"})
	if err != nil {
		t.Fatalf("RunWithStream: %v", err)
	}
	var mirrored int
	got := collect(t, events, func(event agentdomain.ChatEvent) {
		if _, ok := event.(agentdomain.ToolExecutionCompletedEvent); ok {
			mirrored = repo.GetMessageCount()
		}
	})

	if !slices.ContainsFunc(got, func(e agentdomain.ChatEvent) bool {
		c, ok := e.(agentdomain.ChatChunkEvent)
		return ok && c.Content == "Listing"
	}) {
		t.Errorf("events %v lack the streamed text", got)
	}
	if mirrored < 3 {
		t.Errorf("mirror held %d entries when the tool result arrived, want at least the user, assistant and tool entries", mirrored)
	}
	inputs := d.frames("run_agent_input")
	messages, _ := inputs[0]["input"].(map[string]any)["messages"].([]any)
	if len(inputs) != 1 || len(messages) != 1 || messages[0].(map[string]any)["content"] != "list files" {
		t.Fatalf("run inputs = %v, want the one new user message", inputs)
	}
	if modes := d.frames("set_mode"); len(modes) != 1 || modes[0]["mode"] != "standard" {
		t.Errorf("mode syncs = %v, want one to standard", modes)
	}
	if repo.GetMessageCount() != len(stored) || thread.Synced("conv-1") != len(stored) {
		t.Errorf("after the turn the mirror has %d entries and %d synced, want the worker's %d", repo.GetMessageCount(), thread.Synced("conv-1"), len(stored))
	}
	eventually(t, "the reconciled history to reach the TUI", func() bool { return len(notified[tui.UpdateHistoryEvent](notifier)) > 0 })
}

func TestAgentAnswersAnApprovalWithAResume(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	agent, repo := agentOver(thread)
	d.setRuns(func(conn *agui.Conn, input map[string]any) {
		if _, resumed := input["resume"]; resumed {
			send(conn, `{"type":"RUN_STARTED","threadId":"conv-1","runId":"r2"}`)
			send(conn, `{"type":"TOOL_CALL_RESULT","messageId":"m2","toolCallId":"tc1","content":"{\"tool_name\":\"Bash\",\"tool_call_id\":\"tc1\",\"success\":true}"}`)
			send(conn, `{"type":"RUN_FINISHED","threadId":"conv-1","runId":"r2","outcome":{"type":"success"}}`)
			return
		}
		send(conn, `{"type":"RUN_STARTED","threadId":"conv-1","runId":"r1"}`)
		send(conn, `{"type":"TOOL_CALL_START","toolCallId":"tc1","toolCallName":"Bash"}`)
		send(conn, `{"type":"TOOL_CALL_ARGS","toolCallId":"tc1","delta":"{\"command\":\"rm\"}"}`)
		send(conn, `{"type":"RUN_FINISHED","threadId":"conv-1","runId":"r1","outcome":{"type":"interrupt","interrupts":[{"id":"tc1","reason":"tool_call","toolCallId":"tc1","message":"judge escalated"}]}}`)
	})
	_ = repo.AddMessage(userEntry("clean up"))

	events, err := agent.RunWithStream(t.Context(), &agentdomain.AgentRequest{RequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	var asked agentdomain.ToolApprovalRequestedEvent
	got := collect(t, events, func(event agentdomain.ChatEvent) {
		if approval, ok := event.(agentdomain.ToolApprovalRequestedEvent); ok {
			asked = approval
			approval.ResponseChan <- agentdomain.ApprovalApprove
		}
	})

	if asked.ToolCall.Function.Name != "Bash" || asked.Context != "judge escalated" {
		t.Fatalf("approval = %+v, want the Bash call with its context", asked)
	}
	resumes := d.frames("run_agent_input")
	if len(resumes) != 2 {
		t.Fatalf("run inputs = %v, want the turn and its resume", resumes)
	}
	resume := resumes[1]["input"].(map[string]any)["resume"].([]any)[0].(map[string]any)
	if resume["interruptId"] != "tc1" || resume["status"] != "resolved" {
		t.Fatalf("resume = %v, want tc1 resolved", resume)
	}
	if !slices.ContainsFunc(got, func(e agentdomain.ChatEvent) bool { _, ok := e.(agentdomain.ToolExecutionCompletedEvent); return ok }) {
		t.Errorf("events %v lack the continuation's tool result", got)
	}
}

func TestAgentFollowOnlyRequestEndsWhenNothingRuns(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	agent, _ := agentOver(thread)

	events, err := agent.RunWithStream(t.Context(), &agentdomain.AgentRequest{RequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	got := collect(t, events, nil)
	if len(got) != 1 {
		t.Fatalf("events = %v, want only the completion ending the turn on a thread at rest", got)
	}
	if complete, ok := got[0].(agentdomain.ChatCompleteEvent); !ok || complete.RequestID != "req-1" {
		t.Fatalf("events = %v, want only the completion ending the turn on a thread at rest", got)
	}
	if inputs := d.frames("run_agent_input"); len(inputs) != 0 {
		t.Fatalf("a follow-only request sent %v", inputs)
	}
}

func TestAgentTellsARestartedDaemonsWorkerTheModeAndModel(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	agent, _ := agentOver(thread)
	turn := func() {
		events, err := agent.RunWithStream(t.Context(), &agentdomain.AgentRequest{RequestID: "req"})
		if err != nil {
			t.Fatalf("RunWithStream: %v", err)
		}
		collect(t, events, nil)
	}
	turn()
	turn()
	d.binding.Close()
	eventually(t, "the offline placement", func() bool { return thread.Placement().Offline })
	again := startDaemon(t, d.port)
	eventually(t, "the follow on the new daemon", func() bool { return len(again.frames("resume_conversation")) == 1 })

	turn()

	for name, daemon := range map[string]*fakeDaemon{"first": d, "restarted": again} {
		modes, models := daemon.frames("set_mode"), daemon.frames("select_model")
		if len(modes) != 1 || len(models) != 1 || models[0]["model"] != "m1" {
			t.Errorf("%s daemon got mode syncs %v and model syncs %v, want one of each to m1", name, modes, models)
		}
	}
}

func TestModelsAdoptTheWorkersModelAndSwitchIt(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	d.answer("list_models", `{"type":"models","models":["m1","m2"],"current":"m2"}`)
	models := NewModels(thread)

	listed, err := models.ListModels(t.Context())
	if err != nil || !slices.Equal(listed, []string{"m1", "m2"}) || models.GetCurrentModel() != "m2" {
		t.Fatalf("ListModels = %v, %v, current %q", listed, err, models.GetCurrentModel())
	}
	if err := models.SelectModel("m1"); err != nil || models.GetCurrentModel() != "m1" {
		t.Fatalf("SelectModel = %v, current %q", err, models.GetCurrentModel())
	}
	if err := models.SelectModel("missing"); err == nil {
		t.Error("an unknown model was selected")
	}
}

func TestModelsRecordWhatTheWorkersGatewaySaysAboutEachModel(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	d.answer("list_models", `{"type":"models","models":["daemon/seer"],"catalog":[{"id":"daemon/seer","context_window":{"tokens":128000},`+
		`"pricing":{"input_per_token":"0.000001","output_per_token":"0.000002"},"modalities":{"input":["text","image"],"output":["text"]}}]}`)

	if _, err := NewModels(thread).ListModels(t.Context()); err != nil {
		t.Fatal(err)
	}

	if window, known := models.LookupContextWindow("daemon/seer"); !known || window != 128000 {
		t.Errorf("context window = %d, %v, want the worker's 128000", window, known)
	}
	if label := models.ModalitiesLabel("daemon/seer"); label != "vision" {
		t.Errorf("modalities = %q, want vision", label)
	}
}

func TestStorageListsAndLoadsTheWorkersConversations(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	d.answer("list_conversations", `{"type":"conversations","conversations":[{"id":"c2","title":"Other","message_count":3}]}`)
	d.setExport([]convdomain.ConversationEntry{userEntry("hi")})
	store := NewStorage(thread)

	listed, err := store.ListConversations(t.Context(), "", 50, 0)
	if err != nil || len(listed) != 1 || listed[0].ID != "c2" || listed[0].MessageCount != 3 {
		t.Fatalf("ListConversations = %+v, %v", listed, err)
	}
	if more, _ := store.ListConversations(t.Context(), "", 50, 50); len(more) != 0 {
		t.Errorf("a second page = %+v, want none", more)
	}
	entries, _, err := store.LoadConversation(t.Context(), "c2")
	if err != nil || len(entries) != 1 {
		t.Fatalf("LoadConversation = %d entries, %v", len(entries), err)
	}
	if !thread.Follows("c2") || thread.Synced("c2") != 1 {
		t.Errorf("loading c2 left the thread following %v with %d synced", thread.Follows("c2"), thread.Synced("c2"))
	}
	if err := store.DeleteConversation(t.Context(), "c2"); err == nil {
		t.Error("a daemon chat deleted a worker's conversation")
	}
}

func TestAgentShowsTheWorkersPlanForReview(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	agent, repo := agentOver(thread)
	d.setRuns(func(conn *agui.Conn, input map[string]any) {
		send(conn, `{"type":"RUN_STARTED","threadId":"conv-1","runId":"r1"}`)
		send(conn, `{"type":"RUN_FINISHED","threadId":"conv-1","runId":"r1","outcome":{"type":"interrupt","interrupts":[{"id":"plan-1","reason":"plan_approval","message":"Review the plan","metadata":{"planId":"plan-1","plan":"1. Do it"}}]}}`)
	})
	_ = repo.AddMessage(userEntry("plan it"))

	events, err := agent.RunWithStream(t.Context(), &agentdomain.AgentRequest{RequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if plan, ok := event.(agentdomain.PlanApprovalRequestedEvent); ok {
			entries := repo.GetMessages()
			last := entries[len(entries)-1]
			text, _ := last.Message.Content.AsMessageContent0()
			if !last.IsPlan || last.PlanApprovalStatus != convdomain.PlanApprovalPending || text != "1. Do it" {
				t.Errorf("the mirror ends with %+v when the plan prompt arrives, want the pending plan", last)
			}
			close(plan.ResponseChan)
			return
		}
	}
	t.Fatal("the run never asked for the plan's approval")
}

func TestAgentKeepsOnePlanWhenTheWorkerAlreadyStoredIt(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	agent, repo := agentOver(thread)
	_ = repo.AddMessage(convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("1. Do it")},
		IsPlan:  true, PlanApprovalStatus: convdomain.PlanApprovalPending,
	})

	agent.mirror(agentdomain.PlanApprovalRequestedEvent{PlanID: "plan-1", PlanContent: "1. Do it"})

	if repo.GetMessageCount() != 1 {
		t.Errorf("the mirror holds %d entries, want the one stored plan", repo.GetMessageCount())
	}
}
