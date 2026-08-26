package infrastructure

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	websocket "github.com/gorilla/websocket"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	toolformatter "github.com/inference-gateway/cli/internal/presentation/tui/toolformatter"
)

// readFrameOfType reads frames until one with the given type arrives, failing
// on timeout. Returns the decoded frame.
func readFrameOfType(t *testing.T, conn *websocket.Conn, typ string) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var frame map[string]any
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("read %q: %v", typ, err)
		}
		if frame["type"] == typ {
			return frame
		}
	}
}

func toolApprovalEvent(id, name, args string) agentdomain.ToolApprovalRequestedEvent {
	return agentdomain.ToolApprovalRequestedEvent{
		RequestID:    id,
		ResponseChan: make(chan agentdomain.ApprovalAction, 1),
		ToolCall: sdk.ChatCompletionMessageToolCall{
			ID:       "call-" + id,
			Function: sdk.ChatCompletionMessageToolCallFunction{Name: name, Arguments: args},
		},
	}
}

func TestExtensionBridgeApprovalRoundTrip(t *testing.T) {
	notifier := &recordingNotifier{}
	events := conversation.NewEventBridge()
	bridge := startBridge(t, bridgeConfig(), notifier, events)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	time.Sleep(50 * time.Millisecond)
	events.Publish(toolApprovalEvent("req-1", "Bash", `{"command":"ls"}`))

	req := readFrameOfType(t, conn, "approval_request")
	if req["request_id"] != "req-1" || req["tool_name"] != "Bash" || req["tool_args"] != `{"command":"ls"}` {
		t.Fatalf("unexpected approval_request: %v", req)
	}

	if err := conn.WriteJSON(map[string]string{"type": "approval_response", "request_id": "req-1", "action": "approve"}); err != nil {
		t.Fatalf("write response: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		found := false
		for _, ev := range notifier.all() {
			resp, ok := ev.(agentdomain.ToolApprovalResponseEvent)
			if !ok {
				continue
			}
			if resp.Action != agentdomain.ApprovalApprove {
				t.Fatalf("expected approve, got %v", resp.Action)
			}
			if resp.ToolCall.Function.Name != "Bash" {
				t.Fatalf("wrong tool call echoed back: %+v", resp.ToolCall)
			}
			found = true
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("notifier never received the approval response")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if resolved := readFrameOfType(t, conn, "approval_resolved"); resolved["request_id"] != "req-1" {
		t.Fatalf("unexpected approval_resolved: %v", resolved)
	}
}

// A tool-call chat event streaming after the approval_request must NOT clear the
// card - only an explicit ToolApprovalResolvedEvent does. Regression test for the
// card vanishing before the user could answer.
func TestExtensionBridgeApprovalSurvivesTrailingEvents(t *testing.T) {
	events := conversation.NewEventBridge()
	bridge := startBridge(t, bridgeConfig(), nil, events)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	time.Sleep(50 * time.Millisecond)
	events.Publish(toolApprovalEvent("req-9", "Write", `{"file_path":"x"}`))
	readFrameOfType(t, conn, "approval_request")

	events.Publish(agentdomain.ChatChunkEvent{Content: "streamed"})
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var frame map[string]any
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("read after trailing chunk: %v", err)
	}
	if frame["type"] == "approval_resolved" {
		t.Fatal("a trailing chat event resolved the approval before the user answered")
	}

	events.Publish(agentdomain.ToolApprovalResolvedEvent{})
	if resolved := readFrameOfType(t, conn, "approval_resolved"); resolved["request_id"] != "req-9" {
		t.Fatalf("unexpected approval_resolved: %v", resolved)
	}
}

func TestExtensionBridgeApprovalUnknownActionRejects(t *testing.T) {
	notifier := &recordingNotifier{}
	events := conversation.NewEventBridge()
	bridge := startBridge(t, bridgeConfig(), notifier, events)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	time.Sleep(50 * time.Millisecond)
	events.Publish(toolApprovalEvent("req-2", "Bash", "{}"))
	readFrameOfType(t, conn, "approval_request")

	if err := conn.WriteJSON(map[string]string{"type": "approval_response", "request_id": "req-2", "action": "wat"}); err != nil {
		t.Fatalf("write response: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, ev := range notifier.all() {
			if resp, ok := ev.(agentdomain.ToolApprovalResponseEvent); ok {
				if resp.Action != agentdomain.ApprovalReject {
					t.Fatalf("unknown action should reject, got %v", resp.Action)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("notifier never received the approval response")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A stale approval_response for an already-answered id must be a no-op, not a
// second decision.
func TestExtensionBridgeApprovalIgnoresUnknownID(t *testing.T) {
	notifier := &recordingNotifier{}
	events := conversation.NewEventBridge()
	bridge := startBridge(t, bridgeConfig(), notifier, events)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "approval_response", "request_id": "ghost", "action": "approve"}); err != nil {
		t.Fatalf("write response: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	for _, ev := range notifier.all() {
		if _, ok := ev.(agentdomain.ToolApprovalResponseEvent); ok {
			t.Fatal("unknown request id must not produce an approval response")
		}
	}
}

// recordingNotifier is a thread-safe agentdomain.UINotifier that collects every
// notified event.
type recordingNotifier struct {
	mu     sync.Mutex
	events []any
}

func (r *recordingNotifier) Notify(event any) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

// all returns a snapshot of the collected events.
func (n *recordingNotifier) all() []any {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]any(nil), n.events...)
}

func bridgeConfig() *config.BrowserUseConfig {
	cfg := config.DefaultBrowserUseConfig()
	cfg.Enabled = true
	cfg.Backend = config.BrowserBackendExtension
	cfg.Extension.Port = 0
	cfg.Extension.Token = "test-token"
	cfg.Browser.TimeoutSeconds = 2
	return cfg
}

func startBridge(t *testing.T, cfg *config.BrowserUseConfig, notifier agentdomain.UINotifier, events agentdomain.EventBridge) *ExtensionBridge {
	t.Helper()
	bridge := NewExtensionBridge(cfg, notifier, nil, events, nil, "test-session", "")
	if err := bridge.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(bridge.Close)
	return bridge
}

func startBridgeWithRepo(t *testing.T, cfg *config.BrowserUseConfig, repo convdomain.ConversationRepository) *ExtensionBridge {
	t.Helper()
	bridge := NewExtensionBridge(cfg, nil, repo, nil, nil, "test-session", "")
	if err := bridge.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(bridge.Close)
	return bridge
}

func startBridgeWithSkills(t *testing.T, cfg *config.BrowserUseConfig, skills agentdomain.SkillsService) *ExtensionBridge {
	t.Helper()
	bridge := NewExtensionBridge(cfg, nil, nil, nil, skills, "test-session", "")
	if err := bridge.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(bridge.Close)
	return bridge
}

// newBridgeRepo builds a persistent repo backed by in-memory storage.
func newBridgeRepo() *conversation.PersistentConversationRepository {
	return conversation.NewPersistentConversationRepository(&toolformatter.ToolFormatterService{}, nil, storage.NewMemoryStorage())
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

func dial(t *testing.T, bridge *ExtensionBridge) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+bridge.Addr()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// hello authenticates and consumes the ack and conversation snapshot frames.
func hello(t *testing.T, conn *websocket.Conn, token string) {
	t.Helper()
	if err := conn.WriteJSON(map[string]string{"type": "browser_hello", "token": token}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var ack map[string]any
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if ack["type"] != "browser_hello_ack" {
		t.Fatalf("expected browser_hello_ack, got %v", ack)
	}
}

func TestExtensionBridgeRejectsBadToken(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)
	conn := dial(t, bridge)

	if err := conn.WriteJSON(map[string]string{"type": "browser_hello", "token": "wrong"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var frame map[string]any
	if err := conn.ReadJSON(&frame); err == nil {
		t.Fatalf("expected connection close, got frame %v", frame)
	}
}

func TestExtensionBridgeFailsFastWithoutConnection(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)

	_, err := bridge.Navigate(context.Background(), "https://example.com")
	if err == nil || !strings.Contains(err.Error(), "no browser extension connected") {
		t.Fatalf("expected no-extension error, got %v", err)
	}
}

func TestExtensionBridgeRefusesToStartWithoutToken(t *testing.T) {
	cfg := bridgeConfig()
	cfg.Extension.Token = ""
	bridge := NewExtensionBridge(cfg, nil, nil, nil, nil, "s", "")
	if err := bridge.Start(); err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("expected token error, got %v", err)
	}
	if _, err := bridge.Navigate(context.Background(), "https://example.com"); err == nil || !strings.Contains(err.Error(), "token is empty") {
		t.Fatalf("expected stored start error from verb, got %v", err)
	}
}

func TestExtensionBridgeNavigateRoundTrip(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	// Fake extension: answer the first browser_command.
	go func() {
		for {
			var cmd map[string]any
			if err := conn.ReadJSON(&cmd); err != nil {
				return
			}
			if cmd["type"] == "browser_command" {
				_ = conn.WriteJSON(map[string]any{
					"type":  "browser_result",
					"id":    cmd["id"],
					"url":   cmd["url"],
					"title": "Example Domain",
				})
				return
			}
		}
	}()

	// The connection is adopted asynchronously after the ack; retry briefly.
	var result browserdomain.BrowserToolResult
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		result, err = bridge.Navigate(context.Background(), "https://example.com")
		if err == nil || !strings.Contains(err.Error(), "no browser extension connected") || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if result.URL != "https://example.com" || result.Title != "Example Domain" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExtensionBridgeUserMessageReachesNotifier(t *testing.T) {
	notifier := &recordingNotifier{}
	bridge := startBridge(t, bridgeConfig(), notifier, nil)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "user_message", "content": "hello from the browser"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range notifier.all() {
			if input, ok := ev.(agentdomain.UserInputEvent); ok {
				if input.Content != "hello from the browser" {
					t.Fatalf("unexpected content: %q", input.Content)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("notifier never received the user message")
}

func TestExtensionBridgeMirrorsChatEvents(t *testing.T) {
	events := conversation.NewEventBridge()
	bridge := startBridge(t, bridgeConfig(), nil, events)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	// Give the chat pump a moment to subscribe, then publish a chunk.
	time.Sleep(50 * time.Millisecond)
	events.Publish(agentdomain.ChatChunkEvent{Content: "streamed text"})

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var frame map[string]any
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		if frame["type"] == "chat_event" {
			return
		}
	}
}

func TestExtensionBridgeReplacesConnection(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)

	first := dial(t, bridge)
	hello(t, first, "test-token")

	second := dial(t, bridge)
	hello(t, second, "test-token")

	// The first connection should be closed by the replacement.
	_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := first.ReadMessage(); err != nil {
			break
		}
	}

	// Commands go to the second connection.
	go func() {
		for {
			var cmd map[string]any
			if err := second.ReadJSON(&cmd); err != nil {
				return
			}
			if cmd["type"] == "browser_command" {
				_ = second.WriteJSON(map[string]any{"type": "browser_result", "id": cmd["id"], "url": cmd["url"]})
				return
			}
		}
	}()

	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = bridge.Read(context.Background(), "")
		if err == nil || !strings.Contains(err.Error(), "no browser extension connected") || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Read after replacement: %v", err)
	}
}

func TestExtensionBridgeServesArtifacts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cat.png"), []byte("PNGDATA"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	bridge := NewExtensionBridge(bridgeConfig(), nil, nil, nil, nil, "s", dir)
	if err := bridge.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(bridge.Close)

	body, status := httpGet(t, "http://"+bridge.Addr()+"/artifacts/cat.png")
	if status != http.StatusOK || body != "PNGDATA" {
		t.Fatalf("serve artifact: status=%d body=%q", status, body)
	}

	_, status = httpGet(t, "http://"+bridge.Addr()+"/artifacts/../extension_bridge.go")
	if status == http.StatusOK {
		t.Fatalf("path traversal was not blocked (status %d)", status)
	}
}

func TestExtensionBridgeWithoutArtifactsDirHasNoRoute(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)
	if _, status := httpGet(t, "http://"+bridge.Addr()+"/artifacts/cat.png"); status == http.StatusOK {
		t.Fatalf("expected no /artifacts/ route, got status %d", status)
	}
}

func httpGet(t *testing.T, url string) (string, int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body), resp.StatusCode
}

func TestExtensionBridgeListConversations(t *testing.T) {
	repo := newBridgeRepo()
	firstID := seedConversation(t, repo, "First convo", "hello one")
	secondID := seedConversation(t, repo, "Second convo", "hello two")

	bridge := startBridgeWithRepo(t, bridgeConfig(), repo)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "list_conversations"}); err != nil {
		t.Fatalf("write list_conversations: %v", err)
	}

	frame := readFrameOfType(t, conn, "conversations")
	raw, ok := frame["conversations"].([]any)
	if !ok || len(raw) != 2 {
		t.Fatalf("expected 2 conversations, got %v", frame["conversations"])
	}

	byID := map[string]map[string]any{}
	for _, c := range raw {
		entry := c.(map[string]any)
		byID[entry["id"].(string)] = entry
	}

	first, ok := byID[firstID]
	if !ok {
		t.Fatalf("first conversation %s missing from %v", firstID, byID)
	}
	if first["title"] != "First convo" {
		t.Fatalf("first title = %v, want %q", first["title"], "First convo")
	}
	if count, _ := first["message_count"].(float64); count != 1 {
		t.Fatalf("first message_count = %v, want 1", first["message_count"])
	}
	if at, _ := first["updated_at"].(string); at == "" {
		t.Fatalf("first updated_at missing: %v", first["updated_at"])
	}

	second, ok := byID[secondID]
	if !ok {
		t.Fatalf("second conversation %s missing from %v", secondID, byID)
	}
	if second["title"] != "Second convo" {
		t.Fatalf("second title = %v, want %q", second["title"], "Second convo")
	}
}

