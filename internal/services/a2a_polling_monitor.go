package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	adk "github.com/inference-gateway/adk/types"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

type A2APollingMonitor struct {
	taskTracker      domain.TaskTracker
	eventChan        chan<- domain.ChatEvent
	messageQueue     domain.MessageQueue
	requestID        string
	conversationRepo domain.ConversationRepository
	mu               sync.RWMutex
	activeMonitors   map[string]context.CancelFunc
	stopChan         chan struct{}
	stopped          bool
}

func NewA2APollingMonitor(
	taskTracker domain.TaskTracker,
	eventChan chan<- domain.ChatEvent,
	messageQueue domain.MessageQueue,
	requestID string,
	conversationRepo domain.ConversationRepository,
) *A2APollingMonitor {
	return &A2APollingMonitor{
		taskTracker:      taskTracker,
		eventChan:        eventChan,
		messageQueue:     messageQueue,
		requestID:        requestID,
		conversationRepo: conversationRepo,
		activeMonitors:   make(map[string]context.CancelFunc),
		stopChan:         make(chan struct{}),
		stopped:          false,
	}
}

func (m *A2APollingMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.stopAllMonitors()
			return
		case <-m.stopChan:
			m.stopAllMonitors()
			return
		case <-ticker.C:
			m.checkForNewPollingTasks(ctx)
		}
	}
}

func (m *A2APollingMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.stopped {
		m.stopped = true
		close(m.stopChan)
	}
}

func (m *A2APollingMonitor) checkForNewPollingTasks(ctx context.Context) {
	if m.taskTracker == nil {
		return
	}

	pollingTasks := m.taskTracker.GetAllPollingTasks()

	for _, taskID := range pollingTasks {
		m.mu.RLock()
		_, alreadyMonitoring := m.activeMonitors[taskID]
		m.mu.RUnlock()

		if alreadyMonitoring {
			continue
		}

		state := m.taskTracker.GetPollingState(taskID)
		if state != nil && state.IsPolling {
			m.MonitorPollingState(ctx, taskID, state)
		}
	}
}

func (m *A2APollingMonitor) MonitorPollingState(ctx context.Context, taskID string, state *domain.TaskPollingState) {
	m.mu.Lock()
	if _, exists := m.activeMonitors[taskID]; exists {
		m.mu.Unlock()
		return
	}

	monitorCtx, cancel := context.WithCancel(ctx)
	m.activeMonitors[taskID] = cancel
	m.mu.Unlock()

	go m.monitorSingleTask(monitorCtx, taskID, state)
}

func (m *A2APollingMonitor) monitorSingleTask(ctx context.Context, taskID string, state *domain.TaskPollingState) {
	defer func() {
		m.mu.Lock()
		delete(m.activeMonitors, taskID)
		m.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case result := <-state.ResultChan:
			if m.taskTracker != nil {
				m.taskTracker.StopPolling(taskID)
			}

			time.Sleep(50 * time.Millisecond)

			m.emitCompletionEvent(state.TaskID, result)

			m.addResultToMessageQueue(state.TaskID, result)
			return

		case statusUpdate := <-state.StatusChan:
			if statusUpdate != nil {
				state.LastKnownState = statusUpdate.State
			}
			m.emitStatusUpdateEvent(statusUpdate)

		case err := <-state.ErrorChan:
			logger.Error("A2A task polling error",
				"agent_url", state.AgentURL,
				"task_id", state.TaskID,
				"error", err)

			if m.taskTracker != nil {
				m.taskTracker.StopPolling(taskID)
			}

			errorMsg := ""
			if err != nil {
				errorMsg = err.Error()
			}
			errorResult := &domain.ToolExecutionResult{
				ToolName: "A2ATask",
				Success:  false,
				Error:    errorMsg,
			}

			m.addResultToMessageQueue(state.TaskID, errorResult)
			m.emitErrorEvent(state.TaskID, err)
			return
		}
	}
}

func (m *A2APollingMonitor) emitCompletionEvent(taskID string, result *domain.ToolExecutionResult) {
	if result == nil {
		logger.Error("Received nil result in emitCompletionEvent",
			"task_id", taskID)
		return
	}

	if result.Success {
		event := domain.A2ATaskCompletedEvent{
			RequestID: m.requestID,
			Timestamp: time.Now(),
			TaskID:    taskID,
			Result:    *result,
		}

		select {
		case m.eventChan <- event:
		default:
			logger.Warn("Failed to emit A2A completion event - channel full",
				"task_id", taskID)
		}
	} else {
		event := domain.A2ATaskFailedEvent{
			RequestID: m.requestID,
			Timestamp: time.Now(),
			TaskID:    taskID,
			Result:    *result,
			Error:     result.Error,
		}

		select {
		case m.eventChan <- event:
		default:
			logger.Warn("Failed to emit A2A failure event - channel full",
				"task_id", taskID)
		}
	}
}

func (m *A2APollingMonitor) emitStatusUpdateEvent(update *domain.A2ATaskStatusUpdate) {
	if update == nil {
		logger.Error("Received nil update in emitStatusUpdateEvent")
		return
	}

	event := domain.A2ATaskStatusUpdateEvent{
		RequestID: m.requestID,
		Timestamp: time.Now(),
		TaskID:    update.TaskID,
		Status:    update.State,
		Message:   update.Message,
	}

	select {
	case m.eventChan <- event:
	default:
	}

	if update.State == string(adk.TaskStateInputRequired) {
		inputRequiredEvent := domain.A2ATaskInputRequiredEvent{
			RequestID: m.requestID,
			Timestamp: time.Now(),
			TaskID:    update.TaskID,
			Message:   update.Message,
			Required:  true,
		}

		select {
		case m.eventChan <- inputRequiredEvent:
		default:
			logger.Warn("Failed to emit A2A input required event - channel full",
				"task_id", update.TaskID)
		}
	}
}

func (m *A2APollingMonitor) emitErrorEvent(taskID string, err error) {
	errorMsg := ""
	if err != nil {
		errorMsg = err.Error()
	}

	event := domain.A2ATaskFailedEvent{
		RequestID: m.requestID,
		Timestamp: time.Now(),
		TaskID:    taskID,
		Result: domain.ToolExecutionResult{
			ToolName: "A2ATask",
			Success:  false,
			Error:    errorMsg,
		},
		Error: errorMsg,
	}

	select {
	case m.eventChan <- event:
	default:
		logger.Warn("Failed to emit A2A error event - channel full",
			"task_id", taskID)
	}
}

func (m *A2APollingMonitor) stopAllMonitors() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, cancel := range m.activeMonitors {
		cancel()
	}

	m.activeMonitors = make(map[string]context.CancelFunc)
}

// addResultToMessageQueue adds the A2A task result to the centralized message queue
func (m *A2APollingMonitor) addResultToMessageQueue(taskID string, result *domain.ToolExecutionResult) {
	if result == nil || m.messageQueue == nil {
		return
	}

	var content string
	formattedResult := m.conversationRepo.FormatToolResultForLLM(result)
	if result.Error != "" {
		content = fmt.Sprintf("[A2A Task Failed: %s]\n\nTask ID: %s\n\n%s",
			result.ToolName,
			taskID,
			formattedResult)
	} else {
		content = fmt.Sprintf("[A2A Task Completed: %s]\n\nTask ID: %s\n\n%s",
			result.ToolName,
			taskID,
			formattedResult)
	}

	message := sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(content),
	}

	m.messageQueue.Enqueue(message, m.requestID)
}
