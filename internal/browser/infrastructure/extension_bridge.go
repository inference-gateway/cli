package infrastructure

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	uuid "github.com/google/uuid"
	websocket "github.com/gorilla/websocket"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	render "github.com/inference-gateway/cli/internal/platform/render"
)

// Bridge wire messages. One flat envelope per frame, discriminated by Type;
// unknown types are ignored for forward compatibility.
type extInbound struct {
	Type             string                     `json:"type"`
	Token            string                     `json:"token,omitempty"`
	ExtensionVersion string                     `json:"extension_version,omitempty"`
	ID               string                     `json:"id,omitempty"`
	URL              string                     `json:"url,omitempty"`
	Title            string                     `json:"title,omitempty"`
	Content          string                     `json:"content,omitempty"`
	Events           []string                   `json:"events,omitempty"`
	Error            string                     `json:"error,omitempty"`
	RequestID        string                     `json:"request_id,omitempty"`
	Action           string                     `json:"action,omitempty"`
	Image            string                     `json:"image,omitempty"` // base64 screenshot bytes
	ImageMimeType    string                     `json:"image_mime_type,omitempty"`
	Tabs             []browserdomain.BrowserTab `json:"tabs,omitempty"`
	ToolName         string                     `json:"tool_name,omitempty"`
	ToolArgs         string                     `json:"tool_args,omitempty"`
}

type extApprovalRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	ToolName  string `json:"tool_name"`
	ToolArgs  string `json:"tool_args"`
}

type extApprovalResolved struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

type extToolResult struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}

type extModels struct {
	Type   string   `json:"type"`
	Models []string `json:"models"`
}

type extHelloAck struct {
	Type string `json:"type"`
}

type extBrowserCommand struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Action     string `json:"action"`
	URL        string `json:"url,omitempty"`
	Selector   string `json:"selector,omitempty"`
	Text       string `json:"text,omitempty"`
	PressEnter bool   `json:"press_enter,omitempty"`
	TimeoutMs  int    `json:"timeout_ms"`
}

type extSnapshot struct {
	Type     string        `json:"type"`
	Messages []sdk.Message `json:"messages"`
}

type extConversationSummary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

type extConversations struct {
	Type          string                   `json:"type"`
	Conversations []extConversationSummary `json:"conversations"`
}

type extSkills struct {
	Type   string                     `json:"type"`
	Skills []agentdomain.SkillSummary `json:"skills"`
}

type extChatEvent struct {
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event"`
}

// ExtensionBridge hosts the localhost WebSocket endpoint the opentask browser
// extension dials into. It implements browserdomain.BrowserDriver by forwarding the
// browser-use verbs to the connected extension, and it mirrors the chat
// conversation to the extension (AG-UI event stream out, user messages in).
//
// ponytail: one connection, one implied tab - multi-tab / multi-CLI routing
// when someone actually needs it.
type ExtensionBridge struct {
	cfg          *config.BrowserUseConfig
	notifier     agentdomain.UINotifier
	repo         convdomain.ConversationRepository
	events       agentdomain.EventBridge
	skills       agentdomain.SkillsService
	sessionID    string
	artifactsDir string

	// Injected late via SetToolExecution; all nilable.
	toolSvc      agentdomain.ToolService
	approval     agentdomain.ApprovalPolicy
	models       convdomain.ModelService
	defaultModel string
	agentSvc     agentdomain.AgentService // for interrupt; injected via SetAgentService

	activeRequestID atomic.Value // string: the in-flight chat turn, from ChatStartEvent

	server   *http.Server
	addr     string
	startErr error

	mu                   sync.Mutex // guards conn, connStop, pending, pendingApprovals, pendingToolApprovals
	conn                 *websocket.Conn
	connStop             chan struct{}
	pending              map[string]chan extInbound
	pendingApprovals     map[string]sdk.ChatCompletionMessageToolCall
	pendingToolApprovals map[string]chan bool // approvals for extension-initiated tool_requests

	writeMu sync.Mutex // serializes writes to conn
}

