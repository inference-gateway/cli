// Package toolcoordinator owns the UI side of the tool round-trip - the
// streaming-status events emitted while the model is producing a tool call,
// the approval handshake that forwards the user's accept/reject back to the
// agent, and the execution-progress events while the tool runs.
package toolcoordinator

import (
	"fmt"
	"sync"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// toolApprovalRepoUpdater is the narrow interface the coordinator uses to
// mutate tool-approval state on the conversation repo. Both
// *services.InMemoryConversationRepository and
// *services.PersistentConversationRepository satisfy it (the latter via
// embedding).
type toolApprovalRepoUpdater interface {
	UpdateToolApprovalStatus(action domain.ApprovalAction)
	AddPendingToolCall(toolCall sdk.ChatCompletionMessageToolCall, responseChan chan domain.ApprovalAction) error
}

// Coordinator handles the tool round-trip UI flow.
type Coordinator struct {
	conversationRepo domain.ConversationRepository
	stateManager     domain.StateManager
	directExec       domain.DirectExecutionService
	listener         domain.ChatEventListener

	activeToolCallID   string
	activeToolCallIDMu sync.RWMutex
}

// Options bundles the dependencies needed to construct a Coordinator.
type Options struct {
	ConversationRepo domain.ConversationRepository
	StateManager     domain.StateManager
	DirectExec       domain.DirectExecutionService
	Listener         domain.ChatEventListener
}

// NewCoordinator creates a new tool execution coordinator.
func NewCoordinator(opts Options) *Coordinator {
	return &Coordinator{
		conversationRepo: opts.ConversationRepo,
		stateManager:     opts.StateManager,
		directExec:       opts.DirectExec,
		listener:         opts.Listener,
	}
}

// GetActiveToolCallID returns the currently active tool call id (thread-safe).
func (c *Coordinator) GetActiveToolCallID() string {
	c.activeToolCallIDMu.RLock()
	defer c.activeToolCallIDMu.RUnlock()
	return c.activeToolCallID
}

// SetActiveToolCallID sets the currently active tool call id (thread-safe).
func (c *Coordinator) SetActiveToolCallID(id string) {
	c.activeToolCallIDMu.Lock()
	defer c.activeToolCallIDMu.Unlock()
	c.activeToolCallID = id
}

// HandleToolCallUpdate emits a per-tool-call status update during streaming
// (e.g. "Streaming Read..." → "Completed Read") and keeps the chat listener
// pumping.
func (c *Coordinator) HandleToolCallUpdate(msg domain.ToolCallUpdateEvent) tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			history := c.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
	}

	statusMsg := formatToolCallStatusMessage(msg.ToolName, msg.Status)

	switch msg.Status {
	case domain.ToolCallStreamStatusStreaming:
		cmds = append(cmds, func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    statusMsg,
				StatusType: domain.StatusWorking,
				ToolName:   msg.ToolName,
			}
		})
	default:
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMsg,
				Spinner:    false,
				StatusType: domain.StatusWorking,
				ToolName:   msg.ToolName,
			}
		})
	}

	cmds = c.appendChatListener(cmds)
	return tea.Sequence(cmds...)
}

// HandleToolCallReady refreshes history when a tool call has finished
// streaming and is ready for the next step.
func (c *Coordinator) HandleToolCallReady(_ domain.ToolCallReadyEvent) tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			history := c.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
	}
	cmds = c.appendChatListener(cmds)
	return tea.Sequence(cmds...)
}

// HandleToolApprovalRequested records the pending tool call in the repo,
// sets up the approval UI state, broadcasts a notification, and keeps the
// chat listener pumping while the user decides.
func (c *Coordinator) HandleToolApprovalRequested(msg domain.ToolApprovalRequestedEvent) tea.Cmd {
	c.addPendingToolCall(msg.ToolCall, msg.ResponseChan)
	c.stateManager.SetupApprovalUIState(&msg.ToolCall, msg.ResponseChan)

	c.stateManager.BroadcastEvent(domain.ToolApprovalNotificationEvent{
		RequestID: msg.RequestID,
		Timestamp: msg.Timestamp,
		ToolName:  msg.ToolCall.Function.Name,
		Message:   "Tool approval required - Check terminal for approval",
	})

	cmds := []tea.Cmd{
		func() tea.Msg {
			history := c.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
	}
	cmds = c.appendChatListener(cmds)
	return tea.Sequence(cmds...)
}

// HandleToolApprovalResponse processes the user's accept/reject decision and
// forwards it to the waiting agent through the response channel.
func (c *Coordinator) HandleToolApprovalResponse(msg domain.ToolApprovalResponseEvent) tea.Cmd {
	logger.Info("Coordinator.HandleToolApprovalResponse called",
		"action", msg.Action, "tool", msg.ToolCall.Function.Name)

	c.updateToolApprovalStatus(msg.Action)

	if msg.Action == domain.ApprovalAutoAccept {
		return c.applyAutoAccept(msg)
	}

	c.sendApprovalDecision(msg.Action)
	c.stateManager.ClearApprovalUIState()

	statusMessage, spinner := c.formatApprovalStatus(msg)

	cmds := []tea.Cmd{
		func() tea.Msg {
			history := c.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMessage,
				Spinner:    spinner,
				StatusType: domain.StatusDefault,
				ToolName:   msg.ToolCall.Function.Name,
			}
		},
	}
	cmds = c.appendChatListener(cmds)
	return tea.Batch(cmds...)
}

