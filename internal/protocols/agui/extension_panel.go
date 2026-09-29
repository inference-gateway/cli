package agui

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// Panel frame types a session worker answers on stdin, and the replies it
// writes on stdout. Unknown types are left to the caller.
const (
	inboundUserMessage        = "user_message"
	inboundNewSession         = "new_session"
	inboundResumeConversation = "resume_conversation"
	inboundListHistory        = "list_history"
	inboundListConversations  = "list_conversations"
	inboundListSkills         = "list_skills"
	inboundListModels         = "list_models"
	inboundSelectModel        = "select_model"
	inboundSetMode            = "set_mode"
	inboundToolRequest        = "tool_request"
	inboundApprovalResponse   = "approval_response"

	outboundConversations = "conversations"
	outboundSkills        = "skills"
	outboundHistory       = "history"
	outboundModels        = "models"
	outboundMode          = "mode"
	outboundToolResult    = "tool_result"
)

// conversationListLimit caps list_conversations, mirroring the TUI selector.
const conversationListLimit = 50

// historyListLimit caps list_history replies. The panel only needs recent
// entries for arrow-up recall.
const historyListLimit = 1000

type panelFrame struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Content    string `json:"content"`
	Model      string `json:"model"`
	Mode       string `json:"mode"`
	ToolName   string `json:"tool_name"`
	ToolArgs   string `json:"tool_args"`
	ToolCallID string `json:"tool_call_id"`
	Approved   bool   `json:"approved"`
}

type extMode struct {
	Type string `json:"type"`
	Mode string `json:"mode"`
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

// frameWriter writes one panel frame as one line.
type frameWriter func(frame any)

// PanelDeps are the collaborators the panel units use, all from the worker's
// own container, so every answer is scoped to the worker's project dir.
type PanelDeps struct {
	Conversations convdomain.ConversationRepository
	Skills        agentdomain.SkillsService
	Tools         agentdomain.ToolService
	Approval      agentdomain.ApprovalPolicy
	Models        convdomain.ModelService
	Modes         agentdomain.AgentModeState
	History       storage.ShellHistoryStorage
	DefaultModel  string
}

// Panel answers the panel frames a session worker reads on stdin: listings,
// model and mode switches, direct tool requests and the conversation snapshot.
// Each reply is one JSON line on out, one Write per line.
type Panel struct {
	out io.Writer
	mu  sync.Mutex

	// snapshotReplied marks a just-answered new_session / resume_conversation
	// frame whose reply already shipped a MESSAGES_SNAPSHOT, so the run
	// opening right after it skips its own boot snapshot.
	snapshotReplied bool

	conversations *conversations
	history       *history
	skills        *skills
	models        *modelPicker
	modes         *modes
	tools         *toolRequests
}

// NewPanel builds the panel units, one per frame family, each holding only the
// dependencies it uses.
func NewPanel(deps PanelDeps, out io.Writer) *Panel {
	p := &Panel{out: out}
	p.conversations = &conversations{write: p.write, repo: deps.Conversations}
	p.history = newHistory(p.write, deps.History)
	p.skills = &skills{write: p.write, service: deps.Skills}
	p.models = &modelPicker{write: p.write, service: deps.Models, defaultModel: deps.DefaultModel}
	p.modes = &modes{write: p.write, state: deps.Modes}
	p.tools = newToolRequests(p.write, deps)
	return p
}

// Handle answers one stdin line and reports whether the panel consumed it. A
// user_message is recorded in the shell history but left for the turn loop,
// and an approval_response is consumed only when it answers a tool_request.
func (p *Panel) Handle(line []byte) bool { //nolint:gocyclo,cyclop // one case per panel frame
	var msg panelFrame
	if json.Unmarshal(line, &msg) != nil {
		return false
	}
	switch msg.Type {
	case inboundUserMessage:
		p.history.append(msg.Content)
		return false
	case inboundApprovalResponse:
		return p.tools.resolve(msg.ToolCallID, msg.Approved)
	case inboundNewSession, inboundResumeConversation:
		p.mu.Lock()
		p.snapshotReplied = true
		p.mu.Unlock()
		p.conversations.snapshot()
	case inboundListConversations:
		p.conversations.list()
	case inboundListHistory:
		p.history.list()
	case inboundListSkills:
		p.skills.list()
	case inboundListModels:
		p.models.list()
	case inboundSelectModel:
		p.models.selectModel(msg.Model)
		p.modes.send()
	case inboundSetMode:
		p.modes.set(msg.Mode)
	case inboundToolRequest:
		go p.tools.run(msg)
	default:
		return false
	}
	return true
}

// SnapshotReplied reports and clears whether the panel just answered a
// new_session or resume_conversation frame with a MESSAGES_SNAPSHOT, so the
// run opening right after it need not repeat the same snapshot.
func (p *Panel) SnapshotReplied() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	replied := p.snapshotReplied
	p.snapshotReplied = false
	return replied
}