// NewExtensionBridge builds the bridge. notifier, repo, events, and skills may
// be nil (the matching feature is then skipped); cfg must not be nil.
// artifactsDir, when non-empty, is served read-only at /artifacts/ so the panel
// can display generated images the agent saved locally.
func NewExtensionBridge(cfg *config.BrowserUseConfig, notifier agentdomain.UINotifier, repo convdomain.ConversationRepository, events agentdomain.EventBridge, skills agentdomain.SkillsService, sessionID, artifactsDir string) *ExtensionBridge {
	return &ExtensionBridge{
		cfg:              cfg,
		notifier:         notifier,
		repo:             repo,
		events:           events,
		skills:           skills,
		sessionID:        sessionID,
		artifactsDir:     artifactsDir,
		pending:          make(map[string]chan extInbound),
		pendingApprovals: make(map[string]sdk.ChatCompletionMessageToolCall),

		pendingToolApprovals: make(map[string]chan bool),
	}
}

// SetToolExecution wires the deps answering tool_request and list_models
// frames - after construction, because the container builds the bridge before
// the tool and model services exist. Any argument may be nil/empty.
func (b *ExtensionBridge) SetToolExecution(toolSvc agentdomain.ToolService, approval agentdomain.ApprovalPolicy, models convdomain.ModelService, defaultModel string) {
	b.toolSvc = toolSvc
	b.approval = approval
	b.models = models
	b.defaultModel = defaultModel
}

// Start listens on 127.0.0.1:<port> and serves the /ws endpoint. Errors are
// also stored so later tool calls surface them instead of a silent no-op.
// SetAgentService wires the agent so an `interrupt` frame can cancel the
// in-flight turn. Late, like SetToolExecution: the agent is built after the
// bridge.
func (b *ExtensionBridge) SetAgentService(svc agentdomain.AgentService) {
	b.agentSvc = svc
}

// interrupt cancels the chat turn currently streaming, if any. Idempotent;
// a stale or unknown id is a no-op in AgentService.CancelRequest.
func (b *ExtensionBridge) interrupt() {
	id, _ := b.activeRequestID.Load().(string)
	if b.agentSvc == nil || id == "" {
		return
	}
	_ = b.agentSvc.CancelRequest(id)
}

func (b *ExtensionBridge) Start() error {
	if b.cfg.Extension.Token == "" {
		b.startErr = errors.New("browser_use.extension.token is empty - set a shared secret in browser_use.yaml and in the opentask extension options")
		return b.startErr
	}

	addr := fmt.Sprintf("127.0.0.1:%d", b.cfg.Extension.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		b.startErr = fmt.Errorf("extension bridge failed to listen on %s: %w", addr, err)
		return b.startErr
	}

	b.addr = listener.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", b.handleWS)
	if b.artifactsDir != "" {
		mux.Handle("/artifacts/", http.StripPrefix("/artifacts/", http.FileServer(http.Dir(b.artifactsDir))))
	}
	b.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := b.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("extension bridge server stopped", "error", err)
		}
	}()
	logger.Info("extension bridge listening for the opentask extension", "addr", addr)
	return nil
}

// Addr returns the actual listen address (useful with port 0 in tests).
func (b *ExtensionBridge) Addr() string {
	return b.addr
}

var extUpgrader = websocket.Upgrader{
	// Browser WebSocket clients cannot set custom headers; auth happens via
	// the token in the first message. Origin gating is defense in depth:
	// extension service workers send a *-extension:// origin, local web pages
	// send http(s) origins and are rejected.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" ||
			strings.HasPrefix(origin, "chrome-extension://") ||
			strings.HasPrefix(origin, "moz-extension://") ||
			strings.HasPrefix(origin, "safari-web-extension://")
	},
}

func (b *ExtensionBridge) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := extUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var hello extInbound
	if err := conn.ReadJSON(&hello); err != nil || hello.Type != "browser_hello" ||
		subtle.ConstantTimeCompare([]byte(hello.Token), []byte(b.cfg.Extension.Token)) != 1 {
		logger.Warn("extension bridge rejected a connection with a bad or missing hello")
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if err := conn.WriteJSON(extHelloAck{Type: "browser_hello_ack"}); err != nil {
		_ = conn.Close()
		return
	}
	logger.Info("opentask extension connected", "version", hello.ExtensionVersion)
	b.adopt(conn)
}

