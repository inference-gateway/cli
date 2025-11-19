package components

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
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
	tools         map[string]*ToolExecutionState
	styleProvider *styles.Provider
	blinkState    bool
	visible       bool
	spinnerStep   int
}

type TickMsg struct{}

func NewParallelToolsRenderer(styleProvider *styles.Provider) *ParallelToolsRenderer {
	return &ParallelToolsRenderer{
		tools:         make(map[string]*ToolExecutionState),
		styleProvider: styleProvider,
		visible:       false,
		spinnerStep:   0,
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

	opts := styles.StyleOptions{
		Foreground: r.styleProvider.GetThemeColor("dim"),
		Bold:       true,
	}
	label := r.styleProvider.RenderStyledText("Tools:", opts)
	toolsContent := strings.Join(toolDisplays, " ")
	content := label + " " + toolsContent

	return r.styleProvider.RenderBordered(content, 100)
}

func (r *ParallelToolsRenderer) renderToolBadge(tool *ToolExecutionState) string {
	var icon string
	var colorName string

	switch tool.Status {
	case ToolStatusRunning, ToolStatusStarting, ToolStatusSaving:
		icon = icons.GetSpinnerFrame(r.spinnerStep)
		colorName = "accent"

	case ToolStatusQueued:
		icon = icons.QueuedIcon
		colorName = "warning"

	case ToolStatusComplete:
		icon = icons.CheckMark
		colorName = "success"

	case ToolStatusFailed:
		icon = icons.CrossMark
		colorName = "error"

	default:
		icon = icons.BulletIcon
		colorName = "dim"
	}

	toolNameText := r.styleProvider.RenderWithColor(tool.ToolName, r.styleProvider.GetThemeColor("assistant"))
	badgeContent := icon + " " + toolNameText

	return r.styleProvider.RenderWithColor(badgeContent, r.styleProvider.GetThemeColor(colorName))
}

func (r *ParallelToolsRenderer) Clear() {
	r.tools = make(map[string]*ToolExecutionState)
	r.visible = false
}
