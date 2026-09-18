package reset

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
)

// newTestState isolates HOME and the working directory, then loads config the
// way root's PersistentPreRunE does.
func newTestState(t *testing.T) *runtime.State {
	t.Helper()
	for _, env := range os.Environ() {
		if key, _, ok := strings.Cut(env, "="); ok && strings.HasPrefix(key, "INFER_") {
			t.Setenv(key, "")
			require.NoError(t, os.Unsetenv(key))
		}
	}
	t.Setenv("HOME", t.TempDir())
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	root := &cobra.Command{Use: "infer"}
	root.PersistentFlags().BoolP("verbose", "v", false, "")
	root.PersistentFlags().String("tools-bash-allow-append", "", "")
	root.PersistentFlags().String("reminders-file", "", "")

	state := runtime.NewState()
	require.NoError(t, state.Initialize(root))
	return state
}

// seed plants one stale file in a runtime dir and one config file that must survive.
func seed(t *testing.T) (stale, preserved string) {
	t.Helper()
	stale = filepath.Join(config.ProjectRuntimeDir(), "conversations", "session.jsonl")
	preserved = filepath.Join(config.UserSpaceConfigDir(), "config.yaml")
	for _, path := range []string{stale, preserved} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("keep-me"), 0o644))
	}
	return stale, preserved
}

func runReset(t *testing.T, state *runtime.State, sub string) string {
	t.Helper()
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	cmd.SetContext(t.Context())
	require.NoError(t, run(cmd, state, sub))
	return out.String()
}

func TestResetPreviewDeletesNothing(t *testing.T) {
	state := newTestState(t)
	stale, preserved := seed(t)

	output := runReset(t, state, "")

	require.Contains(t, output, filepath.Dir(stale))
	require.Contains(t, output, "infer reset confirm")
	require.FileExists(t, stale)
	require.FileExists(t, preserved)
}

func TestResetConfirmWipesStateAndKeepsConfig(t *testing.T) {
	state := newTestState(t)
	stale, preserved := seed(t)

	output := runReset(t, state, "confirm")

	require.Contains(t, output, "wiped")
	require.NoFileExists(t, stale)
	require.DirExists(t, filepath.Dir(stale))
	require.FileExists(t, preserved)
}
