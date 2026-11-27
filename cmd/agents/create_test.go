package agents

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// runCreate runs infer agents create with args through a fresh command tree and
// returns what it printed.
func runCreate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewCommand(runtime.NewState(), output.NewRenderer())
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"create"}, args...))
	err := root.ExecuteContext(t.Context())
	return out.String(), err
}

func TestCreateWritesAnAgentFromItsRolesTemplate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tests := []struct {
		name     string
		args     []string
		wantFile string
		wantBody string
		wantDesc string
	}{
		{"a role with a template", []string{"jeff", "--role", "tester", "--emoji", "🧪", "--workspace", "ubuntu-desktop", "--tools", "Read,Grep"}, "jeff.md", "You are jeff, the team's tester.", "Tests what it is given"},
		{"a two-word role takes its last word's template", []string{"noah", "--role", "code reviewer"}, "noah.md", "You are noah, the team's code reviewer.", "Reviews a change"},
		{"an unknown role takes the default", []string{"lara", "--role", "accountant", "--description", "Keeps the books."}, "lara.md", "You are lara, the team's accountant.", "Keeps the books."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runCreate(t, tt.args...)
			require.NoError(t, err)
			path := filepath.Join(home, ".infer", "agents", tt.wantFile)
			require.Contains(t, out, "Created "+path)
			require.Contains(t, out, "@"+strings.TrimSuffix(tt.wantFile, ".md")+" reaches it in a room")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Contains(t, string(data), tt.wantBody)
			require.Contains(t, string(data), "description: "+tt.wantDesc)
		})
	}

	written, err := os.ReadFile(filepath.Join(home, ".infer", "agents", "jeff.md"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(written), "---\nname: jeff\ndescription: Tests what"), "frontmatter starts with name then description:\n%s", written)
	require.Contains(t, string(written), "\nrole: tester\nemoji: 🧪\nworkspace: ubuntu-desktop\ntools:\n  - Read\n  - Grep\n---\n", "the emoji stays as typed and the tools are a block list")

	agents := tools.ProjectAgents(t.TempDir())
	var labels []string
	for _, agent := range agents {
		if !agent.BuiltIn {
			labels = append(labels, agent.Label())
		}
	}
	require.ElementsMatch(t, []string{"jeff (tester)", "noah (code reviewer)", "lara (accountant)"}, labels, "the loader reads the written files back with their roles")
}

func TestCreateRefusesABadNameAndAnExistingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := runCreate(t, "Jeff", "--role", "tester")
	require.ErrorContains(t, err, `agent name "Jeff"`)

	_, err = runCreate(t, "jeff")
	require.ErrorContains(t, err, `required flag(s) "role"`)

	_, err = runCreate(t, "jeff", "--role", "tester")
	require.NoError(t, err)
	_, err = runCreate(t, "jeff", "--role", "writer")
	require.ErrorContains(t, err, "exists - edit it, or pass --force")
	data, _ := os.ReadFile(filepath.Join(home, ".infer", "agents", "jeff.md"))
	require.Contains(t, string(data), "role: tester", "the file was kept")

	_, err = runCreate(t, "jeff", "--role", "writer", "--force")
	require.NoError(t, err)
	data, _ = os.ReadFile(filepath.Join(home, ".infer", "agents", "jeff.md"))
	require.Contains(t, string(data), "role: writer")
}

func TestCreateWithProjectWritesTheProjectAgentsDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	t.Chdir(project)

	_, err := runCreate(t, "anna", "--role", "researcher", "--project")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(project, ".infer", "agents", "anna.md"))
	require.NoError(t, err)
}
