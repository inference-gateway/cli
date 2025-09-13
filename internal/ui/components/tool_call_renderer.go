package components

import (
	"fmt"
	"strings"
	"time"

	spinner "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

type ToolCallRenderer struct {
	width         int
	height        int
	spinner       spinner.Model
	toolPreviews  map[string]*domain.ToolCallPreviewEvent
	styles        *toolRenderStyles
	lastUpdate    time.Time
	parallelTools map[string]*ParallelToolState
	spinnerStep   int
}

type ParallelToolState struct {
	CallID      string
	ToolName    string
	Status      string
	NextStatus  string
	StartTime   time.Time
	EndTime     *time.Time
	LastUpdate  time.Time
	MinShowTime time.Duration
}

type toolRenderStyles struct {
	statusStreaming lipgloss.Style
	statusComplete  lipgloss.Style
	statusReady     lipgloss.Style
	statusDefault   lipgloss.Style
	toolCallMeta    lipgloss.Style
	toolCallArgs    lipgloss.Style
	spinner         lipgloss.Style
	toolName        lipgloss.Style
	argsContainer   lipgloss.Style
}

type ToolInfo struct {
	Name   string
	Prefix string
}

func NewToolCallRenderer() *ToolCallRenderer {
	s := spinner.New()
	s.Spinner = spinner.Dot

	styles := &toolRenderStyles{
		statusStreaming: lipgloss.NewStyle().
			Foreground(colors.SpinnerColor.GetLipglossColor()).
			Bold(true),
		statusComplete: lipgloss.NewStyle().
			Foreground(colors.SuccessColor.GetLipglossColor()).
			Bold(true),
		statusReady: lipgloss.NewStyle().
			Foreground(colors.WarningColor.GetLipglossColor()).
			Bold(true),
		statusDefault: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()),
		toolCallMeta: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()).
			Italic(true),
		toolCallArgs: lipgloss.NewStyle().
			Foreground(colors.DimColor.GetLipglossColor()).
			MarginLeft(2),
		spinner: lipgloss.NewStyle().
			Foreground(colors.SpinnerColor.GetLipglossColor()),
		toolName: lipgloss.NewStyle().
			Foreground(colors.AccentColor.GetLipglossColor()).
			Bold(true),
		argsContainer: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), false, false, false, true).
			BorderForeground(colors.DimColor.GetLipglossColor()).
			PaddingLeft(2).
			MarginTop(1),
	}

	s.Style = styles.spinner

	return &ToolCallRenderer{
		spinner:       s,
		toolPreviews:  make(map[string]*domain.ToolCallPreviewEvent),
		parallelTools: make(map[string]*ParallelToolState),
		styles:        styles,
		width:         80,
	}
}

func (r *ToolCallRenderer) Init() tea.Cmd {
	return r.spinner.Tick
}

func (r *ToolCallRenderer) Update(msg tea.Msg) (*ToolCallRenderer, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width = msg.Width
		r.height = msg.Height
		r.updateArgsContainerWidth()

	case domain.ToolCallPreviewEvent:
		r.toolPreviews[msg.ToolCallID] = &msg
		if len(r.toolPreviews) == 1 && r.HasActivePreviews() {
			return r, r.spinner.Tick
		}

	case domain.ToolCallUpdateEvent:
		if preview, exists := r.toolPreviews[msg.ToolCallID]; exists {
			if time.Since(r.lastUpdate) < 50*time.Millisecond {
				return r, nil
			}
			preview.Arguments = msg.Arguments
			preview.Status = msg.Status
			if msg.Status == domain.ToolCallStreamStatusComplete {
				preview.IsComplete = true
			}
			r.lastUpdate = time.Now()
		}

	case domain.ToolCallReadyEvent:
		r.ClearPreviews()

	case domain.ChatCompleteEvent:
		r.ClearPreviews()

	case domain.ParallelToolsStartEvent:
		logger.Debug("ParallelToolsStartEvent received", "tool_count", len(msg.Tools))
		for _, tool := range msg.Tools {
			now := time.Now()
			r.parallelTools[tool.CallID] = &ParallelToolState{
				CallID:      tool.CallID,
				ToolName:    tool.Name,
				Status:      "queued",
				NextStatus:  "",
				StartTime:   now,
				LastUpdate:  now,
				MinShowTime: 400 * time.Millisecond,
			}
			logger.Debug("Tool queued", "tool_name", tool.Name, "call_id", tool.CallID)
		}
		return r, r.spinner.Tick

	case domain.ToolExecutionProgressEvent:
		if state, exists := r.parallelTools[msg.ToolCallID]; exists {
			logger.Debug("Tool status update", "tool_name", state.ToolName, "old_status", state.Status, "new_status", msg.Status, "call_id", msg.ToolCallID)
			state.Status = msg.Status
			if msg.Status == "complete" || msg.Status == "failed" {
				endTime := time.Now()
				state.EndTime = &endTime
			}
		} else {
			logger.Debug("Received progress event for unknown tool", "call_id", msg.ToolCallID, "status", msg.Status)
		}

	case spinner.TickMsg:
		r.spinnerStep = (r.spinnerStep + 1) % 4
		if r.HasActivePreviews() || r.hasActiveParallelTools() {
			r.spinner, cmd = r.spinner.Update(msg)
			return r, cmd
		}
	}

	return r, cmd
}

