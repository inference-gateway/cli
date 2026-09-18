package reset

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	storagemocks "github.com/inference-gateway/cli/tests/mocks/storage"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// seedResetState creates one file in every runtime directory a reset owns and
// every configuration file a reset must preserve, returning their paths.
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
		filepath.Join(userSpace, "telemetry"),
		filepath.Join(userSpace, "schedules"),
		filepath.Join(userSpace, "run"),
	}
	for _, dir := range stateDirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "state.txt"), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, name := range []string{"tts", "voice", "media"} {
		media := filepath.Join(userSpace, "tmp", name)
		if err := os.MkdirAll(media, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(media, "state.txt"), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	configFiles = []string{
		filepath.Join(userSpace, "config.yaml"),
		filepath.Join(userSpace, "projects.yaml"),
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

// confirmWipe resolves the targets and performs the wipe, as `infer reset
// confirm` does.
func confirmWipe(t *testing.T, w *wiper) string {
	t.Helper()
	dirs, sqliteDB, _ := w.targets()
	output, err := w.wipe(context.Background(), dirs, sqliteDB)
	if err != nil {
		t.Fatalf("wipe failed: %v", err)
	}
	return output
}

func jsonlWiper(store storage.ConversationStorage) *wiper {
	return &wiper{
		cfg:   &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypeJsonl}},
		store: store,
	}
}

// TestWipeKeepsConfig covers the core contract: a wipe empties every runtime
// directory while leaving configuration untouched.
func TestWipeKeepsConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	stateDirs, configFiles := seedResetState(t)

	confirmWipe(t, jsonlWiper(nil))

	for _, dir := range stateDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("state dir %s was removed instead of recreated: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Errorf("state dir %s still holds %d entries after reset", dir, len(entries))
		}
	}
	for _, name := range []string{"tts", "voice", "media"} {
		media := filepath.Join(config.UserSpaceConfigDir(), "tmp", name)
		if _, err := os.Stat(filepath.Join(media, "state.txt")); !os.IsNotExist(err) {
			t.Errorf("%s not emptied: state file survived the reset", media)
		}
	}
	for _, file := range configFiles {
		if _, err := os.ReadFile(file); err != nil {
			t.Errorf("config file %s did not survive reset: %v", file, err)
		}
	}
}

// TestWipeInsightsReportsSurvive keeps the saved analysis out of the blast
// radius - the reports are the one thing worth reading after a reset.
func TestWipeInsightsReportsSurvive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	seedResetState(t)

	report := filepath.Join(config.InsightsDir(), "20260918-120000.md")
	if err := os.MkdirAll(filepath.Dir(report), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, []byte("# Insights"), 0o644); err != nil {
		t.Fatal(err)
	}

	confirmWipe(t, jsonlWiper(nil))

	if _, err := os.ReadFile(report); err != nil {
		t.Errorf("insights report did not survive reset: %v", err)
	}
}

// TestWipeSQLiteAndRemote verifies the store-specific behavior: the SQLite
// database file is deleted, and a remote backend is skipped with a notice.
func TestWipeSQLiteAndRemote(t *testing.T) {
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

	sqlite := &wiper{cfg: &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypeSQLite}}}
	confirmWipe(t, sqlite)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(db + suffix); !os.IsNotExist(err) {
			t.Errorf("sqlite database %s still exists after reset", db+suffix)
		}
	}

	stateDirs, _ := seedResetState(t)
	remoteWiper := &wiper{cfg: &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypePostgres}}}
	dirs, sqliteDB, remote := remoteWiper.targets()
	if remote != "postgres" {
		t.Errorf("expected a postgres skip notice, got %q", remote)
	}
	if _, err := remoteWiper.wipe(context.Background(), dirs, sqliteDB); err != nil {
		t.Fatalf("postgres reset failed: %v", err)
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
}

// TestPreviewSkipsMissingTargets keeps the preview honest: it must promise only
// what is actually on disk.
func TestPreviewSkipsMissingTargets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	present := filepath.Join(config.UserSpaceConfigDir(), "plans")
	if err := os.MkdirAll(present, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs, sqliteDB, _ := jsonlWiper(nil).targets()
	output := preview(dirs, sqliteDB)

	if !strings.Contains(output, present) {
		t.Errorf("preview must list the dir that exists, got:\n%s", output)
	}
	if absent := filepath.Join(config.UserSpaceConfigDir(), "tmp"); strings.Contains(output, absent) {
		t.Errorf("preview must not promise %s, which does not exist:\n%s", absent, output)
	}
}

// TestWipePurgesStoreBeforeUnlink covers the SQLite handle problem: the rows go
// through the store's own API, not just the file.
func TestWipePurgesStoreBeforeUnlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	seedResetState(t)

	remaining := []string{"a", "b", "c"}
	store := &storagemocks.FakeConversationStorage{}
	store.ListConversationsStub = func(_ context.Context, _ string, limit, _ int) ([]convdomain.ConversationSummary, error) {
		out := make([]convdomain.ConversationSummary, 0, len(remaining))
		for _, id := range remaining {
			if len(out) == limit {
				break
			}
			out = append(out, convdomain.ConversationSummary{ID: id})
		}
		return out, nil
	}
	store.DeleteConversationStub = func(_ context.Context, id string) error {
		remaining = slices.DeleteFunc(remaining, func(existing string) bool { return existing == id })
		return nil
	}

	sqlite := &wiper{
		cfg:   &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypeSQLite}},
		store: store,
	}
	confirmWipe(t, sqlite)

	if len(remaining) != 0 {
		t.Errorf("store still holds %v after reset", remaining)
	}
	if store.DeleteConversationCallCount() != 3 {
		t.Errorf("expected 3 deletes through the store API, got %d", store.DeleteConversationCallCount())
	}
}

// TestWipeReportsEveryFailure checks a blocked target does not hide the others:
// the error names all of them.
func TestWipeReportsEveryFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	seedResetState(t)

	blocked := filepath.Join(config.UserSpaceConfigDir(), "plans")
	if err := os.Chmod(config.UserSpaceConfigDir(), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(config.UserSpaceConfigDir(), 0o755) })

	w := jsonlWiper(nil)
	dirs, sqliteDB, _ := w.targets()
	_, err := w.wipe(context.Background(), dirs, sqliteDB)
	if err == nil {
		t.Skip("filesystem allowed the removal; nothing to assert")
	}
	if !strings.Contains(err.Error(), blocked) {
		t.Errorf("error must name the blocked target %s, got:\n%v", blocked, err)
	}
}
