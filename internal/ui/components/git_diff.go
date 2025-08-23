package components

import (
	"strings"

	"github.com/inference-gateway/cli/internal/ui/shared"
)

// DiffType represents the type of diff operation
type DiffType int

const (
	DiffTypeNew    DiffType = iota // New file (all green)
	DiffTypeEdit                   // Edit existing file (red/green diff)
	DiffTypeDelete                 // Delete file (all red)
)

// DiffLine represents a single line in a diff with its type and content
type DiffLine struct {
	Type       DiffLineType
	LineNum    int
	Content    string
	OldLineNum int // For context lines, this can differ from LineNum
}

// DiffLineType represents the type of a diff line
type DiffLineType int

const (
	DiffLineAdded   DiffLineType = iota // Green line with +
	DiffLineRemoved                     // Red line with -
	DiffLineContext                     // Unchanged line
)

// GitDiffComponent handles rendering of git-style diffs with line numbers
type GitDiffComponent struct {
	width  int
	height int
}

// NewGitDiffComponent creates a new git diff component
func NewGitDiffComponent() *GitDiffComponent {
	return &GitDiffComponent{}
}

// SetWidth sets the component width
func (d *GitDiffComponent) SetWidth(width int) {
	d.width = width
}

// SetHeight sets the component height
func (d *GitDiffComponent) SetHeight(height int) {
	d.height = height
}

// RenderNewFile renders a complete new file (all content in green)
func (d *GitDiffComponent) RenderNewFile(filePath, content string) string {
	return d.RenderNewFileWithTitle(filePath, content, "← Content to be written →")
}

// RenderNewFileWithTitle renders a complete new file with a custom title
func (d *GitDiffComponent) RenderNewFileWithTitle(filePath, content, title string) string {
	var b strings.Builder
	b.WriteString(title + "\n")

	if content == "" {
		b.WriteString(shared.CreateDiffAddedLine(1, "(empty content)") + "\n")
		return b.String()
	}

	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") && len(lines) > 0 {
		lines = lines[:len(lines)-1] // Remove empty line from trailing newline
	}

	for i, line := range lines {
		lineNum := i + 1
		b.WriteString(shared.CreateDiffAddedLine(lineNum, line) + "\n")
	}

	if len(lines) == 0 {
		b.WriteString(shared.CreateDiffAddedLine(1, "") + "\n")
	}

	return b.String()
}

// RenderDiff renders a standard diff between old and new content
func (d *GitDiffComponent) RenderDiff(oldContent, newContent string) string {
	if oldContent == newContent {
		return "No changes to display.\n"
	}

	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	var diff strings.Builder
	maxLines := len(oldLines)
	if len(newLines) > maxLines {
		maxLines = len(newLines)
	}

	// Find the range of changed lines
	firstChanged := -1
	lastChanged := -1
	for i := 0; i < maxLines; i++ {
		oldLine := ""
		newLine := ""
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			if firstChanged == -1 {
				firstChanged = i
			}
			lastChanged = i
		}
	}

	if firstChanged == -1 {
		return "No changes to display.\n"
	}

	// Add context lines before and after changes
	contextBefore := 3
	contextAfter := 3
	startLine := max(0, firstChanged-contextBefore)
	endLine := min(maxLines-1, lastChanged+contextAfter)

	for i := startLine; i <= endLine; i++ {
		lineNum := i + 1
		oldExists := i < len(oldLines)
		newExists := i < len(newLines)

		switch {
		case oldExists && newExists:
			oldLine := oldLines[i]
			newLine := newLines[i]
			if oldLine != newLine {
				diff.WriteString(shared.CreateDiffRemovedLine(lineNum, oldLine) + "\n")
				diff.WriteString(shared.CreateDiffAddedLine(lineNum, newLine) + "\n")
			} else {
				diff.WriteString(shared.CreateDiffUnchangedLine(lineNum, oldLine) + "\n")
			}
		case oldExists:
			diff.WriteString(shared.CreateDiffRemovedLine(lineNum, oldLines[i]) + "\n")
		case newExists:
			diff.WriteString(shared.CreateDiffAddedLine(lineNum, newLines[i]) + "\n")
		}
	}

	return diff.String()
}

// RenderCustomDiff renders a diff from a list of DiffLine structures
func (d *GitDiffComponent) RenderCustomDiff(lines []DiffLine) string {
	var diff strings.Builder

	for _, line := range lines {
		switch line.Type {
		case DiffLineAdded:
			diff.WriteString(shared.CreateDiffAddedLine(line.LineNum, line.Content) + "\n")
		case DiffLineRemoved:
			diff.WriteString(shared.CreateDiffRemovedLine(line.LineNum, line.Content) + "\n")
		case DiffLineContext:
			diff.WriteString(shared.CreateDiffUnchangedLine(line.LineNum, line.Content) + "\n")
		}
	}

	return diff.String()
}

// RenderDeletedFile renders a file being deleted (all content in red)
func (d *GitDiffComponent) RenderDeletedFile(filePath, content string) string {
	var b strings.Builder
	b.WriteString("← File to be deleted →\n")

	if content == "" {
		b.WriteString(shared.CreateDiffRemovedLine(1, "(empty file)") + "\n")
		return b.String()
	}

	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}

	for i, line := range lines {
		lineNum := i + 1
		b.WriteString(shared.CreateDiffRemovedLine(lineNum, line) + "\n")
	}

	return b.String()
}
