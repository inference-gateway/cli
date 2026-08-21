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

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// Runner owns the LLM streaming lifecycle for a chat session.
//
// Clearing the orchestrator's "active tool call" indicator on chat
// start/error/complete is the orchestrator's responsibility, not the
// runner's - see the ChatHandler wrappers that call SetActiveToolCallID("")
// before delegating to these handlers.
// stateManager is the narrow slice of the app state manager the runner needs:
// chat-session lifecycle, tool-execution teardown, and the view transition on
// completion. *statemanager.StateManager satisfies it.
type stateManager interface {
	agentdomain.ChatSessionManager
	agentdomain.ToolExecutionManager
	tui.ViewManager
}

type Runner struct {
	agentService     agentdomain.AgentService
	conversationRepo convdomain.ConversationRepository
	modelService     convdomain.ModelService
	stateManager     stateManager
	listener         tui.ChatEventListener

	pendingRestoration   string
	pendingRestorationMu sync.RWMutex
}

// Options bundles the dependencies needed to construct a Runner.
type Options struct {
	AgentService     agentdomain.AgentService
	ConversationRepo convdomain.ConversationRepository
	ModelService     convdomain.ModelService
	StateManager     stateManager
	Listener         tui.ChatEventListener
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
func (r *Runner) Start(holder agentdomain.BashDetachChannelHolder) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		currentModel := r.modelService.GetCurrentModel()
		if currentModel == "" {
			return agentdomain.ChatErrorEvent{
				RequestID: "unknown",
				Timestamp: time.Now(),
				Error:     fmt.Errorf("no model selected"),
			}
		}

		entries := r.conversationRepo.GetMessages()
		messages := conversation.BuildAgentMessagesFromEntries(entries)

		requestID := generateRequestID()

		req := &agentdomain.AgentRequest{
			RequestID:  requestID,
			Model:      currentModel,
			Messages:   messages,
			IsChatMode: true,
		}

		ctx = agentdomain.WithChatHandler(ctx, holder)

		eventChan, err := r.agentService.RunWithStream(ctx, req)
		if err != nil {
			return agentdomain.ChatErrorEvent{
				RequestID: requestID,
				Timestamp: time.Now(),
				Error:     err,
			}
		}

		_ = r.stateManager.StartChatSession(requestID, currentModel, eventChan)

		return agentdomain.ChatStartEvent{
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
func (r *Runner) HandleChatStart(_ agentdomain.ChatStartEvent) tea.Cmd {
	_ = r.stateManager.UpdateChatStatus(agentdomain.ChatStatusStarting)

	cmds := []tea.Cmd{
		func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "Starting response...",
				Spinner:    true,
				StatusType: tui.StatusGenerating,
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
func (r *Runner) HandleChatChunk(msg agentdomain.ChatChunkEvent) tea.Cmd {
	chatSession := r.stateManager.GetChatSession()
	if chatSession == nil {
		return r.handleNoChatSession(msg)
	}

	if msg.Content == "" && msg.ReasoningContent == "" {
		return r.handleEmptyContent(chatSession)
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			return tui.StreamingContentEvent{
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
func (r *Runner) HandleChatComplete(msg agentdomain.ChatCompleteEvent) tea.Cmd {
	r.restorePendingModel()
	r.writeSubagentResultFile(msg)

	if msg.Cancelled {
		_ = r.stateManager.UpdateChatStatus(agentdomain.ChatStatusCancelled)
		r.stateManager.EndChatSession()
		r.stateManager.EndToolExecution()
	} else if len(msg.ToolCalls) == 0 {
		_ = r.stateManager.UpdateChatStatus(agentdomain.ChatStatusCompleted)
		r.stateManager.EndToolExecution()
	} else {
		_ = r.stateManager.UpdateChatStatus(agentdomain.ChatStatusWaitingTools)
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			history := r.conversationRepo.GetMessages()
			return tui.UpdateHistoryEvent{History: history}
		},
	}

	for _, toolCall := range msg.ToolCalls {
		tc := toolCall
		cmds = append(cmds, func() tea.Msg {
			return agentdomain.ToolCallPreviewEvent{
				RequestID:  msg.RequestID,
				Timestamp:  msg.Timestamp,
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				Arguments:  tc.Function.Arguments,
				Status:     agentdomain.ToolCallStreamStatusReady,
				IsComplete: false,
			}
		})
	}

	statusMessage := "Response complete"
	if msg.Cancelled {
		statusMessage = "User interrupted"
	}
	cmds = append(cmds, func() tea.Msg {
		return tui.SetStatusEvent{
			Message:    statusMessage,
			Spinner:    false,
			StatusType: tui.StatusDefault,
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
func (r *Runner) writeSubagentResultFile(msg agentdomain.ChatCompleteEvent) {
	path := os.Getenv(scheddomain.EnvSubagentResultFile)
	if path == "" || msg.Cancelled || len(msg.ToolCalls) > 0 {
		return
	}
	answer := lastAssistantText(r.conversationRepo.GetMessages())
	if answer == "" {
		return
	}
	r.writeSubagentResultFileAtomic(path, scheddomain.SubagentResultFile{FinalAssistant: answer, Success: true})
}

// writeSubagentResultFileError records a failed terminal turn for an interactive
// subagent so the parent harvests the error rather than falling back to the pane.
func (r *Runner) writeSubagentResultFileError(runErr error) {
	path := os.Getenv(scheddomain.EnvSubagentResultFile)
	if path == "" {
		return
	}
	rf := scheddomain.SubagentResultFile{
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
func (r *Runner) writeSubagentResultFileAtomic(path string, rf scheddomain.SubagentResultFile) {
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
func lastAssistantText(entries []convdomain.ConversationEntry) string {
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
func (r *Runner) HandleChatError(msg agentdomain.ChatErrorEvent) tea.Cmd {
	r.writeSubagentResultFileError(msg.Error)
	_ = r.stateManager.UpdateChatStatus(agentdomain.ChatStatusError)
	r.stateManager.EndChatSession()
	r.stateManager.EndToolExecution()

	_ = r.stateManager.TransitionToView(tui.ViewStateChat)

	errorMsg := fmt.Sprintf("Chat error: %v", msg.Error)
	if strings.Contains(msg.Error.Error(), "timed out") {
		errorMsg = fmt.Sprintf("⏰ %v\n\nSuggestions:\n• Try breaking your request into smaller parts\n• Check if the server is overloaded\n• Verify your network connection", msg.Error)
	}

	return func() tea.Msg {
		return tui.ShowErrorEvent{
			Error:  errorMsg,
			Sticky: true,
		}
	}
}

// HandleOptimizationStatus surfaces the "Optimizing conversation..." status
// transitions emitted by the conversation optimizer.
func (r *Runner) HandleOptimizationStatus(event agentdomain.OptimizationStatusEvent) tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			spinner := event.IsActive
			statusType := tui.StatusDefault
			if event.IsActive {
				statusType = tui.StatusProcessing
			}
			return tui.SetStatusEvent{
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

func (r *Runner) handleNoChatSession(msg agentdomain.ChatChunkEvent) tea.Cmd {
	if msg.ReasoningContent != "" {
		return func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: tui.StatusThinking,
			}
		}
	}
	return nil
}

func (r *Runner) handleEmptyContent(chatSession *agentdomain.ChatSession) tea.Cmd {
	if chatSession != nil && chatSession.EventChannel != nil {
		return r.listener.ListenForChatEvents(chatSession.EventChannel)
	}
	return nil
}

func (r *Runner) handleStatusUpdate(msg agentdomain.ChatChunkEvent, chatSession *agentdomain.ChatSession) []tea.Cmd {
	previousStatus := chatSession.Status
	newStatus, shouldUpdateStatus := determineNewStatus(msg, previousStatus, chatSession.IsFirstChunk)
	if !shouldUpdateStatus {
		return nil
	}

	_ = r.stateManager.UpdateChatStatus(newStatus)

	if chatSession.IsFirstChunk {
		chatSession.IsFirstChunk = false
		return firstChunkStatusCmd(newStatus)
	}

	if newStatus != previousStatus {
		return statusUpdateCmd(newStatus)
	}

	return nil
}

func determineNewStatus(msg agentdomain.ChatChunkEvent, currentStatus agentdomain.ChatStatus, _ bool) (agentdomain.ChatStatus, bool) {
	if msg.ReasoningContent != "" {
		return agentdomain.ChatStatusThinking, true
	}
	if msg.Content != "" {
		return agentdomain.ChatStatusGenerating, true
	}
	return currentStatus, false
}

func firstChunkStatusCmd(status agentdomain.ChatStatus) []tea.Cmd {
	switch status {
	case agentdomain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: tui.StatusThinking,
			}
		}}
	case agentdomain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "Generating response...",
				Spinner:    true,
				StatusType: tui.StatusGenerating,
			}
		}}
	}
	return nil
}

func statusUpdateCmd(status agentdomain.ChatStatus) []tea.Cmd {
	switch status {
	case agentdomain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return tui.UpdateStatusEvent{
				Message:    "Thinking...",
				StatusType: tui.StatusThinking,
			}
		}}
	case agentdomain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return tui.UpdateStatusEvent{
				Message:    "Generating response...",
				StatusType: tui.StatusGenerating,
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
	warningEntry := convdomain.ConversationEntry{
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

// generateRequestID produces a unique id for a chat request, matching the
// previous handler-local format.
func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}
