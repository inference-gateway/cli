package infrastructure

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

	uuid "github.com/google/uuid"
	websocket "github.com/gorilla/websocket"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

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
	outboundBrowserCommand       = "browser_command"
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

// Browser actions the extension understands on a browser_command frame.
const (
	browserActionNavigate   = "navigate"
	browserActionClick      = "click"
	browserActionType       = "type"
	browserActionRead       = "read"
	browserActionScreenshot = "screenshot"
	browserActionTabs       = "tabs"
)

// approvalActionApprove is the only panel action that grants a tool call.
const approvalActionApprove = "approve"

type extInbound struct {
	Type             string                        `json:"type"`
	Token            string                        `json:"token,omitempty"`
	ExtensionVersion string                        `json:"extension_version,omitempty"`
	ID               string                        `json:"id,omitempty"`
	URL              string                        `json:"url,omitempty"`
	Title            string                        `json:"title,omitempty"`
	Content          string                        `json:"content,omitempty"`
	Events           []string                      `json:"events,omitempty"`
	Error            string                        `json:"error,omitempty"`
	RequestID        string                        `json:"request_id,omitempty"`
	Action           string                        `json:"action,omitempty"`
	Model            string                        `json:"model,omitempty"`
	Image            string                        `json:"image,omitempty"`
	ImageMimeType    string                        `json:"image_mime_type,omitempty"`
	Tabs             []browserdomain.BrowserTab    `json:"tabs,omitempty"`
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
	Config        *config.BrowserUseConfig
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
// extension dials into. It owns the connection, implements
// browserdomain.BrowserDriver by forwarding the browser-use verbs to the
// extension, and routes every other frame to the panel unit that owns it.
type ExtensionBridge struct {
	cfg          *config.BrowserUseConfig
	notifier     agentdomain.UINotifier
	artifactsDir string

	conversations *conversations
	history       *history
	skills        *skills
	models        *models
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
	pending  map[string]chan extInbound
	writeMu  sync.Mutex
}

// NewExtensionBridge builds the bridge and its panel units, one unit per frame
// family, so each unit holds only the dependencies it uses.
// artifactsDir, when non-empty, is served read-only at /artifacts/ so the panel
// can display generated images the agent saved locally.
func NewExtensionBridge(deps Deps) *ExtensionBridge {
	b := &ExtensionBridge{
		cfg:          deps.Config,
		notifier:     deps.Notifier,
		artifactsDir: deps.ArtifactsDir,
		pending:      make(map[string]chan extInbound),
	}
	b.modes = &modes{write: b.write, state: deps.Modes}
	b.approvals = newApprovals(b.write, deps.Notifier)
	b.conversations = &conversations{write: b.write, repo: deps.Conversations}
	b.history = newHistory(b.write, deps.History)
	b.skills = &skills{write: b.write, service: deps.Skills}
	b.models = &models{write: b.write, service: deps.Models, defaultModel: deps.DefaultModel, notifier: deps.Notifier}
	b.tools = newToolRequests(b.write, deps)
	b.mirror = newChatMirror(b.write, deps, b.approvals)
	return b
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
		subtle.ConstantTimeCompare([]byte(hello.Token), []byte(b.cfg.Extension.Token)) != 1 {
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
	b.mu.Unlock()

	b.approvals.reset()
	b.tools.reset()
	b.notifyConnected(true)

	go b.readLoop(conn, stop)
	go b.mirror.run(conn, stop)
	go b.pingLoop(conn, stop)
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
		b.route(conn, stop, msg)
	}
}

// route dispatches one inbound frame to the panel unit that owns its frame
// family. Unknown frame types are ignored for forward compatibility.
func (b *ExtensionBridge) route(conn *websocket.Conn, stop chan struct{}, msg extInbound) {
	switch msg.Type {
	case inboundBrowserResult:
		b.deliverBrowserResult(msg)
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
func (b *ExtensionBridge) deliverBrowserResult(msg extInbound) {
	b.mu.Lock()
	ch, ok := b.pending[msg.ID]
	delete(b.pending, msg.ID)
	b.mu.Unlock()
	if ok {
		ch <- msg
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
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	if err := conn.WriteJSON(v); err != nil {
		logger.Debug("extension bridge write failed", "error", err)
	}
}

// send dispatches one browser command and waits for its result. A failed
// Start is retried first, so a process that lost the port to another infer
// process takes it over once that process exits.
func (b *ExtensionBridge) send(ctx context.Context, cmd extBrowserCommand) (extInbound, error) {
	if err := b.retryStart(); err != nil {
		return extInbound{}, err
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
	result, err := b.send(ctx, extBrowserCommand{Type: outboundBrowserCommand, Action: browserActionNavigate, URL: url})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{Action: browserActionNavigate, URL: result.URL, Title: result.Title}, nil
}

// Click implements browserdomain.BrowserDriver.
func (b *ExtensionBridge) Click(ctx context.Context, selector string) (browserdomain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: outboundBrowserCommand, Action: browserActionClick, Selector: selector})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{Action: browserActionClick, Selector: selector, URL: result.URL, Title: result.Title}, nil
}

// Type implements browserdomain.BrowserDriver.
func (b *ExtensionBridge) Type(ctx context.Context, selector, text string, pressEnter bool) (browserdomain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: outboundBrowserCommand, Action: browserActionType, Selector: selector, Text: text, PressEnter: pressEnter})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{Action: browserActionType, Selector: selector, Text: text, URL: result.URL, Title: result.Title}, nil
}

// Read implements browserdomain.BrowserDriver.
func (b *ExtensionBridge) Read(ctx context.Context, selector string) (browserdomain.BrowserToolResult, error) {
	result, err := b.send(ctx, extBrowserCommand{Type: outboundBrowserCommand, Action: browserActionRead, Selector: selector})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{
		Action:   browserActionRead,
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
	result, err := b.send(ctx, extBrowserCommand{Type: outboundBrowserCommand, Action: browserActionScreenshot})
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
	result, err := b.send(ctx, extBrowserCommand{Type: outboundBrowserCommand, Action: browserActionTabs})
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
