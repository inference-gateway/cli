package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
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

func (qv *QueueBoxView) Render(queuedMessages []domain.QueuedMessage, backgroundTasks []domain.TaskPollingState) string {
	if len(queuedMessages) == 0 && len(backgroundTasks) == 0 {
		return ""
	}

	var sections []string

	if len(backgroundTasks) > 0 {
		sections = append(sections, qv.renderBackgroundTasks(backgroundTasks))
	}

	if len(queuedMessages) > 0 {
		sections = append(sections, qv.renderQueuedMessages(queuedMessages))
	}

	separator := strings.Repeat("─", qv.width-4)
	dimColor := qv.styleProvider.GetThemeColor("dim")
	dimSeparator := qv.styleProvider.RenderWithColor(separator, dimColor)

	contentText := strings.Join(sections, "\n"+dimSeparator+"\n")

	return qv.styleProvider.RenderBorderedBox(contentText, dimColor, 0, 1)
}

func (qv *QueueBoxView) renderBackgroundTasks(backgroundTasks []domain.TaskPollingState) string {
	accentColor := qv.styleProvider.GetThemeColor("accent")

	count := len(backgroundTasks)
	taskWord := "task"
	if count != 1 {
		taskWord = "tasks"
	}

	titleText := fmt.Sprintf("Background Tasks (%d)", count)
	hintText := fmt.Sprintf("  %d active %s running • Type /tasks to view details", count, taskWord)

	return qv.styleProvider.RenderWithColorAndBold(titleText, accentColor) + "\n" + qv.styleProvider.RenderDimText(hintText)
}

func (qv *QueueBoxView) renderQueuedMessages(queuedMessages []domain.QueuedMessage) string {
	accentColor := qv.styleProvider.GetThemeColor("accent")
	titleText := fmt.Sprintf("Queued Messages (%d)", len(queuedMessages))

	var messageLines []string
	for _, queuedMsg := range queuedMessages {
		messageLines = append(messageLines, qv.formatQueuedMessage(queuedMsg))
	}

	return qv.styleProvider.RenderWithColorAndBold(titleText, accentColor) + "\n" + strings.Join(messageLines, "\n")
}

func (qv *QueueBoxView) formatQueuedMessage(queuedMsg domain.QueuedMessage) string {
	accentColor := qv.styleProvider.GetThemeColor("accent")
	dimColor := qv.styleProvider.GetThemeColor("dim")
	preview := qv.formatMessagePreview(queuedMsg)

	formattedLine := fmt.Sprintf("  %s %s",
		qv.styleProvider.RenderWithColor("→", accentColor),
		qv.styleProvider.RenderWithColor(preview, dimColor),
	)

	return formattedLine
}

func (qv *QueueBoxView) formatMessagePreview(queuedMsg domain.QueuedMessage) string {
	msg := queuedMsg.Message

	content := msg.Content

	maxPreviewLength := qv.width - 20
	if maxPreviewLength < 20 {
		maxPreviewLength = 20
	}

	preview := strings.ReplaceAll(content, "\n", " ")
	preview = strings.TrimSpace(preview)

	if len(preview) > maxPreviewLength {
		preview = preview[:maxPreviewLength-3] + "..."
	}

	wrappedPreview := shared.WrapText(preview, maxPreviewLength)

	return wrappedPreview
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