// adopt makes conn the active extension connection, replacing any previous
// one (MV3 service workers restart at will - replacement is the correct
// semantic), and starts its pump goroutines.
func (b *ExtensionBridge) adopt(conn *websocket.Conn) {
	b.mu.Lock()
	if b.conn != nil {
		close(b.connStop)
		_ = b.conn.Close()
	}
	b.conn = conn
	stop := make(chan struct{})
	b.connStop = stop

	b.pendingApprovals = make(map[string]sdk.ChatCompletionMessageToolCall)
	b.pendingToolApprovals = make(map[string]chan bool)
	b.mu.Unlock()

	go b.readLoop(conn, stop)
	go b.chatPump(conn, stop)
	go b.pingLoop(conn, stop)
}

// sendSnapshot ships the current conversation's history so the panel shows it,
// not just events from now on. Sent as the resume_conversation response.
func (b *ExtensionBridge) sendSnapshot(conn *websocket.Conn) {
	if b.repo == nil {
		return
	}
	entries := b.repo.GetMessages()
	messages := make([]sdk.Message, 0, len(entries))
	for _, entry := range entries {
		messages = append(messages, entry.Message)
	}
	b.write(conn, extSnapshot{Type: "conversation_snapshot", Messages: messages})
}

// conversationListLimit caps list_conversations, mirroring the TUI selector.
const conversationListLimit = 50

// conversationLister is the slice of the persistent repo the panel picker needs.
// Declared here (not in domain) because the in-memory fallback repo has no
// listing - the type assertion simply fails there and yields an empty list.
type conversationLister interface {
	ListSavedConversations(ctx context.Context, limit, offset int) ([]convdomain.ConversationSummary, error)
}

// sendConversationList answers list_conversations with the stored conversations
// (newest-first), so the panel can offer the same picker the CLI resumes from.
func (b *ExtensionBridge) sendConversationList(conn *websocket.Conn) {
	lister, ok := b.repo.(conversationLister)
	if !ok {
		b.write(conn, extConversations{Type: "conversations"})
		return
	}
	summaries, err := lister.ListSavedConversations(context.Background(), conversationListLimit, 0)
	if err != nil {
		logger.Debug("extension bridge failed to list conversations", "error", err)
		b.write(conn, extConversations{Type: "conversations"})
		return
	}
	out := make([]extConversationSummary, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, extConversationSummary{
			ID:           s.ID,
			Title:        s.Title,
			UpdatedAt:    s.UpdatedAt,
			MessageCount: s.MessageCount,
		})
	}
	b.write(conn, extConversations{Type: "conversations", Conversations: out})
}

// resumeConversation switches the active conversation to id and snapshots it to
// the panel; the running chat pump then streams live events into it.
func (b *ExtensionBridge) resumeConversation(conn *websocket.Conn, id string) {
	if b.repo == nil || id == "" {
		return
	}
	if err := b.repo.LoadConversation(context.Background(), id); err != nil {
		logger.Debug("extension bridge failed to resume conversation", "id", id, "error", err)
		return
	}
	b.sendSnapshot(conn)
}

// sendSkillList answers list_skills with the agent's discovered skills - the
// same set (project, .agents, user, plugin, catalog) the skills service already
// merged with precedence, so the panel's "/" menu mirrors what the TUI offers.
// Sends an empty list when skills are unavailable.
func (b *ExtensionBridge) sendSkillList(conn *websocket.Conn) {
	if b.skills == nil {
		b.write(conn, extSkills{Type: "skills", Skills: []agentdomain.SkillSummary{}})
		return
	}
	loaded := b.skills.List()
	out := make([]agentdomain.SkillSummary, 0, len(loaded))
	for _, sk := range loaded {
		out = append(out, sk.Summary())
	}
	b.write(conn, extSkills{Type: "skills", Skills: out})
}

// sendModelList answers list_models with the models the gateway serves, the
// CLI's configured default model first, so the panel's pickers mirror the CLI.
// Sends an empty list when models are unavailable.
func (b *ExtensionBridge) sendModelList(conn *websocket.Conn) {
	out := []string{}
	if b.models != nil {
		listed, err := b.models.ListModels(context.Background())
		if err != nil {
			logger.Debug("extension bridge failed to list models", "error", err)
		}
		if b.defaultModel != "" {
			out = append(out, b.defaultModel)
		}
		for _, m := range listed {
			if m != b.defaultModel {
				out = append(out, m)
			}
		}
	}
	b.write(conn, extModels{Type: "models", Models: out})
}