func TestExtensionBridgeResumeConversation(t *testing.T) {
	repo := newBridgeRepo()
	targetID := seedConversation(t, repo, "Older convo", "resume me please")
	seedConversation(t, repo, "Current convo", "current message")

	bridge := startBridgeWithRepo(t, bridgeConfig(), repo)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "resume_conversation", "id": targetID}); err != nil {
		t.Fatalf("write resume_conversation: %v", err)
	}

	frame := readFrameOfType(t, conn, "conversation_snapshot")
	msgs, ok := frame["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message in snapshot, got %v", frame["messages"])
	}
	if content := msgs[0].(map[string]any)["content"]; content != "resume me please" {
		t.Fatalf("snapshot content = %v, want %q", content, "resume me please")
	}
	if got := repo.GetCurrentConversationID(); got != targetID {
		t.Fatalf("active conversation = %s, want %s", got, targetID)
	}
}

func TestExtensionBridgeNoAutoSnapshotOnConnect(t *testing.T) {
	repo := newBridgeRepo()
	seedConversation(t, repo, "Some convo", "hello")

	bridge := startBridgeWithRepo(t, bridgeConfig(), repo)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	var frame map[string]any
	if err := conn.ReadJSON(&frame); err == nil {
		t.Fatalf("expected no unsolicited frame after connect, got %v", frame)
	}
}

