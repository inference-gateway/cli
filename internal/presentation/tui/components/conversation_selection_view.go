package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	key "charm.land/bubbles/v2/key"
	spinner "charm.land/bubbles/v2/spinner"
	table "charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// ConversationSelector implements conversation selection UI
type ConversationSelector struct {
	conversations         []convdomain.ConversationSummary
	filteredConversations []convdomain.ConversationSummary
	width                 int
	height                int
	styleProvider         *styles.Provider
	done                  bool
	cancelled             bool
	repo                  convdomain.PersistentConversationRepository
	searchQuery           string
	searchMode            bool
	loading               bool
	loadError             error
	confirmDelete         bool
	deleteError           error
	dataLoaded            bool
	spinner               spinner.Model
	table                 table.Model
}

// NewConversationSelector creates a new conversation selector
func NewConversationSelector(repo convdomain.PersistentConversationRepository, styleProvider *styles.Provider) *ConversationSelector {
	c := &ConversationSelector{
		conversations:         make([]convdomain.ConversationSummary, 0),
		filteredConversations: make([]convdomain.ConversationSummary, 0),
		width:                 80,
		height:                24,
		styleProvider:         styleProvider,
		repo:                  repo,
		searchQuery:           "",
		searchMode:            false,
		loading:               true,
		loadError:             nil,
	}

	c.spinner = newModernSpinner()
	c.table = table.New(
		table.WithColumns([]table.Column{
			{Title: "ID", Width: 38},
			{Title: "Summary", Width: 25},
			{Title: "Messages", Width: 10},
			{Title: "Requests", Width: 8},
			{Title: "Input Tokens", Width: 12},
			{Title: "Output Tokens", Width: 13},
			{Title: "Cost", Width: 10},
		}),
		table.WithFocused(true),
		table.WithHeight(c.tableHeight()),
		table.WithWidth(c.width),
		table.WithStyles(c.tableStyles()),
	)

	return c
}

func (c *ConversationSelector) tableStyles() table.Styles {
	s := table.DefaultStyles()
	if c.styleProvider != nil {
		s.Header = s.Header.Foreground(lipgloss.Color(c.styleProvider.GetThemeColor("dim")))
		s.Selected = s.Selected.Foreground(lipgloss.Color(c.styleProvider.GetThemeColor("accent"))).Bold(true)
	}
	return s
}

func (c *ConversationSelector) tableHeight() int {
	h := c.height - 15
	if h < 3 {
		h = 3
	}
	return h
}

// syncTable refreshes the table rows from the filtered conversations, keeping
// the cursor in range.
func (c *ConversationSelector) syncTable() {
	rows := make([]table.Row, 0, len(c.filteredConversations))
	for _, conv := range c.filteredConversations {
		rows = append(rows, conversationRow(conv))
	}
	c.table.SetRows(rows)
	if c.table.Cursor() >= len(rows) {
		c.table.SetCursor(max(len(rows)-1, 0))
	}
}

// conversationRow renders one conversation as table cells, keeping the
// cost-tier precision of the previous hand-built table.
func conversationRow(conv convdomain.ConversationSummary) table.Row {
	costStr := "-"
	switch cost := conv.CostStats.TotalCost; {
	case cost > 0 && cost < 0.01:
		costStr = fmt.Sprintf("$%.4f", cost)
	case cost > 0 && cost < 1.0:
		costStr = fmt.Sprintf("$%.3f", cost)
	case cost > 0:
		costStr = fmt.Sprintf("$%.2f", cost)
	}

	return table.Row{
		conv.ID,
		formatting.TruncateText(conv.Title, 25),
		fmt.Sprintf("%d", conv.MessageCount),
		fmt.Sprintf("%d", conv.TokenStats.RequestCount),
		fmt.Sprintf("%d", conv.TokenStats.TotalInputTokens),
		fmt.Sprintf("%d", conv.TokenStats.TotalOutputTokens),
		costStr,
	}
}

func (c *ConversationSelector) Init() tea.Cmd {
	return tea.Batch(c.loadConversationsCmd(), c.spinner.Tick)
}

