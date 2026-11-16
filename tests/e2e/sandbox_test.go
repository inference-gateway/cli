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

// TestAgentBashDeniedPathIsRefusedInAutoMode: the deny list must hold in auto
// mode, whose unrestricted allow-list skips the argv path check, so a denied
// file in the project is refused with the same exclusion the file tools give
// while a plain project file stays readable.
func TestAgentBashDeniedPathIsRefusedInAutoMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no bash tool on windows")
	}
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "auth.yaml"), []byte("mocked-credential"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(project, "allowed.txt"), []byte("visible"), 0o644))
	m := startMock(t)

	stdout, _, code := runCLI(t, m.URL, project, "", "headless", "--mode", "auto", "probe the deny list")
	require.Zero(t, code)

	toolResults := contentsByRole(jsonLines(t, stdout), "tool")
	require.Len(t, toolResults, 2)
	require.Contains(t, toolResults[0], "excluded for security", "a denied path must be refused even in auto mode")
	require.Contains(t, toolResults[1], "visible", "a project file stays readable in auto mode")
}
