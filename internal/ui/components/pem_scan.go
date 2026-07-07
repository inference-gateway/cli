package components

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// pemCandidate is a discovered private-key file offered in the setup wizard.
type pemCandidate struct {
	Path    string
	ModTime time.Time
}

// pemScanLimits bound the filesystem scan so it can run synchronously in the
// TUI without a noticeable stall, even on large home directories.
const (
	pemScanMaxEntries = 5000
	pemScanMaxResults = 15
)

// pemScanSkipDirs are directory names never descended into during the scan.
var pemScanSkipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
}

// scanPemFiles searches the places a GitHub App private key typically lives
// (~/Downloads, ~/Desktop, the home directory, and the working directory) for
// .pem files, newest first.
func scanPemFiles() []pemCandidate {
	type root struct {
		dir   string
		depth int
	}

	var roots []root
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots,
			root{filepath.Join(home, "Downloads"), 2},
			root{filepath.Join(home, "Desktop"), 2},
			root{home, 1},
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, root{cwd, 2})
	}

	seen := make(map[string]bool)
	var candidates []pemCandidate
	budget := pemScanMaxEntries
	for _, r := range roots {
		scanPemDir(r.dir, r.depth, &budget, seen, &candidates)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ModTime.After(candidates[j].ModTime)
	})
	if len(candidates) > pemScanMaxResults {
		candidates = candidates[:pemScanMaxResults]
	}
	return candidates
}

func scanPemDir(dir string, depth int, budget *int, seen map[string]bool, out *[]pemCandidate) {
	if depth < 0 || *budget <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if *budget <= 0 {
			return
		}
		*budget--
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			if !pemScanSkipDirs[name] {
				scanPemDir(filepath.Join(dir, name), depth-1, budget, seen, out)
			}
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".pem") {
			continue
		}
		path := filepath.Join(dir, name)
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		info, err := entry.Info()
		if err != nil {
			continue
		}
		*out = append(*out, pemCandidate{Path: abs, ModTime: info.ModTime()})
	}
}

// pemCandidateLabel renders a candidate for the wizard select: the file name,
// its ~-relative directory, and a humanized age.
func pemCandidateLabel(c pemCandidate) string {
	dir := filepath.Dir(c.Path)
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	return fmt.Sprintf("%s  (%s, %s)", filepath.Base(c.Path), dir, humanizeAge(time.Since(c.ModTime)))
}

func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}
