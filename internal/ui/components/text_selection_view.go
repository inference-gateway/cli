package components

import (
	"fmt"
	"strings"

	clipboard "github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// TextSelectionView provides vim-like text selection mode
type TextSelectionView struct {
	lines          []string
	cursorLine     int
	cursorCol      int
	selectionStart *Position
	selectionEnd   *Position
	selecting      bool
	visualLineMode bool
	width          int
	height         int
	scrollOffset   int
	copiedText     string
	styleProvider  *styles.Provider
}

// Position represents a position in the text
type Position struct {
	Line int
	Col  int
}

// NewTextSelectionView creates a new text selection view
func NewTextSelectionView(styleProvider *styles.Provider) *TextSelectionView {
	return &TextSelectionView{
		lines:         []string{},
		cursorLine:    0,
		cursorCol:     0,
		selecting:     false,
		width:         80,
		height:        20,
		scrollOffset:  0,
		styleProvider: styleProvider,
	}
}

// SetLines sets the lines to display from a ConversationView
func (v *TextSelectionView) SetLines(lines []string) {
	v.lines = lines
	v.cursorLine = 0
	v.cursorCol = 0
	v.scrollOffset = 0
}

// SetWidth sets the width of the view
func (v *TextSelectionView) SetWidth(width int) {
	v.width = width
}

// SetHeight sets the height of the view
func (v *TextSelectionView) SetHeight(height int) {
	v.height = height
}

// HandleKey handles keyboard input in selection mode
func (v *TextSelectionView) HandleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "h", "left":
		v.moveCursorLeft()
	case "l", "right":
		v.moveCursorRight()
	case "j", "down":
		v.moveCursorDown()
	case "k", "up":
		v.moveCursorUp()
	case "g":
		v.moveCursorToTop()
	case "G":
		v.moveCursorToBottom()
	case "0", "home":
		v.moveCursorToLineStart()
	case "$", "end":
		v.moveCursorToLineEnd()
	case "w":
		v.moveToNextWord()
	case "b":
		v.moveToPrevWord()
	case "v":
		v.toggleVisualMode()
	case "V":
		v.toggleVisualLineMode()
	case "y":
		return v.yankSelection()
	case "esc", "q":
		v.clearSelection()
		return func() tea.Msg {
			return domain.ExitSelectionModeEvent{}
		}
	case "ctrl+c":
		cmd := v.yankSelection()
		v.clearSelection()
		return tea.Batch(
			cmd,
			func() tea.Msg {
				return domain.ExitSelectionModeEvent{}
			},
		)
	}

	if v.selecting {
		v.selectionEnd = &Position{Line: v.cursorLine, Col: v.cursorCol}
	} else if v.visualLineMode {
		v.selectionEnd = &Position{Line: v.cursorLine, Col: 0}
	}

	v.updateScrollOffset()
	return nil
}

// Movement methods
func (v *TextSelectionView) moveCursorLeft() {
	if v.cursorCol > 0 {
		v.cursorCol--
	} else if v.cursorLine > 0 {
		v.cursorLine--
		if v.cursorLine < len(v.lines) {
			v.cursorCol = len(v.lines[v.cursorLine])
		}
	}
}

func (v *TextSelectionView) moveCursorRight() {
	if v.cursorLine < len(v.lines) {
		lineLen := len(v.lines[v.cursorLine])
		if v.cursorCol < lineLen {
			v.cursorCol++
		} else if v.cursorLine < len(v.lines)-1 {
			v.cursorLine++
			v.cursorCol = 0
		}
	}
}

func (v *TextSelectionView) moveCursorUp() {
	if v.cursorLine > 0 {
		v.cursorLine--
		if v.cursorLine < len(v.lines) {
			lineLen := len(v.lines[v.cursorLine])
			if v.cursorCol > lineLen {
				v.cursorCol = lineLen
			}
		}
	}
}

func (v *TextSelectionView) moveCursorDown() {
	if v.cursorLine < len(v.lines)-1 {
		v.cursorLine++
		lineLen := len(v.lines[v.cursorLine])
		if v.cursorCol > lineLen {
			v.cursorCol = lineLen
		}
	}
}

