package headless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	a2amocks "github.com/inference-gateway/cli/tests/mocks/a2a"
	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	llmmocks "github.com/inference-gateway/cli/tests/mocks/llm"
	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"
	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"
	storagemocks "github.com/inference-gateway/cli/tests/mocks/storage"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	llm "github.com/inference-gateway/cli/internal/platform/llm"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	projects "github.com/inference-gateway/cli/internal/projects"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
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

// runInput is the run_agent_input frame carrying one user message.
func runInput(content string) map[string]any {
	return map[string]any{"type": "run_agent_input", "input": map[string]any{
		"messages": []map[string]any{{"id": "m1", "role": "user", "content": content}},
	}}
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

func TestPanelListIssues(t *testing.T) {
	gh := &agentdomainmocks.FakeGitHubIssueService{}
	gh.IsAvailableReturns(true)
	gh.ListIssuesReturns([]agentdomain.GitHubIssue{
		{Number: 57, Title: "Fix the flaky test", State: "OPEN"},
		{Number: 71, Title: "Ship the release", State: "OPEN"},
	}, nil)
	deps := panelDeps()
	deps.Issues = gh
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "list_issues"})
	frame := readFrame(t, sink, "issues")
	raw, ok := frame["issues"].([]any)
	if !ok || len(raw) != 2 {
		t.Fatalf("expected 2 issues, got %v", frame["issues"])
	}
	first := raw[0].(map[string]any)
	if first["number"] != float64(57) || first["title"] != "Fix the flaky test" || first["state"] != "OPEN" {
		t.Fatalf("unexpected issue entry: %v", first)
	}
}

func TestPanelListIssuesAnswersEmptyWhereTheTUIShowsNothing(t *testing.T) {
	tests := []struct {
		name string
		deps func(deps PanelDeps) PanelDeps
	}{
		{
			name: "no service",
			deps: func(deps PanelDeps) PanelDeps { return deps },
		},
		{
			name: "service unavailable",
			deps: func(deps PanelDeps) PanelDeps {
				gh := &agentdomainmocks.FakeGitHubIssueService{}
				gh.IsAvailableReturns(false)
				deps.Issues = gh
				return deps
			},
		},
		{
			name: "failed list",
			deps: func(deps PanelDeps) PanelDeps {
				gh := &agentdomainmocks.FakeGitHubIssueService{}
				gh.IsAvailableReturns(true)
				gh.ListIssuesReturns(nil, errors.New("gh failed"))
				deps.Issues = gh
				return deps
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := tt.deps(panelDeps())
			p, sink := startPanel(deps)

			handle(t, p, map[string]any{"type": "list_issues"})
			if raw, ok := readFrame(t, sink, "issues")["issues"].([]any); !ok || len(raw) != 0 {
				t.Fatalf("expected an empty issues list, got %v", raw)
			}
		})
	}
}

func TestPanelListShortcuts(t *testing.T) {
	registry := shortcuts.NewRegistry()
	registry.Register(shortcuts.NewExitShortcut())
	registry.Register(shortcuts.NewCustomShortcut(shortcuts.CustomShortcutConfig{Name: "deploy", Description: "Deploy the app"}, nil, nil, nil, nil))
	deps := panelDeps()
	deps.Shortcuts = registry
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "list_shortcuts"})
	listed, ok := readFrame(t, sink, "shortcuts")["shortcuts"].([]any)
	if !ok || len(listed) != 2 {
		t.Fatalf("expected 2 shortcuts, got %v", listed)
	}
	custom, builtin := listed[0].(map[string]any), listed[1].(map[string]any)
	if custom["name"] != "deploy" || custom["description"] != "Deploy the app" || custom["kind"] != "shortcut" {
		t.Errorf("unexpected custom shortcut entry: %v", custom)
	}
	exit := shortcuts.NewExitShortcut()
	if builtin["name"] != "exit" || builtin["usage"] != exit.GetUsage() || builtin["kind"] != "command" {
		t.Errorf("unexpected built-in command entry: %v", builtin)
	}
}

func TestPanelListShortcutsAnswersEmptyWithoutARegistry(t *testing.T) {
	p, sink := startPanel(panelDeps())

	handle(t, p, map[string]any{"type": "list_shortcuts"})
	if listed, ok := readFrame(t, sink, "shortcuts")["shortcuts"].([]any); !ok || len(listed) != 0 {
		t.Fatalf("expected an empty shortcuts list, got %v", listed)
	}
}

