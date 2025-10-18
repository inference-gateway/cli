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
	titleText := fmt.Sprintf("Background Tasks (%d)", len(backgroundTasks))
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(accentColor)).
		Bold(true)

	var taskLines []string
	var lastContextID string

	for i, task := range backgroundTasks {
		if task.ContextID != lastContextID && task.ContextID != "" {
			taskLines = append(taskLines, qv.formatContextHeader(task.ContextID))
			lastContextID = task.ContextID
		}
		taskLines = append(taskLines, qv.formatBackgroundTask(i+1, task))
	}

	return titleStyle.Render(titleText) + "\n" + strings.Join(taskLines, "\n")
}

func (qv *QueueBoxView) formatContextHeader(contextID string) string {
	dimColor := qv.getDimColor()
	shortContextID := contextID
	if len(contextID) > 12 {
		shortContextID = contextID[:12] + "..."
	}

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(dimColor)).
		Italic(true)

	return headerStyle.Render(fmt.Sprintf("    Context: %s", shortContextID))
}

func (qv *QueueBoxView) renderQueuedMessages(queuedMessages []domain.QueuedMessage) string {
	accentColor := qv.getAccentColor()
	titleText := fmt.Sprintf("Queued Messages (%d)", len(queuedMessages))
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(accentColor)).
		Bold(true)

	var messageLines []string
	for i, queuedMsg := range queuedMessages {
		messageLines = append(messageLines, qv.formatQueuedMessage(i+1, queuedMsg))
	}

	return titleStyle.Render(titleText) + "\n" + strings.Join(messageLines, "\n")
}

func (qv *QueueBoxView) formatBackgroundTask(index int, task domain.TaskPollingState) string {
	elapsed := time.Since(task.StartedAt).Round(time.Second)

	agentName := qv.extractAgentName(task.AgentURL)
	taskDesc := qv.extractTaskDescription(task)

	accentColor := qv.getAccentColor()
	dimColor := qv.getDimColor()

	indexStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))
	arrowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))
	agentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.AssistantColor.Lipgloss))

	formattedLine := fmt.Sprintf("%s Task %s %s (%s) %s",
		indexStyle.Render(fmt.Sprintf("      %d.", index)),
		arrowStyle.Render("→"),
		agentStyle.Render(agentName),
		timeStyle.Render(qv.formatElapsed(elapsed)),
		descStyle.Render(taskDesc),
	)

	return formattedLine
}

func (qv *QueueBoxView) formatQueuedMessage(index int, queuedMsg domain.QueuedMessage) string {
	dimColor := qv.getDimColor()
	accentColor := qv.getAccentColor()
	preview := qv.formatMessagePreview(queuedMsg)

	indexStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))
	arrowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))
	previewStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.QueuedMessageColor.Lipgloss))

	formattedLine := fmt.Sprintf("%s %s %s",
		indexStyle.Render(fmt.Sprintf("  %d.", index)),
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

func (qv *QueueBoxView) extractAgentName(agentURL string) string {
	parts := strings.Split(agentURL, "://")
	if len(parts) < 2 {
		return agentURL
	}

	hostPort := parts[1]
	host := strings.Split(hostPort, ":")[0]

	return host
}

func (qv *QueueBoxView) extractTaskDescription(task domain.TaskPollingState) string {
	if task.TaskDescription != "" {
		maxLength := 50
		if qv.width > 100 {
			maxLength = 80
		}

		desc := task.TaskDescription
		if len(desc) > maxLength {
			return desc[:maxLength-3] + "..."
		}
		return desc
	}

	if len(task.TaskID) > 8 {
		return task.TaskID[:8]
	}
	return task.TaskID
}

func (qv *QueueBoxView) formatElapsed(elapsed time.Duration) string {
	if elapsed < time.Minute {
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	}
	if elapsed < time.Hour {
		return fmt.Sprintf("%dm%ds", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(elapsed.Hours()), int(elapsed.Minutes())%60)
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
