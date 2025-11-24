package components

import (
	"fmt"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// ApplicationViewRenderer handles rendering of different application views
type ApplicationViewRenderer struct {
	styleProvider *styles.Provider
}

// NewApplicationViewRenderer creates a new application view renderer
func NewApplicationViewRenderer(styleProvider *styles.Provider) *ApplicationViewRenderer {
	return &ApplicationViewRenderer{
		styleProvider: styleProvider,
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
	todoBoxView *TodoBoxView,
) string {
	width, height := data.Width, data.Height

	headerHeight := 3
	helpBarHeight := 0
	queueBoxHeight := 0
	todoBoxHeight := 0

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

	// Calculate todo box height
	if todoBoxView != nil && todoBoxView.HasTodos() {
		todoBoxHeight = todoBoxView.GetHeight()
	}

	adjustedHeight := height - headerHeight - helpBarHeight - queueBoxHeight - todoBoxHeight
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

	if todoBoxView != nil {
		todoBoxView.SetWidth(width)
	}

	headerText := ""
	if len(data.BackgroundTasks) > 0 {
		headerText = fmt.Sprintf("(%d)", len(data.BackgroundTasks))
	}
	accentColor := r.styleProvider.GetThemeColor("accent")
	header := r.styleProvider.RenderCenteredBoldWithColor(headerText, accentColor, width)
	headerBorder := ""

	conversationArea := conversationView.Render()
	separator := strings.Repeat("─", width)
	inputArea := inputView.Render()

	components := []string{header, headerBorder, conversationArea}

	if queueBoxView != nil && (len(data.QueuedMessages) > 0 || len(data.BackgroundTasks) > 0) {
		queueBoxContent := queueBoxView.Render(data.QueuedMessages, data.BackgroundTasks)
		if queueBoxContent != "" {
			components = append(components, queueBoxContent)
		}
	}

	if todoBoxView != nil && todoBoxView.HasTodos() {
		todoBoxContent := todoBoxView.Render()
		if todoBoxContent != "" {
			components = append(components, todoBoxContent)
		}
	}

	components = append(components, separator)

	if statusHeight > 0 {
		statusContent := statusView.Render()
		if statusContent != "" {
			components = append(components, statusContent)
		}
	}

	components = append(components, inputArea)

	helpBar.SetWidth(width)
	helpBarContent := helpBar.Render()
	if helpBarContent != "" {
		separator := strings.Repeat("─", width)
		components = append(components, separator)
		components = append(components, helpBarContent)
	}

	return strings.Join(components, "\n")
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
