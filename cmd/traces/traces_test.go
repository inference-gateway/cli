package traces

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	output "github.com/inference-gateway/cli/cmd/output"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// runTraces executes `infer traces` with args against an isolated HOME and an
// empty telemetry store, capturing output through SetOut - the seam everything
// the command prints must route through.
func runTraces(t *testing.T, args ...string) string {
	t.Helper()
	for _, env := range os.Environ() {
		key, _, ok := strings.Cut(env, "=")
		if ok && strings.HasPrefix(key, "INFER_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("NO_COLOR", "1")

	renderer := output.NewRenderer()
	cmd := NewCommand(renderer)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())
	return stdout.String()
}

func TestTracesEmptyCapturesThroughSetOut(t *testing.T) {
	out := runTraces(t)

	require.Contains(t, out, "No traces recorded yet.")
	require.NotContains(t, out, "Trace Sessions")
}

func TestTracesListCapturesThroughSetOut(t *testing.T) {
	out := runTraces(t, "--list")

	require.Equal(t, "No traces recorded yet.\n", out)
}

func TestTracesListJSONCapturesThroughSetOut(t *testing.T) {
	var sessions []telemetry.TraceSession
	require.NoError(t, json.Unmarshal([]byte(runTraces(t, "--list", "--format", "json")), &sessions))
	require.Empty(t, sessions)
}
