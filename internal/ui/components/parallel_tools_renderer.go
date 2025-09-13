package components

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
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
	tools      map[string]*ToolExecutionState
	styles     *parallelToolStyles
	blinkState bool
	visible    bool
}

type parallelToolStyles struct {
	executing lipgloss.Style
	queued    lipgloss.Style
	complete  lipgloss.Style
	failed    lipgloss.Style
	toolName  lipgloss.Style
	container lipgloss.Style
	message   lipgloss.Style
	duration  lipgloss.Style
}

type TickMsg struct{}

func NewParallelToolsRenderer() *ParallelToolsRenderer {
	styles := &parallelToolStyles{
		executing: lipgloss.NewStyle().
			Foreground(colors.AccentColor.GetLipglossColor()).
			Bold(true),
		queued: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()),
		complete: lipgloss.NewStyle().
			Foreground(colors.SuccessColor.GetLipglossColor()).
			Bold(true),
		failed: lipgloss.NewStyle().
			Foreground(colors.ErrorColor.GetLipglossColor()).
			Bold(true),
		toolName: lipgloss.NewStyle().
			Bold(true),
		container: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colors.BorderColor.GetLipglossColor()).
			Padding(1).
			Margin(1, 0),
		message: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()).
			Italic(true),
		duration: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()),
	}

	return &ParallelToolsRenderer{
		tools:   make(map[string]*ToolExecutionState),
		styles:  styles,
		visible: false,
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
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
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

	var lines []string

	executing := []string{}
	queued := []string{}
	completed := []string{}
	failed := []string{}

	for _, tool := range r.tools {
		line := r.renderToolLine(tool)

		switch tool.Status {
		case ToolStatusRunning, ToolStatusStarting, ToolStatusSaving:
			executing = append(executing, line)
		case ToolStatusQueued:
			queued = append(queued, line)
		case ToolStatusComplete:
			completed = append(completed, line)
		case ToolStatusFailed:
			failed = append(failed, line)
		}
	}

	lines = append(lines, executing...)
	lines = append(lines, queued...)
	lines = append(lines, completed...)
	lines = append(lines, failed...)

	if len(lines) == 0 {
		return ""
	}

	content := strings.Join(lines, "\n")
	return r.styles.container.Render(content)
}

func (r *ParallelToolsRenderer) renderToolLine(tool *ToolExecutionState) string {
	var style lipgloss.Style
	var icon string

	switch tool.Status {
	case ToolStatusQueued:
		style = r.styles.queued
		icon = icons.QueuedIcon

	case ToolStatusStarting, ToolStatusRunning, ToolStatusSaving:
		if r.blinkState {
			style = r.styles.executing.
				Foreground(colors.AccentColor.GetLipglossColor())
		} else {
			style = r.styles.executing.
				Foreground(colors.DimColor.GetLipglossColor())
		}
		icon = icons.ExecutingIcon

	case ToolStatusComplete:
		style = r.styles.complete
		icon = icons.CheckMark

	case ToolStatusFailed:
		style = r.styles.failed
		icon = icons.CrossMark

	default:
		style = r.styles.queued
		icon = icons.BulletIcon
	}

	toolName := r.styles.toolName.Render(tool.ToolName)
	status := style.Render(fmt.Sprintf("%s %s", icon, toolName))

	if tool.Message != "" {
		status += " " + r.styles.message.Render(fmt.Sprintf("- %s", tool.Message))
	}

	if tool.EndTime != nil {
		duration := tool.EndTime.Sub(tool.StartTime).Round(time.Millisecond)
		status += " " + r.styles.duration.Render(fmt.Sprintf("(%v)", duration))
	}

	return status
}

func (r *ParallelToolsRenderer) Clear() {
	r.tools = make(map[string]*ToolExecutionState)
	r.visible = false
}
