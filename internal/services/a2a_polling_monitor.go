package services

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	adk "github.com/inference-gateway/adk/types"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

type A2APollingMonitor struct {
	taskTracker    domain.TaskTracker
	eventChan      chan<- domain.ChatEvent
	messageQueue   chan<- sdk.Message
	requestID      string
	mu             sync.RWMutex
	activeMonitors map[string]context.CancelFunc
	stopChan       chan struct{}
	stopped        bool
}

func NewA2APollingMonitor(
	taskTracker domain.TaskTracker,
	eventChan chan<- domain.ChatEvent,
	messageQueue chan<- sdk.Message,
	requestID string,
) *A2APollingMonitor {
	return &A2APollingMonitor{
		taskTracker:    taskTracker,
		eventChan:      eventChan,
		messageQueue:   messageQueue,
		requestID:      requestID,
		activeMonitors: make(map[string]context.CancelFunc),
		stopChan:       make(chan struct{}),
		stopped:        false,
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

	pollingAgents := m.taskTracker.GetAllPollingAgents()

	for _, agentURL := range pollingAgents {
		m.mu.RLock()
		_, alreadyMonitoring := m.activeMonitors[agentURL]
		m.mu.RUnlock()

		if alreadyMonitoring {
			continue
		}

		state := m.taskTracker.GetPollingState(agentURL)
		if state != nil && state.IsPolling {
			m.MonitorPollingState(ctx, agentURL, state)
		}
	}
}

func (m *A2APollingMonitor) MonitorPollingState(ctx context.Context, agentURL string, state *domain.TaskPollingState) {
	m.mu.Lock()
	if _, exists := m.activeMonitors[agentURL]; exists {
		m.mu.Unlock()
		return
	}

	monitorCtx, cancel := context.WithCancel(ctx)
	m.activeMonitors[agentURL] = cancel
	m.mu.Unlock()

	go m.monitorSingleTask(monitorCtx, agentURL, state)
}

func (m *A2APollingMonitor) monitorSingleTask(ctx context.Context, agentURL string, state *domain.TaskPollingState) {
	defer func() {
		m.mu.Lock()
		delete(m.activeMonitors, agentURL)
		m.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case result := <-state.ResultChan:
			m.emitCompletionEvent(state.TaskID, result)
			return

		case statusUpdate := <-state.StatusChan:
			m.emitStatusUpdateEvent(statusUpdate)

		case err := <-state.ErrorChan:
			logger.Error("A2A task polling error",
				"agent_url", agentURL,
				"task_id", state.TaskID,
				"error", err)

			m.emitErrorEvent(state.TaskID, err)
			return
		}
	}
}

func (m *A2APollingMonitor) emitCompletionEvent(taskID string, result *domain.ToolExecutionResult) {
	event := domain.A2ATaskCompletedEvent{
		RequestID: m.requestID,
		Timestamp: time.Now(),
		TaskID:    taskID,
		Success:   result.Success,
		Result:    result,
		Error:     result.Error,
	}

	select {
	case m.eventChan <- event:
	default:
		logger.Warn("Failed to emit A2A completion event - channel full",
			"task_id", taskID)
	}

	if m.messageQueue != nil {
		syntheticMessage := m.createSyntheticToolResultMessage(result)
		select {
		case m.messageQueue <- syntheticMessage:
		default:
			logger.Warn("Failed to queue synthetic tool result - queue full",
				"task_id", taskID)
		}
	}
}

func (m *A2APollingMonitor) emitStatusUpdateEvent(update *domain.A2ATaskStatusUpdate) {
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

	event := domain.A2ATaskCompletedEvent{
		RequestID: m.requestID,
		Timestamp: time.Now(),
		TaskID:    taskID,
		Success:   false,
		Error:     errorMsg,
	}

	select {
	case m.eventChan <- event:
	default:
		logger.Warn("Failed to emit A2A error event - channel full",
			"task_id", taskID)
	}
}

func (m *A2APollingMonitor) createSyntheticToolResultMessage(result *domain.ToolExecutionResult) sdk.Message {
	content := "[System] The background A2A task has completed. "

	if !result.Success {
		if result.Error != "" {
			content += "Error: " + result.Error
		}
		return sdk.Message{
			Role:    sdk.User,
			Content: content,
		}
	}

	if result.Data != nil {
		if dataJSON, err := json.Marshal(result.Data); err == nil {
			content += "Result: " + string(dataJSON)
		}
	}

	return sdk.Message{
		Role:    sdk.User,
		Content: content,
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
