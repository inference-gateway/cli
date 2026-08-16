package services

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
	"time"

	uuid "github.com/google/uuid"
	websocket "github.com/gorilla/websocket"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	render "github.com/inference-gateway/cli/internal/render"
)

// Bridge wire messages. One flat envelope per frame, discriminated by Type;
// unknown types are ignored for forward compatibility.
type extInbound struct {
	Type             string              `json:"type"`
	Token            string              `json:"token,omitempty"`
	ExtensionVersion string              `json:"extension_version,omitempty"`
	ID               string              `json:"id,omitempty"`
	URL              string              `json:"url,omitempty"`
	Title            string              `json:"title,omitempty"`
	Content          string              `json:"content,omitempty"`
	Events           []string            `json:"events,omitempty"`
	Error            string              `json:"error,omitempty"`
	RequestID        string              `json:"request_id,omitempty"`
	Action           string              `json:"action,omitempty"`
	Image            string              `json:"image,omitempty"` // base64 screenshot bytes
	ImageMimeType    string              `json:"image_mime_type,omitempty"`
	Tabs             []domain.BrowserTab `json:"tabs,omitempty"`
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

type extSkill struct {
	Name        string `json:"name"` // Skill.DisplayName() - "plugin:name" for plugins
	Description string `json:"description"`
	Scope       string `json:"scope"` // project|agents|user|plugin|catalog
}

type extSkills struct {
	Type   string     `json:"type"`
	Skills []extSkill `json:"skills"`
}

type extChatEvent struct {
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event"`
}

// ExtensionBridge hosts the localhost WebSocket endpoint the opentask browser
// extension dials into. It implements domain.BrowserDriver by forwarding the
// browser-use verbs to the connected extension, and it mirrors the chat
// conversation to the extension (AG-UI event stream out, user messages in).
//
// ponytail: one connection, one implied tab - multi-tab / multi-CLI routing
// when someone actually needs it.
type ExtensionBridge struct {
	cfg          *config.BrowserUseConfig
	notifier     domain.UINotifier
	repo         domain.ConversationRepository
	events       domain.EventBridge
	skills       domain.SkillsService
	sessionID    string
	artifactsDir string

	server   *http.Server
	addr     string
	startErr error

	mu               sync.Mutex // guards conn, connStop, pending, pendingApprovals
	conn             *websocket.Conn
	connStop         chan struct{}
	pending          map[string]chan extInbound
	pendingApprovals map[string]sdk.ChatCompletionMessageToolCall

	writeMu sync.Mutex // serializes writes to conn
}

// NewExtensionBridge builds the bridge. notifier, repo, events, and skills may
// be nil (the matching feature is then skipped); cfg must not be nil.
// artifactsDir, when non-empty, is served read-only at /artifacts/ so the panel
// can display generated images the agent saved locally.
func NewExtensionBridge(cfg *config.BrowserUseConfig, notifier domain.UINotifier, repo domain.ConversationRepository, events domain.EventBridge, skills domain.SkillsService, sessionID, artifactsDir string) *ExtensionBridge {
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
	}
}

// Start listens on 127.0.0.1:<port> and serves the /ws endpoint. Errors are
// also stored so later tool calls surface them instead of a silent no-op.
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
	ListSavedConversations(ctx context.Context, limit, offset int) ([]domain.ConversationSummary, error)
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
		b.write(conn, extSkills{Type: "skills", Skills: []extSkill{}})
		return
	}
	loaded := b.skills.List()
	out := make([]extSkill, 0, len(loaded))
	for _, sk := range loaded {
		out = append(out, extSkill{
			Name:        sk.DisplayName(),
			Description: sk.Description,
			Scope:       string(sk.Scope),
		})
	}
	b.write(conn, extSkills{Type: "skills", Skills: out})
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
				b.notifier.Notify(domain.UserInputEvent{Content: msg.Content})
			}
		case "list_conversations":
			b.sendConversationList(conn)
		case "list_skills":
			b.sendSkillList(conn)
		case "resume_conversation":
			b.resumeConversation(conn, msg.ID)
		case "approval_response":
			b.answerApproval(conn, msg.RequestID, msg.Action)
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

	filtered := make(chan domain.ChatEvent, 100)
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
				if req, isApproval := ev.(domain.ToolApprovalRequestedEvent); isApproval {
					b.requestApproval(conn, req)
					continue
				}
				if _, isResolved := ev.(domain.ToolApprovalResolvedEvent); isResolved {
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
func (b *ExtensionBridge) requestApproval(conn *websocket.Conn, req domain.ToolApprovalRequestedEvent) {
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
		b.notifier.Notify(domain.ToolApprovalResponseEvent{
			Action:   approvalAction(action),
			ToolCall: toolCall,
		})
	}
	b.write(conn, extApprovalResolved{Type: "approval_resolved", RequestID: requestID})
}

