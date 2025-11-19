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
	CallID      string
	ToolName    string
	Status      string
	NextStatus  string
	StartTime   time.Time
	EndTime     *time.Time
	LastUpdate  time.Time
	MinShowTime time.Duration
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

func (r *ToolCallRenderer) Update(msg tea.Msg) (*ToolCallRenderer, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width = msg.Width
		r.height = msg.Height
		r.updateArgsContainerWidth()

	case domain.ToolCallPreviewEvent:
		if _, exists := r.toolPreviews[msg.ToolCallID]; !exists {
			r.toolPreviewsOrder = append(r.toolPreviewsOrder, msg.ToolCallID)
		}
		r.toolPreviews[msg.ToolCallID] = &msg
		if len(r.toolPreviews) == 1 && r.HasActivePreviews() {
			return r, r.spinner.Tick
		}

	case domain.ToolCallUpdateEvent:
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

	case domain.ToolCallReadyEvent:
		r.ClearPreviews()

	case domain.ChatCompleteEvent:
		r.ClearPreviews()

	case domain.ParallelToolsStartEvent:
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

	case domain.ToolExecutionProgressEvent:
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
		statusText = "executing"
		colorName = "spinner"
	case "complete":
		statusIcon = icons.CheckMark
		statusText = "completed"
		colorName = "success"
	case "failed":
		statusIcon = icons.CrossMark
		statusText = "failed"
		colorName = "error"
	default:
		statusIcon = icons.BulletIcon
		statusText = tool.Status
		colorName = "dim"
	}

	toolInfo := r.parseToolName(tool.ToolName)

	statusPart := r.styleProvider.RenderWithColor(fmt.Sprintf("%s %s:%s", statusIcon, toolInfo.Prefix, toolInfo.Name), r.styleProvider.GetThemeColor(colorName))
	metaPart := r.styleProvider.RenderDimText(fmt.Sprintf(" (%s)", statusText))

	return statusPart + metaPart
}
