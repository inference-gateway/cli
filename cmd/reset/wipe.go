package reset

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// wiper removes all local runtime state so the agent starts as if freshly
// installed: conversations, plans, scratch dirs, artifacts, history, backups,
// exports, logs, telemetry, scheduled jobs, pid/lock files, and the userspace
// tmp tree (generated speech, retained recordings, channel media). This is
// machine-wide, not project-scoped - it clears the runtime dirs of every project
// under ~/.infer/projects, which is why the preview lists them all.
// Configuration (config.yaml, custom shortcuts, skills, projects.yaml) and the
// insights reports are preserved. Remote stores (postgres, redis, d1) are
// skipped. Dirs explicitly overridden outside ~/.infer (e.g.
// text_to_speech.output_dir pointed at /data/tts) are outside the userspace
// layer and are left alone.
type wiper struct {
	cfg   *config.Config
	store storage.ConversationStorage
}

// targets lists everything a wipe deletes. remote names the configured remote
// backend, if any.
func (w *wiper) targets() (dirs []string, sqliteDB, remote string) {
	userSpace := config.UserSpaceConfigDir()
	projectsRoot := filepath.Join(userSpace, config.ProjectsDirName)

	if siblings, err := os.ReadDir(projectsRoot); err == nil {
		for _, sibling := range siblings {
			if !sibling.IsDir() {
				continue
			}
			project := filepath.Join(projectsRoot, sibling.Name())
			for _, name := range config.RuntimeArtifactDirNames {
				dirs = append(dirs, filepath.Join(project, name))
			}
			dirs = append(dirs, filepath.Join(project, "conversations"))
		}
	}

	for _, name := range config.UserspaceRuntimeDirNames {
		dirs = append(dirs, filepath.Join(userSpace, name))
	}
	dirs = append(dirs,
		config.TelemetryDir(),
		filepath.Join(userSpace, "schedules"),
		filepath.Join(userSpace, "run"),
		config.DefaultLogsDir(),
	)

	if w.cfg != nil && w.cfg.Storage.Enabled {
		switch w.cfg.Storage.Type {
		case config.StorageTypeSQLite:
			sqliteDB = cmp.Or(w.cfg.Storage.SQLite.Path, storage.DefaultSQLitePath())
		case config.StorageTypeJsonl:
			if path := w.cfg.Storage.Jsonl.Path; path != "" {
				dirs = append(dirs, path)
			}
		case config.StorageTypePostgres, config.StorageTypeRedis, config.StorageTypeD1:
			remote = string(w.cfg.Storage.Type)
		}
	}
	return existing(dirs), sqliteDB, remote
}

// preview describes the wipe without performing it.
func preview(dirs []string, sqliteDB string) string {
	return "This permanently deletes all local runtime state, for every project on this machine:\n" +
		listing(dirs, sqliteDB) +
		"\nConfiguration (config.yaml, shortcuts, skills, projects.yaml) and saved insights are preserved.\n" +
		"Run `infer reset confirm` to proceed, or do nothing to cancel."
}

// existing drops targets that are not on disk, so the preview promises only
// what it will delete.
func existing(dirs []string) []string {
	present := dirs[:0]
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err == nil {
			present = append(present, dir)
		}
	}
	return present
}

// purge empties the store through its own API before the file is unlinked:
// SQLite keeps the deleted inode open, so later writes go nowhere readable.
func (w *wiper) purge(ctx context.Context) {
	if w.store == nil {
		return
	}
	for {
		summaries, err := w.store.ListConversations(ctx, "", 100, 0)
		if err != nil || len(summaries) == 0 {
			if err != nil {
				logger.Warn("failed to list conversations while resetting", "error", err)
			}
			return
		}
		for _, summary := range summaries {
			if err := w.store.DeleteConversation(ctx, summary.ID); err != nil {
				logger.Warn("failed to delete conversation while resetting", "error", err, "id", summary.ID)
				return
			}
		}
	}
}

// wipe deletes each target, recreating directories empty. The SQLite database is
// only deleted; SQLite recreates it on demand, and the WAL sidecars go with it so
// no stale write-ahead log resurrects the old data.
func (w *wiper) wipe(ctx context.Context, dirs []string, sqliteDB string) (string, error) {
	if sqliteDB != "" {
		w.purge(ctx)
	}

	var errs []error
	removed := make([]string, 0, len(dirs))

	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove %s: %w", dir, err))
			continue
		}
		removed = append(removed, dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			errs = append(errs, fmt.Errorf("failed to recreate %s: %w", dir, err))
		}
	}

	wipedDB := ""
	if sqliteDB != "" {
		wipedDB = sqliteDB
		for _, path := range []string{sqliteDB, sqliteDB + "-wal", sqliteDB + "-shm", sqliteDB + "-journal"} {
			if err := os.RemoveAll(path); err != nil {
				errs = append(errs, fmt.Errorf("failed to remove %s: %w", path, err))
				wipedDB = ""
			}
		}
	}

	output := "Local runtime state wiped:\n" + listing(removed, wipedDB)
	if len(errs) > 0 {
		return output, fmt.Errorf("%s\n\nSome targets could not be removed:\n%w", output, errors.Join(errs...))
	}
	return output, nil
}

// listing renders the targets as an indented bullet list.
func listing(dirs []string, sqliteDB string) string {
	var sb strings.Builder
	for _, dir := range dirs {
		fmt.Fprintln(&sb, "  - "+dir)
	}
	if sqliteDB != "" {
		fmt.Fprintln(&sb, "  - "+sqliteDB)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}