// approvalAction maps the wire action to a decision; anything but "approve"
// (including unknown values) is treated as a reject, failing safe.
func approvalAction(action string) domain.ApprovalAction {
	if action == "approve" {
		return domain.ApprovalApprove
	}
	return domain.ApprovalReject
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

// Navigate implements domain.BrowserDriver.
func (b *ExtensionBridge) Navigate(ctx context.Context, url string) (domain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "navigate", URL: url})
	if err != nil {
		return domain.BrowserToolResult{}, err
	}
	return domain.BrowserToolResult{Action: "navigate", URL: result.URL, Title: result.Title}, nil
}

// Click implements domain.BrowserDriver.
func (b *ExtensionBridge) Click(ctx context.Context, selector string) (domain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "click", Selector: selector})
	if err != nil {
		return domain.BrowserToolResult{}, err
	}
	return domain.BrowserToolResult{Action: "click", Selector: selector, URL: result.URL, Title: result.Title}, nil
}

// Type implements domain.BrowserDriver.
func (b *ExtensionBridge) Type(ctx context.Context, selector, text string, pressEnter bool) (domain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "type", Selector: selector, Text: text, PressEnter: pressEnter})
	if err != nil {
		return domain.BrowserToolResult{}, err
	}
	return domain.BrowserToolResult{Action: "type", Selector: selector, Text: text, URL: result.URL, Title: result.Title}, nil
}

// Read implements domain.BrowserDriver.
func (b *ExtensionBridge) Read(ctx context.Context, selector string) (domain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "read", Selector: selector})
	if err != nil {
		return domain.BrowserToolResult{}, err
	}
	return domain.BrowserToolResult{
		Action:   "read",
		Selector: selector,
		URL:      result.URL,
		Title:    result.Title,
		Content:  result.Content,
		Events:   result.Events,
	}, nil
}

// ClickAt implements domain.BrowserDriver. The extension bridge drives clicks
// through chrome.scripting (untrusted synthetic events), which have no reliable
// viewport-coordinate form - that needs chrome.debugger/CDP. Fail clearly.
func (b *ExtensionBridge) ClickAt(_ context.Context, _, _ float64) (domain.BrowserToolResult, error) {
	return domain.BrowserToolResult{}, fmt.Errorf("coordinate click isn't supported on the extension backend; use a CSS or text= selector with BrowserClick")
}

// Screenshot implements domain.BrowserDriver via the extension's captureVisibleTab.
func (b *ExtensionBridge) Screenshot(ctx context.Context) (domain.BrowserScreenshotResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "screenshot"})
	if err != nil {
		return domain.BrowserScreenshotResult{}, err
	}
	if result.Image == "" {
		return domain.BrowserScreenshotResult{}, fmt.Errorf("extension returned no screenshot data")
	}
	mime := result.ImageMimeType
	if mime == "" {
		mime = "image/png"
	}
	return domain.BrowserScreenshotResult{
		Data:     result.Image,
		MimeType: mime,
		URL:      result.URL,
		Title:    result.Title,
	}, nil
}

// Tabs implements domain.BrowserDriver via the extension's chrome.tabs query.
func (b *ExtensionBridge) Tabs(ctx context.Context) ([]domain.BrowserTab, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: "browser_command", Action: "tabs"})
	if err != nil {
		return nil, err
	}
	return result.Tabs, nil
}

// Close implements domain.BrowserDriver: shuts the server and any connection.
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