func TestPanelListTasks(t *testing.T) {
	jobs := &schedmocks.FakeBackgroundTaskRegistry{}
	finished := time.Date(2026, 11, 6, 10, 1, 0, 0, time.UTC)
	jobs.SnapshotReturns([]scheddomain.TrackedJob{
		{Meta: scheddomain.JobMeta{ID: "shell-1", Kind: scheddomain.JobKindShell, Label: "build", Detail: "go build"}, Status: scheddomain.JobRunning},
		{Meta: scheddomain.JobMeta{ID: "sub-1", SessionID: "s1", Kind: scheddomain.JobKindSubagent}, Status: scheddomain.JobCompleted, CompletedAt: &finished, Output: "done"},
	})
	deps := panelDeps()
	deps.Jobs = jobs
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "list_tasks"})
	listed, ok := readFrame(t, sink, "tasks")["tasks"].([]any)
	if !ok || len(listed) != 2 {
		t.Fatalf("expected 2 tasks, got %v", listed)
	}
	shell, sub := listed[0].(map[string]any), listed[1].(map[string]any)
	if shell["id"] != "shell-1" || shell["kind"] != "shell" || shell["detail"] != "go build" || shell["status"] != "running" {
		t.Errorf("unexpected shell entry: %v", shell)
	}
	if sub["session_id"] != "s1" || sub["status"] != "completed" || sub["output"] != "done" || sub["completed_at"] == nil {
		t.Errorf("unexpected subagent entry: %v", sub)
	}
}

func TestPanelListTasksAnswersEmptyWithoutASupervisor(t *testing.T) {
	p, sink := startPanel(panelDeps())

	handle(t, p, map[string]any{"type": "list_tasks"})
	if listed, ok := readFrame(t, sink, "tasks")["tasks"].([]any); !ok || len(listed) != 0 {
		t.Fatalf("expected an empty tasks list, got %v", listed)
	}
}

func TestPanelCancelTask(t *testing.T) {
	running := func(kind scheddomain.JobKind) []scheddomain.TrackedJob {
		return []scheddomain.TrackedJob{{Meta: scheddomain.JobMeta{ID: "job-1", Kind: kind}, Status: scheddomain.JobRunning}}
	}
	tests := []struct {
		name      string
		kind      scheddomain.JobKind
		windErr   error
		wantWound int
		wantA2A   int
		wantError string
	}{
		{name: "a shell winds down", kind: scheddomain.JobKindShell, wantWound: 1},
		{name: "an A2A task is cancelled on its agent", kind: scheddomain.JobKindA2A, wantA2A: 1},
		{name: "a failure is reported", kind: scheddomain.JobKindShell, windErr: errors.New("job not found"), wantWound: 1, wantError: "job not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := &schedmocks.FakeBackgroundTaskRegistry{}
			jobs.SnapshotReturns(running(tt.kind))
			jobs.WindJobReturns(tt.windErr)
			remote := &a2amocks.FakeBackgroundTaskService{}
			deps := panelDeps()
			deps.Jobs, deps.A2ATasks = jobs, remote
			p, sink := startPanel(deps)

			handle(t, p, map[string]any{"type": "cancel_task", "task_id": "job-1"})
			reply := readFrame(t, sink, "task_cancelled")

			if reply["task_id"] != "job-1" {
				t.Errorf("reply named %v, want job-1", reply["task_id"])
			}
			if got, _ := reply["error"].(string); got != tt.wantError {
				t.Errorf("error = %q, want %q", got, tt.wantError)
			}
			if jobs.WindJobCallCount() != tt.wantWound || remote.CancelBackgroundTaskCallCount() != tt.wantA2A {
				t.Errorf("wound %d and cancelled %d on the agent, want %d and %d", jobs.WindJobCallCount(), remote.CancelBackgroundTaskCallCount(), tt.wantWound, tt.wantA2A)
			}
			if tt.wantWound == 1 {
				if id, signal := jobs.WindJobArgsForCall(0); id != "job-1" || signal != scheddomain.WindStop {
					t.Errorf("wound %q with %v, want job-1 stopped", id, signal)
				}
			}
		})
	}
}

func TestPanelListFiles(t *testing.T) {
	served := &agentdomainmocks.FakeFileService{}
	served.ListProjectFilesReturns([]string{"main.go", "docs/readme.md"}, nil)
	failing := &agentdomainmocks.FakeFileService{}
	failing.ListProjectFilesReturns(nil, errors.New("walk failed"))

	tests := []struct {
		name  string
		files agentdomain.FileService
		want  []any
	}{
		{name: "served list", files: served, want: []any{"main.go", "docs/readme.md"}},
		{name: "no service", files: nil, want: []any{}},
		{name: "failed walk", files: failing, want: []any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := panelDeps()
			deps.Files = tt.files
			p, sink := startPanel(deps)

			handle(t, p, map[string]any{"type": "list_files"})
			got, ok := readFrame(t, sink, "files")["files"].([]any)
			if !ok || !slices.Equal(got, tt.want) {
				t.Fatalf("files = %v, want %v", got, tt.want)
			}
		})
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

func TestPanelListModelsCarriesTheGatewaysCatalog(t *testing.T) {
	gateway := &sdkmocks.FakeClient{}
	chat := sdk.ModelModalities{Input: []sdk.Modality{sdk.ModalityText, sdk.ModalityImage}, Output: []sdk.Modality{sdk.ModalityText}}
	gateway.ListModelsReturns(&sdk.ListModelsResponse{Data: []sdk.Model{
		{ID: "a/x", Modalities: &chat, ContextWindow: &sdk.ContextWindow{Tokens: 128000}, Pricing: &sdk.Pricing{InputPerToken: "0.000001", OutputPerToken: "0.000002"}},
	}}, nil)
	deps := panelDeps()
	deps.Models = conversation.NewHTTPModelService(gateway)
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{"type": "list_models"})

	catalog, _ := readFrame(t, sink, "models")["catalog"].([]any)
	if len(catalog) != 1 {
		t.Fatalf("catalog = %v, want the gateway's model with its metadata", catalog)
	}
	entry, _ := catalog[0].(map[string]any)
	if entry["id"] != "a/x" || entry["context_window"] == nil || entry["pricing"] == nil || entry["modalities"] == nil {
		t.Errorf("catalog entry = %v, want its context window, pricing and modalities", entry)
	}
}

