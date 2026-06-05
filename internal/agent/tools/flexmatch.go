package tools

import "strings"

// flexMatchResult is the outcome of a leading/trailing-whitespace-tolerant match. It is
// produced by findFlexibleMatch and consumed by the Edit and MultiEdit tools as a fallback
// after an exact strings.Contains match has failed.
type flexMatchResult struct {
	// matchedBlock is the exact file bytes of the single matched window. Callers replace
	// this (not the model's old_string) so the file's real indentation is preserved.
	matchedBlock string
	// reindentedNew is new_string re-indented to the file's base indentation.
	reindentedNew string
	// found is true only when exactly one window matched under a single uniform shift.
	found bool
}

// findFlexibleMatch attempts a whitespace-tolerant match of oldString within content. It is
// intended ONLY as a fallback once an exact strings.Contains match has already failed, and it
// deliberately refuses to guess: found is true only when exactly one block of file lines
// matches oldString after trimming surrounding whitespace AND the indentation differs by a
// single uniform shift that reproduces every matched line exactly. On found, matchedBlock holds
// the exact file text to replace and reindentedNew holds newString re-aligned to the file's
// indentation. Any ambiguity (zero or multiple candidate blocks, or a non-uniform shift)
// returns found=false, and the caller must fall back to its existing error path.
func findFlexibleMatch(content, oldString, newString string) flexMatchResult {
	miss := flexMatchResult{}

	oldLines := strings.Split(oldString, "\n")
	fileLines := strings.Split(content, "\n")
	if len(oldLines) > len(fileLines) {
		return miss
	}
	if firstNonBlankIndex(oldLines) < 0 {
		return miss // whitespace-only old_string: nothing meaningful to anchor on
	}

	firstIdx, count := countFlexWindows(fileLines, oldLines)
	if count != 1 {
		return miss
	}

	fileWin := fileLines[firstIdx : firstIdx+len(oldLines)]
	block, reindented, ok := verifyUniformAndReindent(fileWin, oldLines, strings.Split(newString, "\n"))
	if !ok {
		return miss
	}
	return flexMatchResult{matchedBlock: block, reindentedNew: reindented, found: true}
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

// verifyUniformAndReindent confirms the indentation difference between the matched file window
// and oldLines is a single uniform shift, then re-indents newLines with that same shift. ok is
// false when the shift is not uniform — i.e. applying it to any non-blank old line fails to
// reproduce the corresponding file line — in which case callers must not apply the edit. block
// is the exact file window text; reindented is newString aligned to the file's indentation.
func verifyUniformAndReindent(fileWin, oldLines, newLines []string) (block, reindented string, ok bool) {
	k := firstNonBlankIndex(oldLines)
	if k < 0 {
		return "", "", false
	}
	oldBase := leadingWhitespace(oldLines[k])
	fileBase := leadingWhitespace(fileWin[k])

	for j := range oldLines {
		if strings.TrimSpace(oldLines[j]) == "" {
			continue // blank lines carry no indentation signal
		}
		got, prefixed := reindentLine(oldLines[j], oldBase, fileBase)
		if !prefixed || strings.TrimRight(got, " \t\r") != strings.TrimRight(fileWin[j], " \t\r") {
			return "", "", false
		}
	}

	out := make([]string, len(newLines))
	for j, line := range newLines {
		if strings.TrimSpace(line) == "" {
			out[j] = line
			continue
		}
		// Lines sharing the old base indent are swapped to the file base; lines the model
		// wrote at a shallower/different indent are kept verbatim (its explicit choice).
		got, _ := reindentLine(line, oldBase, fileBase)
		out[j] = got
	}

	return strings.Join(fileWin, "\n"), strings.Join(out, "\n"), true
}

// reindentLine rewrites a line's leading whitespace by stripping the oldBase prefix and
// prepending fileBase, preserving any indentation beyond the base and the remainder of the
// line. prefixed is false when the line's leading whitespace does not start with oldBase (a
// shallower or inconsistent indent); in that case the original line is returned unchanged.
func reindentLine(line, oldBase, fileBase string) (result string, prefixed bool) {
	lw := leadingWhitespace(line)
	if !strings.HasPrefix(lw, oldBase) {
		return line, false
	}
	return fileBase + lw[len(oldBase):] + line[len(lw):], true
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
