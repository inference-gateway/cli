package agui

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	websocket "github.com/gorilla/websocket"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

var errBrowserExtensionDisconnected = errors.New("the browser extension disconnected before it answered")

// Frame types on the extension bridge wire. One flat envelope per frame,
// discriminated by Type, and unknown types are ignored for forward compatibility.
const (
	inboundBrowserHello       = "browser_hello"
	inboundBrowserResult      = "browser_result"
	inboundUserMessage        = "user_message"
	inboundNewSession         = "new_session"
	inboundListHistory        = "list_history"
	inboundListConversations  = "list_conversations"
	inboundListSkills         = "list_skills"
	inboundSelectModel        = "select_model"
	inboundListModels         = "list_models"
	inboundSetMode            = "set_mode"
	inboundResumeConversation = "resume_conversation"
	inboundInterrupt          = "interrupt"
	inboundToolRequest        = "tool_request"
	inboundApprovalResponse   = "approval_response"

	outboundBrowserHelloAck      = "browser_hello_ack"
	outboundConversationSnapshot = "conversation_snapshot"
	outboundConversations        = "conversations"
	outboundSkills               = "skills"
	outboundHistory              = "history"
	outboundModels               = "models"
	outboundMode                 = "mode"
	outboundChatEvent            = "chat_event"
	outboundApprovalRequest      = "approval_request"
	outboundApprovalResolved     = "approval_resolved"
	outboundToolResult           = "tool_result"
	outboundInterrupted          = "interrupted"
)

// approvalActionApprove is the only panel action that grants a tool call.
const approvalActionApprove = "approve"

type extInbound struct {
	Type             string                        `json:"type"`
	Token            string                        `json:"token,omitempty"`
	ExtensionVersion string                        `json:"extension_version,omitempty"`
	ID               string                        `json:"id,omitempty"`
	Content          string                        `json:"content,omitempty"`
	RequestID        string                        `json:"request_id,omitempty"`
	Action           string                        `json:"action,omitempty"`
	Model            string                        `json:"model,omitempty"`
	ToolName         string                        `json:"tool_name,omitempty"`
	ToolArgs         string                        `json:"tool_args,omitempty"`
	Mode             string                        `json:"mode,omitempty"`
	Attachments      []agentdomain.ImageAttachment `json:"attachments,omitempty"`
}

// maxAttachmentBytes caps one decoded attachment. The panel enforces the same
// limit, and this is the trust-boundary check.
const maxAttachmentBytes = 10 * 1024 * 1024

// unsafeFilenameChars matches everything outside the portable filename set.
var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// safeFilename reduces a panel-supplied filename to a single path segment made
// of portable characters, so it can never escape the tmp dir.
func safeFilename(name string) string {
	name = unsafeFilenameChars.ReplaceAllString(filepath.Base(name), "_")
	if name == "" || name == "." || strings.Contains(name, "..") {
		return "file"
	}
	return name
}

// modelImageMimeTypes are the image formats providers accept as image content
// parts. Anything else is handed to the agent as a file path instead.
var modelImageMimeTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// saveAttachments writes each attachment into the project tmp dir (where
// clipboard images also land). Images come back as ImageAttachments with
// SourcePath set so they flow to the model as image parts. Other files come
// back as text notes naming the saved path so the agent can Read them.
func saveAttachments(attachments []agentdomain.ImageAttachment) ([]agentdomain.ImageAttachment, []string) {
	if len(attachments) == 0 {
		return nil, nil
	}
	tmpDir := config.ProjectTmpDir()
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		logger.Warn("failed to create tmp directory", "path", tmpDir, "error", err)
		return nil, nil
	}
	var images []agentdomain.ImageAttachment
	var notes []string
	stamp := time.Now().Format("20060102-150405")
	for i, a := range attachments {
		if a.Data == "" || a.Filename == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil || len(data) > maxAttachmentBytes {
			logger.Warn("skipping extension attachment", "filename", a.Filename, "error", err, "bytes", len(data))
			continue
		}
		name := safeFilename(a.Filename)
		path := filepath.Join(tmpDir, fmt.Sprintf("attachment-%s-%d-%s", stamp, i, name))
		if err := os.WriteFile(path, data, 0644); err != nil {
			logger.Warn("failed to save extension attachment", "path", path, "error", err)
			continue
		}
		switch {
		case modelImageMimeTypes[a.MimeType]:
			a.DisplayName = a.Filename
			a.SourcePath = path
			images = append(images, a)
		case strings.HasPrefix(a.MimeType, "image/"):
			notes = append(notes, fmt.Sprintf("[%s saved at %s; %s is not a model-readable image format, convert it to PNG first (e.g. sips -s format png on macOS, or magick) and then view the PNG]", name, path, a.MimeType))
		default:
			notes = append(notes, fmt.Sprintf("[%s saved at %s]", name, path))
		}
	}
	utils.PruneFilesByModTime(tmpDir, 20, 24*time.Hour, func(e os.DirEntry) bool {
		return strings.HasPrefix(e.Name(), "attachment-")
	})
	return images, notes
}

type extMode struct {
	Type string `json:"type"`
	Mode string `json:"mode"`
}

