package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

func TestIsBashCommandAllowed_SandboxPaths(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	t.Chdir(project)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OUTSIDE", outside)
	t.Setenv("INSIDE", filepath.Join(project, "src"))
	t.Setenv("REF", "")

	for _, dir := range []string{"src", "links"} {
		if err := os.Mkdir(filepath.Join(project, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"main.go", ".env", "src/a.go", "src/b.go"} {
		mustWrite(t, filepath.Join(project, file))
	}
	mustWrite(t, filepath.Join(outside, "secret.txt"))
	mustSymlink(t, outside, filepath.Join(project, "links", "out-dir"))
	mustSymlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(project, "links", "out-file"))

	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Allowed = sandboxdomain.Allow(project)

	tests := []struct {
		command string
		allowed bool
	}{
		{command: "ls -la", allowed: true},
		{command: "head -n 20 main.go", allowed: true},
		{command: "ls *", allowed: true},
		{command: "wc -l src/*.go", allowed: true},
		{command: "tail $INSIDE/a.go", allowed: true},
		{command: "git log $REF", allowed: true},
		{command: "git log --oneline -5", allowed: true},
		{command: "git log --output-indicator-new=+", allowed: true},
		{command: "find . -name '*.go'", allowed: true},
		{command: "mkdir -p build/out", allowed: true},
		{command: "ln -s ../.agents/skills .claude/skills", allowed: true},
		{command: "echo /etc/hosts", allowed: true},
		{command: "gh api repos/{owner}/{repo}/contents/README.md", allowed: true},
		{command: "uniq -c main.go", allowed: true},
		{command: "sort -n main.go", allowed: true},
		{command: "tree -L 2", allowed: true},

		{command: "head ~/.aws/credentials"},
		{command: "tail /etc/hosts"},
		{command: "wc -c < /etc/hosts"},
		{command: "wc -c </etc/hosts"},
		{command: "ls $OUTSIDE"},
		{command: "ls ${OUTSIDE}"},
		{command: `ls "$OUTSIDE"`},
		{command: "head links/out-file"},
		{command: "ls links/out-dir/"},
		{command: "ls links/*"},
		{command: "head .env"},
		{command: "ls .*"},
		{command: "ls ../*"},
		{command: "head {main.go,/etc/hosts}"},
		{command: "ls ~root"},
		{command: "ls $1"},
		{command: "ls ${OUTSIDE:-x}"},
		{command: "sort --files0-from=/etc/hosts"},
		{command: "sort -T/etc main.go"},
		{command: "mkdir ../escape"},
		{command: "ln -s main.go ../escape"},
		{command: "find / -name id_rsa"},

		{command: "sort -o main.go main.go"},
		{command: "sort -uo main.go main.go"},
		{command: "sort --output=main.go main.go"},
		{command: "sort --out=main.go main.go"},
		{command: "tree -o tree.txt"},
		{command: "git diff --output=patch.diff"},
		{command: "git log --output patch.txt"},
		{command: "uniq main.go out.txt"},

		{command: "task test"},
		{command: "make build"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := IsBashCommandAllowed(cfg, tt.command, agentdomain.AgentModeStandard); got != tt.allowed {
				t.Errorf("IsBashCommandAllowed(%q) = %v, want %v (hint: %q)", tt.command, got, tt.allowed, BashCommandRejectionHint(cfg, tt.command))
			}
		})
	}
}
