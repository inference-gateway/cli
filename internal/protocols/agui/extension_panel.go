package agui

import (
	"context"
	"strings"

	websocket "github.com/gorilla/websocket"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// conversationListLimit caps list_conversations, mirroring the TUI selector.
const conversationListLimit = 50

// historyListLimit caps list_history replies. The panel only needs recent
// entries for arrow-up recall.
const historyListLimit = 1000

// conversationLister is the slice of the persistent repo the panel picker needs.
// Declared here (not in domain) because the in-memory fallback repo has no
// listing - the type assertion simply fails there and yields an empty list.
type conversationLister interface {
	ListSavedConversations(ctx context.Context, limit, offset int) ([]convdomain.ConversationSummary, error)
}

// conversations answers the panel's conversation picker from the conversation
// repository the CLI is running on.
type conversations struct {
	write frameWriter
	repo  convdomain.ConversationRepository
}

// snapshot ships the active conversation's history so the panel shows it, not
// just events from now on. Sent as the new_session / resume_conversation reply.
func (c *conversations) snapshot(conn *websocket.Conn) {
	entries := c.repo.GetMessages()
	messages := make([]sdk.Message, 0, len(entries))
	results := map[string]bool{}
	for _, entry := range entries {
		messages = append(messages, entry.Message)
		if entry.ToolExecution != nil && entry.Message.ToolCallID != nil {
			results[*entry.Message.ToolCallID] = entry.ToolExecution.Success
		}
	}
	c.write(conn, extSnapshot{Type: outboundConversationSnapshot, Messages: messages, ToolResults: results})
}

// list answers list_conversations with the stored conversations (newest-first),
// so the panel can offer the same picker the CLI resumes from.
func (c *conversations) list(conn *websocket.Conn) {
	lister, ok := c.repo.(conversationLister)
	if !ok {
		c.write(conn, extConversations{Type: outboundConversations})
		return
	}
	summaries, err := lister.ListSavedConversations(context.Background(), conversationListLimit, 0)
	if err != nil {
		logger.Debug("extension bridge failed to list conversations", "error", err)
		c.write(conn, extConversations{Type: outboundConversations})
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
	c.write(conn, extConversations{Type: outboundConversations, Conversations: out})
}

// start begins a fresh conversation synchronously in the read loop (mirroring
// the /clear shortcut's repo call) and snapshots the now-empty conversation.
// Handling it inline, not through the async notifier, guarantees a user_message
// frame sent right after lands in the new session instead of racing the clear.
func (c *conversations) start(conn *websocket.Conn) {
	if err := c.repo.StartNewConversation("New Conversation"); err != nil {
		logger.Debug("extension bridge failed to start new conversation", "error", err)
		return
	}
	c.snapshot(conn)
}

// resume switches the active conversation to id and snapshots it to the panel.
// The running chat mirror then streams live events into it.
func (c *conversations) resume(conn *websocket.Conn, id string) {
	if id == "" {
		return
	}
	if err := c.repo.LoadConversation(context.Background(), id); err != nil {
		logger.Debug("extension bridge failed to resume conversation", "id", id, "error", err)
		return
	}
	c.snapshot(conn)
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
func (h *history) list(conn *websocket.Conn) {
	loaded, err := h.store.LoadHistory(context.Background(), historyListLimit)
	if err != nil {
		logger.Debug("extension bridge failed to load history", "error", err)
		loaded = nil
	}
	if loaded == nil {
		loaded = []string{}
	}
	h.write(conn, extHistory{Type: outboundHistory, History: loaded})
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
		logger.Warn("extension bridge failed to append history", "error", err)
	}
}

// skills answers list_skills with the agent's discovered skills - the same set
// (project, .agents, user, plugin, catalog) the skills service already merged
// with precedence, so the panel's "/" menu mirrors what the TUI offers.
type skills struct {
	write   frameWriter
	service agentdomain.SkillsService
}

func (s *skills) list(conn *websocket.Conn) {
	loaded := s.service.List()
	out := make([]agentdomain.SkillSummary, 0, len(loaded))
	for _, skill := range loaded {
		out = append(out, skill.Summary())
	}
	s.write(conn, extSkills{Type: outboundSkills, Skills: out})
}

// modelPicker answers the panel's model picker from the CLI's model service,
// listing the gateway's models with the configured default first.
type modelPicker struct {
	write        frameWriter
	service      convdomain.ModelService
	defaultModel string
	notifier     agentdomain.UINotifier
}

// list answers list_models with the models the gateway serves, the CLI's
// configured default model first, so the panel's pickers mirror the CLI.
func (m *modelPicker) list(conn *websocket.Conn) {
	out := []string{}
	listed, err := m.service.ListModels(context.Background())
	if err != nil {
		logger.Debug("extension bridge failed to list models", "error", err)
	}
	if m.defaultModel != "" {
		out = append(out, m.defaultModel)
	}
	for _, name := range listed {
		if name != m.defaultModel {
			out = append(out, name)
		}
	}
	m.write(conn, extModels{Type: outboundModels, Models: out, Current: m.service.GetCurrentModel()})
}

// selectModel switches the CLI's active model (same as the TUI's /model) and
// re-sends the model list, so the panel reflects the outcome whether or not the
// switch was accepted.
func (m *modelPicker) selectModel(conn *websocket.Conn, name string) {
	if name != "" {
		if err := m.service.SelectModel(name); err != nil {
			logger.Debug("extension bridge failed to select model", "model", name, "error", err)
		} else {
			m.notifier.Notify(agentdomain.ModelSelectedEvent{Model: name})
		}
	}
	m.list(conn)
}

// modes mirrors the CLI's current agent mode (standard/plan/auto/auto-with-judge)
// to the panel's toggle, and applies set_mode to the same shared state the TUI's
// shift+tab cycle uses.
type modes struct {
	write frameWriter
	state agentdomain.AgentModeState
}

// send reports the CLI's current agent mode as its canonical mode key.
func (m *modes) send(conn *websocket.Conn) {
	m.write(conn, extMode{Type: outboundMode, Mode: m.state.GetAgentMode().ModeKey()})
}

// set switches the CLI's agent mode - it also governs tool_request approvals -
// and echoes the resulting mode so the panel reflects the outcome either way.
func (m *modes) set(conn *websocket.Conn, mode string) {
	if parsed, ok := agentdomain.ParseAgentMode(mode); ok && parsed != agentdomain.AgentModeReadOnly {
		m.state.SetAgentMode(parsed)
	}
	m.send(conn)
}