// applyAutoAccept flips agent mode to auto-accept, sends approval down the
// response channel, and returns the appropriate UI cmds.
func (c *Coordinator) applyAutoAccept(msg domain.ToolApprovalResponseEvent) tea.Cmd {
	logger.Info("Switching to auto-accept mode for all future tools")
	c.stateManager.SetAgentMode(domain.AgentModeAutoAccept)
	c.sendApprovalDecision(domain.ApprovalApprove)
	c.stateManager.ClearApprovalUIState()

	cmds := []tea.Cmd{
		func() tea.Msg {
			history := c.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Auto-Approve mode enabled - executing tool...",
				Spinner:    true,
				StatusType: domain.StatusDefault,
				ToolName:   msg.ToolCall.Function.Name,
			}
		},
	}
	cmds = c.appendChatListener(cmds)
	return tea.Batch(cmds...)
}

// sendApprovalDecision forwards the user's decision to the agent via the
// response channel held on the approval UI state. Non-blocking; logs a
// warning if the channel is full or closed.
func (c *Coordinator) sendApprovalDecision(action domain.ApprovalAction) {
	approvalState := c.stateManager.GetApprovalUIState()
	if approvalState == nil || approvalState.ResponseChan == nil {
		return
	}
	select {
	case approvalState.ResponseChan <- action:
		logger.Info("Sent approval action to agent", "action", action)
	default:
		logger.Warn("Failed to send approval - channel full or closed")
	}
}

func (c *Coordinator) formatApprovalStatus(msg domain.ToolApprovalResponseEvent) (string, bool) {
	switch msg.Action {
	case domain.ApprovalApprove:
		return fmt.Sprintf("Tool approved - executing %s...", msg.ToolCall.Function.Name), true
	case domain.ApprovalReject:
		return fmt.Sprintf("Tool rejected: %s", msg.ToolCall.Function.Name), false
	}
	return "", false
}

// HandleToolExecutionStarted emits the initial "Starting tool execution"
// status.
func (c *Coordinator) HandleToolExecutionStarted(msg domain.ToolExecutionStartedEvent) tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Starting tool execution (%d tools)", msg.TotalTools),
				Spinner:    true,
				StatusType: domain.StatusWorking,
			}
		},
	}
	cmds = c.appendChatListener(cmds)
	return tea.Sequence(cmds...)
}

// HandleToolExecutionProgress translates a tool-execution progress event into
// the appropriate UI status updates and keeps the right event channel
// pumping (bash, tool, or chat).
func (c *Coordinator) HandleToolExecutionProgress(msg domain.ToolExecutionProgressEvent) tea.Cmd {
	cmds := c.progressStatusCmds(msg)

	if toolEventChan := c.directExec.PendingToolChannel(); toolEventChan != nil {
		cmds = append(cmds, c.listener.ListenForEvents(toolEventChan))
		return tea.Sequence(cmds...)
	}

	if bashEventChan := c.directExec.PendingBashChannel(); bashEventChan != nil {
		cmds = append(cmds, c.listener.ListenForEvents(bashEventChan))
		return tea.Sequence(cmds...)
	}

	cmds = c.appendChatListener(cmds)

	if len(cmds) > 0 {
		return tea.Sequence(cmds...)
	}
	return nil
}

// progressStatusCmds returns the per-status UI commands and updates the
// active-tool-call indicator as the tool moves through its lifecycle.
func (c *Coordinator) progressStatusCmds(msg domain.ToolExecutionProgressEvent) []tea.Cmd {
	switch msg.Status {
	case "starting":
		c.SetActiveToolCallID(msg.ToolCallID)
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    msg.Message,
				Spinner:    true,
				StatusType: domain.StatusWorking,
				ToolName:   msg.ToolName,
			}
		}}
	case "running":
		return c.runningProgressCmds(msg)
	case "completed", "failed":
		c.SetActiveToolCallID("")
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    msg.Message,
				Spinner:    false,
				StatusType: domain.StatusDefault,
				ToolName:   "",
			}
		}}
	case "saving":
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    msg.Message,
				Spinner:    true,
				StatusType: domain.StatusDefault,
				ToolName:   "",
			}
		}}
	}
	return nil
}