// handleToolRequest executes an extension-initiated tool call through the
// standard pipeline (enabled check, the agent's approval policy, execution)
// and writes exactly one tool_result per request id. A dead connection drops
// the write - no queuing or replay. approval_behaviour (prompt/ipc/block) is
// ignored: the extension itself is the prompt surface.
func (b *ExtensionBridge) handleToolRequest(conn *websocket.Conn, stop chan struct{}, msg extInbound) {
	reply := func(success bool, output, errStr string) {
		b.write(conn, extToolResult{Type: "tool_result", ID: msg.ID, Success: success, Output: output, Error: errStr})
	}

	if b.toolSvc == nil || !b.toolSvc.IsToolEnabled(msg.ToolName) {
		reply(false, "", "unknown or disabled tool: "+msg.ToolName)
		return
	}

	toolCall := sdk.ChatCompletionMessageToolCall{
		ID:   msg.ID,
		Type: sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      msg.ToolName,
			Arguments: msg.ToolArgs,
		},
	}

	ctx := context.Background()
	if b.approval != nil && b.approval.ShouldRequireApproval(ctx, &toolCall, true) {
		if !b.awaitToolRequestApproval(conn, stop, toolCall) {
			reply(false, "", "tool call denied")
			return
		}
	}

	result, err := b.toolSvc.ExecuteToolDirect(agentdomain.WithToolApproved(ctx), toolCall.Function)
	if err != nil {
		result = &agentdomain.ToolExecutionResult{ToolName: msg.ToolName, ToolCallID: msg.ID, Success: false, Error: err.Error()}
	}
	b.recordDirectTool(toolCall, result)
	if err != nil {
		reply(false, "", err.Error())
		return
	}
	reply(result.Success, b.toolResultOutput(result), result.Error)
}

// recordDirectTool makes an extension-initiated tool call part of the
// conversation, mirroring the TUI's direct-exec path: persist the assistant
// tool_call + tool result entries, refresh the TUI history, and stream the
// call/result to the panel through the chat pump. Denied/disabled calls never
// reach here - nothing ran.
func (b *ExtensionBridge) recordDirectTool(toolCall sdk.ChatCompletionMessageToolCall, result *agentdomain.ToolExecutionResult) {
	if result.ToolCallID == "" {
		result.ToolCallID = toolCall.ID
	}
	now := time.Now()
	if b.repo != nil {
		toolCalls := []sdk.ChatCompletionMessageToolCall{toolCall}
		_ = b.repo.AddMessage(convdomain.ConversationEntry{
			Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent(""), ToolCalls: &toolCalls},
			Time:    now,
		})
		_ = b.repo.AddMessage(convdomain.ConversationEntry{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent(""), ToolCallID: &toolCall.ID},
			ToolExecution: result,
			Time:          now,
		})
	}
	completed := agentdomain.ToolExecutionCompletedEvent{
		SessionID:     b.sessionID,
		RequestID:     toolCall.ID,
		Timestamp:     now,
		TotalExecuted: 1,
		Results:       []*agentdomain.ToolExecutionResult{result},
	}
	if result.Success {
		completed.SuccessCount = 1
	} else {
		completed.FailureCount = 1
	}
	if b.notifier != nil {
		b.notifier.Notify(completed)
	}
	if b.events != nil {
		b.events.Publish(agentdomain.ChatCompleteEvent{
			RequestID: toolCall.ID,
			Timestamp: now,
			ToolCalls: []sdk.ChatCompletionMessageToolCall{toolCall},
		})
		b.events.Publish(completed)
	}
}

// awaitToolRequestApproval sends an approval_request for an extension-initiated
// tool call and blocks until the panel answers, the connection dies, or the
// approval times out. Anything but an explicit approve is a denial.
func (b *ExtensionBridge) awaitToolRequestApproval(conn *websocket.Conn, stop chan struct{}, toolCall sdk.ChatCompletionMessageToolCall) bool {
	requestID := uuid.NewString()
	decision := make(chan bool, 1)
	b.mu.Lock()
	b.pendingToolApprovals[requestID] = decision
	b.mu.Unlock()
	b.write(conn, extApprovalRequest{
		Type:      "approval_request",
		RequestID: requestID,
		ToolName:  toolCall.Function.Name,
		ToolArgs:  toolCall.Function.Arguments,
	})
	select {
	case approved := <-decision:
		return approved
	case <-stop:
	case <-time.After(constants.ApprovalTimeout):
	}
	b.mu.Lock()
	delete(b.pendingToolApprovals, requestID)
	b.mu.Unlock()
	return false
}

