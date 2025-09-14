package components

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

type ToolExecutionStatus string

const (
	ToolStatusQueued   ToolExecutionStatus = "queued"
	ToolStatusStarting ToolExecutionStatus = "starting"
	ToolStatusRunning  ToolExecutionStatus = "running"
	ToolStatusSaving   ToolExecutionStatus = "saving"
	ToolStatusComplete ToolExecutionStatus = "complete"
	ToolStatusFailed   ToolExecutionStatus = "failed"
)

type ToolExecutionState struct {
	CallID    string
	ToolName  string
	Status    ToolExecutionStatus
	Message   string
	StartTime time.Time
	EndTime   *time.Time
}

type ParallelToolsRenderer struct {
	tools       map[string]*ToolExecutionState
	styles      *parallelToolStyles
	blinkState  bool
	visible     bool
	spinnerStep int
}

type parallelToolStyles struct {
	executing   lipgloss.Style
	queued      lipgloss.Style
	complete    lipgloss.Style
	failed      lipgloss.Style
	toolName    lipgloss.Style
	container   lipgloss.Style
	message     lipgloss.Style
	duration    lipgloss.Style
	statusLabel lipgloss.Style
	toolBadge   lipgloss.Style
	separator   lipgloss.Style
}

type TickMsg struct{}

func NewParallelToolsRenderer() *ParallelToolsRenderer {
	styles := &parallelToolStyles{
		executing: lipgloss.NewStyle().
			Foreground(colors.AccentColor.GetLipglossColor()).
			Bold(true),
		queued: lipgloss.NewStyle().
			Foreground(colors.WarningColor.GetLipglossColor()),
		complete: lipgloss.NewStyle().
			Foreground(colors.SuccessColor.GetLipglossColor()).
			Bold(true),
		failed: lipgloss.NewStyle().
			Foreground(colors.ErrorColor.GetLipglossColor()).
			Bold(true),
		toolName: lipgloss.NewStyle().
			Bold(false).
			Foreground(colors.AssistantColor.GetLipglossColor()),
		container: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colors.BorderColor.GetLipglossColor()).
			Padding(0, 1).
			Margin(0, 0, 1, 0),
		message: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()).
			Italic(true),
		duration: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()),
		statusLabel: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()).
			Bold(true).
			Padding(0, 1),
		toolBadge: lipgloss.NewStyle().
			Padding(0, 1).
			Margin(0, 1, 0, 0).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colors.BorderColor.GetLipglossColor()),
		separator: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()).
			Padding(0, 1),
	}

	return &ParallelToolsRenderer{
		tools:       make(map[string]*ToolExecutionState),
		styles:      styles,
		visible:     false,
		spinnerStep: 0,
	}
}

func (r *ParallelToolsRenderer) Update(msg tea.Msg) (*ParallelToolsRenderer, tea.Cmd) {
	switch msg := msg.(type) {
	case domain.ParallelToolsStartEvent:
		r.visible = true
		for _, tool := range msg.Tools {
			r.tools[tool.CallID] = &ToolExecutionState{
				CallID:    tool.CallID,
				ToolName:  tool.Name,
				Status:    ToolStatusQueued,
				StartTime: time.Now(),
			}
		}
		return r, r.tick()

	case domain.ToolExecutionProgressEvent:
		if state, exists := r.tools[msg.ToolCallID]; exists {
			state.Status = ToolExecutionStatus(msg.Status)
			state.Message = msg.Message

			if msg.Status == string(ToolStatusComplete) || msg.Status == string(ToolStatusFailed) {
				endTime := time.Now()
				state.EndTime = &endTime
			}
		}

		hasExecuting := r.hasExecutingTools()
		if hasExecuting {
			return r, r.tick()
		}

	case TickMsg:
		r.blinkState = !r.blinkState
		r.spinnerStep = (r.spinnerStep + 1) % 4

		hasExecuting := r.hasExecutingTools()
		if hasExecuting {
			return r, r.tick()
		} else {
			r.visible = false
		}
	}

	return r, nil
}