func TestPanelKeepsAClientsNoteAsAnAssistantEntry(t *testing.T) {
	deps := panelDeps()
	p, _ := startPanel(deps)

	if !handle(t, p, map[string]any{"type": "add_note", "content": ""}) {
		t.Fatal("the panel left an add_note to the turn loop")
	}
	if !handle(t, p, map[string]any{"type": "add_note", "content": "pushed", "role": "system"}) {
		t.Fatal("the panel left an add_note to the turn loop")
	}

	entries := deps.Conversations.GetMessages()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want only the note with content", len(entries))
	}
	if text, _ := entries[0].Message.Content.AsMessageContent0(); entries[0].Message.Role != sdk.Assistant || text != "pushed" {
		t.Errorf("entry = %s %q, want an assistant note whatever role the client names", entries[0].Message.Role, text)
	}
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

// historyStore is a shell history store that keeps entries in memory.
func historyStore(entries ...string) *storagemocks.FakeShellHistoryStorage {
	var mu sync.Mutex
	store := &storagemocks.FakeShellHistoryStorage{}
	store.AppendHistoryCalls(func(_ context.Context, project, command string) error {
		mu.Lock()
		defer mu.Unlock()
		if project != projects.Path() {
			return fmt.Errorf("history recorded for %q, want the worker's project", project)
		}
		entries = append(entries, command)
		return nil
	})
	store.LoadHistoryCalls(func(_ context.Context, project string, limit int) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		if project != projects.Path() {
			return nil, fmt.Errorf("history listed for %q, want the worker's project", project)
		}
		out := entries
		if limit > 0 && len(out) > limit {
			out = out[len(out)-limit:]
		}
		return append([]string{}, out...), nil
	})
	return store
}

func TestPanelHistoryRoundTrip(t *testing.T) {
	deps := panelDeps()
	deps.History = historyStore("from the tui")
	p, sink := startPanel(deps)

	for _, msg := range []string{"first", "second"} {
		if handle(t, p, runInput(msg)) {
			t.Fatal("run_agent_input must be left for the turn loop")
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

func TestPanelCompletionRequest(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		answer    llm.Response
		err       error
		wantModel string
		wantFrame map[string]any
	}{
		{
			name:      "runs on the model the request names",
			model:     "openai/gpt-4o",
			answer:    llm.Response{Content: "feat: add x"},
			wantModel: "openai/gpt-4o",
			wantFrame: map[string]any{"content": "feat: add x", "error": ""},
		},
		{
			name:      "runs on the worker's model when the request names none",
			answer:    llm.Response{Reasoning: "fix: y"},
			wantModel: "ollama_cloud/glm-5.3-flash",
			wantFrame: map[string]any{"reasoning": "fix: y", "error": ""},
		},
		{
			name:      "reports a failed call",
			model:     "openai/gpt-4o",
			err:       errors.New("gateway down"),
			wantModel: "openai/gpt-4o",
			wantFrame: map[string]any{"content": "", "error": "gateway down"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completer := &llmmocks.FakeCompleter{}
			completer.CompleteReturns(tt.answer, tt.err)
			models := &convmocks.FakeModelService{}
			models.GetCurrentModelReturns("ollama_cloud/glm-5.3-flash")
			deps := panelDeps()
			deps.Completer, deps.Models = completer, models
			p, sink := startPanel(deps)

			consumed := handle(t, p, map[string]any{
				"type": "completion_request", "id": "c-1", "model": tt.model,
				"system": "be terse", "prompt": "write a commit message", "max_tokens": 1000,
			})
			frame := readFrame(t, sink, "completion_result")

			if !consumed {
				t.Fatal("the panel left its completion_request to the turn loop")
			}
			_, req := completer.CompleteArgsForCall(0)
			want := llm.Request{Model: tt.wantModel, System: "be terse", Prompt: "write a commit message", MaxTokens: 1000}
			if req != want {
				t.Errorf("Complete(%+v), want %+v", req, want)
			}
			if frame["id"] != "c-1" {
				t.Errorf("completion_result id = %v, want c-1", frame["id"])
			}
			for key, value := range tt.wantFrame {
				if frame[key] != value {
					t.Errorf("completion_result %s = %v, want %v", key, frame[key], value)
				}
			}
		})
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