// answerToolRequestApproval resolves an approval_response that belongs to an
// extension-initiated tool_request. Returns false when the id is not ours so
// the caller can fall through to the agent-approval path.
func (b *ExtensionBridge) answerToolRequestApproval(conn *websocket.Conn, requestID, action string) bool {
	b.mu.Lock()
	decision, ok := b.pendingToolApprovals[requestID]
	delete(b.pendingToolApprovals, requestID)
	b.mu.Unlock()
	if !ok {
		return false
	}
	decision <- action == "approve"
	b.write(conn, extApprovalResolved{Type: "approval_resolved", RequestID: requestID})
	return true
}

// toolResultOutput extracts the human-facing output of a tool result: combined
// stdout/stderr for Bash, the canonical LLM formatting otherwise.
func (b *ExtensionBridge) toolResultOutput(result *agentdomain.ToolExecutionResult) string {
	if bash, ok := result.Data.(*agentdomain.BashToolResult); ok {
		return bash.Output
	}
	if b.repo != nil {
		return b.repo.FormatToolResultForLLM(result)
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		return ""
	}
	return string(data)
}

// readLoop handles frames from the extension until the connection dies or is
// replaced.
func (b *ExtensionBridge) readLoop(conn *websocket.Conn, stop chan struct{}) {
	for {
		var msg extInbound
		if err := conn.ReadJSON(&msg); err != nil {
			b.dropConn(conn, stop)
			return
		}
		switch msg.Type {
		case "browser_result":
			b.mu.Lock()
			ch, ok := b.pending[msg.ID]
			delete(b.pending, msg.ID)
			b.mu.Unlock()
			if ok {
				ch <- msg
			}
		case "user_message":
			if b.notifier != nil && msg.Content != "" {
				b.notifier.Notify(agentdomain.UserInputEvent{Content: msg.Content})
			}
		case "list_conversations":
			b.sendConversationList(conn)
		case "list_skills":
			b.sendSkillList(conn)
		case "list_models":
			b.sendModelList(conn)
		case "resume_conversation":
			b.resumeConversation(conn, msg.ID)
		case "interrupt":
			b.interrupt()
		case "tool_request":
			go b.handleToolRequest(conn, stop, msg)
		case "approval_response":
			if !b.answerToolRequestApproval(conn, msg.RequestID, msg.Action) {
				b.answerApproval(conn, msg.RequestID, msg.Action)
			}
		default:
			// Unknown frame types are ignored for forward compatibility.
		}
	}
}

// chatPump mirrors chat events to the extension as chat_event frames, and turns
// ToolApprovalRequestedEvent / ToolApprovalResolvedEvent into approval_request /
// approval_resolved frames so the panel can drive the approval handshake.
func (b *ExtensionBridge) chatPump(conn *websocket.Conn, stop chan struct{}) {
	if b.events == nil {
		return
	}
	sub := b.events.SubscribeFuture()
	defer b.events.Unsubscribe(sub)

	filtered := make(chan agentdomain.ChatEvent, 100)
	go func() {
		defer close(filtered)
		for {
			select {
			case <-stop:
				return
			case ev, ok := <-sub:
				if !ok {
					return
				}
				switch e := ev.(type) {
				case agentdomain.ChatStartEvent:
					b.activeRequestID.Store(e.RequestID)
				case agentdomain.ChatCompleteEvent:
					if len(e.ToolCalls) == 0 || e.Cancelled {
						b.activeRequestID.Store("")
					}
				}
				if req, isApproval := ev.(agentdomain.ToolApprovalRequestedEvent); isApproval {
					b.requestApproval(conn, req)
					continue
				}
				if _, isResolved := ev.(agentdomain.ToolApprovalResolvedEvent); isResolved {
					b.resolvePendingApprovals(conn)
					continue
				}
				select {
				case filtered <- ev:
				case <-stop:
					return
				}
			}
		}
	}()

	writer := &chatEventWriter{bridge: b, conn: conn}
	if err := render.RenderAGUI(filtered, writer, nil, b.sessionID, ""); err != nil {
		logger.Debug("extension bridge chat pump ended", "error", err)
	}
}

