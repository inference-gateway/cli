package components

import (
	"fmt"
	"strings"
	"time"

	viewport "github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	adk "github.com/inference-gateway/adk/types"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	logger "github.com/inference-gateway/cli/internal/logger"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// TaskInfo extends TaskPollingState with additional metadata for UI display
type TaskInfo struct {
	domain.TaskPollingState
	Status      string
	ElapsedTime time.Duration
	TaskRef     *domain.TaskInfo
}

// TaskManagerImpl implements task management UI similar to conversation selection
type TaskManagerImpl struct {
	activeTasks           []TaskInfo
	completedTasks        []TaskInfo
	filteredTasks         []TaskInfo
	selected              int
	width                 int
	height                int
	themeService          domain.ThemeService
	styleProvider         *styles.Provider
	done                  bool
	cancelled             bool
	taskRetentionService  domain.TaskRetentionService
	backgroundTaskService domain.BackgroundTaskService
	searchQuery           string
	searchMode            bool
	loading               bool
	loadError             error
	confirmCancel         bool
	showInfo              bool
	currentView           TaskViewMode
	infoViewport          viewport.Model
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
	themeService domain.ThemeService,
	styleProvider *styles.Provider,
	taskRetentionService domain.TaskRetentionService,
	backgroundTaskService domain.BackgroundTaskService,
) *TaskManagerImpl {
	vp := viewport.New(80, 20)
	vp.SetContent("")

	return &TaskManagerImpl{
		activeTasks:           make([]TaskInfo, 0),
		completedTasks:        make([]TaskInfo, 0),
		filteredTasks:         make([]TaskInfo, 0),
		selected:              0,
		width:                 80,
		height:                24,
		themeService:          themeService,
		styleProvider:         styleProvider,
		taskRetentionService:  taskRetentionService,
		backgroundTaskService: backgroundTaskService,
		searchQuery:           "",
		searchMode:            false,
		loading:               true,
		loadError:             nil,
		currentView:           TaskViewAll,
		infoViewport:          vp,
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
		if t.backgroundTaskService == nil {
			return domain.TasksLoadedEvent{
				ActiveTasks:    []any{},
				CompletedTasks: []any{},
				Error:          fmt.Errorf("background task service not available"),
			}
		}

		backgroundTasks := t.backgroundTaskService.GetBackgroundTasks()
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

		var retainedTaskInfos []domain.TaskInfo
		if t.taskRetentionService != nil {
			retainedTaskInfos = t.taskRetentionService.GetTasks()
		}
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

		interfaceActiveTasks := make([]any, len(activeTasks))
		for i, task := range activeTasks {
			interfaceActiveTasks[i] = task
		}

		interfaceCompletedTasks := make([]any, len(completedTasks))
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

	t.infoViewport.Width = msg.Width
	t.infoViewport.Height = msg.Height - 2

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
	var cmd tea.Cmd

	switch msg.String() {
	case "q", "esc", "i", "ctrl+c":
		t.showInfo = false
		return t, nil
	case "up", "k":
		t.infoViewport.ScrollUp(1)
	case "down", "j":
		t.infoViewport.ScrollDown(1)
	case "pgup", "b":
		t.infoViewport.PageUp()
	case "pgdown", "f", " ":
		t.infoViewport.PageDown()
	case "g":
		t.infoViewport.GotoTop()
	case "G":
		t.infoViewport.GotoBottom()
	default:
		t.infoViewport, cmd = t.infoViewport.Update(msg)
	}

	return t, cmd
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
		err := t.backgroundTaskService.CancelBackgroundTask(task.TaskID)

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
		adk.TaskStateCancelled:     "Cancelled",
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

	accentColor := t.styleProvider.GetThemeColor("accent")
	dimColor := t.styleProvider.GetThemeColor("dim")

	header := t.styleProvider.RenderWithColorAndBold("Task Details", accentColor)
	content.WriteString(header + "\n")

	separatorWidth := t.getSeparatorWidth()
	separator := t.styleProvider.RenderWithColor(strings.Repeat("─", separatorWidth), dimColor)
	content.WriteString(separator + "\n\n")

	content.WriteString(fmt.Sprintf("%-12s %s\n", t.styleProvider.RenderDimText("ID:"), task.TaskID))
	content.WriteString(fmt.Sprintf("%-12s %s\n", t.styleProvider.RenderDimText("Agent URL:"), task.AgentURL))
	content.WriteString(fmt.Sprintf("%-12s %s\n", t.styleProvider.RenderDimText("Status:"), task.Status))
	content.WriteString(fmt.Sprintf("%-12s %s\n", t.styleProvider.RenderDimText("Started:"), task.StartedAt.Format("2006-01-02 15:04:05")))
	content.WriteString(fmt.Sprintf("%-12s %v\n", t.styleProvider.RenderDimText("Elapsed:"), task.ElapsedTime.Round(time.Second)))
	if task.ContextID != "" {
		content.WriteString(fmt.Sprintf("%-12s %s\n", t.styleProvider.RenderDimText("Context:"), task.ContextID))
	}

	if task.TaskRef != nil {
		t.renderTaskHistory(&content, task)
	}

	t.infoViewport.SetContent(content.String())

	var view strings.Builder
	view.WriteString(t.infoViewport.View())
	view.WriteString("\n")

	footerSeparator := t.styleProvider.RenderWithColor(strings.Repeat("─", t.width), dimColor)
	view.WriteString(footerSeparator + "\n")

	helpText := t.styleProvider.RenderDimText("Press ↑↓/j/k to scroll • g/G for top/bottom • PgUp/PgDn to page • 'i' or 'esc' to close")
	view.WriteString(helpText)

	return view.String()
}

// renderTaskHistory renders the task history section
func (t *TaskManagerImpl) renderTaskHistory(content *strings.Builder, task TaskInfo) {
	content.WriteString("\n")

	accentColor := t.styleProvider.GetThemeColor("accent")
	dimColor := t.styleProvider.GetThemeColor("dim")

	separatorWidth := t.getSeparatorWidth()
	separator := t.styleProvider.RenderWithColor(strings.Repeat("─", separatorWidth), dimColor)
	content.WriteString(separator + "\n")

	historyHeader := t.styleProvider.RenderWithColorAndBold("Task History", accentColor)
	content.WriteString(historyHeader + "\n")

	content.WriteString(separator + "\n\n")

	textWidth := t.infoViewport.Width - 4
	if textWidth < 40 {
		textWidth = 40
	}

	for i, historyItem := range task.TaskRef.Task.History {
		if i > 0 {
			content.WriteString("\n")
		}

		t.renderHistoryItemRole(content, string(historyItem.Role))

		for _, part := range historyItem.Parts {
			if part.Text != nil && *part.Text != "" {
				wrappedText := formatting.FormatResponsiveMessage(*part.Text, textWidth)
				lines := strings.Split(wrappedText, "\n")
				for _, line := range lines {
					fmt.Fprintf(content, "  %s\n", line)
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
	accentColor := t.styleProvider.GetThemeColor("accent")
	dimColor := t.styleProvider.GetThemeColor("dim")

	marker := t.styleProvider.RenderWithColor("◆", accentColor)

	switch role {
	case "assistant":
		roleText := t.styleProvider.RenderWithColor("Assistant:", dimColor)
		fmt.Fprintf(content, "%s %s\n", marker, roleText)
	case "user":
		roleText := t.styleProvider.RenderWithColor("User:", dimColor)
		fmt.Fprintf(content, "%s %s\n", marker, roleText)
	default:
		roleText := t.styleProvider.RenderWithColor(fmt.Sprintf("%s:", role), dimColor)
		fmt.Fprintf(content, "%s %s\n", marker, roleText)
	}
}

// renderFinalResult renders the final result message
func (t *TaskManagerImpl) renderFinalResult(content *strings.Builder, task TaskInfo) {
	textWidth := t.infoViewport.Width - 4
	if textWidth < 40 {
		textWidth = 40
	}

	accentColor := t.styleProvider.GetThemeColor("accent")
	dimColor := t.styleProvider.GetThemeColor("dim")

	if len(task.TaskRef.Task.History) > 0 {
		content.WriteString("\n")
	}

	marker := t.styleProvider.RenderWithColor("◆", accentColor)
	roleText := t.styleProvider.RenderWithColor("Assistant (Final Result):", dimColor)
	fmt.Fprintf(content, "%s %s\n", marker, roleText)

	for _, part := range task.TaskRef.Task.Status.Message.Parts {
		if part.Text != nil && *part.Text != "" {
			wrappedText := formatting.FormatResponsiveMessage(*part.Text, textWidth)
			lines := strings.Split(wrappedText, "\n")
			for _, line := range lines {
				fmt.Fprintf(content, "  %s\n", line)
			}
		}
	}
}

func (t *TaskManagerImpl) renderTaskList() string {
	var content strings.Builder

	accentColor := t.styleProvider.GetThemeColor("accent")
	title := t.styleProvider.RenderWithColor("A2A Background Tasks", accentColor)
	fmt.Fprintf(&content, "%s\n\n", title)

	t.writeViewTabs(&content)

	t.writeSearchInfo(&content)

	if len(t.filteredTasks) == 0 {
		errorColor := t.styleProvider.GetThemeColor("error")
		noTasks := t.styleProvider.RenderWithColor("No tasks found.", errorColor)
		fmt.Fprintf(&content, "%s\n", noTasks)
		t.writeFooter(&content)
		return content.String()
	}

	t.writeTableHeader(&content)

	t.writeTaskRows(&content)

	if t.confirmCancel {
		content.WriteString("\n")
		errorColor := t.styleProvider.GetThemeColor("error")
		warning := t.styleProvider.RenderWithColor("⚠ Cancel this task? (y/n)", errorColor)
		fmt.Fprintf(&content, "%s", warning)
	}

	t.writeFooter(&content)

	return content.String()
}

// writeViewTabs writes the view selection tabs
func (t *TaskManagerImpl) writeViewTabs(b *strings.Builder) {
	accentColor := t.styleProvider.GetThemeColor("accent")

	allStyle := "[1] All"
	activeStyle := "[2] Active"
	inputRequiredStyle := "[3] Input Required"
	completedStyle := "[4] Completed"
	canceledStyle := "[5] Canceled"

	switch t.currentView {
	case TaskViewAll:
		allStyle = t.styleProvider.RenderWithColor("[1] All", accentColor)
	case TaskViewActive:
		activeStyle = t.styleProvider.RenderWithColor("[2] Active", accentColor)
	case TaskViewInputRequired:
		inputRequiredStyle = t.styleProvider.RenderWithColor("[3] Input Required", accentColor)
	case TaskViewCompleted:
		completedStyle = t.styleProvider.RenderWithColor("[4] Completed", accentColor)
	case TaskViewCanceled:
		canceledStyle = t.styleProvider.RenderWithColor("[5] Canceled", accentColor)
	}

	tabs := fmt.Sprintf("%s  %s  %s  %s  %s", allStyle, activeStyle, inputRequiredStyle, completedStyle, canceledStyle)
	dimTabs := t.styleProvider.RenderDimText(tabs)
	fmt.Fprintf(b, "%s\n", dimTabs)

	separatorWidth := t.width - 4
	if separatorWidth < 0 {
		separatorWidth = 40
	}
	separator := t.styleProvider.RenderDimText(strings.Repeat("─", separatorWidth))
	fmt.Fprintf(b, "%s\n\n", separator)
}

// writeSearchInfo writes the search information section
func (t *TaskManagerImpl) writeSearchInfo(b *strings.Builder) {
	if t.searchMode {
		statusColor := t.styleProvider.GetThemeColor("status")
		accentColor := t.styleProvider.GetThemeColor("accent")
		searchText := t.styleProvider.RenderWithColor(fmt.Sprintf("Search: %s", t.searchQuery), statusColor)
		cursor := t.styleProvider.RenderWithColor("│", accentColor)
		fmt.Fprintf(b, "%s%s\n\n", searchText, cursor)
	} else {
		info := fmt.Sprintf("Press / to search • %d tasks available", len(t.filteredTasks))
		dimInfo := t.styleProvider.RenderDimText(info)
		fmt.Fprintf(b, "%s\n\n", dimInfo)
	}
}

// writeTableHeader writes the table header with column labels
func (t *TaskManagerImpl) writeTableHeader(b *strings.Builder) {
	header := fmt.Sprintf("  %-36s │ %-38s │ %-30s │ %-15s │ %-12s", "Context ID", "Task ID", "Agent", "Status", "Elapsed")
	dimHeader := t.styleProvider.RenderDimText(header)
	fmt.Fprintf(b, "%s\n", dimHeader)

	separator := t.styleProvider.RenderDimText(strings.Repeat("─", t.width-4))
	fmt.Fprintf(b, "%s\n", separator)
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
		accentColor := t.styleProvider.GetThemeColor("accent")
		rowText := fmt.Sprintf("▶ %-36s │ %-38s │ %-30s │ %-15s │ %-12s", contextID, taskID, agentURL, status, elapsed)
		fmt.Fprintf(b, "%s\n", t.styleProvider.RenderWithColor(rowText, accentColor))
	} else {
		fmt.Fprintf(b, "  %-36s │ %-38s │ %-30s │ %-15s │ %-12s\n",
			contextID, taskID, agentURL, status, elapsed)
	}
}

// writeFooter writes the footer section with keyboard shortcuts
func (t *TaskManagerImpl) writeFooter(b *strings.Builder) {
	b.WriteString("\n")
	separator := t.styleProvider.RenderDimText(strings.Repeat("─", t.width))
	b.WriteString(separator)
	b.WriteString("\n")

	if t.searchMode {
		help := t.styleProvider.RenderDimText("Type to search, ↑↓ to navigate, Enter to view, Esc to clear search")
		fmt.Fprintf(b, "%s", help)
	} else {
		help := t.styleProvider.RenderDimText("Use ↑↓ arrows to navigate, Enter/i for info, c to cancel, / to search, r to refresh, q/Esc to quit")
		fmt.Fprintf(b, "%s", help)
	}
}

// truncateString truncates a string to the specified length with ellipsis
func (t *TaskManagerImpl) truncateString(s string, maxLen int) string {
	actualWidth := t.styleProvider.GetWidth(s)

	if actualWidth <= maxLen {
		return s
	}

	if maxLen <= 3 {
		return "..."
	}

	stripped := t.stripAnsi(s)
	if len(stripped) <= maxLen-3 {
		return stripped + "..."
	}
	return stripped[:maxLen-3] + "..."
}

// stripAnsi removes ANSI escape sequences from a string
func (t *TaskManagerImpl) stripAnsi(text string) string {
	var result strings.Builder
	inEscape := false

	for i := 0; i < len(text); i++ {
		if text[i] == '\033' && i+1 < len(text) && text[i+1] == '[' {
			inEscape = true
			i++
			continue
		}

		if inEscape {
			if (text[i] >= 'A' && text[i] <= 'Z') || (text[i] >= 'a' && text[i] <= 'z') {
				inEscape = false
			}
			continue
		}

		result.WriteByte(text[i])
	}

	return result.String()
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
