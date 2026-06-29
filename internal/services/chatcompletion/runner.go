package chatcompletion

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// Runner owns the LLM streaming lifecycle for a chat session.
//
// Clearing the orchestrator's "active tool call" indicator on chat
// start/error/complete is the orchestrator's responsibility, not the
// runner's - see the ChatHandler wrappers that call SetActiveToolCallID("")
// before delegating to these handlers.
type Runner struct {
	agentService     domain.AgentService
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	stateManager     domain.StateManager
	listener         domain.ChatEventListener

	pendingRestoration   string
	pendingRestorationMu sync.RWMutex
}

// Options bundles the dependencies needed to construct a Runner.
type Options struct {
	AgentService     domain.AgentService
	ConversationRepo domain.ConversationRepository
	ModelService     domain.ModelService
	StateManager     domain.StateManager
	Listener         domain.ChatEventListener
}

// NewRunner creates a new ChatCompletionRunner.
func NewRunner(opts Options) *Runner {
	return &Runner{
		agentService:     opts.AgentService,
		conversationRepo: opts.ConversationRepo,
		modelService:     opts.ModelService,
		stateManager:     opts.StateManager,
		listener:         opts.Listener,
	}
}

// Start kicks off a streaming chat completion. The returned tea.Cmd performs
// the request (synchronously in the returned closure) and emits a
// ChatStartEvent on success or ChatErrorEvent on failure. The holder is
// attached to the request context so the agent core can find the
// BashDetachChannelHolder when launching tools that may need backgrounding.
func (r *Runner) Start(holder domain.BashDetachChannelHolder) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		currentModel := r.modelService.GetCurrentModel()
		if currentModel == "" {
			return domain.ChatErrorEvent{
				RequestID: "unknown",
				Timestamp: time.Now(),
				Error:     fmt.Errorf("no model selected"),
			}
		}

		entries := r.conversationRepo.GetMessages()
		messages := BuildAgentMessagesFromEntries(entries)

		requestID := generateRequestID()

		req := &domain.AgentRequest{
			RequestID:  requestID,
			Model:      currentModel,
			Messages:   messages,
			IsChatMode: true,
		}

		ctx = domain.WithChatHandler(ctx, holder)

		eventChan, err := r.agentService.RunWithStream(ctx, req)
		if err != nil {
			return domain.ChatErrorEvent{
				RequestID: requestID,
				Timestamp: time.Now(),
				Error:     err,
			}
		}

		_ = r.stateManager.StartChatSession(requestID, currentModel, eventChan)

		return domain.ChatStartEvent{
			RequestID: requestID,
			Model:     currentModel,
			Timestamp: time.Now(),
		}
	}
}

// SetPendingRestoration records that a temporary /model switch is in effect
// and that originalModel should be restored once the next completion ends.
func (r *Runner) SetPendingRestoration(originalModel string) {
	r.pendingRestorationMu.Lock()
	defer r.pendingRestorationMu.Unlock()
	r.pendingRestoration = originalModel
}

