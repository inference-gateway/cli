package sandboxinfra

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

func TestPersistGrant(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	for _, path := range []string{
		filepath.Join(home, config.ConfigDirName, "config.yaml"),
		filepath.Join(config.ConfigDirName, "mcp.yaml"),
	} {
		if err := PersistGrant(sandboxdomain.Allowed{Path: path, Access: sandboxdomain.AccessWrite}); err == nil {
			t.Fatalf("a grant inside the config dir must not persist, %s slid through", path)
		}
	}
	policyFile, err := config.UserSandboxPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(policyFile); !os.IsNotExist(statErr) {
		t.Fatal("a refused grant must not write sandbox.yaml")
	}

	grant := sandboxdomain.Allowed{Path: filepath.Join(t.TempDir(), "tools"), Access: sandboxdomain.AccessWrite}
	if err := PersistGrant(grant); err != nil {
		t.Fatalf("expected %s to persist, got %v", grant.Path, err)
	}
	policy, err := config.LoadSandbox(policyFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Filesystem.Allowed) == 0 || policy.Filesystem.Allowed[0] != grant {
		t.Fatalf("expected %s in front of the allowed entries, got %v", grant.Path, policy.Filesystem.Allowed)
	}
}