func (v *TextSelectionView) moveCursorToTop() {
	v.cursorLine = 0
	v.cursorCol = 0
}

func (v *TextSelectionView) moveCursorToBottom() {
	if len(v.lines) > 0 {
		v.cursorLine = len(v.lines) - 1
		v.cursorCol = len(v.lines[v.cursorLine])
	}
}

func (v *TextSelectionView) moveCursorToLineStart() {
	v.cursorCol = 0
}

func (v *TextSelectionView) moveCursorToLineEnd() {
	if v.cursorLine < len(v.lines) {
		v.cursorCol = len(v.lines[v.cursorLine])
	}
}

func (v *TextSelectionView) moveToNextWord() {
	if v.cursorLine >= len(v.lines) {
		return
	}

	line := v.lines[v.cursorLine]
	for v.cursorCol < len(line) && !isSpace(line[v.cursorCol]) {
		v.cursorCol++
	}

	for v.cursorCol < len(line) && isSpace(line[v.cursorCol]) {
		v.cursorCol++
	}

	if v.cursorCol >= len(line) && v.cursorLine < len(v.lines)-1 {
		v.cursorLine++
		v.cursorCol = 0
		if v.cursorLine < len(v.lines) {
			newLine := v.lines[v.cursorLine]
			for v.cursorCol < len(newLine) && isSpace(newLine[v.cursorCol]) {
				v.cursorCol++
			}
		}
	}
}

func (v *TextSelectionView) moveToPrevWord() {
	if v.cursorCol > 0 {
		v.cursorCol--
		if v.cursorLine < len(v.lines) {
			line := v.lines[v.cursorLine]

			for v.cursorCol > 0 && isSpace(line[v.cursorCol]) {
				v.cursorCol--
			}

			for v.cursorCol > 0 && !isSpace(line[v.cursorCol-1]) {
				v.cursorCol--
			}
		}
	} else if v.cursorLine > 0 {
		v.cursorLine--
		if v.cursorLine < len(v.lines) {
			v.cursorCol = len(v.lines[v.cursorLine])
		}
	}
}

func isSpace(ch byte) bool {
	return ch == ' ' || ch == '\t'
}

// Selection methods
func (v *TextSelectionView) toggleVisualMode() {
	if v.visualLineMode {
		v.visualLineMode = false
	}

	v.selecting = !v.selecting
	if v.selecting {
		v.selectionStart = &Position{Line: v.cursorLine, Col: v.cursorCol}
		v.selectionEnd = &Position{Line: v.cursorLine, Col: v.cursorCol}
	} else {
		v.clearSelection()
	}
}

func (v *TextSelectionView) toggleVisualLineMode() {
	v.selecting = false
	v.visualLineMode = !v.visualLineMode
	if v.visualLineMode {
		v.selectionStart = &Position{Line: v.cursorLine, Col: 0}
		v.selectionEnd = &Position{Line: v.cursorLine, Col: 0}
	} else {
		v.clearSelection()
	}
}

func (v *TextSelectionView) clearSelection() {
	v.selecting = false
	v.visualLineMode = false
	v.selectionStart = nil
	v.selectionEnd = nil
}

// yankSelection copies the selected text to clipboard
func (v *TextSelectionView) yankSelection() tea.Cmd {
	if v.selectionStart == nil || v.selectionEnd == nil {
		if v.cursorLine < len(v.lines) {
			text := v.lines[v.cursorLine]
			v.copiedText = text
			_ = clipboard.WriteAll(text)
			return func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    "📋 Yanked 1 line",
					Spinner:    false,
					StatusType: domain.StatusDefault,
				}
			}
		}
		return nil
	}

	text := v.getSelectedText()
	v.copiedText = text
	_ = clipboard.WriteAll(text)

	start := v.selectionStart
	end := v.selectionEnd
	if start.Line > end.Line || (start.Line == end.Line && start.Col > end.Col) {
		start, end = end, start
	}
	lineCount := end.Line - start.Line + 1
	msg := fmt.Sprintf("📋 Yanked %d line(s)", lineCount)

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    msg,
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}
}

