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
func seedResetState(t *testing.T) (projectDirs, userDirs, configFiles []string) {
	t.Helper()

	userSpace := config.UserSpaceConfigDir()
	runtimeRoot := config.ProjectRuntimeDir()

	projectDirs = []string{
		filepath.Join(runtimeRoot, "conversations"),
		filepath.Join(runtimeRoot, "history"),
		filepath.Join(runtimeRoot, "backups"),
		filepath.Join(runtimeRoot, "tmp"),
		filepath.Join(runtimeRoot, "artifacts"),
		filepath.Join(runtimeRoot, "exports"),
	}
	userDirs = []string{
		filepath.Join(userSpace, "artifacts"),
		filepath.Join(userSpace, "plans"),
		filepath.Join(userSpace, "tmp"),
		filepath.Join(userSpace, "logs"),
		filepath.Join(userSpace, "telemetry"),
		filepath.Join(userSpace, "schedules"),
		filepath.Join(userSpace, "run"),
	}
	for _, dir := range append(append([]string{}, projectDirs...), userDirs...) {
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
	return projectDirs, userDirs, configFiles
}

// confirmWipe resolves the targets and performs the wipe, as `infer reset
// confirm` does.
func confirmWipe(t *testing.T, w *wiper) string {
	t.Helper()
	output, err := w.wipe(context.Background(), w.resolve())
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

	projectDirs, userDirs, configFiles := seedResetState(t)

	confirmWipe(t, jsonlWiper(nil))

	for _, dir := range projectDirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("per-project runtime dir %s must be deleted, not recreated empty", dir)
		}
	}
	if _, err := os.Stat(config.ProjectRuntimeDir()); err != nil {
		t.Errorf("the project dir itself must survive, projects.yaml references it: %v", err)
	}
	for _, dir := range userDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("userspace dir %s was removed instead of recreated: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Errorf("userspace dir %s still holds %d entries after reset", dir, len(entries))
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
	_, _, _ = seedResetState(t)

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

	projectDirs, _, _ := seedResetState(t)
	remoteWiper := &wiper{cfg: &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypePostgres}}}
	found := remoteWiper.resolve()
	if found.remote != "postgres" {
		t.Errorf("expected a postgres skip notice, got %q", found.remote)
	}
	output, err := remoteWiper.wipe(context.Background(), found)
	if err != nil {
		t.Fatalf("postgres reset failed: %v", err)
	}
	if !strings.Contains(output, "postgres") {
		t.Errorf("expected a postgres notice in the output, got:\n%s", output)
	}
	for _, dir := range projectDirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("remote config must not skip local dirs: %s survived", dir)
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

	output := preview(jsonlWiper(nil).resolve())

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
	_, _, _ = seedResetState(t)

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
	_, _, _ = seedResetState(t)

	blocked := filepath.Join(config.UserSpaceConfigDir(), "plans")
	if err := os.Chmod(config.UserSpaceConfigDir(), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(config.UserSpaceConfigDir(), 0o755) })

	w := jsonlWiper(nil)
	_, err := w.wipe(context.Background(), w.resolve())
	if err == nil {
		t.Skip("filesystem allowed the removal; nothing to assert")
	}
	if !strings.Contains(err.Error(), blocked) {
		t.Errorf("error must name the blocked target %s, got:\n%v", blocked, err)
	}
}

// TestStaleOnlyWhenNoReadingExists guards the ambiguity in the slug encoding:
// a live project whose path contains a dash must never look stale, because a
// false positive deletes its whole runtime directory.
func TestStaleOnlyWhenNoReadingExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dashed := filepath.Join(home, "repos", "my-project")
	if err := os.MkdirAll(dashed, 0o755); err != nil {
		t.Fatal(err)
	}

	slug := func(path string) string { return strings.ReplaceAll(path, string(filepath.Separator), "-") }

	for _, tc := range []struct {
		name string
		slug string
		want bool
	}{
		{"path with a dash resolves", slug(dashed), false},
		{"plain path resolves", slug(filepath.Join(home, "repos")), false},
		{"missing path is stale", slug(filepath.Join(home, "repos", "gone")), true},
		{"missing dashed path is stale", slug(filepath.Join(home, "repos", "my-other")), true},
		{"non-path slug is never stale", "workspace", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stale(tc.slug); got != tc.want {
				t.Errorf("stale(%q) = %v, want %v", tc.slug, got, tc.want)
			}
		})
	}
}

// TestStaleProjectsPrunedWholeOthersKeepTheirDir pins the split the user asked
// for: a live project keeps its directory and loses only the runtime subdirs,
// while a project whose source is gone is removed entirely.
func TestStaleProjectsPrunedWholeOthersKeepTheirDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	live := t.TempDir()

	liveSlug := strings.ReplaceAll(live, string(filepath.Separator), "-")
	staleSlug := strings.ReplaceAll(filepath.Join(home, "deleted-checkout"), string(filepath.Separator), "-")

	projects := filepath.Join(config.UserSpaceConfigDir(), config.ProjectsDirName)
	for _, slug := range []string{liveSlug, staleSlug} {
		if err := os.MkdirAll(filepath.Join(projects, slug, "conversations"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	w := jsonlWiper(nil)
	if _, err := w.wipe(context.Background(), w.resolve()); err != nil {
		t.Fatalf("wipe failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projects, liveSlug)); err != nil {
		t.Errorf("live project dir must survive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projects, liveSlug, "conversations")); !os.IsNotExist(err) {
		t.Errorf("live project runtime subdir must be pruned, not recreated")
	}
	if _, err := os.Stat(filepath.Join(projects, staleSlug)); !os.IsNotExist(err) {
		t.Errorf("stale project dir must be removed entirely")
	}
}
