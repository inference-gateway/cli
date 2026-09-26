package reset

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// wiper removes all local runtime state so the agent starts as if freshly
// installed: conversations, plans, scratch dirs, artifacts, history, backups,
// exports, logs, telemetry, scheduled jobs, pid/lock files and the userspace
// tmp tree - machine-wide, across every project under ~/.infer/projects.
// Config, shortcuts, skills, insights and remote stores are preserved; dirs outside ~/.infer are left alone.
type wiper struct {
	cfg   *config.Config
	store storage.ConversationStorage
}

// targets is what a wipe removes. prune dirs are deleted outright; empty dirs
// are deleted and recreated because they are fixed userspace locations. The
// per-project runtime dirs are pruned so a reset leaves no empty skeleton
// behind - the project dir itself stays, since projects.yaml references it.
type targets struct {
	prune     []string
	empty     []string
	sqliteDB  string
	remote    string
	gitMemory bool
}

// all lists every directory the wipe touches, pruned dirs first, for the
// preview and the result listing.
func (t targets) all() []string {
	return append(append([]string{}, t.prune...), t.empty...)
}

// resolve lists everything a wipe deletes, dropping targets that are not on
// disk so the preview promises only what it will remove.
func (w *wiper) resolve() targets {
	userSpace := config.UserSpaceConfigDir()
	projectsRoot := filepath.Join(userSpace, config.ProjectsDirName)

	var t targets
	if siblings, err := os.ReadDir(projectsRoot); err == nil {
		for _, sibling := range siblings {
			if !sibling.IsDir() {
				continue
			}
			project := filepath.Join(projectsRoot, sibling.Name())
			if stale(sibling.Name()) {
				t.prune = append(t.prune, project)
				continue
			}
			for _, name := range config.RuntimeArtifactDirNames {
				t.prune = append(t.prune, filepath.Join(project, name))
			}
			t.prune = append(t.prune, filepath.Join(project, "conversations"))
		}
	}

	for _, name := range config.UserspaceRuntimeDirNames {
		t.empty = append(t.empty, filepath.Join(userSpace, name))
	}
	t.empty = append(t.empty,
		config.TelemetryDir(),
		filepath.Join(userSpace, "schedules"),
		filepath.Join(userSpace, "run"),
		config.DefaultLogsDir(),
	)

	if w.cfg != nil {
		if memoryDir, err := w.cfg.ResolveMemoryDir(); err == nil {
			t.empty = append(t.empty, memoryDir)
			t.gitMemory = w.cfg.Memory.Backend.Type == "git"
		}
	}

	if w.cfg != nil && w.cfg.Storage.Enabled {
		switch w.cfg.Storage.Type {
		case config.StorageTypeSQLite:
			t.sqliteDB = cmp.Or(w.cfg.Storage.SQLite.Path, storage.DefaultSQLitePath())
		case config.StorageTypeJsonl:
			if path := w.cfg.Storage.Jsonl.Path; path != "" {
				t.empty = append(t.empty, path)
			}
		case config.StorageTypePostgres, config.StorageTypeRedis, config.StorageTypeD1:
			t.remote = string(w.cfg.Storage.Type)
		}
	}

	t.prune, t.empty = existing(t.prune), existing(t.empty)
	return t
}

// stale reports whether a project slug names a working directory that no longer
// exists, in which case the whole runtime dir goes rather than just its
// subdirectories. The slug is the cwd with every separator replaced by "-"
// (config.projectRuntimeSlug), which is ambiguous - /a/my-project and
// /a/my/project collide - so a slug is stale only when NO reading of it exists on disk.
func stale(slug string) bool {
	if !strings.HasPrefix(slug, "-") {
		return false // "workspace", "default": not an absolute path, never stale
	}
	return !resolves(string(filepath.Separator), strings.Split(strings.TrimPrefix(slug, "-"), "-"))
}

// resolves walks the slug parts, treating each "-" as either a separator or a
// literal dash, and reports whether any reading resolves to a real directory.
// Every candidate must exist before the walk continues, so the filesystem prunes
// the search instead of it enumerating all 2^n splits.
func resolves(dir string, parts []string) bool {
	if len(parts) == 0 {
		return true
	}
	segment := ""
	for i, part := range parts {
		if i > 0 {
			segment += "-"
		}
		segment += part
		next := filepath.Join(dir, segment)
		if info, err := os.Stat(next); err != nil || !info.IsDir() {
			continue
		}
		if resolves(next, parts[i+1:]) {
			return true
		}
	}
	return false
}

// sqlitePaths lists the database file and its WAL sidecars; all of them are
// removed, and all of them count toward the reported space.
func sqlitePaths(db string) []string {
	return []string{db, db + "-wal", db + "-shm", db + "-journal"}
}

// preview describes the wipe without performing it, ending with the total
// space the wipe would reclaim.
func preview(t targets) string {
	var reclaimable int64
	for _, dir := range t.all() {
		reclaimable += size(dir)
	}
	if t.sqliteDB != "" {
		for _, path := range sqlitePaths(t.sqliteDB) {
			reclaimable += size(path)
		}
	}
	return "This permanently deletes all local runtime state, for every project on this machine:\n" +
		listing(t.all(), t.sqliteDB) +
		"\nConfiguration (config.yaml, shortcuts, skills, projects.yaml) and saved insights are preserved.\n" +
		"Run `infer reset confirm` to proceed, or do nothing to cancel.\n" +
		"Total reclaimable space: " + formatting.FormatBytes(reclaimable)
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

// size best-effort reports how many bytes a path occupies, walking whole
// directory trees. Unreadable entries are skipped, never failed: the total is
// a report for the human, not an audit, and must not break the reset.
func size(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
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

// wipe removes each target. Pruned dirs are deleted outright; the rest are
// recreated empty because they are fixed userspace locations. The SQLite
// database is only deleted; SQLite recreates it on demand, and the WAL sidecars
// go with it so no stale write-ahead log resurrects the old data.
func (w *wiper) wipe(ctx context.Context, t targets) (string, error) {
	if t.sqliteDB != "" {
		w.purge(ctx)
	}

	var errs []error
	var reclaimed int64
	removed := make([]string, 0, len(t.prune)+len(t.empty))

	for _, dir := range t.prune {
		occupied := size(dir)
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove %s: %w", dir, err))
			continue
		}
		reclaimed += occupied
		removed = append(removed, dir)
	}

	for _, dir := range t.empty {
		occupied := size(dir)
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove %s: %w", dir, err))
			continue
		}
		reclaimed += occupied
		removed = append(removed, dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			errs = append(errs, fmt.Errorf("failed to recreate %s: %w", dir, err))
		}
	}

	wipedDB := ""
	if t.sqliteDB != "" {
		wipedDB = t.sqliteDB
		for _, path := range sqlitePaths(t.sqliteDB) {
			occupied := size(path)
			if err := os.RemoveAll(path); err != nil {
				errs = append(errs, fmt.Errorf("failed to remove %s: %w", path, err))
				wipedDB = ""
				continue
			}
			reclaimed += occupied
		}
	}

	output := "Local runtime state wiped:\n" + listing(removed, wipedDB)
	if t.remote != "" {
		output += "\nNote: " + t.remote + " storage is remote - only local state was cleared; the remote store was left untouched."
	}
	if t.gitMemory {
		output += "\nNote: memory is backed by a git remote - the local clone was cleared, and the remote memory syncs back on the next run."
	}
	output += "\nTotal reclaimed space: " + formatting.FormatBytes(reclaimed)
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