// getSelectedText returns the currently selected text
func (v *TextSelectionView) getSelectedText() string {
	if v.selectionStart == nil || v.selectionEnd == nil {
		return ""
	}

	start := v.selectionStart
	end := v.selectionEnd

	if start.Line > end.Line || (start.Line == end.Line && start.Col > end.Col) {
		start, end = end, start
	}

	if v.visualLineMode {
		var lines []string
		for i := start.Line; i <= end.Line && i < len(v.lines); i++ {
			lines = append(lines, v.lines[i])
		}
		return strings.Join(lines, "\n")
	}

	if start.Line == end.Line {
		if start.Line < len(v.lines) {
			line := v.lines[start.Line]
			if start.Col < len(line) && end.Col <= len(line) {
				return line[start.Col:end.Col]
			}
		}
		return ""
	}

	var result []string
	for i := start.Line; i <= end.Line && i < len(v.lines); i++ {
		line := v.lines[i]
		lineText := v.getLineTextForSelection(i, line, start, end)
		if lineText != "" {
			result = append(result, lineText)
		}
	}

	return strings.Join(result, "\n")
}

// getLineTextForSelection extracts the text from a line based on selection bounds
func (v *TextSelectionView) getLineTextForSelection(lineIdx int, line string, start, end *Position) string {
	if lineIdx == start.Line && start.Col < len(line) {
		return line[start.Col:]
	}
	if lineIdx == end.Line && end.Col <= len(line) {
		return line[:end.Col]
	}
	if lineIdx > start.Line && lineIdx < end.Line {
		return line
	}
	return ""
}

// updateScrollOffset updates the scroll offset to keep cursor visible
func (v *TextSelectionView) updateScrollOffset() {
	visibleLines := max(1, v.height-4)

	if v.cursorLine < v.scrollOffset {
		v.scrollOffset = v.cursorLine
	} else if v.cursorLine >= v.scrollOffset+visibleLines {
		v.scrollOffset = v.cursorLine - visibleLines + 1
	}
}

func (v *TextSelectionView) Render() string {
	if len(v.lines) == 0 {
		return "No conversation to select from.\n\nPress ESC to return to chat."
	}

	var b strings.Builder

	mode := "SELECTION MODE"
	if v.selecting {
		mode = "VISUAL"
	} else if v.visualLineMode {
		mode = "VISUAL LINE"
	}

	accentColor := v.styleProvider.GetThemeColor("accent")
	header := v.styleProvider.RenderWithColorAndBold(fmt.Sprintf("-- %s --", mode), accentColor)
	b.WriteString(header)
	b.WriteString("\n")

	visibleLines := max(1, v.height-4)
	for i := 0; i < visibleLines && v.scrollOffset+i < len(v.lines); i++ {
		lineIdx := v.scrollOffset + i
		line := v.lines[lineIdx]

		isSelected := v.isLineSelected(lineIdx)

		renderedLine := v.renderDisplayLine(lineIdx, line, isSelected)
		b.WriteString(renderedLine)

		b.WriteString("\n")
	}

	debugInfo := ""
	if v.selecting {
		debugInfo = fmt.Sprintf(" | Visual: %d,%d -> %d,%d",
			v.selectionStart.Line, v.selectionStart.Col,
			v.selectionEnd.Line, v.selectionEnd.Col)
	} else if v.visualLineMode {
		debugInfo = fmt.Sprintf(" | Visual Line: %d -> %d",
			v.selectionStart.Line, v.selectionEnd.Line)
	}

	position := fmt.Sprintf("Line %d/%d, Col %d%s", v.cursorLine+1, len(v.lines), v.cursorCol+1, debugInfo)
	b.WriteString(v.styleProvider.RenderDimText(position))

	return b.String()
}

