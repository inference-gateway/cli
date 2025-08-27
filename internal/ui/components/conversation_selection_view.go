package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
)

// ConversationSelectorImpl implements conversation selection UI
type ConversationSelectorImpl struct {
	conversations         []shortcuts.ConversationSummary
	filteredConversations []shortcuts.ConversationSummary
	selected              int
	width                 int
	height                int
	themeService          domain.ThemeService
	done                  bool
	cancelled             bool
	repo                  shortcuts.PersistentConversationRepository
	searchQuery           string
	searchMode            bool
	loading               bool
	loadError             error
	confirmDelete         bool
	deleteError           error
}

// NewConversationSelector creates a new conversation selector
func NewConversationSelector(repo shortcuts.PersistentConversationRepository, themeService domain.ThemeService) *ConversationSelectorImpl {
	c := &ConversationSelectorImpl{
		conversations:         make([]shortcuts.ConversationSummary, 0),
		filteredConversations: make([]shortcuts.ConversationSummary, 0),
		selected:              0,
		width:                 80,
		height:                24,
		themeService:          themeService,
		repo:                  repo,
		searchQuery:           "",
		searchMode:            false,
		loading:               true,
		loadError:             nil,
	}

	return c
}

func (c *ConversationSelectorImpl) Init() tea.Cmd {
	return c.loadConversationsCmd()
}

func (c *ConversationSelectorImpl) loadConversationsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		time.Sleep(10 * time.Millisecond)

		conversations, err := c.repo.ListSavedConversations(ctx, 50, 0)

		interfaceConversations := make([]interface{}, len(conversations))
		for i, conv := range conversations {
			interfaceConversations[i] = conv
		}

		return domain.ConversationsLoadedEvent{
			Conversations: interfaceConversations,
			Error:         err,
		}
	}
}

func (c *ConversationSelectorImpl) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case domain.ConversationsLoadedEvent:
		return c.handleConversationsLoaded(msg)
	case tea.WindowSizeMsg:
		return c.handleWindowResize(msg)
	case tea.KeyMsg:
		// Don't handle key input while loading
		if c.loading {
			return c, nil
		}
		return c.handleKeyInput(msg)
	}

	return c, nil
}

func (c *ConversationSelectorImpl) handleConversationsLoaded(msg domain.ConversationsLoadedEvent) (tea.Model, tea.Cmd) {
	c.loading = false
	c.loadError = msg.Error

	if msg.Error == nil {
		conversations := make([]shortcuts.ConversationSummary, len(msg.Conversations))
		for i, conv := range msg.Conversations {
			if summary, ok := conv.(shortcuts.ConversationSummary); ok {
				conversations[i] = summary
			}
		}

		c.conversations = conversations
		c.filteredConversations = make([]shortcuts.ConversationSummary, len(conversations))
		copy(c.filteredConversations, conversations)

		if len(c.filteredConversations) > 0 {
			c.selected = 0
		}
	} else {
		logger.Error("ConversationSelector failed to load conversations", "error", msg.Error)
	}

	return c, nil
}

func (c *ConversationSelectorImpl) handleWindowResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	c.width = msg.Width
	c.height = msg.Height
	return c, nil
}

func (c *ConversationSelectorImpl) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if c.confirmDelete {
		return c.handleDeleteConfirmation(msg)
	}

	switch msg.String() {
	case "ctrl+c", "esc":
		if c.searchMode {
			return c.handleSearchClear()
		}
		return c.handleCancel()
	case "up":
		return c.handleNavigationUp()
	case "down":
		return c.handleNavigationDown()
	case "enter", " ":
		return c.handleSelection()
	case "d", "delete":
		if !c.searchMode && len(c.filteredConversations) > 0 {
			return c.handleDeleteRequest()
		}
		return c, nil
	case "/":
		if !c.searchMode {
			return c.handleSearchToggle()
		}
		return c.handleCharacterInput(msg)
	case "backspace":
		return c.handleBackspace()
	default:
		return c.handleCharacterInput(msg)
	}
}

func (c *ConversationSelectorImpl) handleCancel() (tea.Model, tea.Cmd) {
	c.cancelled = true
	c.done = true
	return c, nil
}

func (c *ConversationSelectorImpl) handleNavigationUp() (tea.Model, tea.Cmd) {
	if c.selected > 0 {
		c.selected--
	}
	return c, nil
}

func (c *ConversationSelectorImpl) handleNavigationDown() (tea.Model, tea.Cmd) {
	if c.selected < len(c.filteredConversations)-1 {
		c.selected++
	}
	return c, nil
}

