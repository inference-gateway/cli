package components

import (
	"strings"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// ApplicationViewRenderer handles rendering of different application views
type ApplicationViewRenderer struct {
	styleProvider *styles.Provider
	heights       componentHeights
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
	ToolExecution  *tui.ToolExecutionSession
	QueuedMessages []convdomain.QueuedMessage
}

// Layout sizes the chat components for the current state. Call it from Update
// (not View) whenever state changes. The transcript gets every row the measured
// chrome leaves, so the frame always fills the terminal.
func (r *ApplicationViewRenderer) Layout(
	data ChatInterfaceData,
	conversationView tui.ConversationRenderer,
	inputView tui.InputComponent,
	autocomplete tui.AutocompleteComponent,
	inputStatusBar tui.InputStatusBarComponent,
	statusView tui.StatusComponent,
	modeIndicator *ModeIndicator,
	helpBar tui.HelpBarComponent,
	queueBoxView *QueueBoxView,
	todoBoxView *TodoBoxView,
	approvalBoxView *ApprovalBoxView,
	questionFormView *QuestionFormView,
	snippetAttachments *SnippetAttachmentsView,
	historySearch *HistorySearchView,
	subagentList *SubagentList,
) {
	if data.Width == 0 || data.Height == 0 {
		return
	}

	r.heights = componentHeights{
		inputHeight:  tui.CalculateInputHeight(data.Height),
		statusHeight: tui.CalculateStatusHeight(data.Height),
	}
	r.setComponentDimensions(data, conversationView, inputView, autocomplete, inputStatusBar, statusView,
		queueBoxView, todoBoxView, approvalBoxView, questionFormView, snippetAttachments, historySearch)

	chrome := r.assembleComponents(data, r.renderHeader(data, data.Width), "", inputView.Render(), conversationView, statusView, modeIndicator,
		inputView, inputStatusBar, subagentList, autocomplete, helpBar, queueBoxView, todoBoxView, approvalBoxView, questionFormView, snippetAttachments, historySearch, data.Width, r.heights.statusHeight)
	chromeLines := strings.Count(strings.Join(chrome, "\n"), "\n")

	r.heights.conversationHeight = max(minConversationHeight, data.Height-chromeLines)
	conversationView.SetHeight(r.heights.conversationHeight)
}

// ConversationHeight returns the transcript height set by the last Layout call.
func (r *ApplicationViewRenderer) ConversationHeight() int {
	return r.heights.conversationHeight
}

// RenderChatInterface renders the main chat interface using the sizes set by
// the last Layout call.
func (r *ApplicationViewRenderer) RenderChatInterface(
	data ChatInterfaceData,
	conversationView tui.ConversationRenderer,
	inputView tui.InputComponent,
	autocomplete tui.AutocompleteComponent,
	inputStatusBar tui.InputStatusBarComponent,
	subagentList *SubagentList,
	statusView tui.StatusComponent,
	modeIndicator *ModeIndicator,
	helpBar tui.HelpBarComponent,
	queueBoxView *QueueBoxView,
	todoBoxView *TodoBoxView,
	approvalBoxView *ApprovalBoxView,
	questionFormView *QuestionFormView,
	snippetAttachments *SnippetAttachmentsView,
	historySearch *HistorySearchView,
) string {
	width := data.Width

	header := r.renderHeader(data, width)
	conversationArea := conversationView.Render()
	inputArea := inputView.Render()

	components := r.assembleComponents(data, header, conversationArea, inputArea, conversationView, statusView, modeIndicator,
		inputView, inputStatusBar, subagentList, autocomplete, helpBar, queueBoxView, todoBoxView, approvalBoxView, questionFormView, snippetAttachments, historySearch, width, r.heights.statusHeight)

	return strings.Join(components, "\n")
}

const minConversationHeight = 3

// componentHeights holds the heights the layout hands to the sized components
type componentHeights struct {
	conversationHeight int
	inputHeight        int
	statusHeight       int
}

// setComponentDimensions sets the width of every component and the heights
// that are known before the chrome is measured
func (r *ApplicationViewRenderer) setComponentDimensions(
	data ChatInterfaceData,
	conversationView tui.ConversationRenderer,
	inputView tui.InputComponent,
	autocomplete tui.AutocompleteComponent,
	inputStatusBar tui.InputStatusBarComponent,
	statusView tui.StatusComponent,
	queueBoxView *QueueBoxView,
	todoBoxView *TodoBoxView,
	approvalBoxView *ApprovalBoxView,
	questionFormView *QuestionFormView,
	snippetAttachments *SnippetAttachmentsView,
	historySearch *HistorySearchView,
) {
	width := data.Width
	conversationView.SetWidth(formatting.GetResponsiveWidth(width))
	inputView.SetWidth(width)
	inputView.SetHeight(r.heights.inputHeight)
	inputStatusBar.SetWidth(width)
	statusView.SetWidth(width)

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

	if snippetAttachments != nil {
		snippetAttachments.SetWidth(width)
	}

	if approvalBoxView != nil {
		approvalBoxView.SetWidth(width)
		approvalBoxView.SetHeight(data.Height)
	}

	if questionFormView != nil {
		questionFormView.SetWidth(width)
		questionFormView.SetHeight(data.Height)
	}

	if historySearch != nil {
		historySearch.SetWidth(width)
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
	conversationView tui.ConversationRenderer,
	statusView tui.StatusComponent,
	modeIndicator *ModeIndicator,
	inputView tui.InputComponent,
	inputStatusBar tui.InputStatusBarComponent,
	subagentList *SubagentList,
	autocomplete tui.AutocompleteComponent,
	helpBar tui.HelpBarComponent,
	queueBoxView *QueueBoxView,
	todoBoxView *TodoBoxView,
	approvalBoxView *ApprovalBoxView,
	questionFormView *QuestionFormView,
	snippetAttachments *SnippetAttachmentsView,
	historySearch *HistorySearchView,
	width, statusHeight int,
) []string {
	components := []string{header, "", conversationArea}

	components = r.appendQueueBox(components, data, queueBoxView)
	components = r.appendTodoBox(components, todoBoxView)
	components = r.appendStatusRow(components, statusView, modeIndicator, width, statusHeight)
	components = r.appendApprovalBox(components, approvalBoxView)
	components = r.appendQuestionForm(components, questionFormView)
	components = r.appendHistorySearch(components, historySearch)
	components = append(components, inputArea)
	components = r.appendSnippetAttachments(components, snippetAttachments)
	components = r.appendAutocomplete(components, autocomplete)
	components = r.appendInputStatusBar(components, inputView, inputStatusBar)
	components = r.appendSubagentList(components, subagentList)
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

// appendSnippetAttachments appends the snippet attachments tree (file + line
// ranges) directly below the input when any snippet is pending.
func (r *ApplicationViewRenderer) appendSnippetAttachments(
	components []string,
	snippetAttachments *SnippetAttachmentsView,
) []string {
	if snippetAttachments != nil {
		if content := snippetAttachments.Render(); content != "" {
			components = append(components, content)
		}
	}
	return components
}

// appendSubagentList appends the live sub-agent elapsed rows right-aligned
// below the status-indicator row while sub-agents run or linger.
func (r *ApplicationViewRenderer) appendSubagentList(components []string, subagentList *SubagentList) []string {
	if subagentList == nil {
		return components
	}
	if subagentRows := subagentList.Render(); subagentRows != "" {
		components = append(components, subagentRows)
	}
	return components
}

// appendStatusRow appends the row shared by the status message and the agent
// mode label, which is right-aligned on the first line of the status text. The
// row is appended even when both are empty so a mode change never reflows the
// layout.
func (r *ApplicationViewRenderer) appendStatusRow(
	components []string,
	statusView tui.StatusComponent,
	modeIndicator *ModeIndicator,
	width, statusHeight int,
) []string {
	var status string
	if statusHeight > 0 {
		status = statusView.Render()
	}

	var mode string
	if modeIndicator != nil {
		mode = modeIndicator.Text()
	}

	return append(components, r.statusRow(status, mode, width))
}

// statusRow lays the mode label out on the right of the first status line,
// truncating that line when the two cannot share the row. Without a mode the
// status text is returned unchanged.
func (r *ApplicationViewRenderer) statusRow(status, mode string, width int) string {
	if mode == "" {
		return status
	}

	lines := strings.Split(status, "\n")
	contentWidth := width - 4
	budget := contentWidth - 1 - r.styleProvider.GetWidth(mode)
	lines[0] = r.styleProvider.PlaceHorizontal(contentWidth, formatting.TruncateText(lines[0], budget), mode)
	return strings.Join(lines, "\n")
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

// appendQuestionForm appends the AskUserQuestion form box if a question is pending
func (r *ApplicationViewRenderer) appendQuestionForm(
	components []string,
	questionFormView *QuestionFormView,
) []string {
	if questionFormView != nil {
		if questionContent := questionFormView.Render(); questionContent != "" {
			components = append(components, "", questionContent)
		}
	}
	return components
}

// appendHistorySearch renders the Ctrl+R prompt-history search overlay above
// the input while it is open
func (r *ApplicationViewRenderer) appendHistorySearch(
	components []string,
	historySearch *HistorySearchView,
) []string {
	if historySearch != nil {
		if content := historySearch.Render(); content != "" {
			components = append(components, "", content)
		}
	}
	return components
}

// appendAutocomplete appends autocomplete content if visible
func (r *ApplicationViewRenderer) appendAutocomplete(
	components []string,
	autocomplete tui.AutocompleteComponent,
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
	inputView tui.InputComponent,
	inputStatusBar tui.InputStatusBarComponent,
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
	helpBar tui.HelpBarComponent,
	width int,
) []string {
	helpBar.SetWidth(width)
	if helpBarContent := helpBar.Render(); helpBarContent != "" {
		helpBarSeparator := strings.Repeat("─", width)
		components = append(components, helpBarSeparator, helpBarContent)
	}
	return components
}
