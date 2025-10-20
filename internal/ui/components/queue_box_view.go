package components

import (
	"fmt"
	"strings"

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

	var sections []string

	if len(backgroundTasks) > 0 {
		sections = append(sections, qv.renderBackgroundTasks(backgroundTasks))
	}

	if len(queuedMessages) > 0 {
		sections = append(sections, qv.renderQueuedMessages(queuedMessages))
	}

	separator := strings.Repeat("─", qv.width-4)
	dimSeparator := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colors.DimColor.Lipgloss)).
		Render(separator)

	contentText := strings.Join(sections, "\n"+dimSeparator+"\n")

	boxStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colors.DimColor.Lipgloss))

	return boxStyle.Render(contentText)
}

func (qv *QueueBoxView) renderBackgroundTasks(backgroundTasks []domain.TaskPollingState) string {
	accentColor := qv.getAccentColor()
	dimColor := qv.getDimColor()

	count := len(backgroundTasks)
	taskWord := "task"
	if count != 1 {
		taskWord = "tasks"
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(accentColor)).
		Bold(true)

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(dimColor)).
		Italic(true)

	titleText := fmt.Sprintf("Background Tasks (%d)", count)
	hintText := fmt.Sprintf("  %d active %s running • Type /tasks to view details", count, taskWord)

	return titleStyle.Render(titleText) + "\n" + hintStyle.Render(hintText)
}

func (qv *QueueBoxView) renderQueuedMessages(queuedMessages []domain.QueuedMessage) string {
	accentColor := qv.getAccentColor()
	titleText := fmt.Sprintf("Queued Messages (%d)", len(queuedMessages))
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(accentColor)).
		Bold(true)

	var messageLines []string
	for _, queuedMsg := range queuedMessages {
		messageLines = append(messageLines, qv.formatQueuedMessage(queuedMsg))
	}

	return titleStyle.Render(titleText) + "\n" + strings.Join(messageLines, "\n")
}

func (qv *QueueBoxView) formatQueuedMessage(queuedMsg domain.QueuedMessage) string {
	accentColor := qv.getAccentColor()
	preview := qv.formatMessagePreview(queuedMsg)

	arrowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))
	previewStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.QueuedMessageColor.Lipgloss))

	formattedLine := fmt.Sprintf("  %s %s",
		arrowStyle.Render("→"),
		previewStyle.Render(preview),
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

func (qv *QueueBoxView) getAccentColor() string {
	if qv.themeService != nil {
		return qv.themeService.GetCurrentTheme().GetAccentColor()
	}
	return colors.AccentColor.Lipgloss
}

func (qv *QueueBoxView) getDimColor() string {
	return colors.DimColor.Lipgloss
}
