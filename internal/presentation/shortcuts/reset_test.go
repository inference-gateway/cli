package shortcuts

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

// confirmReset runs the real two-step flow: preview, then confirm. /reset confirm
// on its own only previews, by design.
func confirmReset(t *testing.T, r *ResetShortcut) ShortcutResult {
	t.Helper()
	if _, err := r.Execute(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	res, err := r.Execute(context.Background(), []string{"confirm"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func jsonlConfig() *config.Config {
	return &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypeJsonl}}
}

// TestResetShortcut_WipesStateKeepsConfig covers the core contract: confirming
// empties every runtime directory while leaving configuration untouched.
func TestResetShortcut_WipesStateKeepsConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	stateDirs, configFiles := seedResetState(t)

	res := confirmReset(t, NewResetShortcut(jsonlConfig(), nil, nil, nil))
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

	res, err := NewResetShortcut(jsonlConfig(), nil, nil, nil).Execute(context.Background(), nil)
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
		if _, err := os.Stat(filepath.Join(dir, "state.txt")); err != nil {
			t.Errorf("preview deleted %s without confirmation", dir)
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
	res := confirmReset(t, NewResetShortcut(cfg, nil, nil, nil))
	if !res.Success {
		t.Fatalf("sqlite reset failed: %s", res.Output)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(db + suffix); !os.IsNotExist(err) {
			t.Errorf("sqlite database %s still exists after reset", db+suffix)
		}
	}

	stateDirs, _ := seedResetState(t)
	cfg = &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypePostgres}}
	res = confirmReset(t, NewResetShortcut(cfg, nil, nil, nil))
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

// TestResetShortcut_InsightsPreviewsWithoutDeleting verifies /reset insights
// reports and previews but deletes nothing, even when insights are unavailable.
func TestResetShortcut_InsightsPreviewsWithoutDeleting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	stateDirs, configFiles := seedResetState(t)

	res, err := NewResetShortcut(jsonlConfig(), nil, nil, nil).Execute(context.Background(), []string{"insights"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("/reset insights failed: %s", res.Output)
	}
	if !strings.Contains(res.Output, "Insights unavailable") {
		t.Errorf("a nil generator must explain itself, got:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "confirm") || !strings.Contains(res.Output, stateDirs[0]) {
		t.Errorf("/reset insights must still show the preview, got:\n%s", res.Output)
	}

	for _, dir := range stateDirs {
		if _, err := os.Stat(filepath.Join(dir, "state.txt")); err != nil {
			t.Fatalf("/reset insights deleted %s", dir)
		}
	}
	for _, file := range configFiles {
		if _, err := os.ReadFile(file); err != nil {
			t.Errorf("/reset insights removed config file %s", file)
		}
	}
}

// TestResetShortcut_Subcommands checks both arguments reach autocomplete.
func TestResetShortcut_Subcommands(t *testing.T) {
	subs := NewResetShortcut(jsonlConfig(), nil, nil, nil).GetSubcommands()
	want := map[string]bool{"insights": false, "confirm": false}
	for _, s := range subs {
		want[s.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("subcommand %q missing from autocomplete", name)
		}
	}
}

// TestResetShortcut_UnknownArg keeps the error message in step with the usage.
func TestResetShortcut_UnknownArg(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	res, err := NewResetShortcut(jsonlConfig(), nil, nil, nil).Execute(context.Background(), []string{"--insights"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || !strings.Contains(res.Output, "insights") {
		t.Errorf("expected an unknown-argument error naming the valid args, got:\n%s", res.Output)
	}
}

// TestResetShortcut_ConfirmNeedsPreview covers the guard on the destructive path:
// a cold /reset confirm previews instead of wiping, so a tab-completed confirm
// cannot take out every project on the machine.
func TestResetShortcut_ConfirmNeedsPreview(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	stateDirs, _ := seedResetState(t)
	reset := NewResetShortcut(jsonlConfig(), nil, nil, nil)

	res, err := reset.Execute(context.Background(), []string{"confirm"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "Nothing has been deleted") {
		t.Errorf("a cold confirm must preview instead of wiping, got:\n%s", res.Output)
	}
	for _, dir := range stateDirs {
		if _, err := os.Stat(filepath.Join(dir, "state.txt")); err != nil {
			t.Fatalf("cold confirm wiped %s", dir)
		}
	}

	if res, err = reset.Execute(context.Background(), []string{"confirm"}); err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("armed confirm failed: %s", res.Output)
	}
	for _, dir := range stateDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("state dir %s removed instead of recreated: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Errorf("armed confirm did not empty %s", dir)
		}
	}
}

// TestResetShortcut_PreviewSkipsMissingTargets keeps the preview honest: it must
// promise only what is actually on disk.
func TestResetShortcut_PreviewSkipsMissingTargets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	present := filepath.Join(config.UserSpaceConfigDir(), "plans")
	if err := os.MkdirAll(present, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := NewResetShortcut(jsonlConfig(), nil, nil, nil).Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, present) {
		t.Errorf("preview must list the dir that exists, got:\n%s", res.Output)
	}
	if absent := filepath.Join(config.UserSpaceConfigDir(), "tmp"); strings.Contains(res.Output, absent) {
		t.Errorf("preview must not promise %s, which does not exist:\n%s", absent, res.Output)
	}
}

// TestResetShortcut_PurgesStoreBeforeUnlink covers the SQLite handle problem: the
// rows go through the store's own API, not just the file.
func TestResetShortcut_PurgesStoreBeforeUnlink(t *testing.T) {
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

	cfg := &config.Config{Storage: config.StorageConfig{Enabled: true, Type: config.StorageTypeSQLite}}
	if res := confirmReset(t, NewResetShortcut(cfg, nil, nil, store)); !res.Success {
		t.Fatalf("reset failed: %s", res.Output)
	}
	if len(remaining) != 0 {
		t.Errorf("store still holds %v after reset", remaining)
	}
	if store.DeleteConversationCallCount() != 3 {
		t.Errorf("expected 3 deletes through the store API, got %d", store.DeleteConversationCallCount())
	}
}

// TestResetShortcut_ReportsEveryFailure checks a blocked target does not hide the
// others: the error names all of them.
func TestResetShortcut_ReportsEveryFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	seedResetState(t)

	blocked := filepath.Join(config.UserSpaceConfigDir(), "plans")
	if err := os.Chmod(config.UserSpaceConfigDir(), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(config.UserSpaceConfigDir(), 0o755) })

	reset := NewResetShortcut(jsonlConfig(), nil, nil, nil)
	if _, err := reset.Execute(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	res, err := reset.Execute(context.Background(), []string{"confirm"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Skip("filesystem allowed the removal; nothing to assert")
	}
	if !strings.Contains(res.Output, blocked) {
		t.Errorf("the failure report must name the blocked target %s, got:\n%s", blocked, res.Output)
	}
}