// chatEventWriter adapts the AG-UI line stream to chat_event frames.
type chatEventWriter struct {
	bridge *ExtensionBridge
	conn   *websocket.Conn
	buf    []byte
}

func (w *chatEventWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := -1
		for i, c := range w.buf {
			if c == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			return len(p), nil
		}
		line := make([]byte, idx)
		copy(line, w.buf[:idx])
		w.buf = w.buf[idx+1:]
		if len(line) > 0 {
			w.bridge.write(w.conn, extChatEvent{Type: "chat_event", Event: line})
		}
	}
}

// requestApproval stashes the pending tool call and asks the panel to decide.
func (b *ExtensionBridge) requestApproval(conn *websocket.Conn, req agentdomain.ToolApprovalRequestedEvent) {
	b.mu.Lock()
	b.pendingApprovals[req.RequestID] = req.ToolCall
	b.mu.Unlock()
	b.write(conn, extApprovalRequest{
		Type:      "approval_request",
		RequestID: req.RequestID,
		ToolName:  req.ToolCall.Function.Name,
		ToolArgs:  req.ToolCall.Function.Arguments,
	})
}

// resolvePendingApprovals clears any outstanding approval cards on a
// ToolApprovalResolvedEvent. A duplicate approval_resolved is harmless - the
// panel ignores unknown ids (the panel path already cleared it in answerApproval).
func (b *ExtensionBridge) resolvePendingApprovals(conn *websocket.Conn) {
	b.mu.Lock()
	if len(b.pendingApprovals) == 0 {
		b.mu.Unlock()
		return
	}
	ids := make([]string, 0, len(b.pendingApprovals))
	for id := range b.pendingApprovals {
		ids = append(ids, id)
	}
	b.pendingApprovals = make(map[string]sdk.ChatCompletionMessageToolCall)
	b.mu.Unlock()
	for _, id := range ids {
		b.write(conn, extApprovalResolved{Type: "approval_resolved", RequestID: id})
	}
}

// answerApproval turns a panel approval_response into the same
// ToolApprovalResponseEvent the terminal emits, then confirms the card cleared.
func (b *ExtensionBridge) answerApproval(conn *websocket.Conn, requestID, action string) {
	b.mu.Lock()
	toolCall, ok := b.pendingApprovals[requestID]
	delete(b.pendingApprovals, requestID)
	b.mu.Unlock()
	if !ok {
		return
	}
	if b.notifier != nil {
		b.notifier.Notify(agentdomain.ToolApprovalResponseEvent{
			Action:   approvalAction(action),
			ToolCall: toolCall,
		})
	}
	b.write(conn, extApprovalResolved{Type: "approval_resolved", RequestID: requestID})
}

// approvalAction maps the wire action to a decision; anything but "approve"
// (including unknown values) is treated as a reject, failing safe.
func approvalAction(action string) agentdomain.ApprovalAction {
	if action == "approve" {
		return agentdomain.ApprovalApprove
	}
	return agentdomain.ApprovalReject
}

func (b *ExtensionBridge) pingLoop(conn *websocket.Conn, stop chan struct{}) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			b.writeMu.Lock()
			err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			b.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

// dropConn clears conn if it is still the active connection.
func (b *ExtensionBridge) dropConn(conn *websocket.Conn, stop chan struct{}) {
	b.mu.Lock()
	if b.conn == conn {
		b.conn = nil
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
	b.mu.Unlock()
	_ = conn.Close()
}

func (b *ExtensionBridge) write(conn *websocket.Conn, v any) {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	if err := conn.WriteJSON(v); err != nil {
		logger.Debug("extension bridge write failed", "error", err)
	}
}

// send dispatches one browser command and waits for its result.
func (b *ExtensionBridge) send(ctx context.Context, cmd extBrowserCommand) (extInbound, error) {
	if b.startErr != nil {
		return extInbound{}, b.startErr
	}

	b.mu.Lock()
	conn := b.conn
	if conn == nil {
		b.mu.Unlock()
		return extInbound{}, fmt.Errorf("no browser extension connected on port %d - install the opentask extension and set its bridge port/token to match browser_use.yaml", b.cfg.Extension.Port)
	}
	cmd.ID = uuid.NewString()
	cmd.TimeoutMs = b.timeoutSeconds() * 1000
	ch := make(chan extInbound, 1)
	b.pending[cmd.ID] = ch
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, cmd.ID)
		b.mu.Unlock()
	}()

	b.write(conn, cmd)

	// The extension enforces timeout_ms per action; the grace period covers
	// transport latency and dead service workers.
	timer := time.NewTimer(time.Duration(b.timeoutSeconds()+5) * time.Second)
	defer timer.Stop()

	select {
	case result := <-ch:
		if result.Error != "" {
			return extInbound{}, fmt.Errorf("failed to %s: %s", cmd.Action, result.Error)
		}
		return result, nil
	case <-ctx.Done():
		return extInbound{}, ctx.Err()
	case <-timer.C:
		return extInbound{}, fmt.Errorf("timed out waiting for the browser extension to %s - is the opentask extension still running?", cmd.Action)
	}
}