func TestExtensionBridgeListSkills(t *testing.T) {
	skills := &agentdomainmocks.FakeSkillsService{}
	skills.ListReturns([]agentdomain.Skill{
		{Name: "tmux", Description: "drive tmux", Scope: agentdomain.SkillScopeUser},
		{Name: "deploy", Description: "ship it", Scope: agentdomain.SkillScopeAgents},
		{Name: "notion", Scope: agentdomain.SkillScopePlugin, PluginName: "notion"},
	})
	bridge := startBridgeWithSkills(t, bridgeConfig(), skills)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "list_skills"}); err != nil {
		t.Fatalf("write list_skills: %v", err)
	}

	frame := readFrameOfType(t, conn, "skills")
	raw, ok := frame["skills"].([]any)
	if !ok || len(raw) != 3 {
		t.Fatalf("expected 3 skills, got %v", frame["skills"])
	}

	byName := map[string]map[string]any{}
	for _, s := range raw {
		e := s.(map[string]any)
		byName[e["name"].(string)] = e
	}
	if byName["tmux"]["scope"] != "user" {
		t.Fatalf("tmux scope = %v, want user", byName["tmux"]["scope"])
	}
	if byName["deploy"]["scope"] != "agents" {
		t.Fatalf("deploy scope = %v, want agents", byName["deploy"]["scope"])
	}
	if _, ok := byName["notion:notion"]; !ok {
		t.Fatalf("plugin skill missing qualified name, got keys %v", byName)
	}
}

