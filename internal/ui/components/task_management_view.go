package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// TaskInfo extends TaskPollingState with additional metadata for UI display
type TaskInfo struct {
	domain.TaskPollingState
	AgentName   string
	Status      string
	ElapsedTime time.Duration
	Completed   bool
	Canceled    bool
}

// TaskManagerImpl implements task management UI similar to conversation selection
type TaskManagerImpl struct {
	activeTasks      []TaskInfo
	completedTasks   []TaskInfo
	filteredTasks    []TaskInfo
	selected         int
	width            int
	height           int
	themeService     domain.ThemeService
	done             bool
	cancelled        bool
	stateManager     domain.StateManager
	toolService      domain.ToolService
	taskTracker      domain.TaskTracker
	retentionService TaskRetentionService
	searchQuery      string
	searchMode       bool
	loading          bool
	loadError        error
	confirmCancel    bool
	cancelError      error
	showInfo         bool
	currentView      TaskViewMode
	maxCompleted     int
}

// TaskRetentionService interface for managing completed tasks
type TaskRetentionService interface {
	AddCompletedTask(task TaskInfo)
	GetCompletedTasks() []TaskInfo
	GetMaxRetention() int
	SetMaxRetention(maxRetention int)
	ClearCompletedTasks()
}

// SimpleTaskRetentionService is a simple in-memory implementation
type SimpleTaskRetentionService struct {
	completedTasks []TaskInfo
	maxRetention   int
}

func (s *SimpleTaskRetentionService) AddCompletedTask(task TaskInfo) {
	// Add to the beginning of the list (most recent first)
	s.completedTasks = append([]TaskInfo{task}, s.completedTasks...)

	// Trim to max retention
	if len(s.completedTasks) > s.maxRetention {
		s.completedTasks = s.completedTasks[:s.maxRetention]
	}
}

func (s *SimpleTaskRetentionService) GetCompletedTasks() []TaskInfo {
	return s.completedTasks
}

func (s *SimpleTaskRetentionService) GetMaxRetention() int {
	return s.maxRetention
}

func (s *SimpleTaskRetentionService) SetMaxRetention(maxRetention int) {
	if maxRetention <= 0 {
		maxRetention = 5
	}

	s.maxRetention = maxRetention

	// Trim existing tasks if new retention is smaller
	if len(s.completedTasks) > maxRetention {
		s.completedTasks = s.completedTasks[:maxRetention]
	}
}

func (s *SimpleTaskRetentionService) ClearCompletedTasks() {
	s.completedTasks = make([]TaskInfo, 0)
}

type TaskViewMode int

const (
	TaskViewActive TaskViewMode = iota
	TaskViewCompleted
	TaskViewAll
)

// NewTaskManager creates a new task manager UI component
func NewTaskManager(
	stateManager domain.StateManager,
	toolService domain.ToolService,
	themeService domain.ThemeService,
) *TaskManagerImpl {
	// Create a simple in-memory retention service
	retentionService := &SimpleTaskRetentionService{
		completedTasks: make([]TaskInfo, 0),
		maxRetention:   5, // Default retention
	}

	return &TaskManagerImpl{
		activeTasks:      make([]TaskInfo, 0),
		completedTasks:   make([]TaskInfo, 0),
		filteredTasks:    make([]TaskInfo, 0),
		selected:         0,
		width:            80,
		height:           24,
		themeService:     themeService,
		stateManager:     stateManager,
		toolService:      toolService,
		taskTracker:      toolService.GetTaskTracker(),
		retentionService: retentionService,
		searchQuery:      "",
		searchMode:       false,
		loading:          true,
		loadError:        nil,
		currentView:      TaskViewActive,
		maxCompleted:     5, // Default retention of 5 completed tasks
	}
}

func (t *TaskManagerImpl) Init() tea.Cmd {
	return t.loadTasksCmd()
}

