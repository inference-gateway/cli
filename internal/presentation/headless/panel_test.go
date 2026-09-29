package headless

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

// lineSink collects the panel's output lines, one per Write.
type lineSink chan []byte

func (s lineSink) Write(p []byte) (int, error) {
	s <- append([]byte(nil), p...)
	return len(p), nil
}

// readFrame reads panel lines until one of the given type arrives.
func readFrame(t *testing.T, sink lineSink, typ string) map[string]any {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case line := <-sink:
			var frame map[string]any
			if err := json.Unmarshal(line, &frame); err != nil {
				t.Fatalf("panel wrote a non-JSON line %q: %v", line, err)
			}
			if frame["type"] == typ || (frame["type"] == "CUSTOM" && frame["name"] == typ) {
				return frame
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %q", typ)
		}
	}
}

func panelDeps() PanelDeps {
	return PanelDeps{
		Conversations: newPanelRepo(),
		Skills:        &agentdomainmocks.FakeSkillsService{},
		Tools:         &agentdomainmocks.FakeToolService{},
		Approval:      &agentdomainmocks.FakeApprovalPolicy{},
		Models:        &convmocks.FakeModelService{},
		Modes:         statemanager.NewStore(false),
	}
}

func startPanel(deps PanelDeps) (*Panel, lineSink) {
	sink := make(lineSink, 16)
	return NewPanel(deps, sink), sink
}

func handle(t *testing.T, p *Panel, frame map[string]any) bool {
	t.Helper()
	line, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return p.Handle(line)
}

func newPanelRepo() *conversation.PersistentConversationRepository {
	return conversation.NewPersistentConversationRepository(nil, nil, storage.NewMemoryStorage())
}

// seedConversation starts, fills, and saves a conversation, returning its id.
func seedConversation(t *testing.T, repo *conversation.PersistentConversationRepository, title, content string) string {
	t.Helper()
	if err := repo.StartNewConversation(title); err != nil {
		t.Fatalf("StartNewConversation: %v", err)
	}
	entry := convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(content)},
		Time:    time.Now(),
	}
	if err := repo.AddMessage(entry); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if err := repo.SaveConversation(context.Background()); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	return repo.GetCurrentConversationID()
}

func TestPanelListConversations(t *testing.T) {
	repo := newPanelRepo()
	firstID := seedConversation(t, repo, "First convo", "hello one")
	secondID := seedConversation(t, repo, "Second convo", "hello two")
	deps := panelDeps()
	deps.Conversations = repo
	p, sink := startPanel(deps)

	if !handle(t, p, map[string]any{"type": "list_conversations", "project_dir": "/p"}) {
		t.Fatal("list_conversations was not consumed")
	}
	frame := readFrame(t, sink, "conversations")
	raw, ok := frame["conversations"].([]any)
	if !ok || len(raw) != 2 {
		t.Fatalf("expected 2 conversations, got %v", frame["conversations"])
	}
	byID := map[string]map[string]any{}
	for _, c := range raw {
		entry := c.(map[string]any)
		byID[entry["id"].(string)] = entry
	}
	if byID[firstID]["title"] != "First convo" || byID[secondID]["title"] != "Second convo" {
		t.Fatalf("unexpected conversations: %v", byID)
	}
	if count, _ := byID[firstID]["message_count"].(float64); count != 1 {
		t.Fatalf("first message_count = %v, want 1", byID[firstID]["message_count"])
	}
	if at, _ := byID[firstID]["updated_at"].(string); at == "" {
		t.Fatalf("first updated_at missing: %v", byID[firstID])
	}
}

func TestPanelSnapshotsTheWorkersConversation(t *testing.T) {
	for _, typ := range []string{"new_session", "resume_conversation"} {
		t.Run(typ, func(t *testing.T) {
			repo := newPanelRepo()
			seedConversation(t, repo, "Current", "resume me please")
			deps := panelDeps()
			deps.Conversations = repo
			p, sink := startPanel(deps)

			handle(t, p, map[string]any{"type": typ, "project_dir": "/p", "id": "ignored"})
			frame := readFrame(t, sink, "MESSAGES_SNAPSHOT")
			msgs, ok := frame["messages"].([]any)
			if !ok || len(msgs) != 1 {
				t.Fatalf("expected 1 message in snapshot, got %v", frame["messages"])
			}
			if content := msgs[0].(map[string]any)["content"]; content != "resume me please" {
				t.Fatalf("snapshot content = %v, want %q", content, "resume me please")
			}
		})
	}
}