func TestExtensionBridgeListSkillsWithoutServiceIsEmpty(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "list_skills"}); err != nil {
		t.Fatalf("write list_skills: %v", err)
	}

	frame := readFrameOfType(t, conn, "skills")
	if raw, ok := frame["skills"].([]any); ok && len(raw) != 0 {
		t.Fatalf("expected no skills, got %v", frame["skills"])
	}
}

func startBridgeWithTools(t *testing.T, cfg *config.BrowserUseConfig, toolSvc agentdomain.ToolService, approval agentdomain.ApprovalPolicy, models convdomain.ModelService, defaultModel string) *ExtensionBridge {
	t.Helper()
	bridge := startBridge(t, cfg, nil, nil)
	bridge.SetToolExecution(toolSvc, approval, models, defaultModel)
	return bridge
}

func TestExtensionBridgeToolRequestUnknownToolKeepsSocketOpen(t *testing.T) {
	toolSvc := &agentdomainmocks.FakeToolService{}
	toolSvc.IsToolEnabledReturns(false)
	bridge := startBridgeWithTools(t, bridgeConfig(), toolSvc, nil, nil, "")
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "tool_request", "id": "req-1", "tool_name": "Nope", "tool_args": "{}"}); err != nil {
		t.Fatalf("write tool_request: %v", err)
	}

	frame := readFrameOfType(t, conn, "tool_result")
	if frame["id"] != "req-1" || frame["success"] != false || frame["error"] == "" {
		t.Fatalf("unexpected tool_result: %v", frame)
	}

	if err := conn.WriteJSON(map[string]string{"type": "list_skills"}); err != nil {
		t.Fatalf("write list_skills: %v", err)
	}
	readFrameOfType(t, conn, "skills")
}