// HandleChatStart transitions chat status to Starting and emits the initial
// "Starting response..." status. Clearing the orchestrator's active-tool
// indicator is the orchestrator's responsibility (see ChatHandler wrapper).
func (r *Runner) HandleChatStart(_ domain.ChatStartEvent) tea.Cmd {
	_ = r.stateManager.UpdateChatStatus(domain.ChatStatusStarting)

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Starting response...",
				Spinner:    true,
				StatusType: domain.StatusGenerating,
			}
		},
	}

	if chatSession := r.stateManager.GetChatSession(); chatSession != nil {
		cmds = append(cmds, r.listener.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

// HandleChatChunk forwards a streaming content delta to the UI and adjusts
// chat status if the chunk indicates a thinking → generating transition (or
// vice versa).
func (r *Runner) HandleChatChunk(msg domain.ChatChunkEvent) tea.Cmd {
	chatSession := r.stateManager.GetChatSession()
	if chatSession == nil {
		return r.handleNoChatSession(msg)
	}

	if msg.Content == "" && msg.ReasoningContent == "" {
		return r.handleEmptyContent(chatSession)
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.StreamingContentEvent{
				RequestID:        msg.RequestID,
				Content:          msg.Content,
				ReasoningContent: msg.ReasoningContent,
				Delta:            true,
				Model:            chatSession.Model,
			}
		},
	}

	statusCmds := r.handleStatusUpdate(msg, chatSession)
	cmds = append(cmds, statusCmds...)

	if cs := r.stateManager.GetChatSession(); cs != nil && cs.EventChannel != nil {
		cmds = append(cmds, r.listener.ListenForChatEvents(cs.EventChannel))
	}

	return tea.Sequence(cmds...)
}

// HandleChatComplete restores the pending model (if any), updates chat
// status, refreshes history, emits tool-call previews, and signals completion.
func (r *Runner) HandleChatComplete(msg domain.ChatCompleteEvent) tea.Cmd {
	r.restorePendingModel()
	r.writeSubagentResultFile(msg)

	if msg.Cancelled {
		_ = r.stateManager.UpdateChatStatus(domain.ChatStatusCancelled)
		r.stateManager.EndChatSession()
		r.stateManager.EndToolExecution()
	} else if len(msg.ToolCalls) == 0 {
		_ = r.stateManager.UpdateChatStatus(domain.ChatStatusCompleted)
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			history := r.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{History: history}
		},
	}

	for _, toolCall := range msg.ToolCalls {
		tc := toolCall
		cmds = append(cmds, func() tea.Msg {
			return domain.ToolCallPreviewEvent{
				RequestID:  msg.RequestID,
				Timestamp:  msg.Timestamp,
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Arguments:  tc.Function.Arguments,
				Status:     domain.ToolCallStreamStatusReady,
				IsComplete: false,
			}
		})
	}

	statusMessage := "Response complete"
	if msg.Cancelled {
		statusMessage = "User interrupted"
	}
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMessage,
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession := r.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, r.listener.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

// writeSubagentResultFile lets an interactive subagent's `infer chat` hand its
// last assistant message back to the parent Agent tool. When launched as an
// interactive subagent the parent sets INFER_SUBAGENT_RESULT_FILE; on each fully
// completed turn (a final answer, no pending tool calls) we write the last
// assistant message as a SubagentResultFile so the parent delivers the real
// answer instead of scraping the tmux pane's chrome. A no-op for a normal chat.
//
// The message comes from the conversation, not the event: ChatCompleteEvent.Message
// is not populated (see publishChatComplete), but the assistant turn is already in
// the repo by the time this runs (streaming appends it before emitting the event).
func (r *Runner) writeSubagentResultFile(msg domain.ChatCompleteEvent) {
	path := os.Getenv(domain.EnvSubagentResultFile)
	if path == "" || msg.Cancelled || len(msg.ToolCalls) > 0 {
		return
	}
	answer := lastAssistantText(r.conversationRepo.GetMessages())
	if answer == "" {
		return
	}
	r.writeSubagentResultFileAtomic(path, domain.SubagentResultFile{FinalAssistant: answer, Success: true})
}

// writeSubagentResultFileError records a failed terminal turn for an interactive
// subagent so the parent harvests the error rather than falling back to the pane.
func (r *Runner) writeSubagentResultFileError(runErr error) {
	path := os.Getenv(domain.EnvSubagentResultFile)
	if path == "" {
		return
	}
	rf := domain.SubagentResultFile{
		FinalAssistant: lastAssistantText(r.conversationRepo.GetMessages()), // partial text, may be ""
		Success:        false,
	}
	if runErr != nil {
		rf.Error = runErr.Error()
	}
	r.writeSubagentResultFileAtomic(path, rf)
}

// writeSubagentResultFileAtomic marshals rf and writes it to path via a temp file
// and rename, so a polling parent never reads a half-written file.
func (r *Runner) writeSubagentResultFileAtomic(path string, rf domain.SubagentResultFile) {
	data, err := json.Marshal(rf)
	if err != nil {
		logger.Warn("subagent result file: marshal failed", "error", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logger.Warn("subagent result file: write failed", "error", err, "path", path)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		logger.Warn("subagent result file: rename failed", "error", err, "path", path)
	}
}

// lastAssistantText returns the content of the last non-empty assistant message
// in entries (backward scan), or "" if none. The interactive analogue of the
// headless lastAssistantBefore (cmd/agent.go).
func lastAssistantText(entries []domain.ConversationEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Message.Role != sdk.Assistant {
			continue
		}
		text, err := e.Message.Content.AsMessageContent0()
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(text); s != "" {
			return s
		}
	}
	return ""
}

// HandleChatError tears down session state and emits a sticky error event
// (with a friendlier message for "timed out" errors).
func (r *Runner) HandleChatError(msg domain.ChatErrorEvent) tea.Cmd {
	r.writeSubagentResultFileError(msg.Error)
	_ = r.stateManager.UpdateChatStatus(domain.ChatStatusError)
	r.stateManager.EndChatSession()
	r.stateManager.EndToolExecution()

	_ = r.stateManager.TransitionToView(domain.ViewStateChat)

	errorMsg := fmt.Sprintf("Chat error: %v", msg.Error)
	if strings.Contains(msg.Error.Error(), "timed out") {
		errorMsg = fmt.Sprintf("⏰ %v\n\nSuggestions:\n• Try breaking your request into smaller parts\n• Check if the server is overloaded\n• Verify your network connection", msg.Error)
	}

	return func() tea.Msg {
		return domain.ShowErrorEvent{
			Error:  errorMsg,
			Sticky: true,
		}
	}
}

// HandleOptimizationStatus surfaces the "Optimizing conversation..." status
// transitions emitted by the conversation optimizer.
func (r *Runner) HandleOptimizationStatus(event domain.OptimizationStatusEvent) tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			spinner := event.IsActive
			statusType := domain.StatusDefault
			if event.IsActive {
				statusType = domain.StatusProcessing
			}
			return domain.SetStatusEvent{
				Message:    event.Message,
				Spinner:    spinner,
				StatusType: statusType,
			}
		},
	}

	if chatSession := r.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, r.listener.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

