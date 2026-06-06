package tools

import "strings"

// flexMatchResult is the outcome of a leading-whitespace-tolerant match. It is produced by
// findFlexibleMatch and consumed by the Edit and MultiEdit tools as a fallback after an exact
// strings.Contains match has failed.
type flexMatchResult struct {
	// matchedBlock is the exact file bytes of the single matched window. Callers replace
	// this (not the model's old_string) so the file's real indentation is preserved.
	matchedBlock string
	// reindentedNew is new_string re-indented to the file's indentation.
	reindentedNew string
	// found is true only when exactly one window matched under a consistent indent mapping.
	found bool
}

// findFlexibleMatch attempts a whitespace-tolerant match of oldString within content. It is
// intended ONLY as a fallback once an exact strings.Contains match has already failed, and it
// deliberately refuses to guess: found is true only when exactly one block of file lines matches
// oldString after trimming surrounding whitespace AND each distinct old-line indentation maps
// consistently to a single file indentation across the block. On found, matchedBlock holds the
// exact file text to replace and reindentedNew holds newString re-aligned to the file's
// indentation (each new line's leading whitespace remapped via the same learned mapping). Any
// ambiguity (zero or multiple candidate blocks, or a conflicting indent mapping) returns
// found=false, and the caller must fall back to its existing error path.
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

// buildIndentMap records how each distinct old-line indentation maps to the file's actual
// indentation across the matched window, requiring the mapping to be consistent — the same old
// indent must always correspond to the same file indent. ok is false on any conflict, which
// signals the indentation relationship is ambiguous and the edit must not be applied. Blank
// lines carry no indentation signal and are skipped.
//
// Deriving the mapping per distinct indent (rather than as a single uniform shift anchored on the
// first line) is what lets a block whose header sits at column 0 — a Go struct/import/func — match:
// the header maps "" -> "" while the body maps "\t\t" -> "\t", independently and consistently.
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
