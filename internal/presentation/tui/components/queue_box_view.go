package components

import (
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

type QueueBoxView struct {
	width         int
	styleProvider *styles.Provider
	toolFormatter tui.ToolFormatter
}

func NewQueueBoxView(styleProvider *styles.Provider) *QueueBoxView {
	return &QueueBoxView{
		width:         80,
		styleProvider: styleProvider,
	}
}

// SetToolFormatter wires the shared tool formatter so queued tool calls show their
// width-aware argument preview instead of a bare "Name(...)".
func (qv *QueueBoxView) SetToolFormatter(f tui.ToolFormatter) {
	qv.toolFormatter = f
}

func (qv *QueueBoxView) SetWidth(width int) {
	qv.width = width
}

func (qv *QueueBoxView) SetHeight(height int) {
}

func (qv *QueueBoxView) Render(queuedMessages []convdomain.QueuedMessage) string {
	if len(queuedMessages) == 0 {
		return ""
	}

	return qv.renderQueuedMessages(queuedMessages)
}

func (qv *QueueBoxView) renderQueuedMessages(queuedMessages []convdomain.QueuedMessage) string {
	var messageLines []string
	for _, queuedMsg := range queuedMessages {
		messageLines = append(messageLines, qv.formatQueuedMessage(queuedMsg))
	}

	return strings.Join(messageLines, "\n")
}

// jobResultHeaderRe matches the "[<Kind> Completed/Failed: <label>]" header
// Supervisor.formatResult writes on every finished job's queue note.
var jobResultHeaderRe = regexp.MustCompile(`^\[[A-Za-z0-9 ]+ (Completed|Failed): `)

// queueSourceMarker maps a queued message's origin to its display tag so a
// typed message reads differently from an agent or job result.
func queueSourceMarker(source convdomain.QueuedMessageSource) string {
	switch source {
	case convdomain.QueueSourceComposer:
		return "[You]"
	case convdomain.QueueSourceStdin:
		return "[Stdin]"
	case convdomain.QueueSourceA2A:
		return "[A2A]"
	case convdomain.QueueSourceShell:
		return "[Shell]"
	case convdomain.QueueSourceSubagent:
		return "[Subagent]"
	default:
		return "[Job]"
	}
}

// isJobSource reports whether the entry came from a supervisor job (result or
// note) rather than a human.
func isJobSource(source convdomain.QueuedMessageSource) bool {
	switch source {
	case convdomain.QueueSourceA2A, convdomain.QueueSourceShell, convdomain.QueueSourceSubagent, convdomain.QueueSourceJob:
		return true
	default:
		return false
	}
}

func (qv *QueueBoxView) formatQueuedMessage(queuedMsg convdomain.QueuedMessage) string {
	dimColor := qv.styleProvider.GetThemeColor("dim")
	preview := qv.formatMessagePreview(queuedMsg)

	formattedLine := fmt.Sprintf("   %s %s", queueSourceMarker(queuedMsg.Source), preview)

	return qv.styleProvider.RenderWithColor(formattedLine, dimColor)
}

func (qv *QueueBoxView) formatMessagePreview(queuedMsg convdomain.QueuedMessage) string {
	msg := queuedMsg.Message

	if msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
		return qv.formatToolCallsPreview(*msg.ToolCalls)
	}

	contentStr, err := msg.Content.AsMessageContent0()
	if err != nil {
		contentStr = formatting.ExtractTextFromContent(msg.Content, nil)
	}

	if isJobSource(queuedMsg.Source) && jobResultHeaderRe.MatchString(contentStr) {
		header, _, _ := strings.Cut(contentStr, "\n")
		return strings.TrimSpace(header)
	}

	content := contentStr

	maxPreviewLength := qv.width - 20
	if maxPreviewLength < 20 {
		maxPreviewLength = 20
	}

	preview := strings.ReplaceAll(content, "\n", " ")
	preview = strings.TrimSpace(preview)

	if len(preview) > maxPreviewLength {
		preview = preview[:maxPreviewLength-3] + "..."
	}

	wrappedPreview := formatting.WrapText(preview, maxPreviewLength)

	return wrappedPreview
}

func (qv *QueueBoxView) formatToolCallsPreview(toolCalls []sdk.ChatCompletionMessageToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}

	if len(toolCalls) > 1 {
		return fmt.Sprintf("%d tools", len(toolCalls))
	}

	toolCall := toolCalls[0]
	if qv.toolFormatter != nil {
		return qv.toolFormatter.RenderToolSummary("", toolCall.Function.Name, parseToolArgs(toolCall.Function.Arguments), "", qv.width)
	}
	return fmt.Sprintf("%s(...)", toolCall.Function.Name)
}

func (qv *QueueBoxView) Init() tea.Cmd {
	return nil
}

func (qv *QueueBoxView) View() tea.View {
	return tea.NewView("")
}

func (qv *QueueBoxView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		qv.SetWidth(windowMsg.Width)
	}
	return qv, nil
}
