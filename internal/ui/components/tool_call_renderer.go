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
	width              int
	height             int
	spinner            spinner.Model
	toolPreviews       map[string]*domain.ToolCallPreviewEvent
	toolPreviewsOrder  []string
	styleProvider      *styles.Provider
	lastUpdate         time.Time
	parallelTools      map[string]*ParallelToolState
	parallelToolsOrder []string
	spinnerStep        int
}

type ParallelToolState struct {
	CallID       string
	ToolName     string
	Status       string
	NextStatus   string
	StartTime    time.Time
	EndTime      *time.Time
	LastUpdate   time.Time
	MinShowTime  time.Duration
	OutputBuffer []string
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
		toolPreviews:  make(map[string]*domain.ToolCallPreviewEvent),
		parallelTools: make(map[string]*ParallelToolState),
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
	r.updateArgsContainerWidth()
}

func (r *ToolCallRenderer) handleToolCallPreview(msg domain.ToolCallPreviewEvent) (*ToolCallRenderer, tea.Cmd) {
	if _, exists := r.toolPreviews[msg.ToolCallID]; !exists {
		r.toolPreviewsOrder = append(r.toolPreviewsOrder, msg.ToolCallID)
	}
	r.toolPreviews[msg.ToolCallID] = &msg
	if len(r.toolPreviews) == 1 && r.HasActivePreviews() {
		return r, r.spinner.Tick
	}
	return r, nil
}

func (r *ToolCallRenderer) handleToolCallUpdate(msg domain.ToolCallUpdateEvent) (*ToolCallRenderer, tea.Cmd) {
	if preview, exists := r.toolPreviews[msg.ToolCallID]; exists {
		if time.Since(r.lastUpdate) < constants.ToolCallUpdateThrottle {
			return r, nil
		}
		preview.Arguments = msg.Arguments
		preview.Status = msg.Status
		if msg.Status == domain.ToolCallStreamStatusComplete {
			preview.IsComplete = true
		}
		r.lastUpdate = time.Now()
	}
	return r, nil
}

func (r *ToolCallRenderer) handleParallelToolsStart(msg domain.ParallelToolsStartEvent) (*ToolCallRenderer, tea.Cmd) {
	for _, tool := range msg.Tools {
		now := time.Now()
		if _, exists := r.parallelTools[tool.CallID]; !exists {
			r.parallelToolsOrder = append(r.parallelToolsOrder, tool.CallID)
		}
		r.parallelTools[tool.CallID] = &ParallelToolState{
			CallID:      tool.CallID,
			ToolName:    tool.Name,
			Status:      "queued",
			NextStatus:  "",
			StartTime:   now,
			LastUpdate:  now,
			MinShowTime: constants.ToolCallMinShowTime,
		}
	}
	return r, r.spinner.Tick
}

func (r *ToolCallRenderer) handleToolExecutionProgress(msg domain.ToolExecutionProgressEvent) (*ToolCallRenderer, tea.Cmd) {
	if state, exists := r.parallelTools[msg.ToolCallID]; exists {
		state.Status = msg.Status
		if msg.Status == "complete" || msg.Status == "failed" {
			endTime := time.Now()
			state.EndTime = &endTime
		}
		if r.hasActiveParallelTools() {
			return r, r.spinner.Tick
		}
	}
	return r, nil
}

func (r *ToolCallRenderer) handleBashOutputStream(msg domain.BashOutputChunkEvent) (*ToolCallRenderer, tea.Cmd) {
	if state, exists := r.parallelTools[msg.ToolCallID]; exists {
		// Add output to the buffer (limit to last 10 lines for display)
		state.OutputBuffer = append(state.OutputBuffer, msg.Output)
		if len(state.OutputBuffer) > 10 {
			state.OutputBuffer = state.OutputBuffer[len(state.OutputBuffer)-10:]
		}
		state.LastUpdate = time.Now()
		if r.hasActiveParallelTools() {
			return r, r.spinner.Tick
		}
	}
	return r, nil
}

func (r *ToolCallRenderer) handleSpinnerTick(msg spinner.TickMsg) (*ToolCallRenderer, tea.Cmd) {
	r.spinnerStep = (r.spinnerStep + 1) % 4
	hasActivePreviews := r.HasActivePreviews()
	hasActiveTools := r.hasActiveParallelTools()
	if hasActivePreviews || hasActiveTools {
		var cmd tea.Cmd
		r.spinner, cmd = r.spinner.Update(msg)
		return r, cmd
	}
	return r, nil
}

