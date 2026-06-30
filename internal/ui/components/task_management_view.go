package components

import (
	"fmt"
	"slices"
	"strings"
	"time"

	spinner "charm.land/bubbles/v2/spinner"
	viewport "charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	adk "github.com/inference-gateway/adk/types"

	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	logger "github.com/inference-gateway/cli/internal/logger"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// TaskInfo extends TaskPollingState with additional metadata for UI display.
// Kind/Label/Detail carry the background-work kind (A2A task, shell, subagent)
// and its kind-specific columns so the view can render one table per kind from a
// single flat, selectable list. A2A rows keep using the embedded
// TaskPollingState/TaskRef; shell and subagent rows populate Kind/Label/Detail.
type TaskInfo struct {
	domain.TaskPollingState
	Status      string
	ElapsedTime time.Duration
	TaskRef     *domain.TaskInfo
	Kind        domain.JobKind
	Label       string
	Detail      string
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
	backgroundJobRegistry domain.BackgroundTaskRegistry
	searchQuery           string
	searchMode            bool
	loading               bool
	loadError             error
	confirmCancel         bool
	showInfo              bool
	currentView           TaskViewMode
	infoViewport          viewport.Model
	spinner               spinner.Model
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
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent("")

	sp := spinner.New()
	sp.Spinner = spinner.Dot

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
		spinner:               sp,
	}
}

func (t *TaskManagerImpl) Init() tea.Cmd {
	return tea.Batch(t.loadTasksCmd(), t.spinner.Tick)
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
				Kind:             domain.JobKindA2A,
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
				Kind:        domain.JobKindA2A,
			}
			completedTasks = append(completedTasks, taskInfo)
		}

		// Shells and subagents come from the unified supervisor snapshot (running
		// and recently-finished, bounded by each kind's completed_retention). A2A
		// jobs are skipped here - their rows are sourced above from the richer A2A
		// poller / retention service (history, artifacts, context id).
		if t.backgroundJobRegistry != nil {
			for _, job := range t.backgroundJobRegistry.Snapshot() {
				if job.Meta.Kind == domain.JobKindA2A {
					continue
				}
				row := jobToTaskInfo(job)
				if job.Status == domain.JobRunning {
					activeTasks = append(activeTasks, row)
				} else {
					completedTasks = append(completedTasks, row)
				}
			}
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

// jobToTaskInfo adapts a supervised background job (shell or subagent) to a
// task-view row. Elapsed runs to the completion time for finished jobs and to
// now for running ones.
func jobToTaskInfo(job domain.TrackedJob) TaskInfo {
	label := job.Meta.Label
	if label == "" {
		label = job.Meta.ID
	}
	end := time.Now()
	if job.CompletedAt != nil {
		end = *job.CompletedAt
	}
	return TaskInfo{
		TaskPollingState: domain.TaskPollingState{
			TaskID:    job.Meta.ID,
			StartedAt: job.Meta.StartedAt,
		},
		Status:      jobStatusLabel(job.Status),
		ElapsedTime: end.Sub(job.Meta.StartedAt),
		Kind:        job.Meta.Kind,
		Label:       label,
		Detail:      job.Meta.Detail,
	}
}

// jobStatusLabel renders a supervised job's status with the same vocabulary the
// A2A rows use (Running / Completed / Failed).
func jobStatusLabel(s domain.JobStatus) string {
	switch s {
	case domain.JobRunning:
		return "Running"
	case domain.JobCompleted:
		return "Completed"
	case domain.JobFailed:
		return "Failed"
	default:
		return string(s)
	}
}

func (t *TaskManagerImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case domain.TasksLoadedEvent:
		return t.handleTasksLoaded(msg)
	case domain.BackgroundTasksChangedEvent:
		// A background job changed status (submitted/signalled/finished). Reload so
		// the open /tasks view reflects it live, replacing render-time polling.
		if t.loading {
			return t, nil
		}
		return t, t.loadTasksCmd()
	case domain.TaskCancelledEvent:
		return t.handleTaskCancelled(msg)
	case tea.WindowSizeMsg:
		return t.handleWindowResize(msg)
	case tea.KeyMsg:
		if t.loading {
			return t, nil
		}
		return t.handleKeyInput(msg)
	case spinner.TickMsg:
		if !t.loading {
			return t, nil
		}
		var cmd tea.Cmd
		t.spinner, cmd = t.spinner.Update(msg)
		return t, cmd
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
		logger.Error("task cancellation failed", "task_id", msg.TaskID, "error", msg.Error)
		// Even if cancellation failed, reload tasks to show current state
	} else {
		logger.Info("task cancelled, reloading tasks", "task_id", msg.TaskID)
	}

	// Reload tasks to reflect the canceled task in completed tasks list
	return t, t.loadTasksCmd()
}

