package shortcuts

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// ResetShortcut wipes all local runtime state so the agent starts as if freshly
// installed: conversations, plans, scratch dirs, artifacts, history, backups,
// exports and logs. This is machine-wide, not project-scoped - it clears the
// runtime dirs of every project under ~/.infer/projects, which is why the
// preview lists them all. Configuration (config.yaml, custom shortcuts, skills,
// projects.json) and the insights reports are preserved. Remote stores
// (postgres, redis, d1) are skipped - /reset only clears local state.
type ResetShortcut struct {
	cfg      *config.Config
	repo     PersistentConversationRepository
	insights *InsightsGenerator
	store    storage.ConversationStorage

	mu          sync.Mutex
	previewedAt time.Time
}

// confirmWindow is how long a preview arms /reset confirm.
const confirmWindow = 5 * time.Minute

func NewResetShortcut(cfg *config.Config, repo PersistentConversationRepository, insights *InsightsGenerator, store storage.ConversationStorage) *ResetShortcut {
	return &ResetShortcut{cfg: cfg, repo: repo, insights: insights, store: store}
}

func (r *ResetShortcut) GetName() string { return "reset" }
func (r *ResetShortcut) GetDescription() string {
	return "Wipe all local runtime state and start fresh"
}
func (r *ResetShortcut) GetUsage() string              { return "/reset [insights|confirm]" }
func (r *ResetShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (r *ResetShortcut) GetSubcommands() []Subcommand {
	return []Subcommand{
		{Name: "insights", Description: "Analyze the sessions first, then preview the wipe"},
		{Name: "confirm", Description: "Actually delete everything listed in the preview"},
	}
}

func (r *ResetShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	dirs, sqliteDB, remote := r.targets()

	if len(args) == 0 {
		return ShortcutResult{Output: r.preview(dirs, sqliteDB), Success: true}, nil
	}

	switch args[0] {
	case "insights":
		return ShortcutResult{
			Output:  r.withInsights(ctx) + r.preview(dirs, sqliteDB),
			Success: true,
		}, nil
	case "confirm":
		if !r.armed() {
			return ShortcutResult{
				Output: "Nothing has been deleted. This wipes every project on this machine, so it needs a preview first:\n\n" +
					r.preview(dirs, sqliteDB),
				Success: true,
			}, nil
		}
	default:
		return ShortcutResult{
			Output:  fmt.Sprintf("Unknown argument %q - usage: /reset (preview), /reset insights, or /reset confirm", args[0]),
			Success: false,
		}, nil
	}

	// The repo saves the outgoing conversation here and the purge below removes
	// it; starting the new session after the wipe would write it back.
	if r.repo != nil {
		if err := r.repo.StartNewConversation("New Conversation"); err != nil {
			return ShortcutResult{
				Output:  fmt.Sprintf("Failed to start new conversation: %v", err),
				Success: false,
			}, nil
		}
	}

	if sqliteDB != "" {
		r.purge(ctx)
	}

	output, err := r.reset(dirs, sqliteDB)
	if err != nil {
		return ShortcutResult{Output: err.Error(), Success: false}, nil
	}
	if remote != "" {
		output += "\nNote: " + remote + " storage is remote - /reset only clears local state; the remote store was left untouched."
	}

	return ShortcutResult{
		Output:     output,
		Success:    true,
		SideEffect: SideEffectStartNewConversation,
		Data:       "New Conversation",
	}, nil
}

// armed reports whether a preview was shown recently enough to treat the next
// confirm as deliberate. Process-scoped: the accident it guards against is a
// tab-completed /reset confirm, which only exists in the chat TUI.
func (r *ResetShortcut) armed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.previewedAt.IsZero() && time.Since(r.previewedAt) < confirmWindow
}

func (r *ResetShortcut) arm() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.previewedAt = time.Now()
}

// purge empties the store through its own API before the file is unlinked:
// SQLite keeps the deleted inode open, so later writes go nowhere readable.
func (r *ResetShortcut) purge(ctx context.Context) {
	if r.store == nil {
		return
	}
	for {
		summaries, err := r.store.ListConversations(ctx, "", 100, 0)
		if err != nil || len(summaries) == 0 {
			if err != nil {
				logger.Warn("failed to list conversations while resetting", "error", err)
			}
			return
		}
		for _, summary := range summaries {
			if err := r.store.DeleteConversation(ctx, summary.ID); err != nil {
				logger.Warn("failed to delete conversation while resetting", "error", err, "id", summary.ID)
				return
			}
		}
	}
}

// preview describes the wipe without performing it, and arms the confirm.
func (r *ResetShortcut) preview(dirs []string, sqliteDB string) string {
	r.arm()
	return "This permanently deletes all local runtime state, for every project on this machine:\n" +
		listing(dirs, sqliteDB) +
		"\nConfiguration (config.yaml, shortcuts, skills, projects.json) and saved insights are preserved.\n" +
		"Run `/reset confirm` to proceed, or do nothing to cancel."
}

// withInsights renders the analysis that precedes the preview. A failure is
// reported but never blocks the reset.
func (r *ResetShortcut) withInsights(ctx context.Context) string {
	if !r.insights.Available() {
		return "Insights unavailable: they need conversation storage enabled and a configured model.\n\n"
	}
	markdown, path, err := r.insights.Generate(ctx, time.Time{})
	if err != nil {
		return fmt.Sprintf("Could not generate insights: %v\n\n", err)
	}
	return markdown + "\nSaved to " + path + " (this file survives the reset)\n\n"
}

// targets lists everything /reset deletes: the runtime subdirectories of every
// project, the userspace runtime dirs, the log dir and the local conversation
// store. remote names the configured remote backend, if any.
func (r *ResetShortcut) targets() (dirs []string, sqliteDB, remote string) {
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
	dirs = append(dirs, config.DefaultLogsDir())

	if r.cfg != nil && r.cfg.Storage.Enabled {
		switch r.cfg.Storage.Type {
		case config.StorageTypeSQLite:
			sqliteDB = cmp.Or(r.cfg.Storage.SQLite.Path, storage.DefaultSQLitePath())
		case config.StorageTypeJsonl:
			if path := r.cfg.Storage.Jsonl.Path; path != "" {
				dirs = append(dirs, path)
			}
		case config.StorageTypePostgres, config.StorageTypeRedis, config.StorageTypeD1:
			remote = string(r.cfg.Storage.Type)
		}
	}
	return existing(dirs), sqliteDB, remote
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

// reset deletes each target, recreating directories empty. The SQLite database
// is only deleted; SQLite recreates it on demand, and the WAL sidecars go with
// it so no stale write-ahead log resurrects the old data.
func (r *ResetShortcut) reset(dirs []string, sqliteDB string) (string, error) {
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

	output := "Local runtime state wiped and a fresh session started:\n" + listing(removed, wipedDB)
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