func (r *ToolCallRenderer) SetWidth(width int) {
	r.width = width
	r.updateArgsContainerWidth()
}

func (r *ToolCallRenderer) updateArgsContainerWidth() {
	r.styles.argsContainer = r.styles.argsContainer.Width(r.width - 6)
	r.styles.toolCallArgs = r.styles.toolCallArgs.Width(r.width - 8)
}

func (r *ToolCallRenderer) RenderPreviews() string {
	var allPreviews []string

	for _, preview := range r.toolPreviews {
		if r.shouldShowPreview(preview) {
			allPreviews = append(allPreviews, r.renderToolPreview(preview))
		}
	}

	now := time.Now()
	for callID, tool := range r.parallelTools {
		if (tool.Status == "complete" || tool.Status == "failed") && tool.EndTime != nil {
			showDuration := now.Sub(*tool.EndTime)
			if showDuration > 1000*time.Millisecond {
				delete(r.parallelTools, callID)
				continue
			}
		}

		allPreviews = append(allPreviews, r.renderParallelTool(tool))
	}

	if len(allPreviews) == 0 {
		return ""
	}

	return strings.Join(allPreviews, "\n")
}

func (r *ToolCallRenderer) RenderToolCalls(toolCalls []sdk.ChatCompletionMessageToolCall, status string) string {
	if len(toolCalls) == 0 {
		return ""
	}

	var rendered []string
	for _, toolCall := range toolCalls {
		rendered = append(rendered, r.renderCompletedToolCall(toolCall, status))
	}

	return strings.Join(rendered, "\n")
}

func (r *ToolCallRenderer) shouldShowPreview(*domain.ToolCallPreviewEvent) bool {
	return true
}

func (r *ToolCallRenderer) renderToolPreview(preview *domain.ToolCallPreviewEvent) string {
	var statusIcon string
	var statusText string
	var statusStyle lipgloss.Style

	switch preview.Status {
	case domain.ToolCallStreamStatusStreaming:
		statusIcon = icons.GetSpinnerFrame(r.spinnerStep)
		statusText = "executing"
		statusStyle = r.styles.statusStreaming
	case domain.ToolCallStreamStatusComplete:
		statusIcon = icons.CheckMark
		statusText = "completed"
		statusStyle = r.styles.statusComplete
	case domain.ToolCallStreamStatusReady:
		statusIcon = icons.QueuedIcon
		statusText = "ready"
		statusStyle = r.styles.statusDefault.Foreground(colors.DimColor.GetLipglossColor())
	default:
		statusIcon = icons.BulletIcon
		statusText = "unknown"
		statusStyle = r.styles.statusDefault
	}

	toolInfo := r.parseToolName(preview.ToolName)
	argsPreview := r.formatArgsPreview(preview.Arguments)

	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		statusStyle.Render(fmt.Sprintf("%s %s:%s", statusIcon, toolInfo.Prefix, toolInfo.Name)),
		r.styles.toolCallMeta.Render(fmt.Sprintf(" (%s)", statusText)),
	)

	if argsPreview != "" {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			header,
			r.styles.toolCallArgs.Render(fmt.Sprintf("  %s", argsPreview)),
		)
	}

	return header
}

