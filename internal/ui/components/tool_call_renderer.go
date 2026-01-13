package components

import (
	"fmt"
	"strings"
	"time"

	spinner "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

type ToolCallRenderer struct {
	width            int
	height           int
	spinner          spinner.Model
	tools            map[string]*ToolRenderState
	toolsOrder       []string
	styleProvider    *styles.Provider
	keyHintFormatter KeyHintFormatter
	lastUpdate       time.Time
	lastTimerRender  time.Time
	spinnerStep      int
}

// KeyHintFormatter provides formatted key hints for actions
type KeyHintFormatter interface {
	GetKeyHint(actionID, defaultLabel string) string
}

// ToolRenderState represents the unified rendering state for all tool executions
type ToolRenderState struct {
	CallID           string
	ToolName         string
	Status           string
	Arguments        string
	StartTime        time.Time
	EndTime          *time.Time
	LastUpdate       time.Time
	OutputBuffer     []string
	TotalOutputLines int
	IsComplete       bool
	IsExpanded       bool
}

type ToolInfo struct {
	Name   string
	Prefix string
}

func NewToolCallRenderer(styleProvider *styles.Provider) *ToolCallRenderer {
	s := spinner.New()
	customDot := spinner.Dot
	customDot.FPS = 100 * time.Millisecond
	s.Spinner = customDot

	return &ToolCallRenderer{
		spinner:       s,
		tools:         make(map[string]*ToolRenderState),
		styleProvider: styleProvider,
		width:         80,
	}
}

func (r *ToolCallRenderer) Init() tea.Cmd {
	return r.spinner.Tick
}

func (r *ToolCallRenderer) Update(msg tea.Msg) (*ToolCallRenderer, tea.Cmd) { // nolint:gocyclo
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.handleWindowSize(msg)

	case domain.ToolCallPreviewEvent:
		return r.handleToolCallPreview(msg)

	case domain.ToolCallUpdateEvent:
		return r.handleToolCallUpdate(msg)

	case domain.ToolCallReadyEvent:
		r.ClearPreviews()

	case domain.ChatCompleteEvent:
		r.ClearPreviews()

	case domain.ToolExecutionProgressEvent:
		return r.handleToolExecutionProgress(msg)

	case domain.BashOutputChunkEvent:
		return r.handleBashOutputStream(msg)

	case spinner.TickMsg:
		return r.handleSpinnerTick(msg)
	}

	return r, cmd
}

func (r *ToolCallRenderer) handleWindowSize(msg tea.WindowSizeMsg) {
	r.width = msg.Width
	r.height = msg.Height
}

func (r *ToolCallRenderer) handleToolCallPreview(msg domain.ToolCallPreviewEvent) (*ToolCallRenderer, tea.Cmd) {
	now := time.Now()

	if _, exists := r.tools[msg.ToolCallID]; !exists {
		r.toolsOrder = append(r.toolsOrder, msg.ToolCallID)
	}

	r.tools[msg.ToolCallID] = &ToolRenderState{
		CallID:     msg.ToolCallID,
		ToolName:   msg.ToolName,
		Status:     string(msg.Status),
		Arguments:  msg.Arguments,
		StartTime:  now,
		LastUpdate: now,
		IsComplete: msg.IsComplete,
	}

	if len(r.tools) == 1 && r.hasActiveTools() {
		return r, r.spinner.Tick
	}
	return r, nil
}

func (r *ToolCallRenderer) handleToolCallUpdate(msg domain.ToolCallUpdateEvent) (*ToolCallRenderer, tea.Cmd) {
	if state, exists := r.tools[msg.ToolCallID]; exists {
		if time.Since(r.lastUpdate) < constants.ToolCallUpdateThrottle {
			return r, nil
		}
		state.Arguments = msg.Arguments
		state.Status = string(msg.Status)
		if msg.Status == domain.ToolCallStreamStatusComplete {
			state.IsComplete = true
		}
		state.LastUpdate = time.Now()
		r.lastUpdate = time.Now()
	}
	return r, nil
}