func (c *ConversationSelectorImpl) handleSelection() (tea.Model, tea.Cmd) {
	if len(c.filteredConversations) > 0 && c.selected >= 0 && c.selected < len(c.filteredConversations) {
		selectedConversation := c.filteredConversations[c.selected]
		c.done = true
		return c, func() tea.Msg {
			return domain.ConversationSelectedEvent{ConversationID: selectedConversation.ID}
		}
	}
	return c, nil
}

func (c *ConversationSelectorImpl) handleSearchToggle() (tea.Model, tea.Cmd) {
	c.searchMode = true
	return c, nil
}

func (c *ConversationSelectorImpl) handleSearchClear() (tea.Model, tea.Cmd) {
	c.searchMode = false
	c.searchQuery = ""
	c.updateSearch()
	return c, nil
}

func (c *ConversationSelectorImpl) handleBackspace() (tea.Model, tea.Cmd) {
	if c.searchMode && len(c.searchQuery) > 0 {
		c.searchQuery = c.searchQuery[:len(c.searchQuery)-1]
		c.updateSearch()
	}
	return c, nil
}

func (c *ConversationSelectorImpl) handleCharacterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if c.searchMode && len(msg.String()) == 1 && msg.String()[0] >= 32 {
		c.searchQuery += msg.String()
		c.updateSearch()
	}
	return c, nil
}

func (c *ConversationSelectorImpl) updateSearch() {
	c.filterConversations()
	c.selected = 0
}

func (c *ConversationSelectorImpl) View() string {
	var b strings.Builder

	c.writeHeader(&b)

	if c.loading {
		return c.writeLoadingView(&b)
	}

	if c.loadError != nil {
		return c.writeErrorView(&b)
	}

	if c.confirmDelete {
		return c.writeDeleteConfirmation(&b)
	}

	if c.deleteError != nil {
		c.writeDeleteError(&b)
	}

	c.writeSearchInfo(&b)

	if len(c.filteredConversations) == 0 {
		return c.writeEmptyView(&b)
	}

	c.writeConversationList(&b)
	c.writeFooter(&b)

	return b.String()
}

// filterConversations filters the conversations based on the search query
func (c *ConversationSelectorImpl) filterConversations() {
	if c.searchQuery == "" {
		c.filteredConversations = make([]shortcuts.ConversationSummary, len(c.conversations))
		copy(c.filteredConversations, c.conversations)
		return
	}

	c.filteredConversations = c.filteredConversations[:0]
	query := strings.ToLower(c.searchQuery)

	for _, conv := range c.conversations {
		if strings.Contains(strings.ToLower(conv.Title), query) ||
			strings.Contains(strings.ToLower(conv.Summary), query) {
			c.filteredConversations = append(c.filteredConversations, conv)
		}
	}
}

func (c *ConversationSelectorImpl) handleDeleteRequest() (tea.Model, tea.Cmd) {
	if len(c.filteredConversations) == 0 || c.selected >= len(c.filteredConversations) {
		return c, nil
	}

	c.confirmDelete = true
	c.deleteError = nil
	return c, nil
}

func (c *ConversationSelectorImpl) handleDeleteConfirmation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return c.performDelete()
	case "n", "N", "esc":
		c.confirmDelete = false
		c.deleteError = nil
		return c, nil
	default:
		return c, nil
	}
}

func (c *ConversationSelectorImpl) performDelete() (tea.Model, tea.Cmd) {
	if c.selected >= len(c.filteredConversations) {
		c.confirmDelete = false
		return c, nil
	}

	conv := c.filteredConversations[c.selected]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.repo.DeleteSavedConversation(ctx, conv.ID); err != nil {
		c.deleteError = err
		c.confirmDelete = false
		logger.Error("Failed to delete conversation", "error", err, "id", conv.ID)
		return c, nil
	}

	for i, origConv := range c.conversations {
		if origConv.ID == conv.ID {
			c.conversations = append(c.conversations[:i], c.conversations[i+1:]...)
			break
		}
	}

	c.filteredConversations = append(c.filteredConversations[:c.selected], c.filteredConversations[c.selected+1:]...)

	if c.selected >= len(c.filteredConversations) && c.selected > 0 {
		c.selected--
	}

	c.confirmDelete = false
	c.deleteError = nil
	return c, nil
}

// IsSelected returns true if a conversation was selected
func (c *ConversationSelectorImpl) IsSelected() bool {
	return c.done && !c.cancelled && !c.loading && len(c.filteredConversations) > 0
}

// IsCancelled returns true if selection was cancelled
func (c *ConversationSelectorImpl) IsCancelled() bool {
	return c.cancelled
}

// GetSelected returns the selected conversation
func (c *ConversationSelectorImpl) GetSelected() shortcuts.ConversationSummary {
	if c.IsSelected() && len(c.filteredConversations) > 0 && c.selected < len(c.filteredConversations) {
		return c.filteredConversations[c.selected]
	}
	return shortcuts.ConversationSummary{}
}