func TestExtensionBridgeToolRequestApproved(t *testing.T) {
	toolSvc := &agentdomainmocks.FakeToolService{}
	toolSvc.IsToolEnabledReturns(true)
	toolSvc.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{
		Success: true,
		Data:    &agentdomain.BashToolResult{Output: "hi\n"},
	}, nil)
	approval := &agentdomainmocks.FakeApprovalPolicy{}
	approval.ShouldRequireApprovalReturns(true)
	bridge := startBridgeWithTools(t, bridgeConfig(), toolSvc, approval, nil, "")
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "tool_request", "id": "req-2", "tool_name": "Bash", "tool_args": `{"command":"echo hi"}`}); err != nil {
		t.Fatalf("write tool_request: %v", err)
	}

	req := readFrameOfType(t, conn, "approval_request")
	if req["tool_name"] != "Bash" {
		t.Fatalf("unexpected approval_request: %v", req)
	}
	if err := conn.WriteJSON(map[string]string{"type": "approval_response", "request_id": req["request_id"].(string), "action": "approve"}); err != nil {
		t.Fatalf("write approval_response: %v", err)
	}
	readFrameOfType(t, conn, "approval_resolved")

	frame := readFrameOfType(t, conn, "tool_result")
	if frame["id"] != "req-2" || frame["success"] != true || frame["output"] != "hi\n" {
		t.Fatalf("unexpected tool_result: %v", frame)
	}

	ctx, fn := toolSvc.ExecuteToolDirectArgsForCall(0)
	if fn.Name != "Bash" || !agentdomain.IsToolApproved(ctx) {
		t.Fatalf("expected approved Bash execution, got %v approved=%v", fn.Name, agentdomain.IsToolApproved(ctx))
	}
}

