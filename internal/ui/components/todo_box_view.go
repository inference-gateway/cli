package components

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// AutoCollapseDelay is the duration to wait before auto-collapsing after an update
const AutoCollapseDelay = 3 * time.Second

// TodoBoxView displays a collapsible todo list component
type TodoBoxView struct {
	width         int
	height        int
	styleProvider *styles.Provider
	todos         []domain.TodoItem
	expanded      bool
	autoExpanded  bool      // true if expanded due to auto-expand (not user action)
	lastUpdate    time.Time // time of last todo update
}

// NewTodoBoxView creates a new todo box view
func NewTodoBoxView(styleProvider *styles.Provider) *TodoBoxView {
	return &TodoBoxView{
		width:         80,
		height:        10,
		styleProvider: styleProvider,
		todos:         nil,
		expanded:      false,
		autoExpanded:  false,
	}
}

// SetWidth sets the component width
func (tv *TodoBoxView) SetWidth(width int) {
	tv.width = width
}

// SetHeight sets the component height
func (tv *TodoBoxView) SetHeight(height int) {
	tv.height = height
}

// SetTodos updates the todo list and triggers auto-expand
func (tv *TodoBoxView) SetTodos(todos []domain.TodoItem) {
	tv.todos = todos
	tv.lastUpdate = time.Now()

	// Auto-expand when todos are updated
	if len(todos) > 0 && !tv.expanded {
		tv.expanded = true
		tv.autoExpanded = true
	}
}

// GetTodos returns the current todos
func (tv *TodoBoxView) GetTodos() []domain.TodoItem {
	return tv.todos
}

// SetExpanded sets the expanded state (user action)
func (tv *TodoBoxView) SetExpanded(expanded bool) {
	tv.expanded = expanded
	tv.autoExpanded = false // user took control
}

// Toggle toggles the expanded state
func (tv *TodoBoxView) Toggle() {
	tv.expanded = !tv.expanded
	tv.autoExpanded = false // user took control
}

// IsExpanded returns whether the component is expanded
func (tv *TodoBoxView) IsExpanded() bool {
	return tv.expanded
}

// ShouldAutoCollapse returns true if the component should auto-collapse
func (tv *TodoBoxView) ShouldAutoCollapse() bool {
	if !tv.autoExpanded || !tv.expanded {
		return false
	}
	return time.Since(tv.lastUpdate) >= AutoCollapseDelay
}

// AutoCollapse collapses if auto-expanded and delay has passed
func (tv *TodoBoxView) AutoCollapse() bool {
	if tv.ShouldAutoCollapse() {
		tv.expanded = false
		tv.autoExpanded = false
		return true
	}
	return false
}

// HasTodos returns whether there are any todos
func (tv *TodoBoxView) HasTodos() bool {
	return len(tv.todos) > 0
}

// GetHeight returns the height of the rendered component
func (tv *TodoBoxView) GetHeight() int {
	if !tv.HasTodos() {
		return 0
	}
	if !tv.expanded {
		return 1 // collapsed: single line
	}
	// expanded: header + todos + padding
	return len(tv.todos) + 3
}

// Render renders the todo box
func (tv *TodoBoxView) Render() string {
	if !tv.HasTodos() {
		return ""
	}

	if tv.expanded {
		return tv.renderExpanded()
	}
	return tv.renderCollapsed()
}

// renderCollapsed renders the collapsed view with progress indicator
func (tv *TodoBoxView) renderCollapsed() string {
	completed, total := tv.countTasks()
	progressBar := tv.formatMiniProgressBar(completed, total)

	accentColor := tv.styleProvider.GetThemeColor("accent")
	dimColor := tv.styleProvider.GetThemeColor("dim")

	inProgressTask := tv.getInProgressTask()

	var indicator string
	if inProgressTask != "" {
		maxLen := tv.width - 50
		taskPreview := inProgressTask
		if maxLen > 10 && len(inProgressTask) > maxLen {
			taskPreview = inProgressTask[:maxLen-3] + "..."
		}
		indicator = fmt.Sprintf("%s %s %d/%d tasks",
			taskPreview,
			progressBar,
			completed,
			total,
		)
	} else {
		indicator = fmt.Sprintf("%s %d/%d tasks",
			progressBar,
			completed,
			total,
		)
	}

	hint := "(ctrl+t to expand)"

	indicatorStyled := tv.styleProvider.RenderWithColor(indicator, accentColor)
	hintStyled := tv.styleProvider.RenderWithColor(hint, dimColor)

	return fmt.Sprintf("%s %s", indicatorStyled, hintStyled)
}