func (r *ParallelToolsRenderer) hasExecutingTools() bool {
	for _, tool := range r.tools {
		if tool.Status == ToolStatusRunning || tool.Status == ToolStatusStarting || tool.Status == ToolStatusSaving {
			return true
		}
	}
	return false
}

func (r *ParallelToolsRenderer) tick() tea.Cmd {
	return tea.Tick(constants.ParallelToolsTickInterval, func(time.Time) tea.Msg {
		return TickMsg{}
	})
}

func (r *ParallelToolsRenderer) IsVisible() bool {
	return r.visible && len(r.tools) > 0
}

func (r *ParallelToolsRenderer) Render() string {
	if !r.IsVisible() {
		return ""
	}

	var toolDisplays []string

	statusOrder := map[ToolExecutionStatus]int{
		ToolStatusRunning:  1,
		ToolStatusStarting: 1,
		ToolStatusSaving:   1,
		ToolStatusQueued:   2,
		ToolStatusComplete: 3,
		ToolStatusFailed:   4,
	}

	type sortedTool struct {
		tool     *ToolExecutionState
		priority int
	}

	var sortedTools []sortedTool
	for _, tool := range r.tools {
		priority, exists := statusOrder[tool.Status]
		if !exists {
			priority = 5
		}
		sortedTools = append(sortedTools, sortedTool{tool: tool, priority: priority})
	}

	for i := 0; i < len(sortedTools)-1; i++ {
		for j := i + 1; j < len(sortedTools); j++ {
			if sortedTools[i].priority > sortedTools[j].priority {
				sortedTools[i], sortedTools[j] = sortedTools[j], sortedTools[i]
			}
		}
	}

	for _, st := range sortedTools {
		toolDisplay := r.renderToolBadge(st.tool)
		toolDisplays = append(toolDisplays, toolDisplay)
	}

	if len(toolDisplays) == 0 {
		return ""
	}

	label := r.styles.statusLabel.Render("Tools:")
	toolsContent := strings.Join(toolDisplays, " ")
	content := label + " " + toolsContent

	return r.styles.container.Render(content)
}

func (r *ParallelToolsRenderer) renderToolBadge(tool *ToolExecutionState) string {
	var icon string
	var badgeStyle lipgloss.Style

	switch tool.Status {
	case ToolStatusRunning, ToolStatusStarting, ToolStatusSaving:
		icon = icons.GetSpinnerFrame(r.spinnerStep)
		badgeStyle = r.styles.toolBadge.
			BorderForeground(colors.AccentColor.GetLipglossColor()).
			Foreground(colors.AccentColor.GetLipglossColor())

	case ToolStatusQueued:
		icon = icons.QueuedIcon
		badgeStyle = r.styles.toolBadge.
			BorderForeground(colors.WarningColor.GetLipglossColor()).
			Foreground(colors.WarningColor.GetLipglossColor())

	case ToolStatusComplete:
		icon = icons.CheckMark
		badgeStyle = r.styles.toolBadge.
			BorderForeground(colors.SuccessColor.GetLipglossColor()).
			Foreground(colors.SuccessColor.GetLipglossColor())

	case ToolStatusFailed:
		icon = icons.CrossMark
		badgeStyle = r.styles.toolBadge.
			BorderForeground(colors.ErrorColor.GetLipglossColor()).
			Foreground(colors.ErrorColor.GetLipglossColor())

	default:
		icon = icons.BulletIcon
		badgeStyle = r.styles.toolBadge.
			BorderForeground(colors.DimColor.GetLipglossColor()).
			Foreground(colors.DimColor.GetLipglossColor())
	}

	badgeContent := icon + " " + r.styles.toolName.Render(tool.ToolName)
	return badgeStyle.Render(badgeContent)
}

func (r *ParallelToolsRenderer) Clear() {
	r.tools = make(map[string]*ToolExecutionState)
	r.visible = false
}
