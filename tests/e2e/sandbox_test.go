//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	require "github.com/stretchr/testify/require"
)

// TestAgentBashWritesAreConfinedToTheSandbox: in auto mode the allow-list runs
// any command and skips the path check, so the kernel is what keeps a write
// inside the policy. The probe dir sits in the real home, outside every
// writable path.
func TestAgentBashWritesAreConfinedToTheSandbox(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no kernel sandbox on windows")
	}
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	probe, err := os.MkdirTemp(home, "infer-e2e-sandbox-probe-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(probe) })
	project := t.TempDir()
	m := startMock(t)

	stdout, _, code := runCLIWithEnv(t, m.URL, project, "", map[string]string{"E2E_SANDBOX_PROBE_DIR": probe}, "headless", "--mode", "auto", "probe the sandbox")
	require.Zero(t, code)

	toolResults := contentsByRole(jsonLines(t, stdout), "tool")
	require.Len(t, toolResults, 2)
	require.Contains(t, toolResults[0], "sandbox confines writes", "a write outside the policy must fail with the hint")
	require.NoDirExists(t, filepath.Join(probe, "outside"))
	require.DirExists(t, filepath.Join(project, "inside-probe"), "a write inside the project must still land")
}