func (r *ToolCallRenderer) SetWidth(width int) {
	r.width = width
	r.updateArgsContainerWidth()
}

func (r *ToolCallRenderer) updateArgsContainerWidth() {
	// Width is now handled dynamically by styleProvider methods
}

func (r *ToolCallRenderer) RenderPreviews() string {
	var allPreviews []string

	for _, callID := range r.toolPreviewsOrder {
		preview, exists := r.toolPreviews[callID]
		if !exists {
			continue
		}
		if r.shouldShowPreview(preview) {
			allPreviews = append(allPreviews, r.renderToolPreview(preview))
		}
	}

	now := time.Now()
	var remainingTools []string
	for _, callID := range r.parallelToolsOrder {
		tool, exists := r.parallelTools[callID]
		if !exists {
			continue
		}

		if (tool.Status == "complete" || tool.Status == "failed") && tool.EndTime != nil {
			showDuration := now.Sub(*tool.EndTime)
			if showDuration > 1000*time.Millisecond {
				delete(r.parallelTools, callID)
				continue
			}
		}

		allPreviews = append(allPreviews, r.renderParallelTool(tool))
		remainingTools = append(remainingTools, callID)
	}
	r.parallelToolsOrder = remainingTools

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
	var colorName string

	switch preview.Status {
	case domain.ToolCallStreamStatusStreaming:
		statusIcon = icons.GetSpinnerFrame(r.spinnerStep)
		statusText = "executing"
		colorName = "spinner"
	case domain.ToolCallStreamStatusComplete:
		statusIcon = icons.CheckMark
		statusText = "completed"
		colorName = "success"
	case domain.ToolCallStreamStatusReady:
		statusIcon = icons.QueuedIcon
		statusText = "ready"
		colorName = "dim"
	default:
		statusIcon = icons.BulletIcon
		statusText = "unknown"
		colorName = "dim"
	}

	toolInfo := r.parseToolName(preview.ToolName)
	argsPreview := r.formatArgsPreview(preview.Arguments)

	statusPart := r.styleProvider.RenderWithColor(fmt.Sprintf("%s %s:%s", statusIcon, toolInfo.Prefix, toolInfo.Name), r.styleProvider.GetThemeColor(colorName))
	metaPart := r.styleProvider.RenderDimText(fmt.Sprintf(" (%s)", statusText))
	header := statusPart + metaPart

	if argsPreview != "" {
		argsPart := r.styleProvider.RenderDimText(fmt.Sprintf("  %s", argsPreview))
		return r.styleProvider.JoinVertical(header, argsPart)
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

func (r *ToolCallRenderer) ClearPreviews() {
	r.toolPreviews = make(map[string]*domain.ToolCallPreviewEvent)
	r.toolPreviewsOrder = nil
	r.parallelTools = make(map[string]*ParallelToolState)
	r.parallelToolsOrder = nil
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

// formatDuration formats a duration in a human-readable way
func (r *ToolCallRenderer) formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func (r *ToolCallRenderer) renderParallelTool(tool *ParallelToolState) string {
	var statusIcon string
	var statusText string
	var colorName string

	switch tool.Status {
	case "queued":
		statusIcon = icons.QueuedIcon
		statusText = "queued"
		colorName = "dim"
	case "running", "starting", "saving":
		statusIcon = icons.GetSpinnerFrame(r.spinnerStep)
		elapsed := time.Since(tool.StartTime)
		statusText = fmt.Sprintf("running %s", r.formatDuration(elapsed))
		colorName = "spinner"
	case "complete":
		statusIcon = icons.CheckMark
		if tool.EndTime != nil {
			duration := tool.EndTime.Sub(tool.StartTime)
			statusText = fmt.Sprintf("completed in %s", r.formatDuration(duration))
		} else {
			statusText = "completed"
		}
		colorName = "success"
	case "failed":
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

	// For Bash tools, show streaming output if available
	if tool.ToolName == "Bash" && len(tool.OutputBuffer) > 0 {
		var outputLines []string
		outputLines = append(outputLines, header)

		// Show last few lines of output with indentation
		for _, line := range tool.OutputBuffer {
			truncatedLine := line
			maxLineLen := r.width - 6 // Account for indentation
			if maxLineLen < 20 {
				maxLineLen = 20
			}
			if len(truncatedLine) > maxLineLen {
				truncatedLine = truncatedLine[:maxLineLen-3] + "..."
			}
			outputLines = append(outputLines, r.styleProvider.RenderDimText("    "+truncatedLine))
		}

		return strings.Join(outputLines, "\n")
	}

	return header
}