func (t *TaskManagerImpl) loadTasksCmd() tea.Cmd {
	return func() tea.Msg {
		if t.toolService == nil {
			return domain.TasksLoadedEvent{
				ActiveTasks:    []interface{}{},
				CompletedTasks: []interface{}{},
				Error:          fmt.Errorf("tool service not available"),
			}
		}

		backgroundTasks := t.stateManager.GetBackgroundTasks(t.toolService)
		activeTasks := make([]TaskInfo, 0, len(backgroundTasks))

		for _, task := range backgroundTasks {
			agentName := extractAgentName(task.AgentURL)
			elapsed := time.Since(task.StartedAt)

			taskInfo := TaskInfo{
				TaskPollingState: task,
				AgentName:        agentName,
				Status:           "Running",
				ElapsedTime:      elapsed,
				Completed:        false,
				Canceled:         false,
			}
			activeTasks = append(activeTasks, taskInfo)
		}

		// Load completed tasks from retention service
		completedTasks := t.retentionService.GetCompletedTasks()

		// Convert to interface slices for the event
		interfaceActiveTasks := make([]interface{}, len(activeTasks))
		for i, task := range activeTasks {
			interfaceActiveTasks[i] = task
		}

		interfaceCompletedTasks := make([]interface{}, len(completedTasks))
		for i, task := range completedTasks {
			interfaceCompletedTasks[i] = task
		}

		return domain.TasksLoadedEvent{
			ActiveTasks:    interfaceActiveTasks,
			CompletedTasks: interfaceCompletedTasks,
			Error:          nil,
		}
	}
}

func (t *TaskManagerImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case domain.TasksLoadedEvent:
		return t.handleTasksLoaded(msg)
	case tea.WindowSizeMsg:
		return t.handleWindowResize(msg)
	case tea.KeyMsg:
		if t.loading {
			return t, nil
		}
		return t.handleKeyInput(msg)
	}

	return t, nil
}

func (t *TaskManagerImpl) handleTasksLoaded(msg domain.TasksLoadedEvent) (tea.Model, tea.Cmd) {
	t.loading = false
	t.loadError = msg.Error

	if msg.Error == nil {
		// Convert interface slices back to TaskInfo slices
		t.activeTasks = make([]TaskInfo, len(msg.ActiveTasks))
		for i, task := range msg.ActiveTasks {
			if taskInfo, ok := task.(TaskInfo); ok {
				t.activeTasks[i] = taskInfo
			}
		}

		t.completedTasks = make([]TaskInfo, len(msg.CompletedTasks))
		for i, task := range msg.CompletedTasks {
			if taskInfo, ok := task.(TaskInfo); ok {
				t.completedTasks[i] = taskInfo
			}
		}

		t.applyFilters()
	}

	return t, nil
}

func (t *TaskManagerImpl) handleWindowResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	t.width = msg.Width
	t.height = msg.Height
	return t, nil
}

func (t *TaskManagerImpl) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if t.confirmCancel {
		return t.handleCancelConfirmation(msg)
	}

	if t.showInfo {
		return t.handleInfoView(msg)
	}

	if t.searchMode {
		return t.handleSearchInput(msg)
	}

	switch msg.String() {
	case "q", "esc":
		t.cancelled = true
		return t, nil

	case "enter":
		if len(t.filteredTasks) > 0 && t.selected < len(t.filteredTasks) {
			// Default action is to show info
			t.showInfo = true
			return t, nil
		}

	case "up", "k":
		if t.selected > 0 {
			t.selected--
		}

	case "down", "j":
		if t.selected < len(t.filteredTasks)-1 {
			t.selected++
		}

	case "i":
		if len(t.filteredTasks) > 0 && t.selected < len(t.filteredTasks) {
			t.showInfo = true
			return t, nil
		}

	case "c":
		if len(t.filteredTasks) > 0 && t.selected < len(t.filteredTasks) {
			task := t.filteredTasks[t.selected]
			if !task.Completed && !task.Canceled {
				t.confirmCancel = true
				return t, nil
			}
		}

	case "/":
		t.searchMode = true
		return t, nil

	case "1":
		t.currentView = TaskViewActive
		t.applyFilters()

	case "2":
		t.currentView = TaskViewCompleted
		t.applyFilters()

	case "3":
		t.currentView = TaskViewAll
		t.applyFilters()

	case "r":
		return t, t.loadTasksCmd()
	}

	return t, nil
}