func TestExtensionBridgeToolRequestRecordedInConversation(t *testing.T) {
	toolSvc := &agentdomainmocks.FakeToolService{}
	toolSvc.IsToolEnabledReturns(true)
	toolSvc.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{
		ToolName: "Bash",
		Success:  true,
		Data:     &agentdomain.BashToolResult{Output: "hi\n"},
	}, nil)
	repo := newBridgeRepo()
	events := conversation.NewEventBridge()
	bridge := NewExtensionBridge(bridgeConfig(), nil, repo, events, nil, "test-session", "")
	if err := bridge.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(bridge.Close)
	bridge.SetToolExecution(toolSvc, nil, nil, "")
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "tool_request", "id": "req-4", "tool_name": "Bash", "tool_args": `{"command":"echo hi"}`}); err != nil {
		t.Fatalf("write tool_request: %v", err)
	}
	seen := map[string]bool{}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for !seen["tool_result"] || !seen["TOOL_CALL_RESULT"] {
		var frame map[string]any
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("read frames: %v (seen %v)", err, seen)
		}
		seen[frame["type"].(string)] = true
		if ev, ok := frame["event"].(map[string]any); ok {
			seen[ev["type"].(string)] = true
		}
	}
	if !seen["TOOL_CALL_START"] {
		t.Fatalf("expected TOOL_CALL_START, saw %v", seen)
	}

	msgs := repo.GetMessages()
	if len(msgs) != 2 || msgs[0].Message.Role != sdk.Assistant || msgs[1].Message.Role != sdk.Tool {
		t.Fatalf("expected assistant tool_call + tool result entries, got %d: %+v", len(msgs), msgs)
	}
	if msgs[1].ToolExecution == nil || !msgs[1].ToolExecution.Success || msgs[1].ToolExecution.ToolCallID != "req-4" {
		t.Fatalf("unexpected tool entry: %+v", msgs[1].ToolExecution)
	}
}

func TestExtensionBridgeInterruptCancelsActiveTurn(t *testing.T) {
	agentSvc := &agentdomainmocks.FakeAgentService{}
	events := conversation.NewEventBridge()
	bridge := startBridge(t, bridgeConfig(), nil, events)
	bridge.SetAgentService(agentSvc)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	// chatPump subscribes asynchronously after adopt; republish until it has seen the start.
	deadline := time.Now().Add(2 * time.Second)
	for id, _ := bridge.activeRequestID.Load().(string); id != "turn-1" && time.Now().Before(deadline); id, _ = bridge.activeRequestID.Load().(string) {
		events.Publish(agentdomain.ChatStartEvent{RequestID: "turn-1", Timestamp: time.Now()})
		time.Sleep(10 * time.Millisecond)
	}
	if err := conn.WriteJSON(map[string]string{"type": "interrupt"}); err != nil {
		t.Fatalf("write interrupt: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for agentSvc.CancelRequestCallCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if agentSvc.CancelRequestCallCount() != 1 || agentSvc.CancelRequestArgsForCall(0) != "turn-1" {
		t.Fatalf("expected one CancelRequest(turn-1), got %d calls", agentSvc.CancelRequestCallCount())
	}
}

func TestExtensionBridgeToolRequestDenied(t *testing.T) {
	toolSvc := &agentdomainmocks.FakeToolService{}
	toolSvc.IsToolEnabledReturns(true)
	approval := &agentdomainmocks.FakeApprovalPolicy{}
	approval.ShouldRequireApprovalReturns(true)
	bridge := startBridgeWithTools(t, bridgeConfig(), toolSvc, approval, nil, "")
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "tool_request", "id": "req-3", "tool_name": "Bash", "tool_args": "{}"}); err != nil {
		t.Fatalf("write tool_request: %v", err)
	}

	req := readFrameOfType(t, conn, "approval_request")
	if err := conn.WriteJSON(map[string]string{"type": "approval_response", "request_id": req["request_id"].(string), "action": "reject"}); err != nil {
		t.Fatalf("write approval_response: %v", err)
	}

	frame := readFrameOfType(t, conn, "tool_result")
	if frame["id"] != "req-3" || frame["success"] != false {
		t.Fatalf("unexpected tool_result: %v", frame)
	}
	if toolSvc.ExecuteToolDirectCallCount() != 0 {
		t.Fatalf("denied tool call was executed")
	}
}