// extInterrupted tells the panel the current turn ended cancelled (terminal
// Esc/Ctrl+C or the panel's own Stop), so it can clear its "Working" state
// even when no TEXT_MESSAGE_END reaches it, e.g. a cancel mid tool call.
type extInterrupted struct {
	Type string `json:"type"`
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
	Type    string   `json:"type"`
	Models  []string `json:"models"`
	Current string   `json:"current,omitempty"`
}

type extHelloAck struct {
	Type string `json:"type"`
}

type extSnapshot struct {
	Type        string          `json:"type"`
	Messages    []sdk.Message   `json:"messages"`
	ToolResults map[string]bool `json:"tool_results,omitempty"`
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

type extHistory struct {
	Type    string   `json:"type"`
	History []string `json:"history"`
}

type extChatEvent struct {
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event"`
}

// Deps are the collaborators the bridge and its panel units use. The container
// builds every one of them before the bridge, so nothing is wired late and no
// capability is optional.
type Deps struct {
	Extension     config.ExtensionConfig
	Notifier      agentdomain.UINotifier
	Conversations convdomain.ConversationRepository
	Events        agentdomain.EventBridge
	Skills        agentdomain.SkillsService
	Tools         agentdomain.ToolService
	Approval      agentdomain.ApprovalPolicy
	Models        convdomain.ModelService
	Modes         agentdomain.AgentModeState
	Agent         agentdomain.AgentService
	History       storage.ShellHistoryStorage
	DefaultModel  string
	SessionID     string
	ArtifactsDir  string
}

// frameWriter writes one panel frame over the active extension connection.
type frameWriter func(conn *websocket.Conn, frame any)

// ExtensionBridge hosts the localhost WebSocket endpoint the opentask browser
// extension dials into. It owns the connection and the token handshake,
// exposes Request as the browser driver's command/result RPC, and routes every
// other frame to the panel unit that owns it.
type ExtensionBridge struct {
	extension    config.ExtensionConfig
	notifier     agentdomain.UINotifier
	artifactsDir string

	conversations *conversations
	history       *history
	skills        *skills
	models        *modelPicker
	modes         *modes
	tools         *toolRequests
	approvals     *approvals
	mirror        *chatMirror

	server   *http.Server
	addr     string
	startErr error
	startMu  sync.Mutex
	mu       sync.Mutex
	conn     *websocket.Conn
	connStop chan struct{}
	pending  map[string]chan json.RawMessage
	writeMu  sync.Mutex
}

// NewExtensionBridge builds the bridge and its panel units, one unit per frame
// family, so each unit holds only the dependencies it uses.
// artifactsDir, when non-empty, is served read-only at /artifacts/ so the panel
// can display generated images the agent saved locally.
func NewExtensionBridge(deps Deps) *ExtensionBridge {
	b := &ExtensionBridge{
		extension:    deps.Extension,
		notifier:     deps.Notifier,
		artifactsDir: deps.ArtifactsDir,
		pending:      make(map[string]chan json.RawMessage),
	}
	b.modes = &modes{write: b.write, state: deps.Modes}
	b.approvals = newApprovals(b.write, deps.Notifier)
	b.conversations = &conversations{write: b.write, repo: deps.Conversations}
	b.history = newHistory(b.write, deps.History)
	b.skills = &skills{write: b.write, service: deps.Skills}
	b.models = &modelPicker{write: b.write, service: deps.Models, defaultModel: deps.DefaultModel, notifier: deps.Notifier}
	b.tools = newToolRequests(b.write, deps)
	b.mirror = newChatMirror(b.write, deps, b.approvals)
	return b
}

// Start listens on 127.0.0.1:<port> and serves the /ws endpoint. Errors are
// also stored so Request surfaces them instead of a silent no-op.
func (b *ExtensionBridge) Start() error {
	if b.extension.Token == "" {
		b.startErr = errors.New("browser_use.extension.token is empty - set a shared secret in browser_use.yaml and in the opentask extension options")
		return b.startErr
	}

	addr := fmt.Sprintf("127.0.0.1:%d", b.extension.Port)
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

// retryStart re-runs Start when the previous attempt failed and returns the
// resulting start error, nil once the bridge is listening.
func (b *ExtensionBridge) retryStart() error {
	b.startMu.Lock()
	defer b.startMu.Unlock()
	if b.startErr == nil {
		return nil
	}
	b.startErr = nil
	return b.Start()
}

// Addr returns the actual listen address (useful with port 0 in tests).
func (b *ExtensionBridge) Addr() string {
	return b.addr
}

var extUpgrader = websocket.Upgrader{
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
	if err := conn.ReadJSON(&hello); err != nil || hello.Type != inboundBrowserHello ||
		subtle.ConstantTimeCompare([]byte(hello.Token), []byte(b.extension.Token)) != 1 {
		logger.Warn("extension bridge rejected a connection with a bad or missing hello")
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if err := conn.WriteJSON(extHelloAck{Type: outboundBrowserHelloAck}); err != nil {
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
	b.failPendingLocked()
	b.mu.Unlock()

	b.approvals.reset()
	b.tools.reset()
	b.notifyConnected(true)

	go b.readLoop(conn, stop)
	go b.mirror.run(conn, stop)
	go b.pingLoop(conn, stop)
}

// readLoop handles frames from the extension until the connection dies or is
// replaced. browser_result frames are forwarded to Request with their raw
// bytes intact, so callers own the result payload.
func (b *ExtensionBridge) readLoop(conn *websocket.Conn, stop chan struct{}) {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			b.dropConn(conn, stop)
			return
		}
		var msg extInbound
		if err := json.Unmarshal(raw, &msg); err != nil {
			logger.Debug("extension bridge dropped an undecodable frame", "error", err)
			continue
		}
		b.route(conn, stop, raw, msg)
	}
}

// route dispatches one inbound frame to the panel unit that owns its frame
// family. Unknown frame types are ignored for forward compatibility.
func (b *ExtensionBridge) route(conn *websocket.Conn, stop chan struct{}, raw []byte, msg extInbound) {
	switch msg.Type {
	case inboundBrowserResult:
		b.deliverBrowserResult(msg.ID, raw)
	case inboundUserMessage:
		b.submitUserMessage(msg)
	case inboundNewSession:
		b.conversations.start(conn)
	case inboundListHistory:
		b.history.list(conn)
	case inboundListConversations:
		b.conversations.list(conn)
	case inboundListSkills:
		b.skills.list(conn)
	case inboundSelectModel:
		b.models.selectModel(conn, msg.Model)
		b.modes.send(conn)
	case inboundListModels:
		b.models.list(conn)
	case inboundSetMode:
		b.modes.set(conn, msg.Mode)
	case inboundResumeConversation:
		b.conversations.resume(conn, msg.ID)
	case inboundInterrupt:
		b.mirror.interrupt()
	case inboundToolRequest:
		go b.tools.run(conn, stop, msg)
	case inboundApprovalResponse:
		if !b.tools.resolveApproval(conn, msg.RequestID, msg.Action) {
			b.approvals.answer(conn, msg.RequestID, msg.Action)
		}
	}
}

// deliverBrowserResult hands a browser_result frame to the goroutine waiting
// for that command id.
func (b *ExtensionBridge) deliverBrowserResult(id string, raw []byte) {
	b.mu.Lock()
	ch, ok := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if ok {
		ch <- raw
	}
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
	dropped := b.conn == conn
	if dropped {
		b.conn = nil
		b.failPendingLocked()
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
	b.mu.Unlock()
	_ = conn.Close()
	if dropped {
		b.notifyConnected(false)
	}
}

// submitUserMessage turns a panel message into the same user-input event the TUI
// submits, saving any attachments and recording the message in the shared shell
// history.
func (b *ExtensionBridge) submitUserMessage(msg extInbound) {
	if msg.Content != "" {
		images, notes := saveAttachments(msg.Attachments)
		content := strings.Join(append([]string{msg.Content}, notes...), "\n")
		b.notifier.Notify(agentdomain.UserInputEvent{Content: content, Images: images, FromExtension: true})
	}
	b.history.append(msg.Content)
}

// notifyConnected tells the TUI status bar whether an extension is attached.
func (b *ExtensionBridge) notifyConnected(connected bool) {
	b.notifier.Notify(agentdomain.BrowserExtensionStatusEvent{Connected: connected})
}

func (b *ExtensionBridge) write(conn *websocket.Conn, v any) {
	if err := b.writeFrame(conn, v); err != nil {
		logger.Debug("extension bridge write failed", "error", err)
	}
}

func (b *ExtensionBridge) writeFrame(conn *websocket.Conn, v any) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return conn.WriteJSON(v)
}

// failPendingLocked releases every Request still waiting on a connection that
// is gone, so callers get an immediate error instead of waiting out their
// deadline. The caller holds b.mu, which keeps it atomic with the conn swap.
func (b *ExtensionBridge) failPendingLocked() {
	for id, ch := range b.pending {
		close(ch)
		delete(b.pending, id)
	}
}

// Request writes one extension frame and waits for the browser_result carrying
// id, returning that frame's raw JSON. A failed listen is retried first, so a
// process that lost the port to another infer takes it over once that one
// exits. The caller owns the id, the frame (including its timeout_ms), and the
// deadline via ctx.
func (b *ExtensionBridge) Request(ctx context.Context, id string, frame json.RawMessage) (json.RawMessage, error) {
	if err := b.retryStart(); err != nil {
		return nil, err
	}

	b.mu.Lock()
	conn := b.conn
	if conn == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("no browser extension connected on port %d - install the opentask extension and set its bridge port/token to match browser_use.yaml", b.extension.Port)
	}
	ch := make(chan json.RawMessage, 1)
	b.pending[id] = ch
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	if err := b.writeFrame(conn, frame); err != nil {
		return nil, fmt.Errorf("failed to send the command to the browser extension: %w", err)
	}

	select {
	case result, ok := <-ch:
		if !ok {
			return nil, errBrowserExtensionDisconnected
		}
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close shuts the server and any connection down.
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
	b.failPendingLocked()
	b.mu.Unlock()
}
