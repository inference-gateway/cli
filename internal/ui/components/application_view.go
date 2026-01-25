package components

import (
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	ui "github.com/inference-gateway/cli/internal/ui"
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
	Width          int
	Height         int
	ToolExecution  *domain.ToolExecutionSession
	CurrentView    domain.ViewState
	QueuedMessages []domain.QueuedMessage
}

// RenderChatInterface renders the main chat interface
func (r *ApplicationViewRenderer) RenderChatInterface(
	data ChatInterfaceData,
	conversationView ui.ConversationRenderer,
	inputView ui.InputComponent,
	autocomplete ui.AutocompleteComponent,
	inputStatusBar ui.InputStatusBarComponent,
	statusView ui.StatusComponent,
	modeIndicator *ModeIndicator,
	helpBar ui.HelpBarComponent,
	queueBoxView *QueueBoxView,
	todoBoxView *TodoBoxView,
	approvalBoxView *ApprovalBoxView,
) string {
	width, height := data.Width, data.Height

	heights := r.calculateComponentHeights(data, height, helpBar, queueBoxView, todoBoxView, approvalBoxView)

	r.setComponentDimensions(width, conversationView, inputView, autocomplete, inputStatusBar, statusView,
		modeIndicator, queueBoxView, todoBoxView, approvalBoxView, heights)

	header := r.renderHeader(data, width)
	conversationArea := conversationView.Render()
	inputArea := inputView.Render()

	components := r.assembleComponents(data, header, conversationArea, inputArea, statusView, modeIndicator,
		inputView, inputStatusBar, autocomplete, helpBar, queueBoxView, todoBoxView, approvalBoxView, width, heights.statusHeight)

	return strings.Join(components, "\n")
}

// componentHeights holds calculated heights for various components
type componentHeights struct {
	headerHeight       int
	helpBarHeight      int
	queueBoxHeight     int
	todoBoxHeight      int
	approvalBoxHeight  int
	conversationHeight int
	inputHeight        int
	statusHeight       int
}

// calculateComponentHeights calculates the heights for all components
func (r *ApplicationViewRenderer) calculateComponentHeights(
	data ChatInterfaceData,
	totalHeight int,
	helpBar ui.HelpBarComponent,
	queueBoxView *QueueBoxView,
	todoBoxView *TodoBoxView,
	approvalBoxView *ApprovalBoxView,
) componentHeights {
	heights := componentHeights{
		headerHeight: 3,
	}

	if helpBar.IsEnabled() {
		heights.helpBarHeight = 6
	}

	if queueBoxView != nil && len(data.QueuedMessages) > 0 {
		totalItems := len(data.QueuedMessages)
		heights.queueBoxHeight = totalItems + 4
	}

	if todoBoxView != nil && todoBoxView.HasTodos() {
		heights.todoBoxHeight = todoBoxView.GetHeight()
	}

	// Calculate approval box height
	if approvalBoxView != nil {
		approvalContent := approvalBoxView.Render()
		if approvalContent != "" {
			// Count lines in approval content + some padding
			lines := strings.Count(approvalContent, "\n") + 1
			heights.approvalBoxHeight = lines + 2 // Add padding
		}
	}

	adjustedHeight := totalHeight - heights.headerHeight - heights.helpBarHeight -
		heights.queueBoxHeight - heights.todoBoxHeight - heights.approvalBoxHeight
	heights.conversationHeight = ui.CalculateConversationHeight(adjustedHeight)
	heights.inputHeight = ui.CalculateInputHeight(adjustedHeight)
	heights.statusHeight = ui.CalculateStatusHeight(adjustedHeight)

	if heights.conversationHeight < 3 {
		heights.conversationHeight = 3
	}

	return heights
}

// setComponentDimensions sets the width and height for all components
func (r *ApplicationViewRenderer) setComponentDimensions(
	width int,
	conversationView ui.ConversationRenderer,
	inputView ui.InputComponent,
	autocomplete ui.AutocompleteComponent,
	inputStatusBar ui.InputStatusBarComponent,
	statusView ui.StatusComponent,
	modeIndicator *ModeIndicator,
	queueBoxView *QueueBoxView,
	todoBoxView *TodoBoxView,
	approvalBoxView *ApprovalBoxView,
	heights componentHeights,
) {
	conversationWidth := formatting.GetResponsiveWidth(width)

	conversationView.SetWidth(conversationWidth)
	conversationView.SetHeight(heights.conversationHeight)
	inputView.SetWidth(width)
	inputView.SetHeight(heights.inputHeight)
	inputStatusBar.SetWidth(width)
	statusView.SetWidth(width)

	if modeIndicator != nil {
		modeIndicator.SetWidth(width)
	}

	if autocomplete != nil {
		autocomplete.SetWidth(width)
		autocomplete.SetHeight(10)
	}

	if queueBoxView != nil {
		queueBoxView.SetWidth(width)
	}

	if todoBoxView != nil {
		todoBoxView.SetWidth(width)
	}

	if approvalBoxView != nil {
		approvalBoxView.SetWidth(width)
	}
}

