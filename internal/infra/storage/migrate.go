package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	yaml "gopkg.in/yaml.v3"
)

// MigrateLegacyArtifacts imports legacy on-disk schedules, plans, and shell
// history into the configured backend when the backend is not jsonl (the
// default file-based backend). Idempotent: after a successful migration the
// legacy directory is renamed with a .migrated-<timestamp> suffix so subsequent
// runs are no-ops.
func MigrateLegacyArtifacts(ctx context.Context, cfg *config.Config, stores *Stores) error {
	if cfg.Storage.Type == "jsonl" || cfg.Storage.Type == "memory" {
		// JSONL is the file-based backend - artifacts are already on disk
		// where the JSONL backend reads them. Memory backend has no
		// persistence to migrate.
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	// Migrate scheduled jobs from ~/.infer/schedules/
	schedulesDir := filepath.Join(home, config.ConfigDirName, "schedules")
	if err := migrateSchedules(ctx, schedulesDir, stores.ScheduledJobs); err != nil {
		return fmt.Errorf("migrate schedules: %w", err)
	}

	// Migrate plans from <configDir>/plans/
	plansDir := filepath.Join(cfg.GetConfigDir(), "plans")
	if err := migratePlans(ctx, plansDir, stores.Plans); err != nil {
		return fmt.Errorf("migrate plans: %w", err)
	}

	// Migrate shell history from <configDir>/history/history
	historyFile := filepath.Join(cfg.GetConfigDir(), "history", "history")
	if err := migrateShellHistory(ctx, historyFile, stores.ShellHistory); err != nil {
		return fmt.Errorf("migrate shell history: %w", err)
	}

	return nil
}

// migrateSchedules imports YAML schedule files from dir into the store, then
// renames the directory to dir.migrated-<timestamp>.
func migrateSchedules(ctx context.Context, dir string, store ScheduledJobStorage) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read schedules dir: %w", err)
	}

	imported := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var job domain.ScheduledJob
		if err := yaml.Unmarshal(data, &job); err != nil {
			continue
		}
		if err := store.SaveJob(ctx, &job); err != nil {
			return fmt.Errorf("save job %s: %w", job.ID, err)
		}
		imported++
	}

	if imported == 0 {
		return nil
	}

	// Rename the directory to mark it as migrated
	migratedDir := dir + ".migrated-" + time.Now().UTC().Format("20060102-150405")
	if err := os.Rename(dir, migratedDir); err != nil {
		return fmt.Errorf("rename schedules dir %s -> %s: %w", dir, migratedDir, err)
	}
	return nil
}

// migratePlans imports markdown plan files from dir into the store, then
// renames the directory to dir.migrated-<timestamp>.
func migratePlans(ctx context.Context, dir string, store PlanStorage) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read plans dir: %w", err)
	}

	imported := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		plan := parsePlanFile(stem, data)
		if err := store.SavePlan(ctx, plan); err != nil {
			return fmt.Errorf("save plan %s: %w", plan.ID, err)
		}
		imported++
	}

	if imported == 0 {
		return nil
	}

	migratedDir := dir + ".migrated-" + time.Now().UTC().Format("20060102-150405")
	if err := os.Rename(dir, migratedDir); err != nil {
		return fmt.Errorf("rename plans dir %s -> %s: %w", dir, migratedDir, err)
	}
	return nil
}

// migrateShellHistory imports lines from the history file into the store, then
// renames the file to file.migrated-<timestamp>.
func migrateShellHistory(ctx context.Context, path string, store ShellHistoryStorage) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read history file: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	imported := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		unescaped := strings.ReplaceAll(line, "\\n", "\n")
		if err := store.AppendHistory(ctx, unescaped); err != nil {
			return fmt.Errorf("append history: %w", err)
		}
		imported++
	}

	if imported == 0 {
		return nil
	}

	migratedPath := path + ".migrated-" + time.Now().UTC().Format("20060102-150405")
	if err := os.Rename(path, migratedPath); err != nil {
		return fmt.Errorf("rename history file %s -> %s: %w", path, migratedPath, err)
	}
	return nil
}