func (p *Panel) write(frame any) {
	var data []byte
	var err error
	if ev, ok := frame.(aguievents.Event); ok {
		data, err = ev.ToJSON()
	} else {
		data, err = json.Marshal(frame)
	}
	if err != nil {
		logger.Error("failed to marshal a panel frame", "error", err)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := p.out.Write(append(data, '\n')); err != nil {
		logger.Debug("failed to write a panel frame", "error", err)
	}
}

// conversationLister is the slice of the persistent repo the panel picker needs.
// Declared here (not in domain) because the in-memory fallback repo has no
// listing - the type assertion simply fails there and yields an empty list.
type conversationLister interface {
	ListSavedConversations(ctx context.Context, limit, offset int) ([]convdomain.ConversationSummary, error)
}

// conversations answers the panel's conversation picker and snapshot from the
// worker's conversation repository.
type conversations struct {
	write frameWriter
	repo  convdomain.ConversationRepository
}

// snapshot answers new_session and resume_conversation with the worker's
// conversation as an AG-UI MESSAGES_SNAPSHOT. The worker was launched for that
// conversation, so the repository is already on it.
func (c *conversations) snapshot() {
	c.write(aguievents.NewMessagesSnapshotEvent(snapshotMessages(c.repo.GetMessages())))
}

// list answers list_conversations with the stored conversations (newest-first),
// so the panel can offer the same picker the CLI resumes from.
func (c *conversations) list() {
	lister, ok := c.repo.(conversationLister)
	if !ok {
		c.write(extConversations{Type: outboundConversations})
		return
	}
	summaries, err := lister.ListSavedConversations(context.Background(), conversationListLimit, 0)
	if err != nil {
		logger.Debug("panel failed to list conversations", "error", err)
		c.write(extConversations{Type: outboundConversations})
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
	c.write(extConversations{Type: outboundConversations, Conversations: out})
}

// history holds the shared shell input history: panel messages land in the same
// store the TUI's arrow-up navigation walks, and the panel lists it back.
type history struct {
	write frameWriter
	store storage.ShellHistoryStorage
}

// newHistory falls back to an empty store when storage failed to initialize, so
// the handlers never have to check for a missing store.
func newHistory(write frameWriter, store storage.ShellHistoryStorage) *history {
	if store == nil {
		store = noHistory{}
	}
	return &history{write: write, store: store}
}

// noHistory is the empty shell-history store.
type noHistory struct{}

func (noHistory) AppendHistory(context.Context, string) error { return nil }

func (noHistory) LoadHistory(context.Context, int) ([]string, error) { return []string{}, nil }

// list answers list_history with the shared shell input history. An empty store
// still replies with an empty list.
func (h *history) list() {
	loaded, err := h.store.LoadHistory(context.Background(), historyListLimit)
	if err != nil {
		logger.Debug("panel failed to load history", "error", err)
		loaded = nil
	}
	if loaded == nil {
		loaded = []string{}
	}
	h.write(extHistory{Type: outboundHistory, History: loaded})
}

// append records a panel-sent message in the shared shell history, mirroring
// the TUI submit path (trimmed, consecutive duplicates skipped).
func (h *history) append(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	if last, err := h.store.LoadHistory(context.Background(), 1); err == nil && len(last) > 0 && last[len(last)-1] == content {
		return
	}
	if err := h.store.AppendHistory(context.Background(), content); err != nil {
		logger.Warn("panel failed to append history", "error", err)
	}
}

// skills answers list_skills with the agent's discovered skills - the same set
// (project, .agents, user, plugin, catalog) the skills service already merged
// with precedence, so the panel's "/" menu mirrors what the TUI offers.
type skills struct {
	write   frameWriter
	service agentdomain.SkillsService
}

func (s *skills) list() {
	loaded := s.service.List()
	out := make([]agentdomain.SkillSummary, 0, len(loaded))
	for _, skill := range loaded {
		out = append(out, skill.Summary())
	}
	s.write(extSkills{Type: outboundSkills, Skills: out})
}

// modelPicker answers the panel's model picker from the worker's model service,
// listing the gateway's models with the configured default first.
type modelPicker struct {
	write        frameWriter
	service      convdomain.ModelService
	defaultModel string
}

// list answers list_models with the models the gateway serves, the configured
// default model first, so the panel's pickers mirror the CLI.
func (m *modelPicker) list() {
	out := []string{}
	listed, err := m.service.ListModels(context.Background())
	if err != nil {
		logger.Debug("panel failed to list models", "error", err)
	}
	if m.defaultModel != "" {
		out = append(out, m.defaultModel)
	}
	for _, name := range listed {
		if name != m.defaultModel {
			out = append(out, name)
		}
	}
	m.write(extModels{Type: outboundModels, Models: out, Current: m.service.GetCurrentModel()})
}

// selectModel switches the worker's model for its next turns and re-sends the
// model list, so the panel reflects the outcome whether or not the switch was
// accepted.
func (m *modelPicker) selectModel(name string) {
	if name != "" {
		if err := m.service.SelectModel(name); err != nil {
			logger.Debug("panel failed to select model", "model", name, "error", err)
		}
	}
	m.list()
}

// modes reports the worker's agent mode (standard/plan/auto/auto-with-judge)
// and applies set_mode to the state the agent reads on every call.
type modes struct {
	write frameWriter
	state agentdomain.AgentModeState
}

// send reports the current agent mode as its canonical mode key.
func (m *modes) send() {
	m.write(extMode{Type: outboundMode, Mode: m.state.GetAgentMode().ModeKey()})
}

// set switches the agent mode - it also governs tool_request approvals - and
// echoes the resulting mode so the panel reflects the outcome either way.
func (m *modes) set(mode string) {
	if parsed, ok := agentdomain.ParseAgentMode(mode); ok && parsed != agentdomain.AgentModeReadOnly {
		m.state.SetAgentMode(parsed)
	}
	m.send()
}