func (t *TaskManagerImpl) handleWindowResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	t.width = msg.Width
	t.height = msg.Height

	t.infoViewport.SetWidth(msg.Width)
	t.infoViewport.SetHeight(msg.Height - 2)

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
			// Cancel is wired only for active A2A tasks; shells and subagents
			// (also TaskRef == nil) are stopped from their own tools, not here.
			if normalizeKind(task.Kind) == domain.JobKindA2A && task.TaskRef == nil {
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
			logger.Error("failed to cancel task", "task_id", task.TaskID, "error", err)
			return domain.TaskCancelledEvent{
				TaskID: task.TaskID,
				Error:  err,
			}
		}

		logger.Info("task cancelled successfully", "task_id", task.TaskID)
		return domain.TaskCancelledEvent{
			TaskID: task.TaskID,
			Error:  nil,
		}
	}
}

// mapTaskStatus maps task state to display status. Covers every
// adk.TaskState constant so non-terminal states (Working, Submitted,
// AuthRequired, ...) don't fall through to the raw "TASK_STATE_*"
// label. Unknown states fall back to a title-cased rendering of the
// raw value with the "TASK_STATE_" prefix stripped.
func (t *TaskManagerImpl) mapTaskStatus(state adk.TaskState) string {
	statusMap := map[adk.TaskState]string{
		adk.TaskStateSubmitted:     "Submitted",
		adk.TaskStateWorking:       "Working",
		adk.TaskStateCompleted:     "Completed",
		adk.TaskStateFailed:        "Failed",
		adk.TaskStateCancelled:     "Canceled",
		adk.TaskStateRejected:      "Rejected",
		adk.TaskStateInputRequired: "Input Required",
		adk.TaskStateAuthRequired:  "Auth Required",
		adk.TaskStateUnspecified:   "Unknown",
	}

	if displayName, exists := statusMap[state]; exists {
		return displayName
	}

	stateStr := strings.TrimPrefix(string(state), "TASK_STATE_")
	stateStr = strings.ReplaceAll(strings.ToLower(stateStr), "_", " ")
	if stateStr == "" {
		return "Unknown"
	}
	return strings.ToUpper(stateStr[:1]) + stateStr[1:]
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
			// There is no Failed tab, so failed shells/subagents (and failed A2A
			// tasks) belong under Completed; Canceled stays A2A-only.
			if task.Status == "Completed" || task.Status == "Failed" {
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

	// Group rows by kind (A2A, then shells, then subagents) so each kind renders
	// as one contiguous table. Stable sort keeps the within-kind order built above
	// (active rows before completed rows).
	slices.SortStableFunc(baseTasks, func(a, b TaskInfo) int {
		return kindRank(a.Kind) - kindRank(b.Kind)
	})

	if t.searchQuery == "" {
		t.filteredTasks = baseTasks
	} else {
		t.filteredTasks = make([]TaskInfo, 0)
		query := strings.ToLower(t.searchQuery)
		for _, task := range baseTasks {
			if strings.Contains(strings.ToLower(task.AgentURL), query) ||
				strings.Contains(strings.ToLower(task.TaskID), query) ||
				strings.Contains(strings.ToLower(task.Label), query) ||
				strings.Contains(strings.ToLower(task.Detail), query) ||
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

func (t *TaskManagerImpl) View() tea.View {
	return tea.NewView(t.viewContent())
}

func (t *TaskManagerImpl) viewContent() string {
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
	return fmt.Sprintf("%s Loading tasks...", t.spinner.View())
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
	content.WriteString(header)
	content.WriteString("\n")

	separatorWidth := t.getSeparatorWidth()
	separator := t.styleProvider.RenderWithColor(strings.Repeat("─", separatorWidth), dimColor)
	content.WriteString(separator)
	content.WriteString("\n\n")

	fmt.Fprintf(&content, "%-12s %s\n", t.styleProvider.RenderDimText("ID:"), task.TaskID)
	if task.AgentURL != "" {
		fmt.Fprintf(&content, "%-12s %s\n", t.styleProvider.RenderDimText("Agent URL:"), task.AgentURL)
	}
	if task.Detail != "" {
		fmt.Fprintf(&content, "%-12s %s\n", t.styleProvider.RenderDimText("Detail:"), task.Detail)
	}
	fmt.Fprintf(&content, "%-12s %s\n", t.styleProvider.RenderDimText("Status:"), task.Status)
	fmt.Fprintf(&content, "%-12s %s\n", t.styleProvider.RenderDimText("Started:"), task.StartedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&content, "%-12s %v\n", t.styleProvider.RenderDimText("Elapsed:"), task.ElapsedTime.Round(time.Second))
	if task.ContextID != "" {
		fmt.Fprintf(&content, "%-12s %s\n", t.styleProvider.RenderDimText("Context:"), task.ContextID)
	}

	if task.TaskRef != nil {
		t.renderTaskHistory(&content, task)
	}

	t.infoViewport.SetContent(content.String())

	var view strings.Builder
	view.WriteString(t.infoViewport.View())
	view.WriteString("\n")

	footerSeparator := t.styleProvider.RenderWithColor(strings.Repeat("─", t.width), dimColor)
	view.WriteString(footerSeparator)
	view.WriteString("\n")

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
	content.WriteString(separator)
	content.WriteString("\n")

	historyHeader := t.styleProvider.RenderWithColorAndBold("Task History", accentColor)
	content.WriteString(historyHeader)
	content.WriteString("\n")

	content.WriteString(separator)
	content.WriteString("\n\n")

	textWidth := max(t.infoViewport.Width()-4, 40)

	for i, historyItem := range task.TaskRef.Task.History {
		if i > 0 {
			content.WriteString("\n")
		}

		t.renderHistoryItemRole(content, string(historyItem.Role))

		for _, part := range historyItem.Parts {
			if part.Text != nil && *part.Text != "" {
				wrappedText := formatting.FormatResponsiveMessage(*part.Text, textWidth)
				for line := range strings.SplitSeq(wrappedText, "\n") {
					fmt.Fprintf(content, "  %s\n", line)
				}
			}
		}
	}

	if task.TaskRef.Task.Status.Message != nil {
		t.renderFinalResult(content, task)
	}

	if len(task.TaskRef.Task.Artifacts) > 0 {
		t.renderTaskArtifacts(content, task)
	}
}

// renderTaskArtifacts surfaces the agent's produced artifacts (e.g. screenshots,
// generated files) in the Task History panel - for many agents this is the
// real output and Status.Message is empty.
func (t *TaskManagerImpl) renderTaskArtifacts(content *strings.Builder, task TaskInfo) {
	accentColor := t.styleProvider.GetThemeColor("accent")
	dimColor := t.styleProvider.GetThemeColor("dim")

	if len(task.TaskRef.Task.History) > 0 || task.TaskRef.Task.Status.Message != nil {
		content.WriteString("\n")
	}

	marker := t.styleProvider.RenderWithColor("◆", accentColor)
	header := t.styleProvider.RenderWithColor(
		fmt.Sprintf("Agent (Artifacts, %d produced):", len(task.TaskRef.Task.Artifacts)),
		dimColor,
	)
	fmt.Fprintf(content, "%s %s\n", marker, header)

	for i, artifact := range task.TaskRef.Task.Artifacts {
		name := "unnamed"
		if artifact.Name != nil && *artifact.Name != "" {
			name = *artifact.Name
		}
		fmt.Fprintf(content, "  %d. %s", i+1, name)

		if artifact.Metadata != nil {
			if mimeType, ok := (*artifact.Metadata)["mime_type"].(string); ok && mimeType != "" {
				fmt.Fprintf(content, "  (%s)", mimeType)
			}
			if size, ok := (*artifact.Metadata)["size"].(float64); ok && size > 0 {
				fmt.Fprintf(content, "  %d bytes", int64(size))
			}
		}
		content.WriteString("\n")

		if artifact.Metadata != nil {
			if url, ok := (*artifact.Metadata)["url"].(string); ok && url != "" {
				fmt.Fprintf(content, "     %s\n", t.styleProvider.RenderWithColor(url, dimColor))
			}
		}
	}
}

// renderHistoryItemRole renders the role prefix for a history item.
// Handles both ADK enum-style values (ROLE_USER / ROLE_AGENT) and the
// historical lowercase ones (user / assistant).
func (t *TaskManagerImpl) renderHistoryItemRole(content *strings.Builder, role string) {
	accentColor := t.styleProvider.GetThemeColor("accent")
	dimColor := t.styleProvider.GetThemeColor("dim")

	marker := t.styleProvider.RenderWithColor("◆", accentColor)

	label := friendlyRoleLabel(role)
	roleText := t.styleProvider.RenderWithColor(label+":", dimColor)
	fmt.Fprintf(content, "%s %s\n", marker, roleText)
}

// friendlyRoleLabel maps an ADK Role string to a tidy display label.
// "ROLE_USER" → "User", "ROLE_AGENT" → "Agent", "user" → "User",
// "assistant" → "Agent", anything else is title-cased as-is.
func friendlyRoleLabel(role string) string {
	normalized := strings.ToLower(strings.TrimPrefix(strings.ToUpper(role), "ROLE_"))
	switch normalized {
	case "user":
		return "User"
	case "agent":
		return "Agent"
	case "unspecified":
		return "Unknown"
	default:
		if len(normalized) > 0 {
			return strings.ToUpper(normalized[:1]) + normalized[1:]
		}
		return role
	}
}

// renderFinalResult renders the final result message
func (t *TaskManagerImpl) renderFinalResult(content *strings.Builder, task TaskInfo) {
	textWidth := max(t.infoViewport.Width()-4, 40)

	accentColor := t.styleProvider.GetThemeColor("accent")
	dimColor := t.styleProvider.GetThemeColor("dim")

	if len(task.TaskRef.Task.History) > 0 {
		content.WriteString("\n")
	}

	marker := t.styleProvider.RenderWithColor("◆", accentColor)
	roleText := t.styleProvider.RenderWithColor("Agent (Final Result):", dimColor)
	fmt.Fprintf(content, "%s %s\n", marker, roleText)

	for _, part := range task.TaskRef.Task.Status.Message.Parts {
		if part.Text != nil && *part.Text != "" {
			wrappedText := formatting.FormatResponsiveMessage(*part.Text, textWidth)
			for line := range strings.SplitSeq(wrappedText, "\n") {
				fmt.Fprintf(content, "  %s\n", line)
			}
		}
	}
}

func (t *TaskManagerImpl) renderTaskList() string {
	var content strings.Builder

	accentColor := t.styleProvider.GetThemeColor("accent")
	title := t.styleProvider.RenderWithColor("Background Tasks", accentColor)
	fmt.Fprintf(&content, "%s\n\n", title)

	t.writeJobCountsSummary(&content)

	t.writeViewTabs(&content)

	t.writeSearchInfo(&content)

	if len(t.filteredTasks) == 0 {
		errorColor := t.styleProvider.GetThemeColor("error")
		noTasks := t.styleProvider.RenderWithColor("No tasks found.", errorColor)
		fmt.Fprintf(&content, "%s\n", noTasks)
		t.writeFooter(&content)
		return content.String()
	}

	t.writeTaskSections(&content)

	if t.confirmCancel {
		content.WriteString("\n")
		errorColor := t.styleProvider.GetThemeColor("error")
		warning := t.styleProvider.RenderWithColor("⚠ Cancel this task? (y/n)", errorColor)
		fmt.Fprintf(&content, "%s", warning)
	}

	t.writeFooter(&content)

	return content.String()
}

// SetBackgroundTaskRegistry wires the unified registry so the view can show live
// counts of every background-work kind (A2A tasks, shells, subagents), not just
// the A2A tasks listed in the table below.
func (t *TaskManagerImpl) SetBackgroundTaskRegistry(registry domain.BackgroundTaskRegistry) {
	t.backgroundJobRegistry = registry
}

// writeJobCountsSummary writes a one-line summary of all running background work
// from the supervisor: "Running: 2 A2A · 1 shell · 3 subagents".
func (t *TaskManagerImpl) writeJobCountsSummary(b *strings.Builder) {
	if t.backgroundJobRegistry == nil {
		return
	}
	a2a := t.backgroundJobRegistry.CountRunningJobs(domain.JobKindA2A)
	shells := t.backgroundJobRegistry.CountRunningJobs(domain.JobKindShell)
	subagents := t.backgroundJobRegistry.CountRunningJobs(domain.JobKindSubagent)

	dimColor := t.styleProvider.GetThemeColor("dim")
	summary := fmt.Sprintf("Running: %d A2A · %d shells · %d subagents  (total %d)",
		a2a, shells, subagents, a2a+shells+subagents)
	fmt.Fprintf(b, "%s\n\n", t.styleProvider.RenderWithColor(summary, dimColor))
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

// writeTaskSections renders the filtered rows as one table per kind (A2A tasks,
// background shells, subagents). applyFilters has already ordered the rows by
// kind, so a new section header is emitted whenever the kind changes. The row
// index stays global across sections so the ▶ selection highlight is correct.
func (t *TaskManagerImpl) writeTaskSections(b *strings.Builder) {
	prevKind := domain.JobKind("")
	for i, task := range t.filteredTasks {
		kind := normalizeKind(task.Kind)
		if i == 0 || kind != prevKind {
			if i != 0 {
				b.WriteString("\n")
			}
			t.writeSectionHeader(b, kind)
			prevKind = kind
		}
		t.writeTaskRow(b, task, i)
	}
}

// writeSectionHeader writes a per-kind table title plus its column header.
func (t *TaskManagerImpl) writeSectionHeader(b *strings.Builder, kind domain.JobKind) {
	accentColor := t.styleProvider.GetThemeColor("accent")
	title := t.styleProvider.RenderWithColor(sectionTitle(kind), accentColor)
	fmt.Fprintf(b, "%s\n", title)

	dimHeader := t.styleProvider.RenderDimText(t.columnHeader(kind))
	fmt.Fprintf(b, "%s\n", dimHeader)

	separator := t.styleProvider.RenderDimText(strings.Repeat("─", t.width-4))
	fmt.Fprintf(b, "%s\n", separator)
}

// columnHeader returns the column labels for a kind's table. A2A keeps its
// Context ID / Task ID / Agent layout; shells and subagents share a leaner
// ID / Detail layout (Detail = command for shells, mode for subagents).
func (t *TaskManagerImpl) columnHeader(kind domain.JobKind) string {
	switch kind {
	case domain.JobKindShell:
		return fmt.Sprintf("  %-40s │ %-50s │ %-15s │ %-12s", "Shell ID", "Command", "Status", "Elapsed")
	case domain.JobKindSubagent:
		return fmt.Sprintf("  %-40s │ %-50s │ %-15s │ %-12s", "Subagent", "Mode", "Status", "Elapsed")
	default:
		return fmt.Sprintf("  %-36s │ %-38s │ %-30s │ %-15s │ %-12s", "Context ID", "Task ID", "Agent", "Status", "Elapsed")
	}
}

// writeTaskRow writes a single row, dispatching on kind to the matching column
// layout. index is the global position in filteredTasks (drives selection).
func (t *TaskManagerImpl) writeTaskRow(b *strings.Builder, task TaskInfo, index int) {
	switch normalizeKind(task.Kind) {
	case domain.JobKindShell, domain.JobKindSubagent:
		t.writeJobRow(b, task, index)
	default:
		t.writeA2ARow(b, task, index)
	}
}

// writeA2ARow writes an A2A task row: Context ID | Task ID | Agent | Status | Elapsed.
func (t *TaskManagerImpl) writeA2ARow(b *strings.Builder, task TaskInfo, index int) {
	taskID := formatting.TruncateText(task.TaskID, 38)
	agentURL := formatting.TruncateText(task.AgentURL, 30)
	contextID := formatting.TruncateText(task.ContextID, 38)
	if contextID == "" {
		contextID = "-"
	}
	status := formatting.TruncateText(task.Status, 15)
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

// writeJobRow writes a shell/subagent row: ID | Detail | Status | Elapsed.
func (t *TaskManagerImpl) writeJobRow(b *strings.Builder, task TaskInfo, index int) {
	label := task.Label
	if label == "" {
		label = task.TaskID
	}
	id := formatting.TruncateText(label, 40)
	detail := formatting.TruncateText(task.Detail, 50)
	status := formatting.TruncateText(task.Status, 15)
	elapsed := t.formatDuration(task.ElapsedTime)

	if index == t.selected {
		accentColor := t.styleProvider.GetThemeColor("accent")
		rowText := fmt.Sprintf("▶ %-40s │ %-50s │ %-15s │ %-12s", id, detail, status, elapsed)
		fmt.Fprintf(b, "%s\n", t.styleProvider.RenderWithColor(rowText, accentColor))
	} else {
		fmt.Fprintf(b, "  %-40s │ %-50s │ %-15s │ %-12s\n", id, detail, status, elapsed)
	}
}

// normalizeKind treats an unset kind as A2A (A2A rows sourced from the poller
// before kinds were tagged).
func normalizeKind(kind domain.JobKind) domain.JobKind {
	if kind == "" {
		return domain.JobKindA2A
	}
	return kind
}

// kindRank orders rows so each kind forms one contiguous section: A2A, then
// shells, then subagents.
func kindRank(kind domain.JobKind) int {
	switch normalizeKind(kind) {
	case domain.JobKindShell:
		return 1
	case domain.JobKindSubagent:
		return 2
	default:
		return 0
	}
}

// sectionTitle is the heading shown above each kind's table.
func sectionTitle(kind domain.JobKind) string {
	switch kind {
	case domain.JobKindShell:
		return "Background Shells"
	case domain.JobKindSubagent:
		return "Subagents"
	default:
		return "A2A Tasks"
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
