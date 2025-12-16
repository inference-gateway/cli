package components

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// MessageHistorySelector implements the message history selection UI
type MessageHistorySelector struct {
	userMessages     []domain.UserMessageSnapshot
	filteredMessages []domain.UserMessageSnapshot
	selected         int
	width            int
	height           int
	styleProvider    *styles.Provider
	done             bool
	cancelled        bool
	searchMode       bool
	searchQuery      string
}

// NewMessageHistorySelector creates a new message history selector
func NewMessageHistorySelector(messages []domain.UserMessageSnapshot, styleProvider *styles.Provider) *MessageHistorySelector {
	m := &MessageHistorySelector{
		userMessages:     messages,
		filteredMessages: make([]domain.UserMessageSnapshot, len(messages)),
		selected:         len(messages) - 1, // Default to most recent
		width:            80,
		height:           24,
		styleProvider:    styleProvider,
		searchMode:       false,
		searchQuery:      "",
		done:             false,
		cancelled:        false,
	}

	copy(m.filteredMessages, messages)
	return m
}

// Init initializes the component
func (m *MessageHistorySelector) Init() tea.Cmd {
	logger.Info("Message history selector initialized",
		"totalMessages", len(m.userMessages),
		"filteredMessages", len(m.filteredMessages),
		"initialSelected", m.selected)
	return nil
}

// Update handles bubbletea messages
func (m *MessageHistorySelector) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowResize(msg)
	case tea.KeyMsg:
		return m.handleKeyInput(msg)
	}

	return m, nil
}

func (m *MessageHistorySelector) handleWindowResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	return m, nil
}

func (m *MessageHistorySelector) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		if m.searchMode {
			return m.handleSearchClear()
		}
		return m.handleCancel()
	case "up":
		return m.handleNavigationUp()
	case "down":
		return m.handleNavigationDown()
	case "enter", " ":
		return m.handleSelection()
	case "/":
		if !m.searchMode {
			return m.handleSearchToggle()
		}
		return m.handleCharacterInput(msg)
	case "backspace":
		return m.handleBackspace()
	default:
		return m.handleCharacterInput(msg)
	}
}

func (m *MessageHistorySelector) handleCancel() (tea.Model, tea.Cmd) {
	m.cancelled = true
	m.done = true
	return m, func() tea.Msg {
		return nil
	}
}

func (m *MessageHistorySelector) handleNavigationUp() (tea.Model, tea.Cmd) {
	if len(m.filteredMessages) == 0 {
		logger.Warn("Cannot navigate up: no filtered messages")
		return m, nil
	}

	oldSelected := m.selected
	if m.selected > 0 {
		m.selected--
		logger.Debug("Navigated up", "from", oldSelected, "to", m.selected, "total", len(m.filteredMessages))
	} else {
		logger.Debug("Already at first message", "selected", m.selected)
	}
	return m, nil
}

func (m *MessageHistorySelector) handleNavigationDown() (tea.Model, tea.Cmd) {
	if len(m.filteredMessages) == 0 {
		logger.Warn("Cannot navigate down: no filtered messages")
		return m, nil
	}

	oldSelected := m.selected
	maxIndex := len(m.filteredMessages) - 1
	if m.selected < maxIndex {
		m.selected++
		logger.Debug("Navigated down", "from", oldSelected, "to", m.selected, "total", len(m.filteredMessages))
	} else {
		logger.Debug("Already at last message", "selected", m.selected, "maxIndex", maxIndex)
	}
	return m, nil
}

func (m *MessageHistorySelector) handleSelection() (tea.Model, tea.Cmd) {
	if len(m.filteredMessages) > 0 && m.selected >= 0 && m.selected < len(m.filteredMessages) {
		selectedMessage := m.filteredMessages[m.selected]
		m.done = true

		// Log selection for debugging
		logger.Info("Message history selection made",
			"selectedIndex", m.selected,
			"conversationIndex", selectedMessage.Index,
			"message", selectedMessage.TruncatedMsg)

		return m, func() tea.Msg {
			event := domain.MessageHistoryRestoreEvent{
				RequestID:      "message-history-restore",
				Timestamp:      time.Now(),
				RestoreToIndex: selectedMessage.Index,
			}
			logger.Info("Emitting MessageHistoryRestoreEvent", "restoreToIndex", event.RestoreToIndex)
			return event
		}
	}
	return m, nil
}

func (m *MessageHistorySelector) handleSearchToggle() (tea.Model, tea.Cmd) {
	m.searchMode = true
	return m, nil
}

func (m *MessageHistorySelector) handleSearchClear() (tea.Model, tea.Cmd) {
	m.searchMode = false
	m.searchQuery = ""
	m.updateSearch()
	return m, nil
}

func (m *MessageHistorySelector) handleBackspace() (tea.Model, tea.Cmd) {
	if m.searchMode && len(m.searchQuery) > 0 {
		m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		m.updateSearch()
	}
	return m, nil
}

func (m *MessageHistorySelector) handleCharacterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchMode && len(msg.String()) == 1 && msg.String()[0] >= 32 {
		m.searchQuery += msg.String()
		m.updateSearch()
	}
	return m, nil
}

func (m *MessageHistorySelector) updateSearch() {
	m.filterMessages()
	if m.selected >= len(m.filteredMessages) {
		m.selected = len(m.filteredMessages) - 1
	}
	if m.selected < 0 && len(m.filteredMessages) > 0 {
		m.selected = 0
	}
}

