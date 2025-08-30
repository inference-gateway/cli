package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	domain "github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
	"github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

type ToolCallRenderer struct {
	width        int
	height       int
	spinner      spinner.Model
	toolPreviews map[string]*domain.ToolCallPreviewEvent
	styles       *toolRenderStyles
	lastUpdate   time.Time
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
		spinner:      s,
		toolPreviews: make(map[string]*domain.ToolCallPreviewEvent),
		styles:       styles,
		width:        80,
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

	case spinner.TickMsg:
		if r.HasActivePreviews() {
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
	if len(r.toolPreviews) == 0 {
		return ""
	}

	var previews []string
	for _, preview := range r.toolPreviews {
		if r.shouldShowPreview(preview) {
			previews = append(previews, r.renderToolPreview(preview))
		}
	}

	if len(previews) == 0 {
		return ""
	}

	return strings.Join(previews, "\n")
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

func (r *ToolCallRenderer) RenderA2AToolCall(toolName, arguments, status string) string {
	toolInfo := r.parseToolName(toolName)
	return r.renderToolCallContent(toolInfo, arguments, status, true)
}

func (r *ToolCallRenderer) shouldShowPreview(preview *domain.ToolCallPreviewEvent) bool {
	return strings.HasPrefix(preview.ToolName, "a2a_") || strings.HasPrefix(preview.ToolName, "mcp_")
}

func (r *ToolCallRenderer) renderToolPreview(preview *domain.ToolCallPreviewEvent) string {
	var statusIcon string
	var statusText string
	var statusStyle lipgloss.Style

	switch preview.Status {
	case domain.ToolCallStreamStatusStreaming:
		statusIcon = r.spinner.View()
		statusText = "executing on Gateway"
		statusStyle = r.styles.statusStreaming
	case domain.ToolCallStreamStatusComplete:
		statusIcon = "✓"
		statusText = "executed on Gateway"
		statusStyle = r.styles.statusComplete
	case domain.ToolCallStreamStatusReady:
		statusIcon = "⏳"
		statusText = "ready"
		statusStyle = r.styles.statusReady
	default:
		statusIcon = "•"
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
	return r.renderToolCallContent(toolInfo, toolCall.Function.Arguments, status, false)
}

func (r *ToolCallRenderer) renderToolCallContent(toolInfo ToolInfo, arguments, status string, isA2A bool) string {
	var statusIcon string
	var statusText string

	switch status {
	case "executing":
		statusIcon = "⏳"
		statusText = status
		if isA2A {
			statusText = "executing on Gateway"
		}
	case "executed", "completed":
		statusIcon = icons.StyledCheckMark()
		statusText = status
		if isA2A {
			statusText = "executed on Gateway"
		}
	case "error", "failed":
		statusIcon = icons.StyledCrossMark()
		statusText = status
	default:
		statusIcon = "🔧"
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
	if name, found := strings.CutPrefix(toolName, "a2a_"); found {
		return ToolInfo{Name: name, Prefix: "A2A"}
	}

	if name, found := strings.CutPrefix(toolName, "mcp_"); found {
		return ToolInfo{Name: name, Prefix: "MCP"}
	}

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
}

func (r *ToolCallRenderer) HasActivePreviews() bool {
	for _, preview := range r.toolPreviews {
		if r.shouldShowPreview(preview) && !preview.IsComplete {
			return true
		}
	}
	return false
}
