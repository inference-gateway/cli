package tools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
	skills "github.com/inference-gateway/cli/internal/skills"
)

var builtinAgentNames = []string{"explore", "general-purpose", "main", "plan"}

func isolateAgentDirs(t *testing.T) (home, project string) {
	t.Helper()
	home, project = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(project)
	return home, project
}

func embeddedMainBody(t *testing.T) string {
	t.Helper()
	data, err := builtinAgents.ReadFile(builtinAgentsRoot + "/main.md")
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := skills.SplitFrontmatter(data)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(body)
}

func TestBuiltinAgentsLoadWithoutAnyAgentFile(t *testing.T) {
	isolateAgentDirs(t)

	var names []string
	for _, agent := range loadMarkdownAgentsWithBuiltins(nil, testKnownTools()) {
		if agent.source != builtinSource {
			t.Errorf("agent %q source = %q, want %q", agent.name, agent.source, builtinSource)
		}
		names = append(names, agent.name)
	}
	slices.Sort(names)
	if !slices.Equal(names, builtinAgentNames) {
		t.Fatalf("built-in agents = %v, want %v", names, builtinAgentNames)
	}
}

func TestAnAgentFileShadowsTheBuiltinOfItsName(t *testing.T) {
	home, _ := isolateAgentDirs(t)
	writeAgentFile(t, filepath.Join(home, config.ConfigDirName, config.MarkdownAgentsDirName), "main.md",
		"---\nname: main\ndescription: My own main agent.\n---\n\nMy own prompt.")

	var main []markdownAgent
	for _, agent := range loadMarkdownAgentsWithBuiltins(nil, nil) {
		if agent.name == agentdomain.DefaultMainAgentName {
			main = append(main, agent)
		}
	}
	if len(main) != 1 || main[0].systemPrompt != "My own prompt." || main[0].source != "user" {
		t.Fatalf("main agents = %+v, want only the user's", main)
	}
}

func TestMainAgentPrompt(t *testing.T) {
	t.Run("the built-in main body without any file", func(t *testing.T) {
		isolateAgentDirs(t)
		got := MainAgentPrompt("")
		if got != embeddedMainBody(t) || !strings.HasPrefix(got, "Autonomous software engineering agent.") {
			t.Fatalf("MainAgentPrompt = %q, want the embedded main.md body", got)
		}
		if again := MainAgentPrompt(""); again != got {
			t.Fatal("the main prompt must be byte-identical on every resolve, or the prompt cache misses")
		}
	})

	t.Run("a project main.md wins", func(t *testing.T) {
		_, project := isolateAgentDirs(t)
		writeAgentFile(t, filepath.Join(project, config.ConfigDirName, config.MarkdownAgentsDirName), "main.md",
			"---\nname: main\ndescription: The project's main agent.\n---\n\nProject prompt.\n")
		if got := MainAgentPrompt(project); got != "Project prompt." {
			t.Fatalf("MainAgentPrompt = %q, want the project's body", got)
		}
	})
}

func TestResolveThreadAgentResolvesABuiltin(t *testing.T) {
	_, project := isolateAgentDirs(t)

	opts, err := ResolveThreadAgent(project, sessionsdomain.ThreadOptions{Agent: "explore"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(opts.Tools, ToolRead) || slices.Contains(opts.Tools, ToolWrite) {
		t.Fatalf("explore tools = %v, want a read-only allowlist", opts.Tools)
	}
}

func TestSeedBuiltinAgents(t *testing.T) {
	dest := t.TempDir()
	if err := SeedBuiltinAgents(dest, false); err != nil {
		t.Fatal(err)
	}
	seeded := loadMarkdownAgents([]string{dest}, nil)
	if len(seeded) != len(builtinAgentNames) {
		t.Fatalf("seeded %d valid agents, want %d", len(seeded), len(builtinAgentNames))
	}

	mainPath := filepath.Join(dest, "main.md")
	const edit = "---\nname: main\ndescription: Edited.\n---\n\nEdited prompt."
	if err := os.WriteFile(mainPath, []byte(edit), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedBuiltinAgents(dest, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(mainPath); string(got) != edit {
		t.Fatal("a re-seed must keep the user's edit")
	}
	if err := SeedBuiltinAgents(dest, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(mainPath); string(got) == edit {
		t.Fatal("an overwrite must reset the edit to the shipped agent")
	}
}

func TestProjectAgentsListsTheProjectsAgentsAndTheBuiltins(t *testing.T) {
	_, project := isolateAgentDirs(t)
	writeAgentFile(t, filepath.Join(project, config.ConfigDirName, config.MarkdownAgentsDirName), "tester.md", "---\nname: tester\ndescription: Runs the suite.\n---\n\nRun the tests.")

	descriptions := map[string]string{}
	for _, agent := range ProjectAgents(project) {
		descriptions[agent.Name] = agent.Description
	}
	if descriptions["tester"] != "Runs the suite." {
		t.Errorf("tester description = %q, want the project agent's", descriptions["tester"])
	}
	for _, builtin := range []string{"main", "explore"} {
		if _, ok := descriptions[builtin]; !ok {
			t.Errorf("ProjectAgents lacks the built-in %q", builtin)
		}
	}
	for _, agent := range ProjectAgents(t.TempDir()) {
		if agent.Name == "tester" {
			t.Error("another project lists this project's agent")
		}
	}
}