func (c *ConversationSelector) loadConversationsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		time.Sleep(constants.TestSleepDelay / 10)

		conversations, err := c.repo.ListSavedConversations(ctx, 50, 0)

		interfaceConversations := make([]any, len(conversations))
		for i, conv := range conversations {
			interfaceConversations[i] = conv
		}

		return tui.ConversationsLoadedEvent{
			Conversations: interfaceConversations,
			Error:         err,
		}
	}
}

func (c *ConversationSelector) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tui.ConversationsLoadedEvent:
		return c.handleConversationsLoaded(msg)
	case tea.WindowSizeMsg:
		return c.handleWindowResize(msg)
	case tea.KeyPressMsg:
		if c.loading {
			return c, nil
		}
		return c.handleKeyInput(msg)
	case spinner.TickMsg:
		if !c.loading {
			return c, nil
		}
		var cmd tea.Cmd
		c.spinner, cmd = c.spinner.Update(msg)
		return c, cmd
	}

	return c, nil
}

func (c *ConversationSelector) handleConversationsLoaded(msg tui.ConversationsLoadedEvent) (tea.Model, tea.Cmd) {
	c.loading = false
	c.loadError = msg.Error
	c.dataLoaded = true

	if msg.Error == nil {
		conversations := make([]convdomain.ConversationSummary, len(msg.Conversations))
		for i, conv := range msg.Conversations {
			if summary, ok := conv.(convdomain.ConversationSummary); ok {
				conversations[i] = summary
			}
		}

		c.conversations = conversations
		c.filteredConversations = make([]convdomain.ConversationSummary, len(conversations))
		copy(c.filteredConversations, conversations)
		c.syncTable()
		c.table.GotoTop()
	} else {
		logger.Error("conversationSelector failed to load conversations", "error", msg.Error)
	}

	return c, nil
}

func (c *ConversationSelector) handleWindowResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	c.width = msg.Width
	c.height = msg.Height
	c.table.SetWidth(c.width)
	c.table.SetHeight(c.tableHeight())
	return c, nil
}

func (c *ConversationSelector) handleKeyInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if c.confirmDelete {
		return c.handleDeleteConfirmation(msg)
	}

	switch {
	case key.Matches(msg, conversationSelectorKeys.cancel):
		if c.searchMode {
			return c.handleSearchClear()
		}
		return c.handleCancel()
	case key.Matches(msg, conversationSelectorKeys.enter):
		return c.handleSelection()
	case key.Matches(msg, conversationSelectorKeys.delete):
		if !c.searchMode && len(c.filteredConversations) > 0 {
			return c.handleDeleteRequest()
		}
		return c, nil
	case key.Matches(msg, conversationSelectorKeys.search):
		if !c.searchMode {
			return c.handleSearchToggle()
		}
		return c.handleCharacterInput(msg)
	case key.Matches(msg, conversationSelectorKeys.backspace):
		return c.handleBackspace()
	default:
		if c.searchMode {
			return c.handleCharacterInput(msg)
		}
		var cmd tea.Cmd
		c.table, cmd = c.table.Update(msg)
		return c, cmd
	}
}

func (c *ConversationSelector) handleCancel() (tea.Model, tea.Cmd) {
	c.cancelled = true
	c.done = true
	return c, nil
}

func (c *ConversationSelector) handleSelection() (tea.Model, tea.Cmd) {
	if len(c.filteredConversations) > 0 && c.table.Cursor() < len(c.filteredConversations) {
		c.done = true
	}
	return c, nil
}

func (c *ConversationSelector) handleSearchToggle() (tea.Model, tea.Cmd) {
	c.searchMode = true
	return c, nil
}

func (c *ConversationSelector) handleSearchClear() (tea.Model, tea.Cmd) {
	c.searchMode = false
	c.searchQuery = ""
	c.updateSearch()
	return c, nil
}

func (c *ConversationSelector) handleBackspace() (tea.Model, tea.Cmd) {
	if c.searchMode && len(c.searchQuery) > 0 {
		c.searchQuery = c.searchQuery[:len(c.searchQuery)-1]
		c.updateSearch()
	}
	return c, nil
}

func (c *ConversationSelector) handleCharacterInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if c.searchMode && len(msg.String()) == 1 && msg.String()[0] >= 32 {
		c.searchQuery += msg.String()
		c.updateSearch()
	}
	return c, nil
}

