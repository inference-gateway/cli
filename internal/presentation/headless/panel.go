package headless

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	llm "github.com/inference-gateway/cli/internal/platform/llm"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// Panel frame types a session worker answers on stdin, and the replies it
// writes on stdout. Unknown types are left to the caller.
const (
	inboundRunAgentInput      = "run_agent_input"
	inboundNewSession         = "new_session"
	inboundResumeConversation = "resume_conversation"
	inboundListHistory        = "list_history"
	inboundListConversations  = "list_conversations"
	inboundListSkills         = "list_skills"
	inboundListShortcuts      = "list_shortcuts"
	inboundListIssues         = "list_issues"
	inboundListFiles          = "list_files"
	inboundListModels         = "list_models"
	inboundSelectModel        = "select_model"
	inboundSetMode            = "set_mode"
	inboundToolRequest        = "tool_request"
	inboundApprovalResponse   = "approval_response"
	inboundCompletionRequest  = "completion_request"
	inboundAddNote            = "add_note"

	outboundConversations    = "conversations"
	outboundSkills           = "skills"
	outboundShortcuts        = "shortcuts"
	outboundIssues           = "issues"
	outboundFiles            = "files"
	outboundHistory          = "history"
	outboundModels           = "models"
	outboundMode             = "mode"
	outboundToolResult       = "tool_result"
	outboundCompletionResult = "completion_result"
)

// conversationListLimit caps list_conversations, mirroring the TUI selector.
const conversationListLimit = 50

// historyListLimit caps list_history replies. The panel only needs recent
// entries for arrow-up recall.
const historyListLimit = 1000

