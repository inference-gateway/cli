package components

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	adk "github.com/inference-gateway/adk/types"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// TaskInfo extends TaskPollingState with additional metadata for UI display
type TaskInfo struct {
	domain.TaskPollingState
	Status      string
	ElapsedTime time.Duration
	TaskRef     *domain.RetainedTaskInfo // nil for active tasks, non-nil for terminal tasks (completed, failed, canceled, etc.)
}

// TaskManagerImpl implements task management UI similar to conversation selection
type TaskManagerImpl struct {
	activeTasks    []TaskInfo
	completedTasks []TaskInfo
	filteredTasks  []TaskInfo
	selected       int
	width          int
	height         int
	themeService   domain.ThemeService
	done           bool
	cancelled      bool
	stateManager   domain.StateManager
	toolService    domain.ToolService
	taskTracker    domain.TaskTracker
	searchQuery    string
	searchMode     bool
	loading        bool
	loadError      error
	confirmCancel  bool
	showInfo       bool
	currentView    TaskViewMode
}

type TaskViewMode int

const (
	TaskViewAll TaskViewMode = iota
	TaskViewActive
	TaskViewInputRequired
	TaskViewCompleted
	TaskViewCanceled
)

// NewTaskManager creates a new task manager UI component
func NewTaskManager(
	stateManager domain.StateManager,
	toolService domain.ToolService,
	themeService domain.ThemeService,
) *TaskManagerImpl {
	return &TaskManagerImpl{
		activeTasks:    make([]TaskInfo, 0),
		completedTasks: make([]TaskInfo, 0),
		filteredTasks:  make([]TaskInfo, 0),
		selected:       0,
		width:          80,
		height:         24,
		themeService:   themeService,
		stateManager:   stateManager,
		toolService:    toolService,
		taskTracker:    toolService.GetTaskTracker(),
		searchQuery:    "",
		searchMode:     false,
		loading:        true,
		loadError:      nil,
		currentView:    TaskViewAll,
	}
}

func (t *TaskManagerImpl) Init() tea.Cmd {
	return t.loadTasksCmd()
}

// Reset resets the task manager state for reuse
func (t *TaskManagerImpl) Reset() {
	t.done = false
	t.cancelled = false
	t.confirmCancel = false
	t.showInfo = false
	t.searchMode = false
	t.searchQuery = ""
	t.selected = 0
	t.loading = true
	t.loadError = nil
	t.currentView = TaskViewAll
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
			elapsed := time.Since(task.StartedAt)

			displayStatus := "Running"
			if task.LastKnownState != "" {
				displayStatus = t.mapTaskStateToDisplayStatus(task.LastKnownState)
			}

			taskInfo := TaskInfo{
				TaskPollingState: task,
				Status:           displayStatus,
				ElapsedTime:      elapsed,
				TaskRef:          nil,
			}
			activeTasks = append(activeTasks, taskInfo)
		}

		retainedTaskInfos := t.stateManager.GetRetainedTasks()
		completedTasks := make([]TaskInfo, 0, len(retainedTaskInfos))

		for i := range retainedTaskInfos {
			retainedTaskInfo := &retainedTaskInfos[i]
			elapsed := retainedTaskInfo.CompletedAt.Sub(retainedTaskInfo.StartedAt)

			taskInfo := TaskInfo{
				TaskPollingState: domain.TaskPollingState{
					TaskID:          retainedTaskInfo.Task.ID,
					ContextID:       retainedTaskInfo.Task.ContextID,
					AgentURL:        retainedTaskInfo.AgentURL,
					TaskDescription: "",
					StartedAt:       retainedTaskInfo.StartedAt,
				},
				Status:      t.mapTaskStatus(retainedTaskInfo.Task.Status.State),
				ElapsedTime: elapsed,
				TaskRef:     retainedTaskInfo,
			}
			completedTasks = append(completedTasks, taskInfo)
		}

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
	case domain.TaskCancelledEvent:
		return t.handleTaskCancelled(msg)
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