func (r *ToolCallRenderer) handleToolExecutionProgress(msg domain.ToolExecutionProgressEvent) (*ToolCallRenderer, tea.Cmd) {
	now := time.Now()

	state, exists := r.tools[msg.ToolCallID]
	if !exists {
		r.toolsOrder = append(r.toolsOrder, msg.ToolCallID)
		r.tools[msg.ToolCallID] = &ToolRenderState{
			CallID:     msg.ToolCallID,
			ToolName:   msg.ToolName,
			Status:     msg.Status,
			Arguments:  msg.Arguments,
			StartTime:  now,
			LastUpdate: now,
		}
		if len(r.tools) == 1 {
			return r, r.spinner.Tick
		}
		return r, nil
	}

	state.Status = msg.Status
	state.LastUpdate = now
	if msg.Arguments != "" && state.Arguments == "" {
		state.Arguments = msg.Arguments
	}
	if msg.Status == "completed" || msg.Status == "failed" {
		state.EndTime = &now
		state.IsComplete = true
	}
	return r, nil
}

func (r *ToolCallRenderer) handleBashOutputStream(msg domain.BashOutputChunkEvent) (*ToolCallRenderer, tea.Cmd) {
	if state, exists := r.tools[msg.ToolCallID]; exists {
		if msg.Output != "" {
			state.OutputBuffer = append(state.OutputBuffer, msg.Output)
			state.TotalOutputLines++
			if len(state.OutputBuffer) > 7 {
				state.OutputBuffer = state.OutputBuffer[len(state.OutputBuffer)-7:]
			}
		}
		state.LastUpdate = time.Now()
	}
	return r, nil
}

func (r *ToolCallRenderer) handleSpinnerTick(msg spinner.TickMsg) (*ToolCallRenderer, tea.Cmd) {
	var cmd tea.Cmd
	r.spinner, cmd = r.spinner.Update(msg)

	if r.hasActiveTools() {
		return r, cmd
	}

	now := time.Now()
	r.spinnerStep = (r.spinnerStep + 1) % 4
	r.lastTimerRender = now

	return r, cmd
}

func (r *ToolCallRenderer) SetWidth(width int) {
	r.width = width
}

// SetKeyHintFormatter sets the key hint formatter for dynamic keybinding hints
func (r *ToolCallRenderer) SetKeyHintFormatter(formatter KeyHintFormatter) {
	r.keyHintFormatter = formatter
}

