package components

import (
	"fmt"
	"strings"

	lipgloss "github.com/charmbracelet/lipgloss"
	domain "github.com/inference-gateway/cli/internal/domain"
)

type FileSelectionView struct {
	themeService domain.ThemeService
	maxVisible   int
	width        int
}

func NewFileSelectionView(themeService domain.ThemeService) *FileSelectionView {
	return &FileSelectionView{
		themeService: themeService,
		maxVisible:   12,
	}
}

func (f *FileSelectionView) SetWidth(width int) {
	f.width = width
}

func (f *FileSelectionView) RenderView(allFiles []string, searchQuery string, selectedIndex int) string {
	if allFiles == nil {
		return "📁 No files found in current directory\n\nPress ESC to return to chat"
	}

	files := f.filterFiles(allFiles, searchQuery)
	selectedIndex = f.validateSelectedIndex(files, selectedIndex)

	var b strings.Builder
	f.renderHeader(&b, files, allFiles, searchQuery)
	f.renderSearchField(&b, searchQuery)

	if len(files) == 0 {
		return f.renderNoFilesFound(&b, searchQuery)
	}

	f.renderFileList(&b, files, selectedIndex)
	f.renderFooter(&b, files, selectedIndex)
	return b.String()
}

func (f *FileSelectionView) filterFiles(allFiles []string, searchQuery string) []string {
	if searchQuery == "" {
		return allFiles
	}

	var filtered []string
	query := strings.ToLower(searchQuery)
	for _, file := range allFiles {
		if strings.Contains(strings.ToLower(file), query) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func (f *FileSelectionView) validateSelectedIndex(files []string, selectedIndex int) int {
	if selectedIndex >= len(files) {
		return 0
	}
	return selectedIndex
}

func (f *FileSelectionView) renderHeader(b *strings.Builder, files, allFiles []string, searchQuery string) {
	if searchQuery != "" {
		fmt.Fprintf(b, "📁 File Search - %d matches (of %d total files):\n", len(files), len(allFiles))
	} else {
		fmt.Fprintf(b, "📁 Select a file to include in your message (%d files found):\n", len(files))
	}

	if f.width > 0 {
		b.WriteString(strings.Repeat("═", f.width))
	} else {
		b.WriteString(strings.Repeat("═", 80))
	}
	b.WriteString("\n\n")
}

func (f *FileSelectionView) renderSearchField(b *strings.Builder, searchQuery string) {
	b.WriteString("🔍 Search: ")
	if searchQuery != "" {
		userColor := f.themeService.GetCurrentTheme().GetUserColor()
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(userColor))
		fmt.Fprintf(b, "%s│", style.Render(searchQuery))
	} else {
		dimColor := f.themeService.GetCurrentTheme().GetDimColor()
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))
		fmt.Fprintf(b, "%s│", style.Render("type to filter files..."))
	}
	b.WriteString("\n\n")
}

func (f *FileSelectionView) renderNoFilesFound(b *strings.Builder, searchQuery string) string {
	errorColor := f.themeService.GetCurrentTheme().GetErrorColor()
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(errorColor))
	errorMsg := fmt.Sprintf("No files match '%s'", searchQuery)
	fmt.Fprintf(b, "%s\n\n", errorStyle.Render(errorMsg))

	dimColor := f.themeService.GetCurrentTheme().GetDimColor()
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))
	helpText := "Type to search, BACKSPACE to clear search, ESC to cancel"
	b.WriteString(dimStyle.Render(helpText))
	return b.String()
}

func (f *FileSelectionView) renderFileList(b *strings.Builder, files []string, selectedIndex int) {
	startIndex, endIndex := f.calculateVisibleRange(len(files), selectedIndex)

	accentColor := f.themeService.GetCurrentTheme().GetAccentColor()
	dimColor := f.themeService.GetCurrentTheme().GetDimColor()
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))

	for i := startIndex; i < endIndex; i++ {
		file := files[i]
		if i == selectedIndex {
			fmt.Fprintf(b, "%s\n", accentStyle.Render("▶ "+file))
		} else {
			fmt.Fprintf(b, "%s\n", dimStyle.Render("  "+file))
		}
	}
}

func (f *FileSelectionView) calculateVisibleRange(totalFiles, selectedIndex int) (int, int) {
	startIndex := 0
	if selectedIndex >= f.maxVisible {
		startIndex = selectedIndex - f.maxVisible + 1
	}
	endIndex := startIndex + f.maxVisible
	if endIndex > totalFiles {
		endIndex = totalFiles
	}
	return startIndex, endIndex
}

func (f *FileSelectionView) renderFooter(b *strings.Builder, files []string, selectedIndex int) {
	b.WriteString("\n")

	dimColor := f.themeService.GetCurrentTheme().GetDimColor()
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))

	if len(files) > f.maxVisible {
		startIndex, endIndex := f.calculateVisibleRange(len(files), selectedIndex)
		paginationText := fmt.Sprintf("Showing %d-%d of %d matches", startIndex+1, endIndex, len(files))
		fmt.Fprintf(b, "%s\n", dimStyle.Render(paginationText))
		b.WriteString("\n")
	}

	helpText := "Type to search, ↑↓ to navigate, ENTER to select, BACKSPACE to clear, ESC to cancel"
	b.WriteString(dimStyle.Render(helpText))
}
