package tools

import "strings"

// flexMatchResult is the outcome of findFlexibleMatch, the Edit and MultiEdit
// fallback after an exact match failed. found is set only for exactly one
// consistent window. Callers replace matchedBlock, the file's own bytes, with
// reindentedNew so the file's indentation is preserved.
type flexMatchResult struct {
	matchedBlock  string
	reindentedNew string
	found         bool
}

// findFlexibleMatch is a whitespace-tolerant fallback once an exact match of
// oldString failed. It refuses to guess: found is true only when exactly one
// block of lines matches after trimming and its indentation maps consistently.
// matchedBlock is then the file text to replace and reindentedNew is newString
// re-aligned to the file's indentation.
func findFlexibleMatch(content, oldString, newString string) flexMatchResult {
	miss := flexMatchResult{}

	oldLines := strings.Split(oldString, "\n")
	fileLines := strings.Split(content, "\n")
	if len(oldLines) > len(fileLines) {
		return miss
	}
	if firstNonBlankIndex(oldLines) < 0 {
		return miss
	}

	firstIdx, count := countFlexWindows(fileLines, oldLines)
	if count != 1 {
		return miss
	}

	fileWin := fileLines[firstIdx : firstIdx+len(oldLines)]
	indentMap, ok := buildIndentMap(oldLines, fileWin)
	if !ok {
		return miss
	}

	return flexMatchResult{
		matchedBlock:  strings.Join(fileWin, "\n"),
		reindentedNew: reindentNewLines(strings.Split(newString, "\n"), indentMap),
		found:         true,
	}
}

// countFlexWindows counts how many windows of fileLines equal oldLines once surrounding
// whitespace is stripped from every line, returning the index of the first such window. It
// stops early once a second window is found, since callers only care whether the count is one.
func countFlexWindows(fileLines, oldLines []string) (firstIdx, count int) {
	firstIdx = -1
	win := len(oldLines)
	trimmedOld := make([]string, win)
	for j, line := range oldLines {
		trimmedOld[j] = strings.TrimSpace(line)
	}
	for i := 0; i+win <= len(fileLines); i++ {
		if !windowMatchesTrimmed(fileLines[i:i+win], trimmedOld) {
			continue
		}
		count++
		if firstIdx == -1 {
			firstIdx = i
		}
		if count > 1 {
			break
		}
	}
	return firstIdx, count
}

// windowMatchesTrimmed reports whether every line of fileWin equals the corresponding
// pre-trimmed old line once its own surrounding whitespace is stripped.
func windowMatchesTrimmed(fileWin, trimmedOld []string) bool {
	for j := range trimmedOld {
		if strings.TrimSpace(fileWin[j]) != trimmedOld[j] {
			return false
		}
	}
	return true
}

// buildIndentMap maps each distinct old-line indentation to the file's
// indentation across the matched window, skipping blank lines. ok is false on
// any conflict. Mapping per indent rather than one uniform shift lets a block
// whose header sits at column 0, such as a Go func, match its indented body.
func buildIndentMap(oldLines, fileWin []string) (map[string]string, bool) {
	indentMap := make(map[string]string)
	for j := range oldLines {
		if strings.TrimSpace(oldLines[j]) == "" {
			continue
		}
		oldIndent := leadingWhitespace(oldLines[j])
		fileIndent := leadingWhitespace(fileWin[j])
		if existing, seen := indentMap[oldIndent]; seen && existing != fileIndent {
			return nil, false
		}
		indentMap[oldIndent] = fileIndent
	}
	return indentMap, true
}

// reindentNewLines rewrites each new line's leading whitespace using the learned old->file indent
// map, so new_string is emitted in the file's indentation style. A line whose exact indentation
// was not observed in the matched window (or a blank line) is kept as the model wrote it, since
// there is no learned mapping to apply to it.
func reindentNewLines(newLines []string, indentMap map[string]string) string {
	out := make([]string, len(newLines))
	for j, line := range newLines {
		if strings.TrimSpace(line) == "" {
			out[j] = line
			continue
		}
		indent := leadingWhitespace(line)
		if fileIndent, ok := indentMap[indent]; ok {
			out[j] = fileIndent + line[len(indent):]
		} else {
			out[j] = line
		}
	}
	return strings.Join(out, "\n")
}

// leadingWhitespace returns the run of spaces and tabs at the start of line.
func leadingWhitespace(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return line[:i]
		}
	}
	return line
}

// firstNonBlankIndex returns the index of the first line that is not empty after trimming
// surrounding whitespace, or -1 when every line is blank.
func firstNonBlankIndex(lines []string) int {
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			return i
		}
	}
	return -1
}