func (c *Coordinator) runningProgressCmds(msg domain.ToolExecutionProgressEvent) []tea.Cmd {
	if msg.Message == "" {
		c.SetActiveToolCallID(msg.ToolCallID)
		return nil
	}

	if c.GetActiveToolCallID() == msg.ToolCallID {
		return []tea.Cmd{func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    msg.Message,
				StatusType: domain.StatusWorking,
				ToolName:   msg.ToolName,
			}
		}}
	}

	c.SetActiveToolCallID(msg.ToolCallID)
	return []tea.Cmd{func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    msg.Message,
			Spinner:    true,
			StatusType: domain.StatusWorking,
			ToolName:   msg.ToolName,
		}
	}}
}

// HandleToolExecutionCompleted finalizes the tool round-trip: clears the
// active-tool indicator, refreshes history, optionally fires a todo-update
// command, and emits a "Tools completed" status.
func (c *Coordinator) HandleToolExecutionCompleted(msg domain.ToolExecutionCompletedEvent) tea.Cmd {
	c.SetActiveToolCallID("")

	cmds := []tea.Cmd{
		func() tea.Msg {
			history := c.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message: fmt.Sprintf("Tools completed (%d/%d successful) - preparing response...",
					msg.SuccessCount, msg.TotalExecuted),
				Spinner:    true,
				StatusType: domain.StatusPreparing,
			}
		},
	}

	if todoUpdateCmd := extractTodoUpdateCmd(msg.Results); todoUpdateCmd != nil {
		cmds = append(cmds, todoUpdateCmd)
	}

	cmds = c.appendChatListener(cmds)
	return tea.Sequence(cmds...)
}

// HandleToolCancelled refreshes the conversation view so the synthetic
// [cancelled] tool entry that the integrity validator persisted becomes
// visible. No status-bar message - the cancel that triggered this already
// drove its own status ("User interrupted").
func (c *Coordinator) HandleToolCancelled(_ domain.ToolCancelledEvent) tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			history := c.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
	}
	cmds = c.appendChatListener(cmds)
	return tea.Sequence(cmds...)
}

func (c *Coordinator) appendChatListener(cmds []tea.Cmd) []tea.Cmd {
	chatSession := c.stateManager.GetChatSession()
	if chatSession == nil || chatSession.EventChannel == nil {
		return cmds
	}
	return append(cmds, c.listener.ListenForChatEvents(chatSession.EventChannel))
}

// addPendingToolCall stores the pending tool call + response channel on the
// conversation repo. Falls back silently if the repo doesn't implement the
// concrete updater interface.
func (c *Coordinator) addPendingToolCall(call sdk.ChatCompletionMessageToolCall, ch chan domain.ApprovalAction) {
	updater, ok := c.conversationRepo.(toolApprovalRepoUpdater)
	if !ok {
		return
	}
	if err := updater.AddPendingToolCall(call, ch); err != nil {
		logger.Error("Failed to add pending tool call", "error", err)
	}
}

// updateToolApprovalStatus mutates the most recent pending tool entry on the
// conversation repo.
func (c *Coordinator) updateToolApprovalStatus(action domain.ApprovalAction) {
	updater, ok := c.conversationRepo.(toolApprovalRepoUpdater)
	if !ok {
		return
	}
	logger.Info("Updating tool approval status")
	updater.UpdateToolApprovalStatus(action)
}

func formatToolCallStatusMessage(toolName string, status domain.ToolCallStreamStatus) string {
	switch status {
	case domain.ToolCallStreamStatusStreaming:
		return fmt.Sprintf("Streaming %s...", toolName)
	case domain.ToolCallStreamStatusComplete:
		return fmt.Sprintf("Completed %s", toolName)
	default:
		return ""
	}
}

// extractTodoUpdateCmd checks tool results for TodoWrite and returns a
// command to update todos.
func extractTodoUpdateCmd(results []*domain.ToolExecutionResult) tea.Cmd {
	for _, result := range results {
		if result == nil || result.ToolName != "TodoWrite" || !result.Success {
			continue
		}

		todoResult, ok := result.Data.(*domain.TodoWriteToolResult)
		if !ok || todoResult == nil {
			continue
		}

		todos := todoResult.Todos
		return func() tea.Msg {
			return domain.TodoUpdateEvent{
				Todos: todos,
			}
		}
	}
	return nil
}