// isLineSelected checks if a line is within the selection
func (v *TextSelectionView) isLineSelected(lineIdx int) bool {
	if v.selectionStart == nil || v.selectionEnd == nil {
		return false
	}

	start := v.selectionStart.Line
	end := v.selectionEnd.Line

	if start > end {
		start, end = end, start
	}

	return lineIdx >= start && lineIdx <= end
}

// renderLineWithSelection renders a line with character-wise selection
func (v *TextSelectionView) renderLineWithSelection(lineIdx int, line string) string {
	if v.selectionStart == nil || v.selectionEnd == nil || v.visualLineMode {
		return line
	}

	start := v.selectionStart
	end := v.selectionEnd

	if start.Line > end.Line || (start.Line == end.Line && start.Col > end.Col) {
		start, end = end, start
	}

	if lineIdx < start.Line || lineIdx > end.Line {
		return line
	}

	var result strings.Builder

	for i := 0; i < len(line); i++ {
		shouldHighlight := false

		if lineIdx == start.Line && lineIdx == end.Line {
			shouldHighlight = i >= start.Col && i < end.Col
		} else if lineIdx == start.Line {
			shouldHighlight = i >= start.Col
		} else if lineIdx == end.Line {
			shouldHighlight = i < end.Col
		} else {
			shouldHighlight = true
		}

		if shouldHighlight {
			result.WriteString(v.styleProvider.RenderTextSelection(string(line[i])))
		} else {
			result.WriteByte(line[i])
		}
	}

	return result.String()
}

// renderCharAtPosition renders a character at a specific position with appropriate styling
func (v *TextSelectionView) renderCharAtPosition(i, lineLen int, line string, isCursor, shouldHighlight bool) string {
	if isCursor {
		if i < lineLen {
			return v.styleProvider.RenderCursor(string(line[i]))
		}
		return v.styleProvider.RenderCursor(" ")
	}

	if i < lineLen {
		char := string(line[i])
		if shouldHighlight {
			return v.styleProvider.RenderTextSelection(char)
		}
		return char
	}

	return ""
}

// renderDisplayLine renders a line with appropriate cursor and selection highlighting
func (v *TextSelectionView) renderDisplayLine(lineIdx int, line string, isSelected bool) string {
	if lineIdx == v.cursorLine {
		return v.renderLineWithCursor(lineIdx, line, isSelected)
	}

	if !isSelected {
		return line
	}

	if v.visualLineMode {
		return v.styleProvider.RenderVisualLineSelection(line)
	}

	return v.renderLineWithSelection(lineIdx, line)
}

// shouldHighlightChar determines if a character should be highlighted based on selection
func (v *TextSelectionView) shouldHighlightChar(lineIdx, charIdx int, isSelected bool, lineLen int) bool {
	if !isSelected {
		return false
	}

	if v.visualLineMode {
		return charIdx < lineLen
	}

	if v.selectionStart == nil || v.selectionEnd == nil {
		return false
	}

	start := v.selectionStart
	end := v.selectionEnd

	if start.Line > end.Line || (start.Line == end.Line && start.Col > end.Col) {
		start, end = end, start
	}

	if lineIdx == start.Line && lineIdx == end.Line {
		return charIdx >= start.Col && charIdx < end.Col
	}
	if lineIdx == start.Line {
		return charIdx >= start.Col
	}
	if lineIdx == end.Line {
		return charIdx < end.Col
	}
	if lineIdx > start.Line && lineIdx < end.Line {
		return true
	}

	return false
}

// renderLineWithCursor renders a line with a visible cursor
func (v *TextSelectionView) renderLineWithCursor(lineIdx int, line string, isSelected bool) string {
	var result strings.Builder

	lineLen := len(line)
	displayCursorCol := v.cursorCol
	if displayCursorCol > lineLen {
		displayCursorCol = lineLen
	}

	for i := 0; i <= lineLen; i++ {
		isCursor := i == displayCursorCol
		shouldHighlight := v.shouldHighlightChar(lineIdx, i, isSelected, lineLen)

		charRendered := v.renderCharAtPosition(i, lineLen, line, isCursor, shouldHighlight)
		if charRendered != "" {
			result.WriteString(charRendered)
		}
	}

	return result.String()
}