// SetWidth sets the width of the conversation selector
func (c *ConversationSelectorImpl) SetWidth(width int) {
	c.width = width
}

// SetHeight sets the height of the conversation selector
func (c *ConversationSelectorImpl) SetHeight(height int) {
	c.height = height
}

// Reset resets the conversation selector state for reuse
func (c *ConversationSelectorImpl) Reset() {
	c.done = false
	c.cancelled = false
	c.selected = 0
	c.searchQuery = ""
	c.searchMode = false
	c.loading = true
	c.loadError = nil
	c.conversations = make([]shortcuts.ConversationSummary, 0)
	c.filteredConversations = make([]shortcuts.ConversationSummary, 0)
}

// writeHeader writes the header section of the view
func (c *ConversationSelectorImpl) writeHeader(b *strings.Builder) {
	fmt.Fprintf(b, "%sSelect a Conversation%s\n\n",
		c.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)
}

// writeLoadingView writes the loading view and returns the complete string
func (c *ConversationSelectorImpl) writeLoadingView(b *strings.Builder) string {
	fmt.Fprintf(b, "%sLoading conversations...%s\n",
		c.themeService.GetCurrentTheme().GetStatusColor(), colors.Reset)
	return b.String()
}

// writeErrorView writes the error view and returns the complete string
func (c *ConversationSelectorImpl) writeErrorView(b *strings.Builder) string {
	fmt.Fprintf(b, "%sError loading conversations: %v%s\n",
		c.themeService.GetCurrentTheme().GetErrorColor(), c.loadError, colors.Reset)
	return b.String()
}

// writeSearchInfo writes the search information section
func (c *ConversationSelectorImpl) writeSearchInfo(b *strings.Builder) {
	if c.searchMode {
		fmt.Fprintf(b, "%sSearch: %s%s│%s\n\n",
			c.themeService.GetCurrentTheme().GetStatusColor(), c.searchQuery, c.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)
	} else {
		fmt.Fprintf(b, "%sPress / to search • %d conversations available%s\n\n",
			c.themeService.GetCurrentTheme().GetDimColor(), len(c.conversations), colors.Reset)
	}
}

// writeEmptyView writes the empty view and returns the complete string
func (c *ConversationSelectorImpl) writeEmptyView(b *strings.Builder) string {
	if c.searchQuery != "" {
		fmt.Fprintf(b, "%sNo conversations match '%s'%s\n",
			c.themeService.GetCurrentTheme().GetErrorColor(), c.searchQuery, colors.Reset)
	} else if len(c.conversations) == 0 {
		fmt.Fprintf(b, "%sNo saved conversations found. Start chatting to create your first conversation!%s\n",
			c.themeService.GetCurrentTheme().GetErrorColor(), colors.Reset)
	}
	return b.String()
}

// writeConversationList writes the main conversation list
func (c *ConversationSelectorImpl) writeConversationList(b *strings.Builder) {
	c.writeTableHeader(b)

	pagination := c.calculatePagination()

	for i := pagination.start; i < pagination.start+pagination.maxVisible && i < len(c.filteredConversations); i++ {
		conv := c.filteredConversations[i]
		c.writeConversationRow(b, conv, i)
	}

	if len(c.filteredConversations) > pagination.maxVisible {
		fmt.Fprintf(b, "%sShowing %d-%d of %d conversations%s\n",
			c.themeService.GetCurrentTheme().GetDimColor(), pagination.start+1, pagination.start+pagination.maxVisible,
			len(c.filteredConversations), colors.Reset)
	}
}

// writeTableHeader writes the table header
func (c *ConversationSelectorImpl) writeTableHeader(b *strings.Builder) {
	fmt.Fprintf(b, "%s%-22s │ %-40s │ %-20s │ %-10s%s\n",
		c.themeService.GetCurrentTheme().GetDimColor(), "ID", "Summary", "Updated", "Messages", colors.Reset)
	fmt.Fprintf(b, "%s%s%s\n",
		c.themeService.GetCurrentTheme().GetDimColor(), strings.Repeat("─", c.width-4), colors.Reset)
}

// paginationInfo holds pagination calculation results
type paginationInfo struct {
	start      int
	maxVisible int
}

// calculatePagination calculates pagination parameters
func (c *ConversationSelectorImpl) calculatePagination() paginationInfo {
	maxVisible := c.height - 15
	if maxVisible > len(c.filteredConversations) {
		maxVisible = len(c.filteredConversations)
	}
	if maxVisible < 1 {
		maxVisible = 1
	}

	start := 0
	if c.selected >= maxVisible {
		start = c.selected - maxVisible + 1
	}
	if start < 0 {
		start = 0
	}
	if start > len(c.filteredConversations)-maxVisible && len(c.filteredConversations) > maxVisible {
		start = len(c.filteredConversations) - maxVisible
	}

	return paginationInfo{start: start, maxVisible: maxVisible}
}