func (c *ConversationSelector) updateSearch() {
	c.filterConversations()
	c.syncTable()
	c.table.GotoTop()
}

func (c *ConversationSelector) View() tea.View {
	return tea.NewView(c.viewContent())
}

func (c *ConversationSelector) viewContent() string {
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
func (c *ConversationSelector) filterConversations() {
	if c.searchQuery == "" {
		c.filteredConversations = make([]convdomain.ConversationSummary, len(c.conversations))
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

func (c *ConversationSelector) handleDeleteRequest() (tea.Model, tea.Cmd) {
	if len(c.filteredConversations) == 0 || c.table.Cursor() >= len(c.filteredConversations) {
		return c, nil
	}

	c.confirmDelete = true
	c.deleteError = nil
	return c, nil
}

func (c *ConversationSelector) handleDeleteConfirmation(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, conversationSelectorKeys.confirm):
		return c.performDelete()
	case key.Matches(msg, conversationSelectorKeys.deny):
		c.confirmDelete = false
		c.deleteError = nil
		return c, nil
	default:
		return c, nil
	}
}

func (c *ConversationSelector) performDelete() (tea.Model, tea.Cmd) {
	cursor := c.table.Cursor()
	if cursor >= len(c.filteredConversations) {
		c.confirmDelete = false
		return c, nil
	}

	conv := c.filteredConversations[cursor]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.repo.DeleteSavedConversation(ctx, conv.ID); err != nil {
		c.deleteError = err
		c.confirmDelete = false
		logger.Error("failed to delete conversation", "error", err, "id", conv.ID)
		return c, nil
	}

	for i, origConv := range c.conversations {
		if origConv.ID == conv.ID {
			c.conversations = append(c.conversations[:i], c.conversations[i+1:]...)
			break
		}
	}

	c.filteredConversations = append(c.filteredConversations[:cursor], c.filteredConversations[cursor+1:]...)
	c.syncTable()

	c.confirmDelete = false
	c.deleteError = nil
	return c, nil
}

// IsSelected returns true if a conversation was selected
func (c *ConversationSelector) IsSelected() bool {
	return c.done && !c.cancelled && !c.loading && len(c.filteredConversations) > 0
}

// IsCancelled returns true if selection was cancelled
func (c *ConversationSelector) IsCancelled() bool {
	return c.cancelled
}

// GetSelected returns the selected conversation
func (c *ConversationSelector) GetSelected() convdomain.ConversationSummary {
	if c.IsSelected() && c.table.Cursor() < len(c.filteredConversations) {
		return c.filteredConversations[c.table.Cursor()]
	}
	return convdomain.ConversationSummary{}
}

// SetWidth sets the width of the conversation selector
func (c *ConversationSelector) SetWidth(width int) {
	c.width = width
	c.table.SetWidth(width)
}

// SetHeight sets the height of the conversation selector
func (c *ConversationSelector) SetHeight(height int) {
	c.height = height
	c.table.SetHeight(c.tableHeight())
}

// Reset resets the conversation selector state for reuse
func (c *ConversationSelector) Reset() {
	c.done = false
	c.cancelled = false
	c.searchQuery = ""
	c.searchMode = false
	c.loading = true
	c.loadError = nil
	c.conversations = make([]convdomain.ConversationSummary, 0)
	c.filteredConversations = make([]convdomain.ConversationSummary, 0)
	c.dataLoaded = false
	c.syncTable()
	c.table.GotoTop()
}

// NeedsInitialization returns true if the component needs to load data
func (c *ConversationSelector) NeedsInitialization() bool {
	return !c.dataLoaded
}

// writeHeader writes the header section of the view
func (c *ConversationSelector) writeHeader(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n\n", c.styleProvider.RenderWithColor("Select a Conversation", c.styleProvider.GetThemeColor("accent")))
}

// writeLoadingView writes the loading view and returns the complete string
func (c *ConversationSelector) writeLoadingView(b *strings.Builder) string {
	loading := fmt.Sprintf("%s Loading conversations...", c.spinner.View())
	fmt.Fprintf(b, "%s\n", c.styleProvider.RenderWithColor(loading, c.styleProvider.GetThemeColor("status")))
	return b.String()
}

// writeErrorView writes the error view and returns the complete string
func (c *ConversationSelector) writeErrorView(b *strings.Builder) string {
	errorMsg := fmt.Sprintf("Error loading conversations: %v", c.loadError)
	fmt.Fprintf(b, "%s\n", c.styleProvider.RenderWithColor(errorMsg, c.styleProvider.GetThemeColor("error")))
	return b.String()
}

// writeSearchInfo writes the search information section
func (c *ConversationSelector) writeSearchInfo(b *strings.Builder) {
	if c.searchMode {
		fmt.Fprintf(b, "%s%s\n\n",
			c.styleProvider.RenderWithColor("Search: "+c.searchQuery, c.styleProvider.GetThemeColor("status")),
			c.styleProvider.RenderWithColor("│", c.styleProvider.GetThemeColor("accent")))
	} else {
		helpText := fmt.Sprintf("Press / to search • %d conversations available", len(c.conversations))
		fmt.Fprintf(b, "%s\n\n", c.styleProvider.RenderDimText(helpText))
	}
}

// writeEmptyView writes the empty view and returns the complete string
func (c *ConversationSelector) writeEmptyView(b *strings.Builder) string {
	if c.searchQuery != "" {
		msg := fmt.Sprintf("No conversations match '%s'", c.searchQuery)
		fmt.Fprintf(b, "%s\n", c.styleProvider.RenderWithColor(msg, c.styleProvider.GetThemeColor("error")))
	} else if len(c.conversations) == 0 {
		msg := "No saved conversations found. Start chatting to create your first conversation!"
		fmt.Fprintf(b, "%s\n", c.styleProvider.RenderWithColor(msg, c.styleProvider.GetThemeColor("error")))
	}
	return b.String()
}

// writeConversationList writes the main conversation table
func (c *ConversationSelector) writeConversationList(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n", c.table.View())
}

// writeFooter writes the footer section
func (c *ConversationSelector) writeFooter(b *strings.Builder) {
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", c.width))
	b.WriteString("\n")

	if c.searchMode {
		helpText := "Type to search, ↑↓ to navigate, Enter to select, Esc to clear search"
		fmt.Fprintf(b, "%s", c.styleProvider.RenderDimText(helpText))
	} else {
		helpText := "Use ↑↓ arrows to navigate, Enter to select, d to delete, / to search, Esc/Ctrl+C to cancel"
		fmt.Fprintf(b, "%s", c.styleProvider.RenderDimText(helpText))
	}
}

// writeDeleteConfirmation writes the delete confirmation dialog
func (c *ConversationSelector) writeDeleteConfirmation(b *strings.Builder) string {
	if c.table.Cursor() >= len(c.filteredConversations) {
		return b.String()
	}

	conv := c.filteredConversations[c.table.Cursor()]

	c.writeSearchInfo(b)
	c.writeConversationList(b)

	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", c.width))
	b.WriteString("\n\n")

	errorColor := c.styleProvider.GetThemeColor("error")
	accentColor := c.styleProvider.GetThemeColor("accent")

	fmt.Fprintf(b, "%s\n\n", c.styleProvider.RenderWithColor("⚠ Delete Confirmation", errorColor))
	fmt.Fprintf(b, "Are you sure you want to delete this conversation?\n\n")
	fmt.Fprintf(b, "%s\n", c.styleProvider.RenderDimText("ID: "+conv.ID))
	fmt.Fprintf(b, "%s\n\n", c.styleProvider.RenderDimText("Title: "+conv.Title))
	fmt.Fprintf(b, "%s", c.styleProvider.RenderWithColor("Press Y to confirm, N or esc to cancel", accentColor))

	return b.String()
}

// writeDeleteError writes the delete error message
func (c *ConversationSelector) writeDeleteError(b *strings.Builder) {
	errorColor := c.styleProvider.GetThemeColor("error")
	errorMsg := fmt.Sprintf("Error deleting conversation: %v", c.deleteError)
	fmt.Fprintf(b, "%s\n\n", c.styleProvider.RenderWithColor(errorMsg, errorColor))
}