func (r *Runner) handleNoChatSession(msg domain.ChatChunkEvent) tea.Cmd {
	if msg.ReasoningContent != "" {
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: domain.StatusThinking,
			}
		}
	}
	return nil
}

func (r *Runner) handleEmptyContent(chatSession *domain.ChatSession) tea.Cmd {
	if chatSession != nil && chatSession.EventChannel != nil {
		return r.listener.ListenForChatEvents(chatSession.EventChannel)
	}
	return nil
}

func (r *Runner) handleStatusUpdate(msg domain.ChatChunkEvent, chatSession *domain.ChatSession) []tea.Cmd {
	newStatus, shouldUpdateStatus := determineNewStatus(msg, chatSession.Status, chatSession.IsFirstChunk)
	if !shouldUpdateStatus {
		return nil
	}

	_ = r.stateManager.UpdateChatStatus(newStatus)

	if chatSession.IsFirstChunk {
		chatSession.IsFirstChunk = false
		return firstChunkStatusCmd(newStatus)
	}

	if newStatus != chatSession.Status {
		return statusUpdateCmd(newStatus)
	}

	return nil
}

func determineNewStatus(msg domain.ChatChunkEvent, currentStatus domain.ChatStatus, _ bool) (domain.ChatStatus, bool) {
	if msg.ReasoningContent != "" {
		return domain.ChatStatusThinking, true
	}
	if msg.Content != "" {
		return domain.ChatStatusGenerating, true
	}
	return currentStatus, false
}