func (t *TaskManagerImpl) handleTaskCancelled(msg domain.TaskCancelledEvent) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		logger.Error("Task cancellation failed", "task_id", msg.TaskID, "error", msg.Error)
		// Even if cancellation failed, reload tasks to show current state
	} else {
		logger.Info("Task cancelled, reloading tasks", "task_id", msg.TaskID)
	}

	// Reload tasks to reflect the canceled task in completed tasks list
	return t, t.loadTasksCmd()
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
	case "q", "esc", "ctrl+c":
		t.cancelled = true
		return t, nil

	case "enter":
		if len(t.filteredTasks) > 0 && t.selected < len(t.filteredTasks) {
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
			if task.TaskRef == nil {
				t.confirmCancel = true
				return t, nil
			}
		}

	case "/":
		t.searchMode = true
		return t, nil

	case "1", "2", "3", "4", "5":
		t.handleViewSwitch(msg.String())
		return t, nil

	case "r":
		return t, t.loadTasksCmd()
	}

	return t, nil
}

func (t *TaskManagerImpl) handleViewSwitch(key string) {
	switch key {
	case "1":
		t.currentView = TaskViewAll
	case "2":
		t.currentView = TaskViewActive
	case "3":
		t.currentView = TaskViewInputRequired
	case "4":
		t.currentView = TaskViewCompleted
	case "5":
		t.currentView = TaskViewCanceled
	}
	t.applyFilters()
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
	case "q", "esc", "i", "ctrl+c":
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
		err := t.stateManager.CancelBackgroundTask(task.TaskID, t.toolService)

		if err != nil {
			logger.Error("Failed to cancel task", "task_id", task.TaskID, "error", err)
			return domain.TaskCancelledEvent{
				TaskID: task.TaskID,
				Error:  err,
			}
		}

		logger.Info("Task cancelled successfully", "task_id", task.TaskID)
		return domain.TaskCancelledEvent{
			TaskID: task.TaskID,
			Error:  nil,
		}
	}
}

// mapTaskStatus maps task state to display status
func (t *TaskManagerImpl) mapTaskStatus(state adk.TaskState) string {
	statusMap := map[adk.TaskState]string{
		adk.TaskStateCompleted:     "Completed",
		adk.TaskStateFailed:        "Failed",
		adk.TaskStateCanceled:      "Canceled",
		adk.TaskStateRejected:      "Rejected",
		adk.TaskStateInputRequired: "Input Required",
	}

	if displayName, exists := statusMap[state]; exists {
		return displayName
	}

	stateStr := string(state)
	if len(stateStr) > 0 {
		return strings.ToUpper(stateStr[:1]) + stateStr[1:]
	}
	return "Unknown"
}

// mapTaskStateToDisplayStatus maps task state string to display status
func (t *TaskManagerImpl) mapTaskStateToDisplayStatus(state string) string {
	return t.mapTaskStatus(adk.TaskState(state))
}

func (t *TaskManagerImpl) applyFilters() {
	var baseTasks []TaskInfo

	allTasks := append(append([]TaskInfo{}, t.activeTasks...), t.completedTasks...)

	switch t.currentView {
	case TaskViewAll:
		baseTasks = allTasks
	case TaskViewActive:
		baseTasks = t.activeTasks
	case TaskViewInputRequired:
		baseTasks = make([]TaskInfo, 0)
		for _, task := range allTasks {
			if task.Status == "Input Required" {
				baseTasks = append(baseTasks, task)
			}
		}
	case TaskViewCompleted:
		baseTasks = make([]TaskInfo, 0)
		for _, task := range t.completedTasks {
			if task.Status == "Completed" {
				baseTasks = append(baseTasks, task)
			}
		}
	case TaskViewCanceled:
		baseTasks = make([]TaskInfo, 0)
		for _, task := range t.completedTasks {
			if task.Status == "Canceled" {
				baseTasks = append(baseTasks, task)
			}
		}
	}

	if t.searchQuery == "" {
		t.filteredTasks = baseTasks
	} else {
		t.filteredTasks = make([]TaskInfo, 0)
		query := strings.ToLower(t.searchQuery)
		for _, task := range baseTasks {
			if strings.Contains(strings.ToLower(task.AgentURL), query) ||
				strings.Contains(strings.ToLower(task.TaskID), query) ||
				strings.Contains(strings.ToLower(task.Status), query) {
				t.filteredTasks = append(t.filteredTasks, task)
			}
		}
	}

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
	return "Loading tasks..."
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

	content.WriteString("Task Details\n")
	content.WriteString(strings.Repeat("─", t.getSeparatorWidth()) + "\n\n")
	content.WriteString(fmt.Sprintf("ID: %s\n", task.TaskID))
	content.WriteString(fmt.Sprintf("Agent URL: %s\n", task.AgentURL))
	content.WriteString(fmt.Sprintf("Status: %s\n", task.Status))
	content.WriteString(fmt.Sprintf("Started: %s\n", task.StartedAt.Format("2006-01-02 15:04:05")))
	content.WriteString(fmt.Sprintf("Elapsed: %v\n", task.ElapsedTime.Round(time.Second)))
	if task.ContextID != "" {
		content.WriteString(fmt.Sprintf("Context: %s\n", task.ContextID))
	}

	if task.TaskRef != nil {
		t.renderTaskHistory(&content, task)
	}

	content.WriteString("\n")
	content.WriteString(strings.Repeat("─", t.width-4) + "\n")
	content.WriteString("Press 'i' or 'esc' to close")

	return content.String()
}