func TestExtensionBridgeToolRequestWithoutApprovalNeeded(t *testing.T) {
	toolSvc := &agentdomainmocks.FakeToolService{}
	toolSvc.IsToolEnabledReturns(true)
	toolSvc.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{
		Success: false,
		Error:   "exit status 1",
		Data:    &agentdomain.BashToolResult{Output: "boom\n", ExitCode: 1},
	}, nil)
	approval := &agentdomainmocks.FakeApprovalPolicy{}
	bridge := startBridgeWithTools(t, bridgeConfig(), toolSvc, approval, nil, "")
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "tool_request", "id": "req-4", "tool_name": "Bash", "tool_args": "{}"}); err != nil {
		t.Fatalf("write tool_request: %v", err)
	}

	frame := readFrameOfType(t, conn, "tool_result")
	if frame["id"] != "req-4" || frame["success"] != false || frame["output"] != "boom\n" || frame["error"] != "exit status 1" {
		t.Fatalf("unexpected tool_result: %v", frame)
	}
}

func TestExtensionBridgeListModelsDefaultFirst(t *testing.T) {
	models := &convmocks.FakeModelService{}
	models.ListModelsReturns([]string{"a/x", "b/y"}, nil)
	models.GetCurrentModelReturns("b/y")
	models.SelectModelCalls(func(m string) error { models.GetCurrentModelReturns(m); return nil })
	notified := make(chan any, 4)
	bridge := startBridge(t, bridgeConfig(), notifierFunc(func(e any) { notified <- e }), nil)
	bridge.SetToolExecution(nil, nil, models, "b/y")
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "list_models"}); err != nil {
		t.Fatalf("write list_models: %v", err)
	}

	frame := readFrameOfType(t, conn, "models")
	raw, ok := frame["models"].([]any)
	if !ok || len(raw) != 2 || raw[0] != "b/y" || raw[1] != "a/x" {
		t.Fatalf("unexpected models: %v", frame["models"])
	}
	if frame["current"] != "b/y" {
		t.Fatalf("expected current b/y, got %v", frame["current"])
	}

	if err := conn.WriteJSON(map[string]string{"type": "select_model", "model": "a/x"}); err != nil {
		t.Fatalf("write select_model: %v", err)
	}
	frame = readFrameOfType(t, conn, "models")
	if models.SelectModelCallCount() != 1 || models.SelectModelArgsForCall(0) != "a/x" || frame["current"] != "a/x" {
		t.Fatalf("expected SelectModel(a/x) and current a/x, got calls=%d current=%v", models.SelectModelCallCount(), frame["current"])
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-notified:
			if ev, ok := e.(agentdomain.ModelSelectedEvent); ok {
				if ev.Model != "a/x" {
					t.Fatalf("expected ModelSelectedEvent{a/x}, got %#v", ev)
				}
				return
			}
		case <-deadline:
			t.Fatal("expected the TUI to be notified of the model switch")
		}
	}
}

// notifierFunc adapts a func to agentdomain.UINotifier for tests.
type notifierFunc func(any)

func (f notifierFunc) Notify(e any) { f(e) }

func TestExtensionBridgeListModelsWithoutServiceIsEmpty(t *testing.T) {
	bridge := startBridge(t, bridgeConfig(), nil, nil)
	conn := dial(t, bridge)
	hello(t, conn, "test-token")

	if err := conn.WriteJSON(map[string]string{"type": "list_models"}); err != nil {
		t.Fatalf("write list_models: %v", err)
	}

	frame := readFrameOfType(t, conn, "models")
	if raw, ok := frame["models"].([]any); ok && len(raw) != 0 {
		t.Fatalf("expected no models, got %v", frame["models"])
	}
}