func firstChunkStatusCmd(status domain.ChatStatus) []tea.Cmd {
	switch status {
	case domain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: domain.StatusThinking,
			}
		}}
	case domain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Generating response...",
				Spinner:    true,
				StatusType: domain.StatusGenerating,
			}
		}}
	}
	return nil
}

func statusUpdateCmd(status domain.ChatStatus) []tea.Cmd {
	switch status {
	case domain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    "Thinking...",
				StatusType: domain.StatusThinking,
			}
		}}
	case domain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    "Generating response...",
				StatusType: domain.StatusGenerating,
			}
		}}
	}
	return nil
}

// restorePendingModel reverts a temporary /model switch (set via
// SetPendingRestoration) once the assistant turn has finished. Adds a
// visible warning entry to the conversation when restoration fails.
func (r *Runner) restorePendingModel() {
	r.pendingRestorationMu.Lock()
	if r.pendingRestoration == "" {
		r.pendingRestorationMu.Unlock()
		return
	}
	originalModel := r.pendingRestoration
	r.pendingRestoration = ""
	r.pendingRestorationMu.Unlock()

	if err := r.modelService.SelectModel(originalModel); err != nil {
		logger.Error("failed to restore original model", "model", originalModel, "error", err)
		r.addModelRestorationWarning(originalModel)
		return
	}
	logger.Debug("successfully restored original model", "model", originalModel)
}

func (r *Runner) addModelRestorationWarning(originalModel string) {
	warningEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: sdk.NewMessageContent(fmt.Sprintf("[Warning: Failed to restore model to %s]", originalModel)),
		},
		Time: time.Now(),
	}
	if err := r.conversationRepo.AddMessage(warningEntry); err != nil {
		logger.Error("failed to add model restoration warning message", "error", err)
	}
}

// BuildAgentMessagesFromEntries converts conversation entries into the flat
// slice of SDK messages sent to the model.
//
// Two classes of entries are filtered:
//
//  1. Plan-mode entries (entry.IsPlan): synthesized assistant messages used
//     for UI rendering only; their content duplicates the args of the
//     preceding RequestPlanApproval tool call.
//  2. User-initiated bash entries: synthetic assistant + tool pairs created
//     when the user types `!command` directly in chat. Their assistant
//     side has tool_calls but no reasoning_content (the user, not the
//     model, generated them).
//
// Sending either to a thinking-mode provider (DeepSeek, etc.) produces an
// assistant turn lacking `reasoning_content`, which is rejected with HTTP
// 400 ("The reasoning_content in the thinking mode must be passed back to
// the API.").
func BuildAgentMessagesFromEntries(entries []domain.ConversationEntry) []sdk.Message {
	messages := make([]sdk.Message, 0, len(entries))
	for _, entry := range entries {
		if entry.IsPlan {
			continue
		}
		if isUserInitiatedBashEntry(entry) {
			continue
		}
		msg := entry.Message
		if entry.ReasoningContent != "" && msg.Reasoning == nil && msg.ReasoningContent == nil {
			rc := entry.ReasoningContent
			msg.Reasoning = &rc
			msg.ReasoningContent = &rc
		}
		messages = append(messages, msg)
	}
	return messages
}

// isUserInitiatedBashEntry reports whether the entry was synthesized for a
// user-typed `!command` shortcut. Tool-call IDs created by that path are
// prefixed with `user-bash-` (see DirectExecutionService).
func isUserInitiatedBashEntry(entry domain.ConversationEntry) bool {
	const userBashPrefix = "user-bash-"

	if entry.Message.ToolCallID != nil && strings.HasPrefix(*entry.Message.ToolCallID, userBashPrefix) {
		return true
	}

	if entry.Message.ToolCalls != nil {
		for _, tc := range *entry.Message.ToolCalls {
			if strings.HasPrefix(tc.ID, userBashPrefix) {
				return true
			}
		}
	}
	return false
}

// generateRequestID produces a unique id for a chat request, matching the
// previous handler-local format.
func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}