func (r *ToolCallRenderer) RenderPreviews() string {
	var allPreviews []string
	now := time.Now()
	var remainingTools []string

	for _, callID := range r.toolsOrder {
		tool, exists := r.tools[callID]
		if !exists {
			continue
		}

		if (tool.Status == "completed" || tool.Status == "failed") && tool.EndTime != nil {
			showDuration := now.Sub(*tool.EndTime)
			if showDuration > 1000*time.Millisecond {
				delete(r.tools, callID)
				continue
			}
		}

		allPreviews = append(allPreviews, r.renderTool(tool))
		remainingTools = append(remainingTools, callID)
	}
	r.toolsOrder = remainingTools

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

// renderTool renders a unified tool execution state
func (r *ToolCallRenderer) renderTool(tool *ToolRenderState) string {
	var statusIcon string
	var statusText string
	var iconColor string
	var statusColor string

	switch tool.Status {
	case "queued", "ready":
		statusIcon = icons.QueuedIcon
		statusText = "queued"
		iconColor = "dim"
		statusColor = "dim"
	case "running", "starting", "saving", "executing", "streaming":
		statusIcon = icons.GetSpinnerFrame(r.spinnerStep)
		if tool.EndTime == nil {
			elapsed := time.Since(tool.StartTime)
			statusText = fmt.Sprintf("running %s", r.formatDuration(elapsed))
		} else {
			statusText = "executing"
		}
		iconColor = "accent"
		statusColor = "accent"
	case "completed", "executed":
		statusIcon = icons.CheckMark
		if tool.EndTime != nil {
			duration := tool.EndTime.Sub(tool.StartTime)
			statusText = fmt.Sprintf("completed in %s", r.formatDuration(duration))
		} else {
			statusText = "completed"
		}
		iconColor = "success"
		statusColor = "dim"
	case "error", "failed":
		statusIcon = icons.CrossMark
		if tool.EndTime != nil {
			duration := tool.EndTime.Sub(tool.StartTime)
			statusText = fmt.Sprintf("failed after %s", r.formatDuration(duration))
		} else {
			statusText = "failed"
		}
		iconColor = "error"
		statusColor = "error"
	default:
		statusIcon = icons.BulletIcon
		statusText = tool.Status
		iconColor = "dim"
		statusColor = "dim"
	}

	argsPreview := r.formatArgsPreview(tool.Arguments)
	if argsPreview == "" || argsPreview == "{}" {
		argsPreview = ""
	}

	styledIcon := r.styleProvider.RenderWithColor(statusIcon, r.styleProvider.GetThemeColor(iconColor))
	styledStatus := r.styleProvider.RenderWithColor(statusText, r.styleProvider.GetThemeColor(statusColor))

	var singleLine string
	if argsPreview != "" {
		singleLine = fmt.Sprintf("%s %s(%s) %s", styledIcon, tool.ToolName, argsPreview, styledStatus)
	} else {
		singleLine = fmt.Sprintf("%s %s() %s", styledIcon, tool.ToolName, styledStatus)
	}

	if tool.ToolName == "Bash" && len(tool.OutputBuffer) > 0 &&
		(tool.Status == "running" || tool.Status == "starting" || tool.Status == "executing") {
		outputLines := make([]string, 0, len(tool.OutputBuffer)+2)
		outputLines = append(outputLines, singleLine)

		dimColor := r.styleProvider.GetThemeColor("dim")

		truncatedLines := tool.TotalOutputLines - len(tool.OutputBuffer)
		if truncatedLines > 0 {
			truncationText := fmt.Sprintf("  +%d more lines", truncatedLines)
			styledTruncation := r.styleProvider.RenderWithColor(truncationText, dimColor)
			outputLines = append(outputLines, styledTruncation)
		}

		for _, line := range tool.OutputBuffer {
			styledLine := r.styleProvider.RenderWithColor("  "+line, dimColor)
			outputLines = append(outputLines, styledLine)
		}
		return strings.Join(outputLines, "\n")
	}

	return singleLine
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
	case "executed", "completed":
		statusIcon = icons.CheckMark
		statusText = status
	case "error", "failed":
		statusIcon = icons.CrossMark
		statusText = status
	default:
		statusIcon = icons.BulletIcon
		statusText = status
	}

	argsPreview := r.formatArgsPreview(arguments)
	if argsPreview == "" || argsPreview == "{}" {
		argsPreview = ""
	}

	var singleLine string
	if argsPreview != "" {
		singleLine = fmt.Sprintf("%s %s(%s) %s", statusIcon, toolInfo.Name, argsPreview, statusText)
	} else {
		singleLine = fmt.Sprintf("%s %s() %s", statusIcon, toolInfo.Name, statusText)
	}

	toolNameColor := r.styleProvider.GetThemeColor("accent")
	styledLine := r.styleProvider.RenderWithColor(singleLine, toolNameColor)

	return styledLine
}

func (r *ToolCallRenderer) formatArgsPreview(args string) string {
	if args == "" {
		return ""
	}

	args = strings.TrimSpace(args)
	if len(args) > 50 {
		return args[:47] + "..."
	}
	return args
}

func (r *ToolCallRenderer) ClearPreviews() {
	r.tools = make(map[string]*ToolRenderState)
	r.toolsOrder = nil
}

func (r *ToolCallRenderer) HasActivePreviews() bool {
	for _, tool := range r.tools {
		if !tool.IsComplete {
			return true
		}
	}
	return false
}

func (r *ToolCallRenderer) hasActiveTools() bool {
	for _, tool := range r.tools {
		isRunning := tool.Status == "running" ||
			tool.Status == "starting" ||
			tool.Status == "saving" ||
			tool.Status == "executing" ||
			tool.Status == "streaming"

		if isRunning {
			return true
		}
	}
	return false
}

// formatDuration formats a duration in a human-readable way (always in seconds with 1 decimal)
func (r *ToolCallRenderer) formatDuration(d time.Duration) string {
	seconds := d.Seconds()
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	minutes := int(seconds / 60)
	remainingSeconds := seconds - float64(minutes*60)
	return fmt.Sprintf("%dm%.1fs", minutes, remainingSeconds)
}
