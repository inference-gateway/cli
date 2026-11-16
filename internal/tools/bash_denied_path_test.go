package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// TestBashTool_DeniedPathIsRefusedInEveryMode guards the deny list against the
// auto-mode gap: the unrestricted allow-list skips the path check, so an
// approved cat of a denied file would otherwise read the credential it names.
func TestBashTool_DeniedPathIsRefusedInEveryMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	authPath := filepath.Join(home, ".infer", "auth.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(authPath), 0o755))
	require.NoError(t, os.WriteFile(authPath, []byte("mocked"), 0o600))

	tool := NewBashTool(config.DefaultConfig(), &schedmocks.FakeBackgroundShellService{})
	ctx := agentdomain.WithToolApproved(context.Background())
	ctx = agentdomain.WithAgentMode(ctx, agentdomain.AgentModeAutoAccept)
	ctx = agentdomain.WithBashOutputCallback(ctx, func(string) {})

	r, err := tool.Execute(ctx, map[string]any{"command": "cat ~/.infer/auth.yaml"})
	require.NoError(t, err)
	require.False(t, r.Success)
	require.Contains(t, r.Error, "excluded for security")
}
