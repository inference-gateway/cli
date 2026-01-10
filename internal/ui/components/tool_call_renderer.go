package components

import (
	"fmt"
	"strings"
	"time"

	spinner "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
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
	CallID       string
	ToolName     string
	Status       string
	Arguments    string
	StartTime    time.Time
	EndTime      *time.Time
	LastUpdate   time.Time
	OutputBuffer []string
	IsComplete   bool
}

type ToolInfo struct {
	Name   string
	Prefix string
}

func NewToolCallRenderer(styleProvider *styles.Provider) *ToolCallRenderer {
	s := spinner.New()
	s.Spinner = spinner.Dot

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

	case domain.ParallelToolsCompleteEvent:
		r.ClearPreviews()

	case domain.ParallelToolsStartEvent:
		return r.handleParallelToolsStart(msg)

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

func (r *ToolCallRenderer) handleParallelToolsStart(msg domain.ParallelToolsStartEvent) (*ToolCallRenderer, tea.Cmd) {
	for _, tool := range msg.Tools {
		now := time.Now()
		if _, exists := r.tools[tool.CallID]; !exists {
			r.toolsOrder = append(r.toolsOrder, tool.CallID)
		}
		r.tools[tool.CallID] = &ToolRenderState{
			CallID:     tool.CallID,
			ToolName:   tool.Name,
			Status:     tool.Status,
			StartTime:  now,
			LastUpdate: now,
		}
	}
	return r, r.spinner.Tick
}

func (r *ToolCallRenderer) handleToolExecutionProgress(msg domain.ToolExecutionProgressEvent) (*ToolCallRenderer, tea.Cmd) {
	if state, exists := r.tools[msg.ToolCallID]; exists {
		state.Status = msg.Status
		state.LastUpdate = time.Now()
		if msg.Status == "completed" || msg.Status == "failed" {
			endTime := time.Now()
			state.EndTime = &endTime
			state.IsComplete = true
		}
		if r.hasActiveTools() {
			return r, r.spinner.Tick
		}
	}
	return r, nil
}

func (r *ToolCallRenderer) handleBashOutputStream(msg domain.BashOutputChunkEvent) (*ToolCallRenderer, tea.Cmd) {
	if state, exists := r.tools[msg.ToolCallID]; exists {
		state.OutputBuffer = append(state.OutputBuffer, msg.Output)
		if len(state.OutputBuffer) > 10 {
			state.OutputBuffer = state.OutputBuffer[len(state.OutputBuffer)-10:]
		}
		state.LastUpdate = time.Now()
		if r.hasActiveTools() {
			return r, r.spinner.Tick
		}
	}
	return r, nil
}

func (r *ToolCallRenderer) handleSpinnerTick(msg spinner.TickMsg) (*ToolCallRenderer, tea.Cmd) {
	now := time.Now()

	hasRecentBashOutput := false
	for _, tool := range r.tools {
		if tool.ToolName == "Bash" && now.Sub(tool.LastUpdate) < 200*time.Millisecond {
			hasRecentBashOutput = true
			break
		}
	}

	if !hasRecentBashOutput && now.Sub(r.lastTimerRender) < constants.TimerUpdateThrottle {
		if r.hasActiveTools() {
			var cmd tea.Cmd
			r.spinner, cmd = r.spinner.Update(msg)
			return r, cmd
		}
		return r, nil
	}

	r.spinnerStep = (r.spinnerStep + 1) % 4
	r.lastTimerRender = now
	if r.hasActiveTools() {
		var cmd tea.Cmd
		r.spinner, cmd = r.spinner.Update(msg)
		return r, cmd
	}
	return r, nil
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
	var colorName string

	switch tool.Status {
	case "queued", "ready":
		statusIcon = icons.QueuedIcon
		statusText = "queued"
		colorName = "dim"
	case "running", "starting", "saving", "executing", "streaming":
		statusIcon = icons.GetSpinnerFrame(r.spinnerStep)
		if tool.EndTime == nil {
			elapsed := time.Since(tool.StartTime)
			statusText = fmt.Sprintf("running %s", r.formatDuration(elapsed))
		} else {
			statusText = "executing"
		}
		colorName = "spinner"
	case "completed", "executed":
		statusIcon = icons.CheckMark
		if tool.EndTime != nil {
			duration := tool.EndTime.Sub(tool.StartTime)
			statusText = fmt.Sprintf("completed in %s", r.formatDuration(duration))
		} else {
			statusText = "completed"
		}
		colorName = "success"
	case "error", "failed":
		statusIcon = icons.CrossMark
		if tool.EndTime != nil {
			duration := tool.EndTime.Sub(tool.StartTime)
			statusText = fmt.Sprintf("failed after %s", r.formatDuration(duration))
		} else {
			statusText = "failed"
		}
		colorName = "error"
	default:
		statusIcon = icons.BulletIcon
		statusText = tool.Status
		colorName = "dim"
	}

	toolInfo := r.parseToolName(tool.ToolName)

	statusPart := r.styleProvider.RenderWithColor(fmt.Sprintf("%s %s:%s", statusIcon, toolInfo.Prefix, toolInfo.Name), r.styleProvider.GetThemeColor(colorName))
	metaPart := r.styleProvider.RenderDimText(fmt.Sprintf(" (%s)", statusText))
	header := statusPart + metaPart

	var parts []string
	parts = append(parts, header)

	if tool.Arguments != "" && tool.Arguments != "{}" {
		argsPreview := r.formatArgsPreview(tool.Arguments)
		if argsPreview != "" {
			argsPart := r.styleProvider.RenderDimText(fmt.Sprintf("  %s", argsPreview))
			parts = append(parts, argsPart)
		}
	}

	if tool.ToolName == "Bash" && len(tool.OutputBuffer) > 0 {
		indicator := r.styleProvider.RenderDimText(fmt.Sprintf("    [showing last %d lines]", len(tool.OutputBuffer)))
		parts = append(parts, indicator)

		for _, line := range tool.OutputBuffer {
			truncatedLine := line
			maxLineLen := r.width - 6
			if maxLineLen < 20 {
				maxLineLen = 20
			}
			if len(truncatedLine) > maxLineLen {
				truncatedLine = truncatedLine[:maxLineLen-3] + "..."
			}
			parts = append(parts, r.styleProvider.RenderDimText("    "+truncatedLine))
		}
	}

	isRunning := tool.Status == "running" || tool.Status == "starting" || tool.Status == "saving" || tool.Status == "executing" || tool.Status == "streaming"
	if tool.ToolName == "Bash" && isRunning {
		hint := r.getBashRunningHint()
		parts = append(parts, "  "+hint)
	}

	return strings.Join(parts, "\n")
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

	toolNameColor := r.styleProvider.GetThemeColor("accent")
	var header string
	if toolInfo.Prefix != "TOOL" {
		prefixPart := r.styleProvider.RenderWithColorAndBold(toolInfo.Prefix, toolNameColor)
		namePart := r.styleProvider.RenderWithColorAndBold(toolInfo.Name, toolNameColor)
		metaPart := r.styleProvider.RenderDimText(statusText)
		header = fmt.Sprintf("%s %s:%s (%s)", statusIcon, prefixPart, namePart, metaPart)
	} else {
		namePart := r.styleProvider.RenderWithColorAndBold(toolInfo.Name, toolNameColor)
		metaPart := r.styleProvider.RenderDimText(statusText)
		header = fmt.Sprintf("%s %s (%s)", statusIcon, namePart, metaPart)
	}

	if arguments != "" && arguments != "{}" {
		args := strings.TrimSpace(arguments)
		if len(args) > 200 {
			args = args[:197] + "..."
		}

		formattedArgs := r.styleProvider.RenderDimText(args)
		return r.styleProvider.JoinVertical(header, formattedArgs)
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

// getBashRunningHint returns the hint text for running Bash commands with dynamic keybinding support
func (r *ToolCallRenderer) getBashRunningHint() string {
	cancelHint := "Press esc to interrupt"
	backgroundHint := "Press ctrl+b to background"

	if r.keyHintFormatter != nil {
		cancelActionID := config.ActionID(config.NamespaceGlobal, "cancel")
		if hint := r.keyHintFormatter.GetKeyHint(cancelActionID, "interrupt"); hint != "" {
			cancelHint = hint
		}

		backgroundActionID := config.ActionID(config.NamespaceTools, "background_shell")
		if hint := r.keyHintFormatter.GetKeyHint(backgroundActionID, "background"); hint != "" {
			backgroundHint = hint
		}
	}

	hintText := fmt.Sprintf("%s, %s", cancelHint, backgroundHint)
	return r.styleProvider.RenderDimText("• " + hintText)
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