func TestPanelSnapshotReplyFlag(t *testing.T) {
	repo := newPanelRepo()
	seedConversation(t, repo, "Current", "resume me please")
	p, sink := startPanel(panelDeps())

	handle(t, p, map[string]any{"type": "resume_conversation", "project_dir": "/p", "id": "ignored"})
	readFrame(t, sink, "MESSAGES_SNAPSHOT")

	if !p.TakeSnapshotReply() {
		t.Fatal("TakeSnapshotReply() = false, want true right after the frame reply")
	}
	if p.TakeSnapshotReply() {
		t.Fatal("TakeSnapshotReply() = true twice, want the mark consumed once")
	}

	handle(t, p, map[string]any{"type": "list_conversations", "project_dir": "/p"})
	readFrame(t, sink, "conversations")
	if p.TakeSnapshotReply() {
		t.Fatal("TakeSnapshotReply() = true, want false for non-snapshot frames")
	}
}

func TestPanelListSkills(t *testing.T) {
	skills := &agentdomainmocks.FakeSkillsService{}
	skills.ListReturns([]agentdomain.Skill{
		{Name: "tmux", Description: "drive tmux", Scope: agentdomain.SkillScopeUser},
		{Name: "notion", Scope: agentdomain.SkillScopePlugin, PluginName: "notion"},
	})
	deps := panelDeps()
	deps.Skills = skills
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "list_skills"})
	frame := readFrame(t, sink, "skills")
	raw, ok := frame["skills"].([]any)
	if !ok || len(raw) != 2 {
		t.Fatalf("expected 2 skills, got %v", frame["skills"])
	}
	if raw[0].(map[string]any)["scope"] != "user" || raw[1].(map[string]any)["name"] != "notion:notion" {
		t.Fatalf("unexpected skills: %v", raw)
	}
}

func TestPanelListModelsDefaultFirstAndSelect(t *testing.T) {
	models := &convmocks.FakeModelService{}
	models.ListModelsReturns([]string{"a/x", "b/y"}, nil)
	models.GetCurrentModelReturns("b/y")
	models.SelectModelCalls(func(m string) error { models.GetCurrentModelReturns(m); return nil })
	deps := panelDeps()
	deps.Models = models
	deps.DefaultModel = "b/y"
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "list_models"})
	frame := readFrame(t, sink, "models")
	raw, _ := frame["models"].([]any)
	if len(raw) != 2 || raw[0] != "b/y" || raw[1] != "a/x" || frame["current"] != "b/y" {
		t.Fatalf("unexpected models frame: %v", frame)
	}

	handle(t, p, map[string]any{"type": "select_model", "model": "a/x"})
	frame = readFrame(t, sink, "models")
	if models.SelectModelCallCount() != 1 || models.SelectModelArgsForCall(0) != "a/x" || frame["current"] != "a/x" {
		t.Fatalf("expected SelectModel(a/x) and current a/x, got calls=%d current=%v", models.SelectModelCallCount(), frame["current"])
	}
	readFrame(t, sink, "mode")
}

func TestPanelSetMode(t *testing.T) {
	deps := panelDeps()
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "set_mode", "mode": "plan"})
	if frame := readFrame(t, sink, "mode"); frame["mode"] != agentdomain.AgentModePlan.ModeKey() {
		t.Fatalf("mode = %v, want plan", frame["mode"])
	}
	if deps.Modes.GetAgentMode() != agentdomain.AgentModePlan {
		t.Fatalf("agent mode not switched: %v", deps.Modes.GetAgentMode())
	}
}

// fakeHistoryStore is an in-memory storage.ShellHistoryStorage.
type fakeHistoryStore struct {
	mu      sync.Mutex
	entries []string
}

func (f *fakeHistoryStore) AppendHistory(_ context.Context, command string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, command)
	return nil
}

func (f *fakeHistoryStore) LoadHistory(_ context.Context, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.entries
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return append([]string{}, out...), nil
}

func TestPanelHistoryRoundTrip(t *testing.T) {
	deps := panelDeps()
	deps.History = &fakeHistoryStore{entries: []string{"from the tui"}}
	p, sink := startPanel(deps)

	for _, msg := range []string{"first", "first", "  ", "second"} {
		if handle(t, p, map[string]any{"type": "user_message", "content": msg}) {
			t.Fatal("user_message must be left for the turn loop")
		}
	}
	handle(t, p, map[string]any{"type": "list_history"})
	frame := readFrame(t, sink, "history")
	raw, _ := frame["history"].([]any)
	want := []any{"from the tui", "first", "second"}
	if len(raw) != len(want) {
		t.Fatalf("history = %v, want %v", raw, want)
	}
	for i := range want {
		if raw[i] != want[i] {
			t.Fatalf("history = %v, want %v", raw, want)
		}
	}
}

func TestPanelHistoryWithoutStoreSendsEmpty(t *testing.T) {
	p, sink := startPanel(panelDeps())
	handle(t, p, map[string]any{"type": "list_history"})
	if raw, ok := readFrame(t, sink, "history")["history"].([]any); !ok || len(raw) != 0 {
		t.Fatalf("expected an empty history list")
	}
}