func (t *TaskManagerImpl) handleCancelConfirmation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		t.confirmCancel = false
		if t.selected < len(t.filteredTasks) {
			task := t.filteredTasks[t.selected]
			return t, t.cancelTaskCmd(task)
		}

	case "n", "N", "esc":
		t.confirmCancel = false
	}

	return t, nil
}

func (t *TaskManagerImpl) handleInfoView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "i":
		t.showInfo = false
	}

	return t, nil
}

func (t *TaskManagerImpl) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		t.searchMode = false
		t.searchQuery = ""
		t.applyFilters()

	case "enter":
		t.searchMode = false
		t.applyFilters()

	case "backspace":
		if len(t.searchQuery) > 0 {
			t.searchQuery = t.searchQuery[:len(t.searchQuery)-1]
			t.applyFilters()
		}

	default:
		if len(msg.String()) == 1 {
			t.searchQuery += msg.String()
			t.applyFilters()
		}
	}

	return t, nil
}

func (t *TaskManagerImpl) cancelTaskCmd(task TaskInfo) tea.Cmd {
	return func() tea.Msg {
		if t.taskTracker == nil {
			return domain.TaskCancelledEvent{
				TaskID: task.TaskID,
				Error:  fmt.Errorf("task tracker not available"),
			}
		}

		// Send cancel to agent
		err := t.sendCancelToAgent(context.Background(), task.TaskPollingState)
		if err != nil {
			logger.Error("Failed to send cancel to agent", "task_id", task.TaskID, "error", err)
			return domain.TaskCancelledEvent{
				TaskID: task.TaskID,
				Error:  err,
			}
		}

		// Cancel locally
		if task.CancelFunc != nil {
			task.CancelFunc()
		}

		if t.taskTracker != nil {
			t.taskTracker.StopPolling(task.TaskID)
			t.taskTracker.RemoveTask(task.TaskID)
		}

		// Add the canceled task to retention
		canceledTask := task
		canceledTask.Canceled = true
		canceledTask.Status = "Canceled"
		canceledTask.ElapsedTime = time.Since(task.StartedAt)
		t.retentionService.AddCompletedTask(canceledTask)

		return domain.TaskCancelledEvent{
			TaskID: task.TaskID,
			Error:  nil,
		}
	}
}

func (t *TaskManagerImpl) sendCancelToAgent(ctx context.Context, task domain.TaskPollingState) error {
	// This would use the same logic as in the CancelShortcut
	// For now, we'll simulate success
	return nil
}

func (t *TaskManagerImpl) applyFilters() {
	var baseTasks []TaskInfo

	switch t.currentView {
	case TaskViewActive:
		baseTasks = t.activeTasks
	case TaskViewCompleted:
		baseTasks = t.completedTasks
	case TaskViewAll:
		baseTasks = append(append([]TaskInfo{}, t.activeTasks...), t.completedTasks...)
	}

	if t.searchQuery == "" {
		t.filteredTasks = baseTasks
	} else {
		t.filteredTasks = make([]TaskInfo, 0)
		query := strings.ToLower(t.searchQuery)
		for _, task := range baseTasks {
			if strings.Contains(strings.ToLower(task.AgentName), query) ||
				strings.Contains(strings.ToLower(task.TaskID), query) ||
				strings.Contains(strings.ToLower(task.Status), query) {
				t.filteredTasks = append(t.filteredTasks, task)
			}
		}
	}

	// Adjust selection if needed
	if t.selected >= len(t.filteredTasks) {
		t.selected = len(t.filteredTasks) - 1
	}
	if t.selected < 0 {
		t.selected = 0
	}
}

func (t *TaskManagerImpl) View() string {
	if t.loading {
		return t.renderLoading()
	}

	if t.loadError != nil {
		return t.renderError()
	}

	if t.showInfo {
		return t.renderTaskInfo()
	}

	return t.renderTaskList()
}

func (t *TaskManagerImpl) renderLoading() string {
	return fmt.Sprintf("Loading tasks...")
}

func (t *TaskManagerImpl) renderError() string {
	return fmt.Sprintf("Error loading tasks: %v", t.loadError)
}

