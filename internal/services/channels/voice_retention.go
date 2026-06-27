package channels

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// recordingFilePrefix tags retained recordings so pruning only ever touches
// files this package wrote, never anything else a user drops in the directory.
const recordingFilePrefix = "infer-voice-"

// VoiceRetention persists a bounded number of inbound voice/audio recordings to
// a local directory, pruning the oldest once the count exceeds Keep. A nil
// *VoiceRetention or a Keep <= 0 disables retention (the default behavior).
//
// save serializes on mu because the channel manager may process voice messages
// concurrently and they all share one directory.
type VoiceRetention struct {
	Dir  string
	Keep int
	mu   sync.Mutex
}

// save writes data to the retention directory using the extension implied by
// hintPath (the Telegram file path), then prunes the oldest recordings beyond
// Keep. It is best-effort: callers log and continue on error. Returns the path
// written, or "" when retention is disabled.
func (r *VoiceRetention) save(hintPath string, data []byte) (string, error) {
	if r == nil || r.Keep <= 0 {
		return "", nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return "", fmt.Errorf("creating recordings dir: %w", err)
	}

	f, err := os.CreateTemp(r.Dir, recordingFilePrefix+"*"+filepath.Ext(hintPath))
	if err != nil {
		return "", fmt.Errorf("creating recording file: %w", err)
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", fmt.Errorf("writing recording: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("closing recording: %w", err)
	}

	pruneRecordings(r.Dir, r.Keep)
	return name, nil
}

// pruneRecordings removes the oldest retained recordings in dir once their count
// exceeds keep. A keep <= 0 is a no-op. Mirrors pruneSessionImages in the
// channel manager.
func pruneRecordings(dir string, keep int) {
	if keep <= 0 {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), recordingFilePrefix) {
			files = append(files, e)
		}
	}

	if len(files) <= keep {
		return
	}

	slices.SortFunc(files, func(a, b os.DirEntry) int {
		fa, _ := a.Info()
		fb, _ := b.Info()
		if fa == nil || fb == nil {
			return 0
		}
		return fa.ModTime().Compare(fb.ModTime())
	})

	for _, e := range files[:len(files)-keep] {
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			logger.Warn("failed to prune voice recording", "path", path, "error", err)
			continue
		}
		logger.Info("pruned old voice recording", "path", path)
	}
}