type panelFrame struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Input struct {
		Messages []agui.Message `json:"messages"`
	} `json:"input"`
	Model      string `json:"model"`
	Mode       string `json:"mode"`
	ToolName   string `json:"tool_name"`
	ToolArgs   string `json:"tool_args"`
	ToolCallID string `json:"tool_call_id"`
	Approved   bool   `json:"approved"`
	System     string `json:"system"`
	Prompt     string `json:"prompt"`
	MaxTokens  int    `json:"max_tokens"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	TaskID     string `json:"task_id"`
}

type modeFrame struct {
	Type string `json:"type"`
	Mode string `json:"mode"`
}

type toolResultFrame struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}

type modelsFrame struct {
	Type    string      `json:"type"`
	Models  []string    `json:"models"`
	Current string      `json:"current,omitempty"`
	Catalog []sdk.Model `json:"catalog,omitempty"`
}

// modelCatalog is a model service that keeps the gateway's model entries.
type modelCatalog interface {
	Catalog() []sdk.Model
}

type conversationSummary struct {
	ID           string                       `json:"id"`
	Title        string                       `json:"title"`
	UpdatedAt    time.Time                    `json:"updated_at"`
	MessageCount int                          `json:"message_count"`
	TokenStats   convdomain.SessionTokenStats `json:"token_stats"`
	CostStats    convdomain.SessionCostStats  `json:"cost_stats"`
}

type conversationsFrame struct {
	Type          string                `json:"type"`
	Conversations []conversationSummary `json:"conversations"`
}

type skillsFrame struct {
	Type   string                     `json:"type"`
	Skills []agentdomain.SkillSummary `json:"skills"`
}

type issueSummary struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

type issuesFrame struct {
	Type   string         `json:"type"`
	Issues []issueSummary `json:"issues"`
}

type shortcutSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Usage       string `json:"usage"`
	Kind        string `json:"kind"`
}

type shortcutsFrame struct {
	Type      string            `json:"type"`
	Shortcuts []shortcutSummary `json:"shortcuts"`
}

type filesFrame struct {
	Type  string   `json:"type"`
	Files []string `json:"files"`
}

type historyFrame struct {
	Type    string   `json:"type"`
	History []string `json:"history"`
}

// frameWriter writes one panel frame as one line.
type frameWriter func(frame any)

// lineWriter serializes the panel's lines, its own frames and the AG-UI events
// alike, since tool requests answer from their own goroutines.
type lineWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *lineWriter) Write(line []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.Write(line)
}

// PanelDeps are the collaborators the panel units use, all from the worker's
// own container, so every answer is scoped to the worker's project dir.
type PanelDeps struct {
	Conversations convdomain.ConversationRepository
	Skills        agentdomain.SkillsService
	Shortcuts     *shortcuts.Registry
	Issues        agentdomain.GitHubIssueService
	Files         agentdomain.FileService
	Tools         agentdomain.ToolService
	Approval      agentdomain.ApprovalPolicy
	Models        convdomain.ModelService
	Modes         agentdomain.AgentModeState
	History       storage.ShellHistoryStorage
	Storage       storage.ConversationStorage
	Completer     llm.Completer
	DefaultModel  string
	ArtifactsDir  func(sessionID string) string
	Frames        func(name string) (agentdomain.FrameSource, bool)
	Screen computerdomain.ScreenControl
	Queue  convdomain.MessageQueue
	Jobs     scheddomain.BackgroundTaskRegistry
	A2ATasks a2adomain.BackgroundTaskService
}

// Panel answers the panel frames a session worker reads on stdin: listings,
// model and mode switches, direct tool requests and the conversation snapshot.
// Each reply is one JSON line on out, one Write per line.
type Panel struct {
	out *lineWriter

	conversations *conversations
	history       *history
	skills        *skills
	shortcuts     *shortcutList
	issues        *issues
	files         *projectFiles
	models        *modelPicker
	modes         *modes
	tools         *toolRequests
	completions   *completions
	teleports     *teleports
	artifacts     *artifacts
	frames        *frames
	screen        *screenUnit
	tasks         *tasks
}

// NewPanel builds the panel units, one per frame family, each holding only the
// dependencies it uses.
func NewPanel(deps PanelDeps, out io.Writer) *Panel {
	p := &Panel{out: &lineWriter{out: out}}
	p.conversations = &conversations{write: p.write, out: p.out, repo: deps.Conversations}
	p.history = newHistory(p.write, deps.History)
	p.skills = &skills{write: p.write, service: deps.Skills}
	p.shortcuts = &shortcutList{write: p.write, registry: deps.Shortcuts}
	p.issues = &issues{write: p.write, service: deps.Issues}
	p.files = &projectFiles{write: p.write, service: deps.Files}
	p.models = &modelPicker{write: p.write, service: deps.Models, defaultModel: deps.DefaultModel}
	p.modes = &modes{write: p.write, state: deps.Modes}
	p.tools = newToolRequests(p.write, p.out, deps)
	p.completions = &completions{write: p.write, completer: deps.Completer, models: deps.Models}
	p.teleports = &teleports{write: p.write, store: deps.Storage}
	p.artifacts = &artifacts{write: p.write, dir: deps.ArtifactsDir}
	p.frames = &frames{write: p.write, sources: deps.Frames}
	p.screen = newScreenUnit(p.write, deps)
	p.tasks = &tasks{write: p.write, jobs: deps.Jobs, a2a: deps.A2ATasks}
	return p
}

// Handle answers one stdin line and reports whether the panel consumed it. A
// run input's messages are recorded in the shell history but left for the
// turn loop, and an approval_response is consumed only when it answers a
// tool_request.
func (p *Panel) Handle(line []byte) bool { //nolint:gocyclo,cyclop // one case per panel frame
	var msg panelFrame
	if json.Unmarshal(line, &msg) != nil {
		return false
	}
	switch msg.Type {
	case inboundRunAgentInput:
		for _, message := range msg.Input.Messages {
			text, _ := messageParts(message)
			p.history.append(text)
		}
		return false
	case inboundApprovalResponse:
		return p.tools.resolve(msg.ToolCallID, msg.Approved)
	case inboundNewSession, inboundResumeConversation:
		p.conversations.snapshot()
	case inboundListConversations:
		p.conversations.list()
	case inboundListHistory:
		p.history.list()
	case inboundListSkills:
		p.skills.list()
	case inboundListShortcuts:
		p.shortcuts.list()
	case inboundListIssues:
		p.issues.list()
	case inboundListFiles:
		p.files.list()
	case inboundListModels:
		p.models.list()
	case inboundSelectModel:
		p.models.selectModel(msg.Model)
		p.modes.send()
	case inboundSetMode:
		p.modes.set(msg.Mode)
	case inboundToolRequest:
		go p.tools.run(msg)
	case inboundCompletionRequest:
		go p.completions.run(msg)
	case inboundAddNote:
		p.conversations.addNote(msg.Content)
	case inboundExportConversation:
		p.teleports.export(msg.ID)
	case inboundImportConversation:
		p.teleports.importFrom(line)
	case inboundListArtifacts:
		p.artifacts.list(msg.ID)
	case inboundReadArtifact:
		p.artifacts.read(msg.ID, msg.Name)
	case inboundReadFrame:
		p.frames.read(msg.ID)
	case inboundListTasks:
		p.tasks.list()
	case inboundCancelTask:
		go p.tasks.cancel(msg.TaskID)
	case inboundScreenControl:
		p.screen.controlFrame(msg.ID, line)
	case inboundScreenInput:
		p.screen.input(msg.ID, line)
	default:
		return false
	}
	return true
}

func (p *Panel) write(frame any) {
	data, err := json.Marshal(frame)
	if err != nil {
		logger.Error("failed to marshal a panel frame", "error", err)
		return
	}
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
	out   io.Writer
	repo  convdomain.ConversationRepository
}

// addNote keeps a note a client wrote, such as a shortcut's output, as an
// assistant entry. The worker builds the entry so a client cannot add a tool
// result or a system message this way.
func (c *conversations) addNote(content string) {
	if content == "" {
		return
	}
	entry := convdomain.ConversationEntry{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent(content)}, Time: time.Now()}
	if err := c.repo.AddMessage(entry); err != nil {
		logger.Warn("failed to keep a client's note", "error", err)
	}
}

// snapshot answers new_session and resume_conversation with the worker's
// conversation as an AG-UI MESSAGES_SNAPSHOT. The worker was launched for that
// conversation, so the repository is already on it.
func (c *conversations) snapshot() {
	agui.WriteMessagesSnapshot(c.out, snapshotMessages(c.repo.GetMessages()))
}

// list answers list_conversations with the stored conversations (newest-first),
// so the panel can offer the same picker the CLI resumes from.
func (c *conversations) list() {
	lister, ok := c.repo.(conversationLister)
	if !ok {
		c.write(conversationsFrame{Type: outboundConversations})
		return
	}
	summaries, err := lister.ListSavedConversations(context.Background(), conversationListLimit, 0)
	if err != nil {
		logger.Debug("panel failed to list conversations", "error", err)
		c.write(conversationsFrame{Type: outboundConversations})
		return
	}
	out := make([]conversationSummary, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, conversationSummary{
			ID:           s.ID,
			Title:        s.Title,
			UpdatedAt:    s.UpdatedAt,
			MessageCount: s.MessageCount,
			TokenStats:   s.TokenStats,
			CostStats:    s.CostStats,
		})
	}
	c.write(conversationsFrame{Type: outboundConversations, Conversations: out})
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
	h.write(historyFrame{Type: outboundHistory, History: loaded})
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
	s.write(skillsFrame{Type: outboundSkills, Skills: out})
}

// shortcutList answers list_shortcuts with the shortcut registry's entries, the
// built-in commands and custom shortcuts of the chat TUI's "/" menu. Skills
// stay on list_skills.
type shortcutList struct {
	write    frameWriter
	registry *shortcuts.Registry
}

// list answers list_shortcuts with every registry entry sorted by name. A
// worker without a registry still answers with an empty list.
func (s *shortcutList) list() {
	frame := shortcutsFrame{Type: outboundShortcuts, Shortcuts: []shortcutSummary{}}
	if s.registry == nil {
		s.write(frame)
		return
	}
	for _, shortcut := range s.registry.GetAll() {
		frame.Shortcuts = append(frame.Shortcuts, shortcutSummary{
			Name:        shortcut.GetName(),
			Description: shortcut.GetDescription(),
			Usage:       shortcut.GetUsage(),
			Kind:        shortcuts.Kind(shortcut),
		})
	}
	s.write(frame)
}

// issues answers list_issues with the current project's open GitHub issues, the
// "#" menu the chat TUI's input offers. The service degrades to an empty list
// wherever gh, a git repo or auth are missing, so the panel mirrors the TUI.
type issues struct {
	write   frameWriter
	service agentdomain.GitHubIssueService
}

// issueListTimeout bounds the shell-out one list_issues answer waits on, the
// budget the TUI's "#" trigger already uses.
const issueListTimeout = 2 * time.Second

// list answers list_issues with the open issues the service serves - number,
// title and state, newest first. A failed list still answers with an empty one.
func (i *issues) list() {
	frame := issuesFrame{Type: outboundIssues, Issues: []issueSummary{}}
	if i.service == nil || !i.service.IsAvailable() {
		i.write(frame)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), issueListTimeout)
	defer cancel()
	loaded, err := i.service.ListIssues(ctx)
	if err != nil {
		logger.Debug("panel failed to list issues", "error", err)
		i.write(frame)
		return
	}
	for _, issue := range loaded {
		frame.Issues = append(frame.Issues, issueSummary{Number: issue.Number, Title: issue.Title, State: issue.State})
	}
	i.write(frame)
}

// projectFiles answers list_files with the files the file service offers for
// `@<path>` expansion, the "@" menu the chat TUI's input offers.
type projectFiles struct {
	write   frameWriter
	service agentdomain.FileService
}

// list answers list_files with the service's project files. A missing service
// or a failed walk still answers with an empty list.
func (f *projectFiles) list() {
	frame := filesFrame{Type: outboundFiles, Files: []string{}}
	if f.service == nil {
		f.write(frame)
		return
	}
	listed, err := f.service.ListProjectFiles()
	if err != nil {
		logger.Debug("panel failed to list project files", "error", err)
		f.write(frame)
		return
	}
	frame.Files = append(frame.Files, listed...)
	f.write(frame)
}

// modelPicker answers the panel's model picker from the worker's model service,
// listing the gateway's models with the configured default first.
type modelPicker struct {
	write        frameWriter
	service      convdomain.ModelService
	defaultModel string
}

// list answers list_models with the models the gateway serves, the configured
// default model first, so the panel's pickers mirror the CLI. The catalog
// carries their context windows, prices and modalities.
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
	frame := modelsFrame{Type: outboundModels, Models: out, Current: m.service.GetCurrentModel()}
	if catalog, ok := m.service.(modelCatalog); ok {
		frame.Catalog = catalog.Catalog()
	}
	m.write(frame)
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
	m.write(modeFrame{Type: outboundMode, Mode: m.state.GetAgentMode().ModeKey()})
}

// set switches the agent mode - it also governs tool_request approvals - and
// echoes the resulting mode so the panel reflects the outcome either way.
func (m *modes) set(mode string) {
	if parsed, ok := agentdomain.ParseAgentMode(mode); ok && parsed != agentdomain.AgentModeReadOnly {
		m.state.SetAgentMode(parsed)
	}
	m.send()
}