func (t *TaskManagerImpl) renderTaskInfo() string {
	if t.selected >= len(t.filteredTasks) {
		return "No task selected"
	}

	task := t.filteredTasks[t.selected]
	var content strings.Builder

	content.WriteString(fmt.Sprintf("Task Details\n"))
	content.WriteString(strings.Repeat("─", t.width-4) + "\n\n")
	content.WriteString(fmt.Sprintf("ID: %s\n", task.TaskID))
	content.WriteString(fmt.Sprintf("Agent: %s\n", task.AgentName))
	content.WriteString(fmt.Sprintf("URL: %s\n", task.AgentURL))
	content.WriteString(fmt.Sprintf("Status: %s\n", task.Status))
	content.WriteString(fmt.Sprintf("Started: %s\n", task.StartedAt.Format("2006-01-02 15:04:05")))
	content.WriteString(fmt.Sprintf("Elapsed: %v\n", task.ElapsedTime.Round(time.Second)))
	if task.ContextID != "" {
		content.WriteString(fmt.Sprintf("Context: %s\n", task.ContextID))
	}

	content.WriteString("\n")
	content.WriteString("Press 'i' or 'esc' to close")

	return content.String()
}

func (t *TaskManagerImpl) renderTaskList() string {
	var content strings.Builder

	// Header
	content.WriteString("A2A Background Tasks\n")
	content.WriteString(strings.Repeat("─", t.width-4) + "\n")

	// View tabs
	activeStyle := "[1] Active"
	completedStyle := "[2] Completed"
	allStyle := "[3] All"

	switch t.currentView {
	case TaskViewActive:
		activeStyle = "[1] •Active•"
	case TaskViewCompleted:
		completedStyle = "[2] •Completed•"
	case TaskViewAll:
		allStyle = "[3] •All•"
	}

	content.WriteString(fmt.Sprintf("%s  %s  %s\n", activeStyle, completedStyle, allStyle))
	content.WriteString(strings.Repeat("─", t.width-4) + "\n")

	// Search bar
	if t.searchMode {
		content.WriteString(fmt.Sprintf("Search: %s_\n", t.searchQuery))
	} else {
		content.WriteString(fmt.Sprintf("Search: %s (press '/' to search)\n", t.searchQuery))
	}
	content.WriteString("\n")

	// Task list
	if len(t.filteredTasks) == 0 {
		content.WriteString("No tasks found.\n")
	} else {
		for i, task := range t.filteredTasks {
			prefix := "  "
			if i == t.selected {
				prefix = "> "
			}

			status := task.Status
			if task.Completed {
				status = "Completed"
			} else if task.Canceled {
				status = "Canceled"
			}

			content.WriteString(fmt.Sprintf("%s%s (%s) - %v\n",
				prefix, task.AgentName, status, task.ElapsedTime.Round(time.Second)))
		}
	}

	// Cancel confirmation
	if t.confirmCancel {
		content.WriteString("\n")
		content.WriteString("Cancel this task? (y/n)")
	}

	// Help
	content.WriteString("\n")
	content.WriteString(strings.Repeat("─", t.width-4) + "\n")
	content.WriteString("↑/↓: navigate  Enter/i: info  c: cancel  /: search  r: refresh  q: quit")

	return content.String()
}

// extractAgentName extracts the agent name from the agent URL
func extractAgentName(agentURL string) string {
	parts := strings.Split(agentURL, "://")
	if len(parts) < 2 {
		return agentURL
	}
	hostPort := parts[1]
	host := strings.Split(hostPort, ":")[0]
	return host
}

// GetSelectedTask returns the currently selected task (used by parent components)
func (t *TaskManagerImpl) GetSelectedTask() *TaskInfo {
	if t.selected < len(t.filteredTasks) {
		return &t.filteredTasks[t.selected]
	}
	return nil
}

// IsDone returns true if the user has finished with the task manager
func (t *TaskManagerImpl) IsDone() bool {
	return t.done
}

// IsCancelled returns true if the user cancelled the task manager
func (t *TaskManagerImpl) IsCancelled() bool {
	return t.cancelled
}

// SetWidth sets the width of the task manager
func (t *TaskManagerImpl) SetWidth(width int) {
	t.width = width
}

// SetHeight sets the height of the task manager
func (t *TaskManagerImpl) SetHeight(height int) {
	t.height = height
}
