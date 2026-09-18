package shortcuts

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// ResetShortcut wipes all local runtime state so the agent starts as if freshly
// installed: conversations, plans, scratch dirs, artifacts, history, backups,
// exports and logs. Configuration (config.yaml, custom shortcuts, skills,
// projects.json) is preserved. Remote stores (postgres, redis, d1) are skipped -
// /reset only clears local state.
type ResetShortcut struct {
	cfg  *config.Config
	repo PersistentConversationRepository
}

func NewResetShortcut(cfg *config.Config, repo PersistentConversationRepository) *ResetShortcut {
	return &ResetShortcut{cfg: cfg, repo: repo}
}

func (r *ResetShortcut) GetName() string { return "reset" }
func (r *ResetShortcut) GetDescription() string {
	return "Wipe all local runtime state and start fresh"
}
func (r *ResetShortcut) GetUsage() string              { return "/reset [confirm]" }
func (r *ResetShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (r *ResetShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	dirs, sqliteDB, remote := r.targets()

	if len(args) == 0 {
		return ShortcutResult{
			Output: "This permanently deletes all local runtime state:\n" +
				listing(dirs, sqliteDB) +
				"\nConfiguration (config.yaml, shortcuts, skills, projects.json) is preserved.\n" +
				"Proceed (y/N)? Run `/reset confirm` to proceed, or do nothing to cancel.",
			Success: true,
		}, nil
	}

	if args[0] != "confirm" {
		return ShortcutResult{
			Output:  fmt.Sprintf("Unknown argument %q - usage: /reset (preview) or /reset confirm", args[0]),
			Success: false,
		}, nil
	}

	if r.repo != nil {
		if err := r.repo.StartNewConversation("New Conversation"); err != nil {
			return ShortcutResult{
				Output:  fmt.Sprintf("Failed to start new conversation: %v", err),
				Success: false,
			}, nil
		}
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
	return dirs, sqliteDB, remote
}

// reset deletes each target, recreating directories empty. The SQLite database
// is only deleted; SQLite recreates it on demand, and the WAL sidecars go with
// it so no stale write-ahead log resurrects the old data.
func (r *ResetShortcut) reset(dirs []string, sqliteDB string) (string, error) {
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("failed to remove %s: %w", dir, err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("failed to recreate %s: %w", dir, err)
		}
	}

	if sqliteDB != "" {
		for _, path := range []string{sqliteDB, sqliteDB + "-wal", sqliteDB + "-shm", sqliteDB + "-journal"} {
			if err := os.RemoveAll(path); err != nil {
				return "", fmt.Errorf("failed to remove %s: %w", path, err)
			}
		}
	}

	return "Local runtime state wiped and a fresh session started:\n" + listing(dirs, sqliteDB), nil
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
