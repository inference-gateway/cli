package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	history "github.com/inference-gateway/cli/internal/ui/history"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// HistorySearchView implements the recursive history search UI (like bash ctrl+r)
type HistorySearchView struct {
	historyManager  *history.HistoryManager
	allHistory      []string
	filteredHistory []string
	selected        int
	width           int
	height          int
	styleProvider   *styles.Provider
	searchQuery     string
	done            bool
	cancelled       bool
}

// NewHistorySearchView creates a new history search view
func NewHistorySearchView(historyManager *history.HistoryManager, styleProvider *styles.Provider) *HistorySearchView {
	return &HistorySearchView{
		historyManager:  historyManager,
		allHistory:      make([]string, 0),
		filteredHistory: make([]string, 0),
		selected:        0,
		width:           80,
		height:          24,
		styleProvider:   styleProvider,
		searchQuery:     "",
		done:            false,
		cancelled:       false,
	}
}

// Init initializes the history search view
func (h *HistorySearchView) Init() tea.Cmd {
	// Load history from history manager
	count := h.historyManager.GetHistoryCount()
	h.allHistory = make([]string, 0, count)

	// Get history entries by navigating through them
	// We'll access the unexported allHistory field through the manager's navigation
	// For now, we'll use a simple approach - build a list by navigating
	h.historyManager.ResetNavigation()
	tempHistory := make([]string, 0)

	// Navigate to oldest entry first
	for i := 0; i < count; i++ {
		entry := h.historyManager.NavigateUp("")
		if entry != "" {
			// Prepend to maintain chronological order
			tempHistory = append([]string{entry}, tempHistory...)
		}
	}

	h.historyManager.ResetNavigation()
	h.allHistory = tempHistory
	h.filteredHistory = make([]string, len(h.allHistory))
	copy(h.filteredHistory, h.allHistory)

	// Reverse to show most recent first
	h.reverseHistory()

	if len(h.filteredHistory) > 0 {
		h.selected = 0
	}

	return nil
}

// reverseHistory reverses the filtered history to show most recent first
func (h *HistorySearchView) reverseHistory() {
	for i, j := 0, len(h.filteredHistory)-1; i < j; i, j = i+1, j-1 {
		h.filteredHistory[i], h.filteredHistory[j] = h.filteredHistory[j], h.filteredHistory[i]
	}
}

// Update handles messages
func (h *HistorySearchView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width = msg.Width
		h.height = msg.Height
		return h, nil
	case tea.KeyMsg:
		return h.handleKeyInput(msg)
	}
	return h, nil
}

// handleKeyInput processes key inputs
func (h *HistorySearchView) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		h.cancelled = true
		h.done = true
		return h, nil
	case "up", "ctrl+p":
		return h.handleNavigationUp()
	case "down", "ctrl+n":
		return h.handleNavigationDown()
	case "enter", "tab":
		return h.handleSelection()
	case "backspace":
		return h.handleBackspace()
	default:
		return h.handleCharacterInput(msg)
	}
}

// handleNavigationUp moves selection up (to older entries)
func (h *HistorySearchView) handleNavigationUp() (tea.Model, tea.Cmd) {
	if h.selected > 0 {
		h.selected--
	}
	return h, nil
}

// handleNavigationDown moves selection down (to newer entries)
func (h *HistorySearchView) handleNavigationDown() (tea.Model, tea.Cmd) {
	if h.selected < len(h.filteredHistory)-1 {
		h.selected++
	}
	return h, nil
}

// handleSelection handles entry selection
func (h *HistorySearchView) handleSelection() (tea.Model, tea.Cmd) {
	if len(h.filteredHistory) > 0 && h.selected >= 0 && h.selected < len(h.filteredHistory) {
		h.done = true
		selectedEntry := h.filteredHistory[h.selected]
		return h, func() tea.Msg {
			return domain.HistorySearchSelectedEvent{Entry: selectedEntry}
		}
	}
	h.cancelled = true
	h.done = true
	return h, nil
}

// handleBackspace removes last character from search query
func (h *HistorySearchView) handleBackspace() (tea.Model, tea.Cmd) {
	if len(h.searchQuery) > 0 {
		h.searchQuery = h.searchQuery[:len(h.searchQuery)-1]
		h.updateSearch()
	}
	return h, nil
}

// handleCharacterInput adds character to search query
func (h *HistorySearchView) handleCharacterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
		h.searchQuery += key
		h.updateSearch()
	}
	return h, nil
}

// updateSearch filters history based on search query
func (h *HistorySearchView) updateSearch() {
	h.filterHistory()
	h.selected = 0
}

// filterHistory filters entries that contain the search query
func (h *HistorySearchView) filterHistory() {
	if h.searchQuery == "" {
		h.filteredHistory = make([]string, len(h.allHistory))
		copy(h.filteredHistory, h.allHistory)
		return
	}

	h.filteredHistory = h.filteredHistory[:0]
	query := strings.ToLower(h.searchQuery)

	for _, entry := range h.allHistory {
		if strings.Contains(strings.ToLower(entry), query) {
			h.filteredHistory = append(h.filteredHistory, entry)
		}
	}
}