// renderExpanded renders the full expanded view
func (tv *TodoBoxView) renderExpanded() string {
	completed, total := tv.countTasks()
	progressBar := tv.formatProgressBar(completed, total)
	percentage := 0
	if total > 0 {
		percentage = int(float64(completed) / float64(total) * 100)
	}

	accentColor := tv.styleProvider.GetThemeColor("accent")
	dimColor := tv.styleProvider.GetThemeColor("dim")

	var lines []string

	inProgressTask := tv.getInProgressTask()
	var header string
	if inProgressTask != "" {
		header = fmt.Sprintf("%s %s %d%% (%d/%d tasks)",
			inProgressTask,
			progressBar,
			percentage,
			completed,
			total,
		)
	} else {
		header = fmt.Sprintf("%s %d%% (%d/%d tasks)",
			progressBar,
			percentage,
			completed,
			total,
		)
	}
	headerStyled := tv.styleProvider.RenderWithColorAndBold(header, accentColor)
	hint := "(ctrl+t to collapse)"
	hintStyled := tv.styleProvider.RenderWithColor(hint, dimColor)
	lines = append(lines, fmt.Sprintf("%s %s", headerStyled, hintStyled))

	for _, todo := range tv.todos {
		line := tv.formatTodoItem(todo)
		lines = append(lines, "  "+line)
	}

	content := strings.Join(lines, "\n")

	return tv.styleProvider.RenderBorderedBox(content, dimColor, 0, 1)
}

// formatTodoItem formats a single todo item
func (tv *TodoBoxView) formatTodoItem(todo domain.TodoItem) string {
	var checkbox, content string

	switch todo.Status {
	case "completed":
		checkbox = colors.CreateColoredText("✓", colors.SuccessColor)
		content = colors.CreateStrikethroughText(todo.Content)
	case "in_progress":
		checkbox = colors.CreateColoredText("►", colors.AccentColor)
		content = colors.CreateColoredText(todo.Content, colors.AccentColor)
	default:
		checkbox = colors.CreateColoredText("○", colors.DimColor)
		content = todo.Content
	}

	return fmt.Sprintf("%s %s", checkbox, content)
}

// formatProgressBar creates a visual progress bar
func (tv *TodoBoxView) formatProgressBar(completed, total int) string {
	if total == 0 {
		return "[░░░░░░░░░░]"
	}

	barLength := 10
	progress := int(float64(completed) / float64(total) * float64(barLength))

	var bar strings.Builder
	bar.WriteString("[")
	for i := 0; i < barLength; i++ {
		if i < progress {
			bar.WriteString("█")
		} else {
			bar.WriteString("░")
		}
	}
	bar.WriteString("]")

	return bar.String()
}

// formatMiniProgressBar creates a compact progress bar for collapsed view
func (tv *TodoBoxView) formatMiniProgressBar(completed, total int) string {
	if total == 0 {
		return "[░░░░░]"
	}

	barLength := 5
	progress := int(float64(completed) / float64(total) * float64(barLength))

	var bar strings.Builder
	bar.WriteString("[")
	for i := 0; i < barLength; i++ {
		if i < progress {
			bar.WriteString("█")
		} else {
			bar.WriteString("░")
		}
	}
	bar.WriteString("]")

	return bar.String()
}

// countTasks returns completed and total task counts
func (tv *TodoBoxView) countTasks() (completed, total int) {
	total = len(tv.todos)
	for _, todo := range tv.todos {
		if todo.Status == "completed" {
			completed++
		}
	}
	return
}

// getInProgressTask returns the content of the in-progress task, if any
func (tv *TodoBoxView) getInProgressTask() string {
	for _, todo := range tv.todos {
		if todo.Status == "in_progress" {
			return todo.Content
		}
	}
	return ""
}

// Bubble Tea interface

// Init initializes the component
func (tv *TodoBoxView) Init() tea.Cmd {
	return nil
}

// View returns the rendered view
func (tv *TodoBoxView) View() string {
	return tv.Render()
}

// Update handles messages
func (tv *TodoBoxView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		tv.SetWidth(windowMsg.Width)
	}
	return tv, nil
}

// AutoCollapseTickMsg is sent to trigger auto-collapse check
type AutoCollapseTickMsg struct{}

// ScheduleAutoCollapse returns a command that will send AutoCollapseTickMsg after the delay
func ScheduleAutoCollapse() tea.Cmd {
	return tea.Tick(AutoCollapseDelay, func(t time.Time) tea.Msg {
		return AutoCollapseTickMsg{}
	})
}
