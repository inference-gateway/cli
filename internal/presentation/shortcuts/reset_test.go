package shortcuts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// seedResetState creates one file in every runtime directory /reset owns and
// every configuration file /reset must preserve, returning their paths.
func seedResetState(t *testing.T) (stateDirs, configFiles []string) {
	t.Helper()

	userSpace := config.UserSpaceConfigDir()
	runtimeRoot := config.ProjectRuntimeDir()

	stateDirs = []string{
		filepath.Join(runtimeRoot, "conversations"),
		filepath.Join(runtimeRoot, "history"),
		filepath.Join(runtimeRoot, "backups"),
		filepath.Join(runtimeRoot, "tmp"),
		filepath.Join(runtimeRoot, "artifacts"),
		filepath.Join(runtimeRoot, "exports"),
		filepath.Join(userSpace, "artifacts"),
		filepath.Join(userSpace, "plans"),
		filepath.Join(userSpace, "tmp"),
		filepath.Join(userSpace, "logs"),
	}
	for _, dir := range stateDirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "state.txt"), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	configFiles = []string{
		filepath.Join(userSpace, "config.yaml"),
		filepath.Join(userSpace, "projects.json"),
		filepath.Join(userSpace, "shortcuts", "greet.yaml"),
		filepath.Join(userSpace, "skills", "demo", "SKILL.md"),
	}
	for _, file := range configFiles {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("keep me"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return stateDirs, configFiles
}

func jsonlConfig() *config.Config {
	return &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypeJsonl}}
}

// TestResetShortcut_WipesStateKeepsConfig covers the core contract: confirming
// empties every runtime directory while leaving configuration untouched.
func TestResetShortcut_WipesStateKeepsConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir()) // cwd feeds the project slug; any cwd works

	stateDirs, configFiles := seedResetState(t)

	res, err := NewResetShortcut(jsonlConfig(), nil).Execute(context.Background(), []string{"confirm"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("reset failed: %s", res.Output)
	}

	for _, dir := range stateDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("state dir %s was removed instead of recreated: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Errorf("state dir %s still holds %d entries after reset", dir, len(entries))
		}
	}
	for _, file := range configFiles {
		if _, err := os.ReadFile(file); err != nil {
			t.Errorf("config file %s did not survive reset: %v", file, err)
		}
	}
}

// TestResetShortcut_RegisteredAsBuiltin checks /reset is registered in the
// metadata registry that backs /help and `infer shortcuts list`.
func TestResetShortcut_RegisteredAsBuiltin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	reg := NewMetadataRegistry(jsonlConfig())
	if _, ok := reg.Get("reset"); !ok {
		t.Error("reset shortcut missing from the metadata registry")
	}
}

// TestResetShortcut_PreviewRequiresConfirm verifies the y/N gate: /reset lists
// the targets and deletes nothing until it is run again with "confirm".
func TestResetShortcut_PreviewRequiresConfirm(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	stateDirs, configFiles := seedResetState(t)

	res, err := NewResetShortcut(jsonlConfig(), nil).Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("preview failed: %s", res.Output)
	}
	if !strings.Contains(res.Output, "confirm") || !strings.Contains(res.Output, stateDirs[0]) {
		t.Errorf("preview must list the targets and ask for confirmation, got:\n%s", res.Output)
	}

	for _, dir := range stateDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("preview deleted %s without confirmation", dir)
		}
		if len(entries) != 1 {
			t.Errorf("preview emptied %s without confirmation", dir)
		}
	}
	for _, file := range configFiles {
		if _, err := os.ReadFile(file); err != nil {
			t.Errorf("preview removed config file %s", file)
		}
	}
}

// TestResetShortcut_SQLiteAndRemote verifies the store-specific behavior: the
// SQLite database file is deleted, and a remote backend is skipped with a
// notice instead of an error.
func TestResetShortcut_SQLiteAndRemote(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	db := filepath.Join(config.UserSpaceConfigDir(), "conversations.db")
	if err := os.MkdirAll(config.UserSpaceConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(db+suffix, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypeSQLite}}
	res, err := NewResetShortcut(cfg, nil).Execute(context.Background(), []string{"confirm"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("sqlite reset failed: %s", res.Output)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(db + suffix); !os.IsNotExist(err) {
			t.Errorf("sqlite database %s still exists after reset", db+suffix)
		}
	}

	// Remote backend: local dirs are still wiped, the store is skipped.
	stateDirs, _ := seedResetState(t)
	cfg = &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypePostgres}}
	res, err = NewResetShortcut(cfg, nil).Execute(context.Background(), []string{"confirm"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("postgres reset failed: %s", res.Output)
	}
	for _, dir := range stateDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("remote config should not skip local dirs: %s missing: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Errorf("state dir %s not emptied", dir)
		}
	}
	if !strings.Contains(res.Output, "postgres") {
		t.Errorf("expected a postgres skip notice, got:\n%s", res.Output)
	}
}