func (m *MessageHistorySelector) filterMessages() {
	if m.searchQuery == "" {
		m.filteredMessages = make([]domain.UserMessageSnapshot, len(m.userMessages))
		copy(m.filteredMessages, m.userMessages)
		return
	}

	m.filteredMessages = make([]domain.UserMessageSnapshot, 0)
	query := strings.ToLower(m.searchQuery)
	for _, msg := range m.userMessages {
		if strings.Contains(strings.ToLower(msg.Content), query) {
			m.filteredMessages = append(m.filteredMessages, msg)
		}
	}
}

// View renders the component
func (m *MessageHistorySelector) View() string {
	var b strings.Builder

	m.writeHeader(&b)

	if len(m.filteredMessages) == 0 {
		return m.writeEmptyView(&b)
	}

	m.writeSearchInfo(&b)
	m.writeMessageList(&b)
	m.writeFooter(&b)

	return b.String()
}

func (m *MessageHistorySelector) writeHeader(b *strings.Builder) {
	title := "Go Back in Time - Select a Restore Point"
	if m.styleProvider != nil {
		b.WriteString(m.styleProvider.RenderWithColor(title, "accent"))
	} else {
		b.WriteString(title)
	}
	b.WriteString("\n\n")
}

func (m *MessageHistorySelector) writeSearchInfo(b *strings.Builder) {
	if m.searchMode {
		searchText := fmt.Sprintf("Search: %s", m.searchQuery)
		if m.styleProvider != nil {
			b.WriteString(m.styleProvider.RenderDimText(searchText))
		} else {
			b.WriteString(searchText)
		}
		b.WriteString("\n")
	}

	countText := fmt.Sprintf("Showing %d message(s)", len(m.filteredMessages))
	if m.styleProvider != nil {
		b.WriteString(m.styleProvider.RenderDimText(countText))
	} else {
		b.WriteString(countText)
	}
	b.WriteString("\n\n")
}

func (m *MessageHistorySelector) writeMessageList(b *strings.Builder) {
	maxVisible := m.height - 10
	if maxVisible < 5 {
		maxVisible = 5
	}

	start, end := m.calculatePaginationBounds(maxVisible)

	for i := start; i < end; i++ {
		msg := m.filteredMessages[i]
		m.writeMessageEntry(b, msg, i == m.selected)
	}

	if end < len(m.filteredMessages) {
		moreText := fmt.Sprintf("\n... and %d more messages", len(m.filteredMessages)-end)
		if m.styleProvider != nil {
			b.WriteString(m.styleProvider.RenderDimText(moreText))
		} else {
			b.WriteString(moreText)
		}
		b.WriteString("\n")
	}
}

func (m *MessageHistorySelector) calculatePaginationBounds(maxVisible int) (int, int) {
	totalMessages := len(m.filteredMessages)
	if totalMessages <= maxVisible {
		return 0, totalMessages
	}

	start := m.selected - maxVisible/2
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > totalMessages {
		end = totalMessages
		start = end - maxVisible
		if start < 0 {
			start = 0
		}
	}

	return start, end
}

func (m *MessageHistorySelector) writeMessageEntry(b *strings.Builder, msg domain.UserMessageSnapshot, isSelected bool) {
	timestamp := msg.Timestamp.Format("15:04:05")

	var entry string
	if isSelected {
		entry = fmt.Sprintf("▶ [%s] %s", timestamp, msg.TruncatedMsg)
		if m.styleProvider != nil {
			entry = m.styleProvider.RenderWithColor(entry, "accent")
		}
	} else {
		entry = fmt.Sprintf("  [%s] %s", timestamp, msg.TruncatedMsg)
		if m.styleProvider != nil {
			entry = m.styleProvider.RenderDimText(entry)
		}
	}

	b.WriteString(entry)
	b.WriteString("\n")
}

func (m *MessageHistorySelector) writeFooter(b *strings.Builder) {
	b.WriteString("\n")
	helpText := "↑/↓: Navigate | Enter: Restore | /: Search | ESC: Cancel"
	if m.styleProvider != nil {
		b.WriteString(m.styleProvider.RenderDimText(helpText))
	} else {
		b.WriteString(helpText)
	}
}

func (m *MessageHistorySelector) writeEmptyView(b *strings.Builder) string {
	emptyText := "No user messages to restore"
	if m.searchMode && m.searchQuery != "" {
		emptyText = fmt.Sprintf("No messages found matching '%s'", m.searchQuery)
	}

	if m.styleProvider != nil {
		b.WriteString(m.styleProvider.RenderDimText(emptyText))
	} else {
		b.WriteString(emptyText)
	}
	b.WriteString("\n\n")

	helpText := "ESC: Cancel"
	if m.styleProvider != nil {
		b.WriteString(m.styleProvider.RenderDimText(helpText))
	} else {
		b.WriteString(helpText)
	}

	return b.String()
}

// GetSelectedIndex returns the index of the selected message in the conversation
func (m *MessageHistorySelector) GetSelectedIndex() int {
	if m.selected >= 0 && m.selected < len(m.filteredMessages) {
		return m.filteredMessages[m.selected].Index
	}
	return -1
}

// IsSelected returns whether a selection was made
func (m *MessageHistorySelector) IsSelected() bool {
	return m.done && !m.cancelled
}

// IsCancelled returns whether the selection was cancelled
func (m *MessageHistorySelector) IsCancelled() bool {
	return m.cancelled
}