// View renders the history search view
func (h *HistorySearchView) View() string {
	var b strings.Builder

	// Header
	accentColor := h.styleProvider.GetThemeColor("accent")
	title := "Recursive History Search (ctrl+r)"
	fmt.Fprintf(&b, "%s\n\n", h.styleProvider.RenderWithColor(title, accentColor))

	// Search prompt (similar to bash reverse-i-search)
	searchPrompt := fmt.Sprintf("(reverse-i-search)`%s': ", h.searchQuery)
	fmt.Fprintf(&b, "%s", h.styleProvider.RenderWithColor(searchPrompt, accentColor))

	if len(h.filteredHistory) > 0 && h.selected < len(h.filteredHistory) {
		fmt.Fprintf(&b, "%s\n\n", h.filteredHistory[h.selected])
	} else {
		fmt.Fprintf(&b, "\n\n")
	}

	// Results count
	resultInfo := fmt.Sprintf("%d matches found", len(h.filteredHistory))
	if h.searchQuery == "" {
		resultInfo = fmt.Sprintf("%d entries in history", len(h.filteredHistory))
	}
	fmt.Fprintf(&b, "%s\n\n", h.styleProvider.RenderDimText(resultInfo))

	// List of matches (limited to available height)
	if len(h.filteredHistory) > 0 {
		h.writeHistoryList(&b)
	} else {
		noMatchMsg := "No matches found"
		if h.searchQuery == "" {
			noMatchMsg = "History is empty"
		}
		fmt.Fprintf(&b, "%s\n", h.styleProvider.RenderDimText(noMatchMsg))
	}

	// Footer with help
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", h.width))
	b.WriteString("\n")
	helpText := "Type to search • ↑↓/Ctrl+P/Ctrl+N to navigate • Enter/Tab to select • Esc to cancel"
	fmt.Fprintf(&b, "%s", h.styleProvider.RenderDimText(helpText))

	return b.String()
}

// writeHistoryList writes the list of matching history entries
func (h *HistorySearchView) writeHistoryList(b *strings.Builder) {
	maxVisible := h.height - 12
	if maxVisible < 3 {
		maxVisible = 3
	}
	if maxVisible > len(h.filteredHistory) {
		maxVisible = len(h.filteredHistory)
	}

	start := h.selected - maxVisible/2
	if start < 0 {
		start = 0
	}
	if start > len(h.filteredHistory)-maxVisible {
		start = len(h.filteredHistory) - maxVisible
	}
	if start < 0 {
		start = 0
	}

	for i := start; i < start+maxVisible && i < len(h.filteredHistory); i++ {
		entry := h.filteredHistory[i]
		truncated := h.truncateEntry(entry, h.width-6)

		if i == h.selected {
			accentColor := h.styleProvider.GetThemeColor("accent")
			fmt.Fprintf(b, "%s\n", h.styleProvider.RenderWithColor(fmt.Sprintf("▶ %s", truncated), accentColor))
		} else {
			fmt.Fprintf(b, "  %s\n", truncated)
		}
	}

	if len(h.filteredHistory) > maxVisible {
		pageInfo := fmt.Sprintf("\nShowing %d-%d of %d", start+1, start+maxVisible, len(h.filteredHistory))
		fmt.Fprintf(b, "%s\n", h.styleProvider.RenderDimText(pageInfo))
	}
}

// truncateEntry truncates an entry to fit within the specified width
func (h *HistorySearchView) truncateEntry(entry string, maxWidth int) string {
	// Replace newlines with spaces for display
	entry = strings.ReplaceAll(entry, "\n", " ")
	entry = strings.ReplaceAll(entry, "\r", " ")

	if len(entry) <= maxWidth {
		return entry
	}
	if maxWidth <= 3 {
		return entry[:maxWidth]
	}
	return entry[:maxWidth-3] + "..."
}

// SetWidth sets the width of the view
func (h *HistorySearchView) SetWidth(width int) {
	h.width = width
}

// SetHeight sets the height of the view
func (h *HistorySearchView) SetHeight(height int) {
	h.height = height
}

// IsDone returns true if selection is complete
func (h *HistorySearchView) IsDone() bool {
	return h.done
}

// IsCancelled returns true if selection was cancelled
func (h *HistorySearchView) IsCancelled() bool {
	return h.cancelled
}

// GetSelected returns the selected entry
func (h *HistorySearchView) GetSelected() string {
	if h.done && !h.cancelled && len(h.filteredHistory) > 0 && h.selected < len(h.filteredHistory) {
		return h.filteredHistory[h.selected]
	}
	return ""
}

// Reset resets the view state for reuse
func (h *HistorySearchView) Reset() {
	h.searchQuery = ""
	h.selected = 0
	h.done = false
	h.cancelled = false
	h.filteredHistory = make([]string, len(h.allHistory))
	copy(h.filteredHistory, h.allHistory)
}
