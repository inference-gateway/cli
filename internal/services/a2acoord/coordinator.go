package a2acoord

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	tools "github.com/inference-gateway/cli/internal/agent/tools"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// Service handles the UI side of A2A task lifecycle events.
type Service struct {
	conversationRepo     domain.ConversationRepository
	stateManager         domain.StateManager
	taskRetentionService domain.TaskRetentionService
	listener             domain.ChatEventListener
}

// Options bundles the dependencies needed to construct a Service.
type Options struct {
	ConversationRepo     domain.ConversationRepository
	StateManager         domain.StateManager
	TaskRetentionService domain.TaskRetentionService
	Listener             domain.ChatEventListener
}

// NewService creates a new A2A task coordinator.
func NewService(opts Options) *Service {
	return &Service{
		conversationRepo:     opts.ConversationRepo,
		stateManager:         opts.StateManager,
		taskRetentionService: opts.TaskRetentionService,
		listener:             opts.Listener,
	}
}

// HandleTaskSubmitted emits a working status indicating an A2A task was
// submitted to a remote agent and continues listening on the chat channel.
func (s *Service) HandleTaskSubmitted(msg domain.A2ATaskSubmittedEvent) tea.Cmd {
	return tea.Sequence(s.taskSubmittedCmds(msg)...)
}

// HandleTaskCompleted retains the terminal task, emits the formatted result
// as streaming content, refreshes history, and signals completion.
func (s *Service) HandleTaskCompleted(msg domain.A2ATaskCompletedEvent) tea.Cmd {
	return tea.Sequence(s.taskCompletedCmds(msg)...)
}

// HandleTaskFailed produces an error-content event with whatever result the
// agent returned plus the error message.
func (s *Service) HandleTaskFailed(msg domain.A2ATaskFailedEvent) tea.Cmd {
	return tea.Sequence(s.taskFailedCmds(msg)...)
}

// HandleTaskStatusUpdate emits a working-status update for an in-flight A2A
// task and keeps the listener pumping.
func (s *Service) HandleTaskStatusUpdate(msg domain.A2ATaskStatusUpdateEvent) tea.Cmd {
	return tea.Sequence(s.taskStatusUpdateCmds(msg)...)
}

// HandleTaskInputRequired surfaces a warning that the remote agent is waiting
// on user input before it can continue.
func (s *Service) HandleTaskInputRequired(msg domain.A2ATaskInputRequiredEvent) tea.Cmd {
	return tea.Sequence(s.taskInputRequiredCmds(msg)...)
}

// HandleToolCallExecuted reports that a tool was executed on the remote
// gateway as part of an A2A round-trip.
func (s *Service) HandleToolCallExecuted(msg domain.A2AToolCallExecutedEvent) tea.Cmd {
	return tea.Sequence(s.toolCallExecutedCmds(msg)...)
}

// taskSubmittedCmds builds the ordered list of tea.Cmds for a task-submitted
// event without sequencing. Exposed (unexported) for direct testability of
// individual cmd outputs.
func (s *Service) taskSubmittedCmds(msg domain.A2ATaskSubmittedEvent) []tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("A2A task submitted to %s", msg.AgentName),
				Spinner:    true,
				StatusType: domain.StatusWorking,
			}
		},
	}
	return s.appendChatListenerCmd(cmds)
}

func (s *Service) taskCompletedCmds(msg domain.A2ATaskCompletedEvent) []tea.Cmd {
	taskResult := s.retainTaskFromResult(&msg.Result)
	if taskResult == "" {
		taskResult = s.conversationRepo.FormatToolResultForLLM(&msg.Result)
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.StreamingContentEvent{
				RequestID: msg.RequestID,
				Content:   taskResult,
				Delta:     false,
			}
		},
		func() tea.Msg {
			history := s.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "A2A task completed",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	}
	return s.appendChatListenerCmd(cmds)
}

func (s *Service) taskFailedCmds(msg domain.A2ATaskFailedEvent) []tea.Cmd {
	taskResult := s.retainTaskFromResult(&msg.Result)

	var errorContent string
	if taskResult != "" {
		errorContent = fmt.Sprintf("[A2A Task Failed]\n\n%s", taskResult)
	} else {
		formattedResult := s.conversationRepo.FormatToolResultForLLM(&msg.Result)
		errorContent = fmt.Sprintf("[A2A Task Failed]\n\nError: %s\n\n%s", msg.Error, formattedResult)
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.StreamingContentEvent{
				RequestID: msg.RequestID,
				Content:   errorContent,
				Delta:     false,
			}
		},
		func() tea.Msg {
			history := s.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("A2A task failed: %s", msg.Error),
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	}
	return s.appendChatListenerCmd(cmds)
}

func (s *Service) taskStatusUpdateCmds(msg domain.A2ATaskStatusUpdateEvent) []tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    fmt.Sprintf("A2A task %s: %s", msg.Status, msg.Message),
				StatusType: domain.StatusWorking,
			}
		},
	}
	return s.appendChatListenerCmd(cmds)
}

func (s *Service) taskInputRequiredCmds(msg domain.A2ATaskInputRequiredEvent) []tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("⚠️  A2A task requires input: %s", msg.Message),
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	}
	return s.appendChatListenerCmd(cmds)
}

func (s *Service) toolCallExecutedCmds(msg domain.A2AToolCallExecutedEvent) []tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("A2A tool %s executed on gateway", msg.ToolName),
				Spinner:    true,
				StatusType: domain.StatusWorking,
			}
		},
	}
	return s.appendChatListenerCmd(cmds)
}

// retainTaskFromResult pulls the submitted A2A task out of a tool result, hands
// it to the retention service when present, and returns the formatted task
// result body (empty string if the result was not an A2A submit result).
func (s *Service) retainTaskFromResult(result *domain.ToolExecutionResult) string {
	if result.Data == nil {
		return ""
	}

	submitResult, ok := result.Data.(tools.A2ASubmitTaskResult)
	if !ok {
		return ""
	}

	if submitResult.Task != nil && s.taskRetentionService != nil {
		s.taskRetentionService.AddTask(domain.TaskInfo{
			Task:        *submitResult.Task,
			AgentURL:    submitResult.AgentURL,
			StartedAt:   time.Now().Add(-result.Duration),
			CompletedAt: time.Now(),
		})
	}

	return submitResult.TaskResult
}

// appendChatListenerCmd adds the chat event channel listener to cmds when a
// chat session is active. Returns the (possibly unchanged) cmds slice.
func (s *Service) appendChatListenerCmd(cmds []tea.Cmd) []tea.Cmd {
	chatSession := s.stateManager.GetChatSession()
	if chatSession == nil || chatSession.EventChannel == nil {
		return cmds
	}
	return append(cmds, s.listener.ListenForChatEvents(chatSession.EventChannel))
}