// renderTaskHistory renders the task history section
func (t *TaskManagerImpl) renderTaskHistory(content *strings.Builder, task TaskInfo) {
	content.WriteString("\n")
	content.WriteString(strings.Repeat("─", t.width-4) + "\n")
	content.WriteString("Task History\n")
	content.WriteString(strings.Repeat("─", t.width-4) + "\n\n")

	for i, historyItem := range task.TaskRef.Task.History {
		if i > 0 {
			content.WriteString("\n")
		}

		t.renderHistoryItemRole(content, historyItem.Role)

		for _, part := range historyItem.Parts {
			if textPart, ok := part.(adk.TextPart); ok {
				if textPart.Text != "" {
					fmt.Fprintf(content, "  %s\n", textPart.Text)
				}
			}
		}
	}

	if task.TaskRef.Task.Status.Message != nil {
		t.renderFinalResult(content, task)
	}
}

// renderHistoryItemRole renders the role prefix for a history item
func (t *TaskManagerImpl) renderHistoryItemRole(content *strings.Builder, role string) {
	switch role {
	case "assistant":
		content.WriteString("◆ Assistant:\n")
	case "user":
		content.WriteString("◆ User:\n")
	default:
		fmt.Fprintf(content, "◆ %s:\n", role)
	}
}

// renderFinalResult renders the final result message
func (t *TaskManagerImpl) renderFinalResult(content *strings.Builder, task TaskInfo) {
	if len(task.TaskRef.Task.History) > 0 {
		content.WriteString("\n")
	}
	content.WriteString("◆ Assistant (Final Result):\n")
	for _, part := range task.TaskRef.Task.Status.Message.Parts {
		if textPart, ok := part.(adk.TextPart); ok {
			if textPart.Text != "" {
				fmt.Fprintf(content, "  %s\n", textPart.Text)
			}
		}
	}
}

func (t *TaskManagerImpl) renderTaskList() string {
	var content strings.Builder

	fmt.Fprintf(&content, "%sA2A Background Tasks%s\n\n",
		t.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)

	t.writeViewTabs(&content)

	t.writeSearchInfo(&content)

	if len(t.filteredTasks) == 0 {
		fmt.Fprintf(&content, "%sNo tasks found.%s\n",
			t.themeService.GetCurrentTheme().GetErrorColor(), colors.Reset)
		t.writeFooter(&content)
		return content.String()
	}

	t.writeTableHeader(&content)

	t.writeTaskRows(&content)

	if t.confirmCancel {
		content.WriteString("\n")
		fmt.Fprintf(&content, "%s⚠ Cancel this task? (y/n)%s",
			t.themeService.GetCurrentTheme().GetErrorColor(), colors.Reset)
	}

	t.writeFooter(&content)

	return content.String()
}

// writeViewTabs writes the view selection tabs
func (t *TaskManagerImpl) writeViewTabs(b *strings.Builder) {
	allStyle := "[1] All"
	activeStyle := "[2] Active"
	inputRequiredStyle := "[3] Input Required"
	completedStyle := "[4] Completed"
	canceledStyle := "[5] Canceled"

	switch t.currentView {
	case TaskViewAll:
		allStyle = fmt.Sprintf("%s[1] All%s", t.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)
	case TaskViewActive:
		activeStyle = fmt.Sprintf("%s[2] Active%s", t.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)
	case TaskViewInputRequired:
		inputRequiredStyle = fmt.Sprintf("%s[3] Input Required%s", t.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)
	case TaskViewCompleted:
		completedStyle = fmt.Sprintf("%s[4] Completed%s", t.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)
	case TaskViewCanceled:
		canceledStyle = fmt.Sprintf("%s[5] Canceled%s", t.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)
	}

	fmt.Fprintf(b, "%s%s  %s  %s  %s  %s%s\n",
		t.themeService.GetCurrentTheme().GetDimColor(), allStyle, activeStyle, inputRequiredStyle, completedStyle, canceledStyle, colors.Reset)

	separatorWidth := t.width - 4
	if separatorWidth < 0 {
		separatorWidth = 40
	}
	fmt.Fprintf(b, "%s%s%s\n\n",
		t.themeService.GetCurrentTheme().GetDimColor(), strings.Repeat("─", separatorWidth), colors.Reset)
}

