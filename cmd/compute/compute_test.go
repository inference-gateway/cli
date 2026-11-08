package compute

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

// runCompute executes `infer compute` with args through a fresh tree and a
// freshly loaded config, as one invocation of the binary would, and returns
// what it printed.
func runCompute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	state := runtime.NewState()
	require.NoError(t, state.Initialize(&cobra.Command{}))
	command := NewCommand(state)
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs(args)
	err := command.ExecuteContext(t.Context())
	return out.String(), err
}

func TestComputeCommandsEditComputeYAML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(home)
	t.Setenv("WORKSTATION_TOKEN", "the-secret")

	_, err := runCompute(t, "clusters", "add", "k3d", "--namespace", "agents", "--context", "k3d-agents", "--gateway-env", "ANTHROPIC_API_KEY")
	require.NoError(t, err)
	_, err = runCompute(t, "machines", "add", "workstation", "--address", "workstation.lan:52789", "--token", "${WORKSTATION_TOKEN}")
	require.NoError(t, err)
	_, err = runCompute(t, "clusters", "add", "workstation", "--namespace", "agents")
	require.ErrorContains(t, err, `compute "workstation" is defined twice`)
	_, err = runCompute(t, "clusters", "add", "no-namespace")
	require.ErrorContains(t, err, "namespace", "the namespace flag is required")

	clusters, err := runCompute(t, "clusters", "list")
	require.NoError(t, err)
	require.Contains(t, clusters, "k3d-agents")
	require.Contains(t, clusters, "ANTHROPIC_API_KEY")
	machines, err := runCompute(t, "machines", "list")
	require.NoError(t, err)
	require.Contains(t, machines, "workstation.lan:52789")
	require.NotContains(t, machines, "the-secret", "a machine list never prints a token")

	written, err := os.ReadFile(filepath.Join(home, config.ConfigDirName, config.ComputeFileName))
	require.NoError(t, err)
	require.Contains(t, string(written), "${WORKSTATION_TOKEN}", "a reference is saved as written")
	require.NotContains(t, string(written), "the-secret")

	require.NoError(t, os.WriteFile(filepath.Join(home, config.ConfigDirName, config.DaemonFileName), []byte("workspaces:\n  - name: reviewer\n    compute: k3d\n    image: infer:1\n"), 0o644))
	_, err = runCompute(t, "clusters", "remove", "k3d")
	require.ErrorContains(t, err, "used by workspace reviewer")
	out, err := runCompute(t, "machines", "remove", "workstation")
	require.NoError(t, err)
	require.Contains(t, out, "Removed machine workstation")

	machines, err = runCompute(t, "machines", "list")
	require.NoError(t, err)
	require.True(t, strings.Contains(machines, "No machines"), "the removed machine is gone: %s", machines)
}
