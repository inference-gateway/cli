package handlers

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
)

// BackgroundTaskHandler handles background task related events
type BackgroundTaskHandler struct {
	backgroundTaskManager domain.BackgroundTaskManager
}

// NewBackgroundTaskHandler creates a new background task handler
func NewBackgroundTaskHandler(backgroundTaskManager domain.BackgroundTaskManager) *BackgroundTaskHandler {
	return &BackgroundTaskHandler{
		backgroundTaskManager: backgroundTaskManager,
	}
}

// CanHandle checks if this handler can handle the given message
func (h *BackgroundTaskHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case domain.BackgroundTaskToggleEvent, domain.BackgroundTaskStartedEvent, domain.BackgroundTaskCompletedEvent:
		return true
	default:
		return false
	}
}

// Handle processes the message and returns any resulting commands
func (h *BackgroundTaskHandler) Handle(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	switch event := msg.(type) {
	case domain.BackgroundTaskToggleEvent:
		return nil, h.HandleBackgroundTaskToggle()
	case domain.BackgroundTaskStartedEvent:
		return nil, h.HandleBackgroundTaskStarted(event)
	case domain.BackgroundTaskCompletedEvent:
		return nil, h.HandleBackgroundTaskCompleted(event)
	default:
		return nil, nil
	}
}

// GetPriority returns the handler priority
func (h *BackgroundTaskHandler) GetPriority() int {
	return 100
}

// GetName returns the handler name
func (h *BackgroundTaskHandler) GetName() string {
	return "BackgroundTaskHandler"
}

// HandleBackgroundTaskToggle handles the ctrl+b key binding to show background task status
func (h *BackgroundTaskHandler) HandleBackgroundTaskToggle() tea.Cmd {
	if h.backgroundTaskManager == nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error: "Background task manager not available",
			}
		}
	}

	activeTasks := h.backgroundTaskManager.GetActiveTasks()
	allTasks := h.backgroundTaskManager.GetAllTasks()

	if len(allTasks) == 0 {
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message: "No background tasks",
				Spinner: false,
			}
		}
	}

	var statusMessage string
	if len(activeTasks) > 0 {
		statusMessage = h.formatActiveTasksMessage(activeTasks)
	} else {
		statusMessage = h.formatInactiveTasksMessage(allTasks)
	}

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message: statusMessage,
			Spinner: false,
		}
	}
}

// HandleBackgroundTaskStarted handles when a background task is started
func (h *BackgroundTaskHandler) HandleBackgroundTaskStarted(event domain.BackgroundTaskStartedEvent) tea.Cmd {
	logger.Info("Background task started", "task_id", event.TaskID, "agent_url", event.AgentURL)

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message: "Background task started: " + event.Description,
			Spinner: false,
		}
	}
}

// HandleBackgroundTaskCompleted handles when a background task is completed
func (h *BackgroundTaskHandler) HandleBackgroundTaskCompleted(event domain.BackgroundTaskCompletedEvent) tea.Cmd {
	if event.Success {
		logger.Info("Background task completed successfully", "task_id", event.TaskID)
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message: "Background task completed: " + event.TaskID,
				Spinner: false,
			}
		}
	} else {
		logger.Error("Background task failed", "task_id", event.TaskID, "error", event.Error)
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error: "Background task failed: " + event.Error,
			}
		}
	}
}

// UpdateBackgroundTaskCount updates the status view with current background task count
func (h *BackgroundTaskHandler) UpdateBackgroundTaskCount() tea.Cmd {
	if h.backgroundTaskManager == nil {
		return nil
	}

	activeCount := h.backgroundTaskManager.GetActiveTaskCount()

	return func() tea.Msg {
		return domain.BackgroundTaskCountUpdateEvent{
			Count: activeCount,
		}
	}
}

func (h *BackgroundTaskHandler) formatActiveTasksMessage(activeTasks []*domain.BackgroundTask) string {
	statusMessage := "Active background tasks:\n"
	for _, task := range activeTasks {
		duration := time.Since(task.StartTime)
		statusMessage += "• " + task.Description + " [" + string(task.Status) + "] (" + duration.Round(time.Second).String() + ")\n"
	}
	return statusMessage
}

func (h *BackgroundTaskHandler) formatInactiveTasksMessage(allTasks []*domain.BackgroundTask) string {
	statusMessage := "No active background tasks"
	if len(allTasks) == 0 {
		return statusMessage
	}

	completedCount, failedCount := h.countTasksByStatus(allTasks)
	statusMessage += " (Recent: " + string(rune(completedCount)) + " completed, " + string(rune(failedCount)) + " failed)"
	return statusMessage
}

func (h *BackgroundTaskHandler) countTasksByStatus(tasks []*domain.BackgroundTask) (int, int) {
	completedCount := 0
	failedCount := 0

	for _, task := range tasks {
		switch task.Status {
		case domain.BackgroundTaskStatusCompleted:
			completedCount++
		case domain.BackgroundTaskStatusFailed:
			failedCount++
		}
	}

	return completedCount, failedCount
}
