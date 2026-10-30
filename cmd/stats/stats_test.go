package stats

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// runStats executes `infer stats` with args against an isolated HOME and an
// empty telemetry store, capturing output through SetOut - the seam everything
// the command prints must route through.
func runStats(t *testing.T, args ...string) string {
	t.Helper()
	splitHomeProjectEnv(t)

	state := runtime.NewState()
	require.NoError(t, state.Initialize(&cobra.Command{}))

	cmd := NewCommand(state, output.NewRenderer())
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())
	return stdout.String()
}

func splitHomeProjectEnv(t *testing.T) {
	t.Helper()
	for _, env := range os.Environ() {
		key, _, ok := strings.Cut(env, "=")
		if ok && strings.HasPrefix(key, "INFER_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("NO_COLOR", "1")
}

func TestStatsEmptyTelemetryCapturesThroughSetOut(t *testing.T) {
	out := runStats(t)

	require.Contains(t, out, "No telemetry recorded yet.")
	require.Contains(t, out, "Telemetry accumulates as you use `infer chat` / `infer headless`")
	require.NotContains(t, out, "Tool Calls")
}

func TestStatsJSONCapturesThroughSetOut(t *testing.T) {
	var stats telemetry.Stats
	require.NoError(t, json.Unmarshal([]byte(runStats(t, "--format", "json")), &stats))
	require.Nil(t, stats.Tools, "an empty telemetry store must aggregate to no tools")
	require.Nil(t, stats.Models)
	require.Nil(t, stats.Sessions)
}