func (r *ToolCallRenderer) renderCompletedToolCall(toolCall sdk.ChatCompletionMessageToolCall, status string) string {
	toolInfo := ToolInfo{Name: toolCall.Function.Name, Prefix: "TOOL"}
	return r.renderToolCallContent(toolInfo, toolCall.Function.Arguments, status)
}

func (r *ToolCallRenderer) renderToolCallContent(toolInfo ToolInfo, arguments, status string) string {
	var statusIcon string
	var statusText string

	switch status {
	case "queued":
		statusIcon = icons.QueuedIcon
		statusText = "queued"
	case "executing", "running", "starting", "saving":
		statusIcon = icons.GetSpinnerFrame(r.spinnerStep)
		statusText = status
	case "executed", "completed", "complete":
		statusIcon = icons.CheckMark
		statusText = status
	case "error", "failed":
		statusIcon = icons.CrossMark
		statusText = status
	default:
		statusIcon = icons.BulletIcon
		statusText = status
	}

	var header string
	if toolInfo.Prefix != "TOOL" {
		header = fmt.Sprintf("%s %s:%s (%s)",
			statusIcon,
			r.styles.toolName.Render(toolInfo.Prefix),
			r.styles.toolName.Render(toolInfo.Name),
			r.styles.toolCallMeta.Render(statusText))
	} else {
		header = fmt.Sprintf("%s %s (%s)",
			statusIcon,
			r.styles.toolName.Render(toolInfo.Name),
			r.styles.toolCallMeta.Render(statusText))
	}

	if arguments != "" && arguments != "{}" {
		args := strings.TrimSpace(arguments)
		if len(args) > 200 {
			args = args[:197] + "..."
		}

		formattedArgs := r.styles.toolCallArgs.Render(args)
		return fmt.Sprintf("%s\n%s", header, r.styles.argsContainer.Render(formattedArgs))
	}

	return header
}

func (r *ToolCallRenderer) parseToolName(toolName string) ToolInfo {
	return ToolInfo{Name: toolName, Prefix: "TOOL"}
}

func (r *ToolCallRenderer) formatArgsPreview(args string) string {
	if args == "" {
		return ""
	}

	args = strings.TrimSpace(args)
	if len(args) > 100 {
		return args[:97] + "..."
	}
	return args
}

func (r *ToolCallRenderer) ClearPreviews() {
	r.toolPreviews = make(map[string]*domain.ToolCallPreviewEvent)
	r.parallelTools = make(map[string]*ParallelToolState)
}

func (r *ToolCallRenderer) HasActivePreviews() bool {
	for _, preview := range r.toolPreviews {
		if r.shouldShowPreview(preview) && !preview.IsComplete {
			return true
		}
	}
	return false
}

func (r *ToolCallRenderer) hasActiveParallelTools() bool {
	for _, tool := range r.parallelTools {
		if tool.Status == "running" || tool.Status == "starting" || tool.Status == "saving" {
			return true
		}
	}
	return false
}

func (r *ToolCallRenderer) renderParallelTool(tool *ParallelToolState) string {
	var statusIcon string
	var statusText string
	var statusStyle lipgloss.Style

	switch tool.Status {
	case "queued":
		statusIcon = icons.QueuedIcon
		statusText = "queued"
		statusStyle = r.styles.statusDefault.Foreground(colors.DimColor.GetLipglossColor())
	case "running", "starting", "saving":
		statusIcon = icons.GetSpinnerFrame(r.spinnerStep)
		statusText = "executing"
		statusStyle = r.styles.statusStreaming
	case "complete":
		statusIcon = icons.CheckMark
		statusText = "completed"
		statusStyle = r.styles.statusComplete
	case "failed":
		statusIcon = icons.CrossMark
		statusText = "failed"
		statusStyle = r.styles.statusComplete.Foreground(colors.ErrorColor.GetLipglossColor())
	default:
		statusIcon = icons.BulletIcon
		statusText = tool.Status
		statusStyle = r.styles.statusDefault
	}

	toolInfo := r.parseToolName(tool.ToolName)

	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		statusStyle.Render(fmt.Sprintf("%s %s:%s", statusIcon, toolInfo.Prefix, toolInfo.Name)),
		r.styles.toolCallMeta.Render(fmt.Sprintf(" (%s)", statusText)),
	)

	return header
}