// renderHeader renders the header section
func (r *ApplicationViewRenderer) renderHeader(_ ChatInterfaceData, width int) string {
	headerText := ""
	accentColor := r.styleProvider.GetThemeColor("accent")
	return r.styleProvider.RenderCenteredBoldWithColor(headerText, accentColor, width)
}

// assembleComponents assembles all rendered components into a slice
func (r *ApplicationViewRenderer) assembleComponents(
	data ChatInterfaceData,
	header, conversationArea, inputArea string,
	statusView ui.StatusComponent,
	modeIndicator *ModeIndicator,
	inputView ui.InputComponent,
	inputStatusBar ui.InputStatusBarComponent,
	autocomplete ui.AutocompleteComponent,
	helpBar ui.HelpBarComponent,
	queueBoxView *QueueBoxView,
	todoBoxView *TodoBoxView,
	approvalBoxView *ApprovalBoxView,
	width, statusHeight int,
) []string {
	components := []string{header, "", conversationArea}

	components = r.appendQueueBox(components, data, queueBoxView)
	components = r.appendTodoBox(components, todoBoxView)
	components = r.appendModeIndicator(components, modeIndicator)
	components = r.appendStatusView(components, statusView, statusHeight)
	components = r.appendApprovalBox(components, approvalBoxView)
	components = append(components, inputArea)
	components = r.appendAutocomplete(components, autocomplete)
	components = r.appendInputStatusBar(components, inputView, inputStatusBar)
	components = r.appendHelpBar(components, helpBar, width)

	return components
}

// appendQueueBox appends queue box content if available
func (r *ApplicationViewRenderer) appendQueueBox(
	components []string,
	data ChatInterfaceData,
	queueBoxView *QueueBoxView,
) []string {
	if queueBoxView != nil && len(data.QueuedMessages) > 0 {
		if queueBoxContent := queueBoxView.Render(data.QueuedMessages); queueBoxContent != "" {
			components = append(components, "", queueBoxContent, "")
		}
	}
	return components
}

// appendTodoBox appends todo box content if available
func (r *ApplicationViewRenderer) appendTodoBox(
	components []string,
	todoBoxView *TodoBoxView,
) []string {
	if todoBoxView != nil && todoBoxView.HasTodos() {
		if todoBoxContent := todoBoxView.Render(); todoBoxContent != "" {
			components = append(components, todoBoxContent)
		}
	}
	return components
}

// appendStatusView appends status view content if available
func (r *ApplicationViewRenderer) appendStatusView(
	components []string,
	statusView ui.StatusComponent,
	statusHeight int,
) []string {
	if statusHeight > 0 {
		if statusContent := statusView.Render(); statusContent != "" {
			components = append(components, statusContent)
		}
	}
	return components
}

// appendModeIndicator appends mode indicator content if available
func (r *ApplicationViewRenderer) appendModeIndicator(
	components []string,
	modeIndicator *ModeIndicator,
) []string {
	if modeIndicator != nil {
		if modeContent := modeIndicator.Render(); modeContent != "" {
			components = append(components, modeContent)
		}
	}
	return components
}

// appendApprovalBox appends approval box content if available
func (r *ApplicationViewRenderer) appendApprovalBox(
	components []string,
	approvalBoxView *ApprovalBoxView,
) []string {
	if approvalBoxView != nil {
		if approvalContent := approvalBoxView.Render(); approvalContent != "" {
			components = append(components, "", approvalContent)
		}
	}
	return components
}

// appendAutocomplete appends autocomplete content if visible
func (r *ApplicationViewRenderer) appendAutocomplete(
	components []string,
	autocomplete ui.AutocompleteComponent,
) []string {
	if autocomplete != nil && autocomplete.IsVisible() {
		if autocompleteContent := autocomplete.Render(); autocompleteContent != "" {
			components = append(components, autocompleteContent)
		}
	}
	return components
}

// appendInputStatusBar appends input status bar content
func (r *ApplicationViewRenderer) appendInputStatusBar(
	components []string,
	inputView ui.InputComponent,
	inputStatusBar ui.InputStatusBarComponent,
) []string {
	inputStatusBar.SetInputText(inputView.GetInput())
	if inputStatusBarContent := inputStatusBar.Render(); inputStatusBarContent != "" {
		components = append(components, inputStatusBarContent)
	}
	return components
}

// appendHelpBar appends help bar content if available
func (r *ApplicationViewRenderer) appendHelpBar(
	components []string,
	helpBar ui.HelpBarComponent,
	width int,
) []string {
	helpBar.SetWidth(width)
	if helpBarContent := helpBar.Render(); helpBarContent != "" {
		helpBarSeparator := strings.Repeat("─", width)
		components = append(components, helpBarSeparator, helpBarContent)
	}
	return components
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
		return formatting.FormatWarning("No files available for selection")
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
