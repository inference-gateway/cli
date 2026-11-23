package infrastructure

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// JSONLLog keeps one room's log as an append-only JSONL file at
// ~/.infer/projects/<slug of the room's project>/rooms/<room>.jsonl.
type JSONLLog struct{}

// NewJSONLLog returns the file-backed room log writing under the userspace
// projects root.
func NewJSONLLog() *JSONLLog {
	return &JSONLLog{}
}

// Append adds post to the room's log, stamping a post that carries no time.
// The log is append-only: the file is always opened for appending.
func (l *JSONLLog) Append(room roomsdomain.Room, post roomsdomain.Post) error {
	path, err := l.path(room)
	if err != nil {
		return err
	}
	if post.Time.IsZero() {
		post.Time = time.Now()
	}
	line, err := json.Marshal(post)
	if err != nil {
		return fmt.Errorf("encoding room post: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening room log %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("appending room log %s: %w", path, err)
	}
	return nil
}

// Read returns the room's posts in the order they were appended. A room whose
// log was never written has none.
func (l *JSONLLog) Read(room roomsdomain.Room) ([]roomsdomain.Post, error) {
	path, err := l.path(room)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading room log %s: %w", path, err)
	}
	var posts []roomsdomain.Post
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var post roomsdomain.Post
		if err := json.Unmarshal([]byte(line), &post); err != nil {
			return nil, fmt.Errorf("decoding room log %s: %w", path, err)
		}
		posts = append(posts, post)
	}
	return posts, nil
}

// path is where a room's log lives: the userspace projects root, the slug of
// the room's project, under rooms/. The name check is repeated here because
// the path is built from it, so a bad name cannot escape the rooms dir even on
// a caller that skipped config validation.
func (l *JSONLLog) path(room roomsdomain.Room) (string, error) {
	if !roomsdomain.ValidName(room.Name) {
		return "", fmt.Errorf("invalid room name %q: must keep to letters, digits, dashes and underscores", room.Name)
	}
	return filepath.Join(config.UserSpaceConfigDir(), config.ProjectsDirName, config.ProjectSlug(room.Project), "rooms", room.Name+".jsonl"), nil
}
