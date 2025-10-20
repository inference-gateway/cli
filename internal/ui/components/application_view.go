package components

import (
	"fmt"
	"strings"

	lipgloss "github.com/charmbracelet/lipgloss"
	domain "github.com/inference-gateway/cli/internal/domain"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// ApplicationViewRenderer handles rendering of different application views
type ApplicationViewRenderer struct {
	themeService domain.ThemeService
}

// NewApplicationViewRenderer creates a new application view renderer
func NewApplicationViewRenderer(themeService domain.ThemeService) *ApplicationViewRenderer {
	return &ApplicationViewRenderer{
		themeService: themeService,
	}
}

// ChatInterfaceData holds the data needed to render the chat interface
type ChatInterfaceData struct {
	Width           int
	Height          int
	ToolExecution   *domain.ToolExecutionSession
	CurrentView     domain.ViewState
	QueuedMessages  []domain.QueuedMessage
	BackgroundTasks []domain.TaskPollingState
}

// RenderChatInterface renders the main chat interface
func (r *ApplicationViewRenderer) RenderChatInterface(
	data ChatInterfaceData,
	conversationView shared.ConversationRenderer,
	inputView shared.InputComponent,
	statusView shared.StatusComponent,
	helpBar shared.HelpBarComponent,
	queueBoxView *QueueBoxView,
) string {
	width, height := data.Width, data.Height

	headerHeight := 3
	helpBarHeight := 0
	queueBoxHeight := 0

	helpBar.SetWidth(width)
	if helpBar.IsEnabled() {
		helpBarHeight = 6
	}

	if queueBoxView != nil && (len(data.QueuedMessages) > 0 || len(data.BackgroundTasks) > 0) {
		totalItems := len(data.QueuedMessages) + len(data.BackgroundTasks)
		queueBoxHeight = totalItems + 4
		if len(data.BackgroundTasks) > 0 && len(data.QueuedMessages) > 0 {
			queueBoxHeight += 2
		}
	}

	adjustedHeight := height - headerHeight - helpBarHeight - queueBoxHeight
	conversationHeight := shared.CalculateConversationHeight(adjustedHeight)
	inputHeight := shared.CalculateInputHeight(adjustedHeight)
	statusHeight := shared.CalculateStatusHeight(adjustedHeight)

	if conversationHeight < 3 {
		conversationHeight = 3
	}

	conversationView.SetWidth(width)
	conversationView.SetHeight(conversationHeight)
	inputView.SetWidth(width)
	inputView.SetHeight(inputHeight)
	statusView.SetWidth(width)

	if queueBoxView != nil {
		queueBoxView.SetWidth(width)
	}

	headerStyle := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Foreground(lipgloss.Color(r.themeService.GetCurrentTheme().GetAccentColor())).
		Bold(true).
		Padding(0, 1)

	headerText := ""
	if len(data.BackgroundTasks) > 0 {
		headerText = fmt.Sprintf("(%d)", len(data.BackgroundTasks))
	}
	header := headerStyle.Render(headerText)
	headerBorder := ""

	conversationStyle := lipgloss.NewStyle().
		Height(conversationHeight)

	inputStyle := lipgloss.NewStyle().
		Width(width)

	conversationArea := conversationStyle.Render(conversationView.Render())
	separator := colors.CreateSeparator(width, "─")
	inputArea := inputStyle.Render(inputView.Render())

	components := []string{header, headerBorder, conversationArea, separator}

	if statusHeight > 0 {
		statusContent := statusView.Render()
		if statusContent != "" {
			components = append(components, statusContent)
		}
	}

	if queueBoxView != nil && (len(data.QueuedMessages) > 0 || len(data.BackgroundTasks) > 0) {
		queueBoxContent := queueBoxView.Render(data.QueuedMessages, data.BackgroundTasks)
		if queueBoxContent != "" {
			components = append(components, queueBoxContent)
		}
	}

	components = append(components, inputArea)

	helpBar.SetWidth(width)
	helpBarContent := helpBar.Render()
	if helpBarContent != "" {
		separator := colors.CreateSeparator(width, "─")
		components = append(components, separator)

		helpBarStyle := lipgloss.NewStyle().
			Width(width).
			Padding(1, 1)
		components = append(components, helpBarStyle.Render(helpBarContent))
	}

	return lipgloss.JoinVertical(lipgloss.Left, components...)
}

// FileSelectionData holds the data needed to render the file selection view
type FileSelectionData struct {
	Width         int
	Files         []string
	SearchQuery   string
	SelectedIndex int
}

// RenderFileSelection renders the file selection view
func (r *ApplicationViewRenderer) RenderFileSelection(
	data FileSelectionData,
	fileSelectionView *FileSelectionView,
) string {
	if len(data.Files) == 0 {
		return shared.FormatWarning("No files available for selection")
	}

	fileSelectionView.SetWidth(data.Width)

	files := r.filterFiles(data.Files, data.SearchQuery)
	selectedIndex := data.SelectedIndex
	if selectedIndex >= len(files) {
		selectedIndex = 0
	}

	return fileSelectionView.RenderView(data.Files, data.SearchQuery, selectedIndex)
}

// filterFiles filters files based on search query
func (r *ApplicationViewRenderer) filterFiles(allFiles []string, searchQuery string) []string {
	if searchQuery == "" {
		return allFiles
	}

	var files []string
	for _, file := range allFiles {
		if strings.Contains(strings.ToLower(file), strings.ToLower(searchQuery)) {
			files = append(files, file)
		}
	}
	return files
}