func TestPanelToolRequestUnknownTool(t *testing.T) {
	deps := panelDeps()
	deps.Tools.(*agentdomainmocks.FakeToolService).IsToolEnabledReturns(false)
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "tool_request", "id": "req-1", "tool_name": "Nope", "tool_args": "{}"})
	frame := readFrame(t, sink, "tool_result")
	if frame["id"] != "req-1" || frame["success"] != false || frame["error"] == "" {
		t.Fatalf("unexpected tool_result: %v", frame)
	}
}

func approvingToolDeps(needsApproval bool) PanelDeps {
	deps := panelDeps()
	tools := deps.Tools.(*agentdomainmocks.FakeToolService)
	tools.IsToolEnabledReturns(true)
	tools.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{
		ToolName: "Bash",
		Success:  true,
		Data:     &agentdomain.BashToolResult{Output: "hi\n"},
	}, nil)
	deps.Approval.(*agentdomainmocks.FakeApprovalPolicy).ShouldRequireApprovalReturns(needsApproval)
	return deps
}

func TestPanelToolRequestApproved(t *testing.T) {
	deps := approvingToolDeps(true)
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "tool_request", "id": "req-2", "tool_name": "Bash", "tool_args": `{"command":"echo hi"}`})
	req := readFrame(t, sink, "approval_request")["value"].(map[string]any)
	if req["tool_call_id"] != "req-2" || req["tool_name"] != "Bash" {
		t.Fatalf("unexpected approval_request: %v", req)
	}
	if !handle(t, p, map[string]any{"type": "approval_response", "tool_call_id": "req-2", "approved": true}) {
		t.Fatal("the tool_request's approval_response was not consumed")
	}

	frame := readFrame(t, sink, "tool_result")
	if frame["id"] != "req-2" || frame["success"] != true || frame["output"] != "hi\n" {
		t.Fatalf("unexpected tool_result: %v", frame)
	}
	ctx, fn := deps.Tools.(*agentdomainmocks.FakeToolService).ExecuteToolDirectArgsForCall(0)
	if fn.Name != "Bash" || !agentdomain.IsToolApproved(ctx) {
		t.Fatalf("expected approved Bash execution, got %v approved=%v", fn.Name, agentdomain.IsToolApproved(ctx))
	}
}

func TestPanelToolRequestDenied(t *testing.T) {
	deps := approvingToolDeps(true)
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "tool_request", "id": "req-3", "tool_name": "Bash", "tool_args": "{}"})
	readFrame(t, sink, "approval_request")
	handle(t, p, map[string]any{"type": "approval_response", "tool_call_id": "req-3", "approved": false})

	if frame := readFrame(t, sink, "tool_result"); frame["id"] != "req-3" || frame["success"] != false {
		t.Fatalf("unexpected tool_result: %v", frame)
	}
	if deps.Tools.(*agentdomainmocks.FakeToolService).ExecuteToolDirectCallCount() != 0 {
		t.Fatal("denied tool call was executed")
	}
}

func TestPanelLeavesForeignApprovalsToTheTurn(t *testing.T) {
	p, _ := startPanel(panelDeps())
	if handle(t, p, map[string]any{"type": "approval_response", "tool_call_id": "agent-call", "approved": true}) {
		t.Fatal("an approval the panel does not own must reach the running turn")
	}
}

func TestPanelToolRequestRecordedInConversation(t *testing.T) {
	deps := approvingToolDeps(false)
	repo := newPanelRepo()
	deps.Conversations = repo
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "tool_request", "id": "req-4", "tool_name": "Bash", "tool_args": `{"command":"echo hi"}`})
	readFrame(t, sink, "tool_result")

	msgs := repo.GetMessages()
	if len(msgs) != 2 || msgs[0].Message.Role != sdk.Assistant || msgs[1].Message.Role != sdk.Tool {
		t.Fatalf("expected assistant tool_call + tool result entries, got %d: %+v", len(msgs), msgs)
	}
	if msgs[1].ToolExecution == nil || !msgs[1].ToolExecution.Success || msgs[1].ToolExecution.ToolCallID != "req-4" {
		t.Fatalf("unexpected tool entry: %+v", msgs[1].ToolExecution)
	}

	handle(t, p, map[string]any{"type": "resume_conversation"})
	first := readFrame(t, sink, "MESSAGES_SNAPSHOT")["messages"].([]any)[0].(map[string]any)
	if calls, _ := first["toolCalls"].([]any); len(calls) != 1 {
		t.Fatalf("expected toolCalls on the assistant snapshot entry, got %v", first)
	}
}

func TestPanelIgnoresUnknownFrames(t *testing.T) {
	p, _ := startPanel(panelDeps())
	for _, typ := range []string{"interrupt", "user_question_response", "unknown_frame"} {
		if handle(t, p, map[string]any{"type": typ}) {
			t.Fatalf("%s must not be consumed by the panel", typ)
		}
	}
}