// writeSearchInfo writes the search information section
func (t *TaskManagerImpl) writeSearchInfo(b *strings.Builder) {
	if t.searchMode {
		fmt.Fprintf(b, "%sSearch: %s%s│%s\n\n",
			t.themeService.GetCurrentTheme().GetStatusColor(), t.searchQuery, t.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)
	} else {
		fmt.Fprintf(b, "%sPress / to search • %d tasks available%s\n\n",
			t.themeService.GetCurrentTheme().GetDimColor(), len(t.filteredTasks), colors.Reset)
	}
}

// writeTableHeader writes the table header with column labels
func (t *TaskManagerImpl) writeTableHeader(b *strings.Builder) {
	fmt.Fprintf(b, "%s  %-36s │ %-38s │ %-30s │ %-15s │ %-12s%s\n",
		t.themeService.GetCurrentTheme().GetDimColor(), "Context ID", "Task ID", "Agent", "Status", "Elapsed", colors.Reset)
	fmt.Fprintf(b, "%s%s%s\n",
		t.themeService.GetCurrentTheme().GetDimColor(), strings.Repeat("─", t.width-4), colors.Reset)
}

// writeTaskRows writes all task rows in table format
func (t *TaskManagerImpl) writeTaskRows(b *strings.Builder) {
	for i, task := range t.filteredTasks {
		t.writeTaskRow(b, task, i)
	}
}

// writeTaskRow writes a single task row in table format
func (t *TaskManagerImpl) writeTaskRow(b *strings.Builder, task TaskInfo, index int) {
	taskID := t.truncateString(task.TaskID, 38)
	agentURL := t.truncateString(task.AgentURL, 30)
	contextID := t.truncateString(task.ContextID, 38)
	if contextID == "" {
		contextID = "-"
	}
	status := t.truncateString(task.Status, 15)
	elapsed := t.formatDuration(task.ElapsedTime)

	if index == t.selected {
		fmt.Fprintf(b, "%s▶ %-36s │ %-38s │ %-30s │ %-15s │ %-12s%s\n",
			t.themeService.GetCurrentTheme().GetAccentColor(), contextID, taskID, agentURL, status, elapsed, colors.Reset)
	} else {
		fmt.Fprintf(b, "  %-36s │ %-38s │ %-30s │ %-15s │ %-12s\n",
			contextID, taskID, agentURL, status, elapsed)
	}
}

// writeFooter writes the footer section with keyboard shortcuts
func (t *TaskManagerImpl) writeFooter(b *strings.Builder) {
	b.WriteString("\n")
	b.WriteString(colors.CreateSeparator(t.width, "─"))
	b.WriteString("\n")

	if t.searchMode {
		fmt.Fprintf(b, "%sType to search, ↑↓ to navigate, Enter to view, Esc to clear search%s",
			t.themeService.GetCurrentTheme().GetDimColor(), colors.Reset)
	} else {
		fmt.Fprintf(b, "%sUse ↑↓ arrows to navigate, Enter/i for info, c to cancel, / to search, r to refresh, q/Esc to quit%s",
			t.themeService.GetCurrentTheme().GetDimColor(), colors.Reset)
	}
}

// truncateString truncates a string to the specified length with ellipsis
func (t *TaskManagerImpl) truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// formatDuration formats a duration into a human-readable string
func (t *TaskManagerImpl) formatDuration(d time.Duration) string {
	rounded := d.Round(time.Second)
	if rounded < time.Minute {
		return fmt.Sprintf("%ds", int(rounded.Seconds()))
	}
	if rounded < time.Hour {
		mins := int(rounded.Minutes())
		secs := int(rounded.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	hours := int(rounded.Hours())
	mins := int(rounded.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
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

// getSeparatorWidth returns a safe width for separator strings
func (t *TaskManagerImpl) getSeparatorWidth() int {
	width := t.width - 4
	if width < 1 {
		return 40
	}
	return width
}
