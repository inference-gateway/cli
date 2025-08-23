package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// ApplicationViewRenderer handles rendering of different application views
type ApplicationViewRenderer struct {
	theme shared.Theme
}

// NewApplicationViewRenderer creates a new application view renderer
func NewApplicationViewRenderer(theme shared.Theme) *ApplicationViewRenderer {
	return &ApplicationViewRenderer{
		theme: theme,
	}
}

// ChatInterfaceData holds the data needed to render the chat interface
type ChatInterfaceData struct {
	Width, Height   int
	ToolExecution   *domain.ToolExecutionSession
	ApprovalUIState *domain.ApprovalUIState
	CurrentView     domain.ViewState
}

// RenderChatInterface renders the main chat interface
func (r *ApplicationViewRenderer) RenderChatInterface(
	data ChatInterfaceData,
	conversationView shared.ConversationRenderer,
	inputView shared.InputComponent,
	statusView shared.StatusComponent,
	helpBar shared.HelpBarComponent,
	approvalView shared.ApprovalComponent,
) string {
	width, height := data.Width, data.Height

	headerHeight := 3
	helpBarHeight := 0

	helpBar.SetWidth(width)
	if helpBar.IsEnabled() {
		helpBarHeight = 6
	}

	adjustedHeight := height - headerHeight - helpBarHeight
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

	headerStyle := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Foreground(shared.HeaderColor.GetLipglossColor()).
		Bold(true).
		Padding(0, 1)

	header := headerStyle.Render("")
	headerBorder := ""

	conversationStyle := lipgloss.NewStyle().
		Height(conversationHeight)

	inputStyle := lipgloss.NewStyle().
		Width(width)

	conversationArea := conversationStyle.Render(conversationView.Render())
	separator := shared.CreateSeparator(width, "─")
	inputArea := inputStyle.Render(inputView.Render())

	components := []string{header, headerBorder, conversationArea, separator}

	if statusHeight > 0 {
		statusContent := statusView.Render()
		if statusContent != "" {
			components = append(components, statusContent)
		}
	}

	if r.hasPendingApproval(data) {
		toolExecution := data.ToolExecution
		selectedIndex := int(domain.ApprovalApprove)
		if data.ApprovalUIState != nil {
			selectedIndex = data.ApprovalUIState.SelectedIndex
		}

		approvalView.SetWidth(width)
		approvalView.SetHeight(height)
		approvalContent := approvalView.Render(toolExecution, selectedIndex)
		if approvalContent != "" {
			components = append(components, approvalContent)
		}
	} else {
		components = append(components, inputArea)
	}

	helpBar.SetWidth(width)
	helpBarContent := helpBar.Render()
	if helpBarContent != "" {
		separator := shared.CreateSeparator(width, "─")
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

// hasPendingApproval checks if there's a pending tool call that requires approval
func (r *ApplicationViewRenderer) hasPendingApproval(data ChatInterfaceData) bool {
	return data.ToolExecution != nil &&
		data.ToolExecution.Status == domain.ToolExecutionStatusWaitingApproval &&
		(data.CurrentView == domain.ViewStateChat || data.CurrentView == domain.ViewStateToolApproval)
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
