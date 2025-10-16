package components

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
	domain "github.com/inference-gateway/cli/internal/domain"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
)

type QueueBoxView struct {
	width        int
	themeService domain.ThemeService
}

func NewQueueBoxView(themeService domain.ThemeService) *QueueBoxView {
	return &QueueBoxView{
		width:        80,
		themeService: themeService,
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
	warningColor := qv.getWarningColor()

	var sections []string

	if len(backgroundTasks) > 0 {
		titleText := fmt.Sprintf("Background Tasks (%d)", len(backgroundTasks))
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(warningColor)).
			Bold(true)

		var taskLines []string
		for i, task := range backgroundTasks {
			elapsed := time.Since(task.StartedAt).Round(time.Second)
			line := fmt.Sprintf("  %d. 🔧 %s - %s (running %v)", i+1, task.AgentURL, task.TaskID[:8], elapsed)
			taskLines = append(taskLines, line)
		}

		sections = append(sections, titleStyle.Render(titleText)+"\n"+strings.Join(taskLines, "\n"))
	}

	if len(queuedMessages) > 0 {
		queuedColor := qv.getQueuedMessageColor()
		titleText := fmt.Sprintf("Queued Messages (%d)", len(queuedMessages))
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(queuedColor))

		var messageLines []string
		for i, queuedMsg := range queuedMessages {
			preview := qv.formatMessagePreview(queuedMsg)
			line := fmt.Sprintf("  %d. %s", i+1, preview)
			messageLines = append(messageLines, line)
		}

		sections = append(sections, titleStyle.Render(titleText)+"\n"+strings.Join(messageLines, "\n"))
	}

	contentText := strings.Join(sections, "\n\n")

	queuedColor := qv.getQueuedMessageColor()
	boxStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color(queuedColor))

	return boxStyle.Render(contentText)
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

func (qv *QueueBoxView) getWarningColor() string {
	return colors.WarningColor.ANSI
}

func (qv *QueueBoxView) getQueuedMessageColor() string {
	if qv.themeService != nil {
		return colors.QueuedMessageColor.Lipgloss
	}
	return colors.QueuedMessageColor.Lipgloss
}