// writeConversationRow writes a single conversation row
func (c *ConversationSelectorImpl) writeConversationRow(b *strings.Builder, conv shortcuts.ConversationSummary, index int) {
	shortID := c.truncateString(conv.ID, 20)
	summary := c.truncateString(conv.Title, 40)
	updatedAt := c.formatUpdatedAt(conv.UpdatedAt)
	msgCount := fmt.Sprintf("%d", conv.MessageCount)

	if index == c.selected {
		fmt.Fprintf(b, "%s▶ %-20s │ %-40s │ %-20s │ %-10s%s\n",
			c.themeService.GetCurrentTheme().GetAccentColor(), shortID, summary, updatedAt, msgCount, colors.Reset)
	} else {
		fmt.Fprintf(b, "  %-20s │ %-40s │ %-20s │ %-10s\n",
			shortID, summary, updatedAt, msgCount)
	}
}

// truncateString truncates a string to the specified length with ellipsis
func (c *ConversationSelectorImpl) truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// formatUpdatedAt formats the updatedAt timestamp
func (c *ConversationSelectorImpl) formatUpdatedAt(updatedAt string) string {
	if len(updatedAt) <= 20 {
		return updatedAt
	}

	formatted := c.formatDateTimeParts(updatedAt)
	if len(formatted) > 20 {
		formatted = formatted[:20]
	}
	return formatted
}

// formatDateTimeParts formats date and time parts
func (c *ConversationSelectorImpl) formatDateTimeParts(updatedAt string) string {
	if !strings.Contains(updatedAt, " ") {
		return updatedAt
	}

	parts := strings.Split(updatedAt, " ")
	if len(parts) < 2 {
		return updatedAt
	}

	datePart := parts[0]
	timePart := parts[1]

	if !strings.Contains(timePart, ":") {
		return updatedAt
	}

	timeComponents := strings.Split(timePart, ":")
	if len(timeComponents) < 2 {
		return updatedAt
	}

	return fmt.Sprintf("%s %s:%s", datePart, timeComponents[0], timeComponents[1])
}

// writeFooter writes the footer section
func (c *ConversationSelectorImpl) writeFooter(b *strings.Builder) {
	b.WriteString("\n")
	b.WriteString(colors.CreateSeparator(c.width, "─"))
	b.WriteString("\n")

	if c.searchMode {
		fmt.Fprintf(b, "%sType to search, ↑↓ to navigate, Enter to select, Esc to clear search%s",
			c.themeService.GetCurrentTheme().GetDimColor(), colors.Reset)
	} else {
		fmt.Fprintf(b, "%sUse ↑↓ arrows to navigate, Enter to select, d to delete, / to search, Esc/Ctrl+C to cancel%s",
			c.themeService.GetCurrentTheme().GetDimColor(), colors.Reset)
	}
}

// writeDeleteConfirmation writes the delete confirmation dialog
func (c *ConversationSelectorImpl) writeDeleteConfirmation(b *strings.Builder) string {
	if c.selected >= len(c.filteredConversations) {
		return b.String()
	}

	conv := c.filteredConversations[c.selected]

	c.writeSearchInfo(b)
	c.writeConversationList(b)

	b.WriteString("\n")
	b.WriteString(colors.CreateSeparator(c.width, "─"))
	b.WriteString("\n\n")

	fmt.Fprintf(b, "%s⚠ Delete Confirmation%s\n\n",
		c.themeService.GetCurrentTheme().GetErrorColor(), colors.Reset)

	fmt.Fprintf(b, "Are you sure you want to delete this conversation?\n\n")
	fmt.Fprintf(b, "%sID: %s%s\n", c.themeService.GetCurrentTheme().GetDimColor(), conv.ID, colors.Reset)
	fmt.Fprintf(b, "%sTitle: %s%s\n\n", c.themeService.GetCurrentTheme().GetDimColor(), conv.Title, colors.Reset)

	fmt.Fprintf(b, "%sPress Y to confirm, N or Esc to cancel%s",
		c.themeService.GetCurrentTheme().GetAccentColor(), colors.Reset)

	return b.String()
}

// writeDeleteError writes the delete error message
func (c *ConversationSelectorImpl) writeDeleteError(b *strings.Builder) {
	fmt.Fprintf(b, "%sError deleting conversation: %v%s\n\n",
		c.themeService.GetCurrentTheme().GetErrorColor(), c.deleteError, colors.Reset)
}
