package tools

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// compactMaxFilesPerDir caps how many files a single directory line lists in
// the compact format; the overall line budget comes from the max_files arg.
const compactMaxFilesPerDir = 25

// compactProjectListing produces the compact one-directory-per-line listing
// for a git repository rooted at path, or "" when path is not inside a git
// repository (callers fall back to the text tree).
func compactProjectListing(path string, maxLines int) string {
	return formatProjectListing(gitProjectFiles(path), maxLines, compactMaxFilesPerDir)
}

// gitProjectFiles returns tracked plus untracked non-ignored file paths
// (relative, slash-separated) or nil when path is not in a git repository.
func gitProjectFiles(path string) []string {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// formatProjectListing groups file paths one directory per line, ordered by
// directory depth then name so root files come first and shallow structure
// survives the line budget. Hidden files and directories (any dot-prefixed
// path component) are omitted to save tokens - the text format still shows
// them on demand. To save further tokens, a "name.go*" entry means both
// name.go and name_test.go exist in that directory. Both the per-directory
// file list and the total line count are capped, with explicit "+N more"
// markers so truncation is never silent.
func formatProjectListing(paths []string, maxLines, maxFilesPerDir int) string {
	byDir := make(map[string][]string)
	for _, p := range paths {
		if p == "" || strings.HasPrefix(p, ".") || strings.Contains(p, "/.") {
			continue
		}
		byDir[filepath.Dir(p)] = append(byDir[filepath.Dir(p)], filepath.Base(p))
	}
	if len(byDir) == 0 {
		return ""
	}
	for dir, files := range byDir {
		byDir[dir] = collapseTestSiblings(files)
	}

	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		di, dj := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/")
		if dirs[i] == "." {
			di = -1
		}
		if dirs[j] == "." {
			dj = -1
		}
		if di != dj {
			return di < dj
		}
		return dirs[i] < dirs[j]
	})

	var b strings.Builder
	for i, dir := range dirs {
		if i >= maxLines {
			fmt.Fprintf(&b, "... +%d more directories\n", len(dirs)-i)
			break
		}
		files := byDir[dir]
		sort.Strings(files)
		suffix := ""
		if len(files) > maxFilesPerDir {
			suffix = fmt.Sprintf(",+%d more", len(files)-maxFilesPerDir)
			files = files[:maxFilesPerDir]
		}
		fmt.Fprintf(&b, "%s/: %s%s\n", dir, strings.Join(files, ","), suffix)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// collapseTestSiblings folds name_test.go into a "name.go*" marker when
// name.go exists in the same directory; standalone test files stay listed.
func collapseTestSiblings(files []string) []string {
	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[f] = true
	}
	out := files[:0]
	for _, f := range files {
		if base, ok := strings.CutSuffix(f, "_test.go"); ok && present[base+".go"] {
			continue
		}
		if present[strings.TrimSuffix(f, ".go")+"_test.go"] && strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
			f += "*"
		}
		out = append(out, f)
	}
	return out
}
