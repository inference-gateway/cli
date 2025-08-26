package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
)

// FileSelectionHandler handles file selection logic and state management
type FileSelectionHandler struct {
	view *FileSelectionView
}

// NewFileSelectionHandler creates a new file selection handler
func NewFileSelectionHandler(theme shared.Theme) *FileSelectionHandler {
	return &FileSelectionHandler{
		view: NewFileSelectionView(theme),
	}
}

// HandleKeyEvent processes key events for file selection
func (h *FileSelectionHandler) HandleKeyEvent(
	keyMsg tea.KeyMsg,
	files []string,
	searchQuery string,
	selectedIndex int,
) (newSearchQuery string, newSelectedIndex int, action FileSelectionAction, selectedFile string) {
	filteredFiles := h.filterFiles(files, searchQuery)

	switch keyMsg.String() {
	case "up":
		if selectedIndex > 0 {
			return searchQuery, selectedIndex - 1, FileSelectionActionNone, ""
		}
		return searchQuery, selectedIndex, FileSelectionActionNone, ""

	case "down":
		if selectedIndex < len(filteredFiles)-1 {
			return searchQuery, selectedIndex + 1, FileSelectionActionNone, ""
		}
		return searchQuery, selectedIndex, FileSelectionActionNone, ""

	case "enter", "return":
		if len(filteredFiles) > 0 && selectedIndex >= 0 && selectedIndex < len(filteredFiles) {
			return searchQuery, selectedIndex, FileSelectionActionSelect, filteredFiles[selectedIndex]
		}
		return searchQuery, selectedIndex, FileSelectionActionNone, ""

	case "backspace":
		if len(searchQuery) > 0 {
			return searchQuery[:len(searchQuery)-1], selectedIndex, FileSelectionActionNone, ""
		}
		return searchQuery, selectedIndex, FileSelectionActionNone, ""

	case "esc":
		return searchQuery, selectedIndex, FileSelectionActionCancel, ""

	default:
		if len(keyMsg.String()) == 1 && keyMsg.String()[0] >= 32 && keyMsg.String()[0] <= 126 {
			char := keyMsg.String()
			newQuery := searchQuery + char
			return newQuery, 0, FileSelectionActionNone, ""
		}
		return searchQuery, selectedIndex, FileSelectionActionNone, ""
	}
}

// FileSelectionAction represents the type of action taken in file selection
type FileSelectionAction int

const (
	FileSelectionActionNone FileSelectionAction = iota
	FileSelectionActionSelect
	FileSelectionActionCancel
)

// UpdateInputWithSelectedFile updates input text with the selected file
func (h *FileSelectionHandler) UpdateInputWithSelectedFile(currentInput string, cursor int, selectedFile string) (newInput string, newCursor int) {
	atIndex := h.findAtSymbolIndex(currentInput, cursor)
	replacement := "@" + selectedFile + " "

	if atIndex >= 0 {
		before := currentInput[:atIndex]
		after := currentInput[cursor:]
		return before + replacement + after, atIndex + len(replacement)
	}

	newInput = currentInput + replacement
	return newInput, len(newInput)
}

// findAtSymbolIndex finds the position of the @ symbol before the cursor
func (h *FileSelectionHandler) findAtSymbolIndex(input string, cursor int) int {
	for i := cursor - 1; i >= 0; i-- {
		if input[i] == '@' {
			return i
		}
	}
	return -1
}

// filterFiles filters files based on search query
func (h *FileSelectionHandler) filterFiles(allFiles []string, searchQuery string) []string {
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

// RenderFileSelection renders the file selection view
func (h *FileSelectionHandler) RenderFileSelection(data FileSelectionData) string {
	if len(data.Files) == 0 {
		return shared.FormatWarning("No files available for selection")
	}

	h.view.SetWidth(data.Width)

	files := h.filterFiles(data.Files, data.SearchQuery)
	selectedIndex := data.SelectedIndex
	if selectedIndex >= len(files) {
		selectedIndex = 0
	}

	return h.view.RenderView(data.Files, data.SearchQuery, selectedIndex)
}

// CreateStatusMessage creates appropriate status messages for file selection actions
func (h *FileSelectionHandler) CreateStatusMessage(action FileSelectionAction, selectedFile string) tea.Cmd {
	switch action {
	case FileSelectionActionSelect:
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message: fmt.Sprintf("📁 File selected: %s", selectedFile),
				Spinner: false,
			}
		}
	case FileSelectionActionCancel:
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message: "File selection cancelled",
				Spinner: false,
			}
		}
	default:
		return nil
	}
}