func (b *ExtensionBridge) timeoutSeconds() int {
	if b.cfg.Browser.TimeoutSeconds > 0 {
		return b.cfg.Browser.TimeoutSeconds
	}
	return 30
}

// Navigate implements browserdomain.BrowserDriver.
func (b *ExtensionBridge) Navigate(ctx context.Context, url string) (browserdomain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "navigate", URL: url})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{Action: "navigate", URL: result.URL, Title: result.Title}, nil
}

// Click implements browserdomain.BrowserDriver.
func (b *ExtensionBridge) Click(ctx context.Context, selector string) (browserdomain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "click", Selector: selector})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{Action: "click", Selector: selector, URL: result.URL, Title: result.Title}, nil
}

// Type implements browserdomain.BrowserDriver.
func (b *ExtensionBridge) Type(ctx context.Context, selector, text string, pressEnter bool) (browserdomain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "type", Selector: selector, Text: text, PressEnter: pressEnter})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{Action: "type", Selector: selector, Text: text, URL: result.URL, Title: result.Title}, nil
}

// Read implements browserdomain.BrowserDriver.
func (b *ExtensionBridge) Read(ctx context.Context, selector string) (browserdomain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "read", Selector: selector})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{
		Action:   "read",
		Selector: selector,
		URL:      result.URL,
		Title:    result.Title,
		Content:  result.Content,
		Events:   result.Events,
	}, nil
}

// ClickAt implements browserdomain.BrowserDriver. The extension bridge drives clicks
// through chrome.scripting (untrusted synthetic events), which have no reliable
// viewport-coordinate form - that needs chrome.debugger/CDP. Fail clearly.
func (b *ExtensionBridge) ClickAt(_ context.Context, _, _ float64) (browserdomain.BrowserToolResult, error) {
	return browserdomain.BrowserToolResult{}, fmt.Errorf("coordinate click isn't supported on the extension backend; use a CSS or text= selector with BrowserClick")
}

// Screenshot implements browserdomain.BrowserDriver via the extension's captureVisibleTab.
func (b *ExtensionBridge) Screenshot(ctx context.Context) (browserdomain.BrowserScreenshotResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "screenshot"})
	if err != nil {
		return browserdomain.BrowserScreenshotResult{}, err
	}
	if result.Image == "" {
		return browserdomain.BrowserScreenshotResult{}, fmt.Errorf("extension returned no screenshot data")
	}
	mime := result.ImageMimeType
	if mime == "" {
		mime = "image/png"
	}
	return browserdomain.BrowserScreenshotResult{
		Data:     result.Image,
		MimeType: mime,
		URL:      result.URL,
		Title:    result.Title,
	}, nil
}

// Tabs implements browserdomain.BrowserDriver via the extension's chrome.tabs query.
func (b *ExtensionBridge) Tabs(ctx context.Context) ([]browserdomain.BrowserTab, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "tabs"})
	if err != nil {
		return nil, err
	}
	return result.Tabs, nil
}

// Close implements browserdomain.BrowserDriver: shuts the server and any connection.
func (b *ExtensionBridge) Close() {
	if b.server != nil {
		_ = b.server.Close()
	}
	b.mu.Lock()
	if b.conn != nil {
		select {
		case <-b.connStop:
		default:
			close(b.connStop)
		}
		_ = b.conn.Close()
		b.conn = nil
	}
	b.mu.Unlock()
}
