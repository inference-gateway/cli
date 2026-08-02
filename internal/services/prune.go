package services

import (
	"os"
	"path/filepath"
	"slices"
	"time"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// pruneFilesByModTime removes files in dir that fall outside the retention
// window: when keep > 0 only the newest keep files matching match survive, and
// when maxAge > 0 files older than maxAge are removed regardless of count.
// Zero values disable the respective limit. Only files accepted by match are
// ever touched, so a producer's unrelated files stay safe.
func pruneFilesByModTime(dir string, keep int, maxAge time.Duration, match func(os.DirEntry) bool) {
	if keep <= 0 && maxAge <= 0 {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && match(e) {
			files = append(files, e)
		}
	}

	slices.SortFunc(files, func(a, b os.DirEntry) int {
		fi, _ := a.Info()
		fj, _ := b.Info()
		if fi == nil || fj == nil {
			return 0
		}
		return fi.ModTime().Compare(fj.ModTime())
	})

	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}

	for i, e := range files {
		overCount := keep > 0 && i < len(files)-keep
		tooOld := false
		if !cutoff.IsZero() {
			if fi, err := e.Info(); err == nil && fi.ModTime().Before(cutoff) {
				tooOld = true
			}
		}
		if !overCount && !tooOld {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			logger.Warn("failed to prune file", "path", path, "error", err)
		}
	}
}
