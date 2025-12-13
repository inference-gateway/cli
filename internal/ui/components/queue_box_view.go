package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	sdk "github.com/inference-gateway/sdk"
)

type QueueBoxView struct {
	width         int
	styleProvider *styles.Provider
}

func NewQueueBoxView(styleProvider *styles.Provider) *QueueBoxView {
	return &QueueBoxView{
		width:         80,
		styleProvider: styleProvider,
	}
}

func (qv *QueueBoxView) SetWidth(width int) {
	qv.width = width
}

func (qv *QueueBoxView) SetHeight(height int) {
}

func (qv *QueueBoxView) Render(queuedMessages []domain.QueuedMessage) string {
	if len(queuedMessages) == 0 {
		return ""
	}

	return qv.renderQueuedMessages(queuedMessages)
}

func (qv *QueueBoxView) renderQueuedMessages(queuedMessages []domain.QueuedMessage) string {
	var messageLines []string
	for _, queuedMsg := range queuedMessages {
		messageLines = append(messageLines, qv.formatQueuedMessage(queuedMsg))
	}

	return strings.Join(messageLines, "\n")
}

func (qv *QueueBoxView) formatQueuedMessage(queuedMsg domain.QueuedMessage) string {
	dimColor := qv.styleProvider.GetThemeColor("dim")
	preview := qv.formatMessagePreview(queuedMsg)

	formattedLine := fmt.Sprintf("   %s", preview)

	return qv.styleProvider.RenderWithColor(formattedLine, dimColor)
}

func (qv *QueueBoxView) formatMessagePreview(queuedMsg domain.QueuedMessage) string {
	msg := queuedMsg.Message

	if msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
		return qv.formatToolCallsPreview(*msg.ToolCalls)
	}

	contentStr, err := msg.Content.AsMessageContent0()
	if err != nil {
		contentStr = formatting.ExtractTextFromContent(msg.Content, nil)
	}

	if strings.HasPrefix(contentStr, "[A2A Task Completed:") || strings.HasPrefix(contentStr, "[A2A Task Failed:") {
		lines := strings.Split(contentStr, "\n")
		if len(lines) > 0 {
			return strings.TrimSpace(lines[0])
		}
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
		return fmt.Sprintf("%d tool calls queued", len(toolCalls))
	}

	toolCall := toolCalls[0]
	return fmt.Sprintf("Tool: %s(...)", toolCall.Function.Name)
}

func (qv *QueueBoxView) Init() tea.Cmd {
	return nil
}

func (qv *QueueBoxView) View() string {
	return ""
}

func (qv *QueueBoxView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		qv.SetWidth(windowMsg.Width)
	}
	return qv, nil
}
