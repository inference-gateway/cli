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
	home := t.TempDir()
	t.Chdir(project)
	t.Setenv("HOME", home)
	t.Setenv("OUTSIDE", outside)
	t.Setenv("INSIDE", filepath.Join(project, "src"))
	t.Setenv("REF", "")

	for _, dir := range []string{filepath.Join(project, "src"), filepath.Join(project, "links"), filepath.Join(project, "nested"), filepath.Join(home, ".aws")} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"main.go", ".env", ".env.local", "nested/.env.production", "src/a.go", "src/b.go"} {
		mustWrite(t, filepath.Join(project, file))
	}
	mustWrite(t, filepath.Join(outside, "secret.txt"))
	mustWrite(t, filepath.Join(home, ".aws", "credentials"))
	mustSymlink(t, outside, filepath.Join(project, "links", "out-dir"))
	mustSymlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(project, "links", "out-file"))

	cfg := config.DefaultConfig()
	cfg.Tools.Bash.Mode.All.Allow = append(cfg.Tools.Bash.Mode.All.Allow, `git commit( .*)?`, `gh pr create( .*)?`)
	cfg.Tools.Sandbox.Filesystem.Allowed = sandboxdomain.Allow(project)

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
		{command: "gh pr view 1 --json files -q .files[].path", allowed: true},
		{command: "gh pr view 1 --json files -q .[0].path", allowed: true},
		{command: "gh api user/repos --jq .[].full_name", allowed: true},
		{command: "git commit -m '/fix: typo' main.go", allowed: true},
		{command: "gh pr create --title 'Fix [a] bug' --body 'see /docs/b/c'", allowed: true},
		{command: "ls /no/such/dir", allowed: true},
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
		{command: "head .env.local"},
		{command: "cat nested/.env.production"},
		{command: "cat < .env.local"},
		{command: "ls .*"},
		{command: "ls .?"},
		{command: "ls .??*"},
		{command: "cat .e*"},
		{command: "ls .[!e]*"},
		{command: "ls .[[:alpha:]]*"},
		{command: "git commit -m msg .env"},
		{command: "gh pr create --body-file /etc/hosts"},
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

func TestIsBashCommandAllowed_DefaultPolicyReadsNullDevice(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()

	tests := []struct {
		command string
		allowed bool
	}{
		{command: "wc -c main.go /dev/null", allowed: true},
		{command: "wc -c /dev/null /etc/hosts"},
		{command: "mkdir /dev/null"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := IsBashCommandAllowed(cfg, tt.command, agentdomain.AgentModeStandard); got != tt.allowed {
				t.Errorf("IsBashCommandAllowed(%q) = %v, want %v", tt.command, got, tt.allowed)
			}
		})
	}
}

// TestIsBashCommandAllowed_PlanModeCreatesOnlyInScratchpad: allow-listed
// creating commands follow the plan-mode scratchpad gate, with relative paths
// resolving against the project working directory like in every other mode.
func TestIsBashCommandAllowed_PlanModeCreatesOnlyInScratchpad(t *testing.T) {
	project := t.TempDir()
	t.Chdir(project)
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Filesystem.Allowed = sandboxdomain.Allow(project)
	scratchpad := config.ProjectScratchpadDir()

	tests := []struct {
		command string
		allowed bool
	}{
		{command: "mkdir -p build/out"},
		{command: "mkdir " + filepath.Join(scratchpad, "out"), allowed: true},
		{command: "ls " + filepath.Join(project, "src"), allowed: true},
		{command: "mkdir " + filepath.Join(project, "out")},
		{command: "ln -s main.go " + filepath.Join(project, "link")},
		{command: "mkdir ../escape"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := IsBashCommandAllowed(cfg, tt.command, agentdomain.AgentModePlan); got != tt.allowed {
				t.Errorf("IsBashCommandAllowed(%q, plan) = %v, want %v", tt.command, got, tt.allowed)
			}
		})
	}
}
