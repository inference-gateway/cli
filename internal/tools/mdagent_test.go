package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

func writeAgentFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(dir+"/"+name, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func testKnownTools() map[string]bool {
	return map[string]bool{ToolRead: true, ToolGrep: true, ToolTree: true, ToolBash: true, ToolEdit: true}
}

func TestLoadMarkdownAgents(t *testing.T) {
	known := testKnownTools()
	proj := t.TempDir()
	user := t.TempDir()

	writeAgentFile(t, proj, "code-reviewer.md", `---
name: code-reviewer
description: Reviews a diff for correctness bugs.
model: deepseek/deepseek-v4-pro
tools: Read, Grep
color: blue
---

You are a senior reviewer.`)

	writeAgentFile(t, proj, "gemini-style.md", `---
name: gemini-style
description: A Gemini CLI style agent.
tools:
  - Read
  - Tree
temperature: 0.7
max_turns: 10
---

Body here.`)

	writeAgentFile(t, proj, "claude-style.md", `---
name: claude-style
description: A Claude Code style agent.
model: sonnet
tools: Glob, Grep, Read
disallowedTools: Bash, Glob
maxTurns: 5
permissionMode: default
---

Claude body.`)

	writeAgentFile(t, proj, "inherits.md", `---
name: inherits
description: Inherits model and tools.
---

Plain body.`)

	writeAgentFile(t, proj, "explicit-inherit.md", `---
name: explicit-inherit
description: Model inherit is explicit.
model: inherit
tools: [Read]
---

Body.`)

	writeAgentFile(t, proj, "renamed.md", `---
name: not-the-filename
description: Name need not match the file name.
tools: Read
---

Body.`)

	writeAgentFile(t, proj, "editor.md", `---
name: editor
description: Reads and edits files.
tools:
  - Read
  - Edit
---

Editor body.`)

	writeAgentFile(t, user, "user-agent.md", `---
name: user-agent
description: Lives in the user-global directory.
tools: [Grep]
---

User body.`)

	agents := loadMarkdownAgents([]string{proj, user}, known)
	byName := make(map[string]markdownAgent, len(agents))
	for _, a := range agents {
		byName[a.name] = a
	}

	a := byName["code-reviewer"]
	if a.description != "Reviews a diff for correctness bugs." || a.model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("code-reviewer not parsed: %+v", a)
	}
	if !slices.Equal(a.tools, []string{ToolGrep, ToolRead}) {
		t.Fatalf("comma tools = %v, want [Grep Read]", a.tools)
	}
	if a.systemPrompt != "You are a senior reviewer." {
		t.Fatalf("body = %q", a.systemPrompt)
	}

	g := byName["gemini-style"]
	if !slices.Equal(g.tools, []string{ToolRead, ToolTree}) {
		t.Fatalf("list tools = %v, want [Read Tree]", g.tools)
	}

	c := byName["claude-style"]
	if c.model != "" {
		t.Fatalf("claude alias model %q must fall back to inherit", c.model)
	}
	if !slices.Equal(c.tools, []string{ToolGrep, ToolRead}) {
		t.Fatalf("tools after dropping Glob = %v, want [Grep Read] (Bash was never requested)", c.tools)
	}
	if e := byName["editor"]; !slices.Equal(e.tools, []string{ToolEdit, ToolRead}) {
		t.Fatalf("editor tools = %v, want [Edit Read]", e.tools)
	}

	if i := byName["inherits"]; i.model != "" || i.tools != nil {
		t.Fatalf("inherits agent must have no model and no allowlist: %+v", i)
	}
	if e := byName["explicit-inherit"]; e.model != "" || !slices.Equal(e.tools, []string{ToolRead}) {
		t.Fatalf("explicit-inherit not parsed: %+v", e)
	}
	if r := byName["not-the-filename"]; r.name != "not-the-filename" {
		t.Fatalf("agent name need not match the file name, got %+v", r)
	}
	if u := byName["user-agent"]; u.tools == nil {
		t.Fatalf("user-global agent not loaded: %+v", u)
	}
}

func TestLoadMarkdownAgents_ProjectWinsOnCollision(t *testing.T) {
	proj := t.TempDir()
	user := t.TempDir()
	writeAgentFile(t, proj, "shared.md", "---\nname: shared\ndescription: project version\ntools: Read\n---\nproject body")
	writeAgentFile(t, user, "shared.md", "---\nname: shared\ndescription: user version\ntools: Grep\n---\nuser body")

	agents := loadMarkdownAgents([]string{proj, user}, testKnownTools())
	if len(agents) != 1 {
		t.Fatalf("agents = %d, want 1", len(agents))
	}
	if agents[0].description != "project version" || agents[0].systemPrompt != "project body" {
		t.Fatalf("project file must win on name collision: %+v", agents[0])
	}
}

func TestLoadMarkdownAgents_SkipsInvalidFiles(t *testing.T) {
	known := testKnownTools()
	dir := t.TempDir()

	writeAgentFile(t, dir, "no-frontmatter.md", "Just a body.")
	writeAgentFile(t, dir, "broken-yaml.md", "---\nname: [broken\n---\nbody")
	writeAgentFile(t, dir, "no-name.md", "---\ndescription: missing name\n---\nbody")
	writeAgentFile(t, dir, "bad-name.md", "---\nname: Bad Name\ndescription: invalid\n---\nbody")
	writeAgentFile(t, dir, "long-name.md", "---\nname: "+string(make([]byte, 65))+"\ndescription: too long\n---\nbody")
	writeAgentFile(t, dir, "no-description.md", "---\nname: no-description\n---\nbody")
	writeAgentFile(t, dir, "all-unknown-tools.md", "---\nname: all-unknown-tools\ndescription: d\ntools: Glob, WebGrep\n---\nbody")
	writeAgentFile(t, dir, "empty-list.md", "---\nname: empty-list\ndescription: d\ntools: []\n---\nbody")
	writeAgentFile(t, dir, "disallow-all.md", "---\nname: disallow-all\ndescription: d\ntools: Read, Grep\ndisallowedTools: [Read, Grep]\n---\nbody")
	writeAgentFile(t, dir, "ignored.txt", "---\nname: ignored\ndescription: not markdown\n---\nbody")

	agents := loadMarkdownAgents([]string{dir}, known)
	if len(agents) != 0 {
		t.Fatalf("invalid files must all be skipped, got %+v", agents)
	}
}

func TestLoadMarkdownAgents_MissingDirsAndSubdirsIgnored(t *testing.T) {
	if agents := loadMarkdownAgents([]string{"/nonexistent/infer-agents"}, testKnownTools()); len(agents) != 0 {
		t.Fatalf("missing directories must load zero agents, got %+v", agents)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/subdir", 0o755); err != nil {
		t.Fatal(err)
	}
	agents := loadMarkdownAgents([]string{dir}, testKnownTools())
	if len(agents) != 0 {
		t.Fatalf("subdirectories are not agent definitions, got %+v", agents)
	}
}

func TestResolveMarkdownAgentModel(t *testing.T) {
	cases := map[string]string{
		"":                          "",
		"inherit":                   "",
		"Inherit":                   "",
		"deepseek/deepseek-v4-pro":  "deepseek/deepseek-v4-pro",
		"anthropic/claude-sonnet-4": "anthropic/claude-sonnet-4",
	}
	for in, want := range cases {
		if got := resolveMarkdownAgentModel(in, "f.md"); got != want {
			t.Errorf("resolveMarkdownAgentModel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := resolveMarkdownAgentModel("sonnet", "f.md"); got != "" {
		t.Errorf("claude alias %q must warn and fall back to inherit, got %q", "sonnet", got)
	}
}

func TestDeriveSubagentMode(t *testing.T) {
	if got := deriveSubagentMode([]string{ToolRead, ToolGrep, ToolTree}, toolManifests); got != agentdomain.AgentModeReadOnly {
		t.Errorf("all read-only tools -> ReadOnly, got %v", got)
	}
	if got := deriveSubagentMode([]string{ToolRead, ToolBash}, toolManifests); got != agentdomain.AgentModeStandard {
		t.Errorf("any mutating tool -> ReadWrite, got %v", got)
	}
}

func TestResolveThreadAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := t.TempDir()
	projectAgents := filepath.Join(proj, config.ConfigDirName, agentsSubdir)
	userAgents := filepath.Join(home, config.ConfigDirName, agentsSubdir)

	writeAgentFile(t, projectAgents, "reviewer.md", `---
name: reviewer
description: Reviews a diff.
model: deepseek/deepseek-v4-pro
tools: Read, Grep, Bash, project_tool
disallowedTools: Bash
---

You review diffs.`)
	writeAgentFile(t, userAgents, "reviewer.md", `---
name: reviewer
description: The user's reviewer, shadowed by the project's.
---

User body.`)
	writeAgentFile(t, userAgents, "helper.md", `---
name: helper
description: Inherits the model and drops Bash.
model: inherit
disallowedTools: Bash
---

You help.`)
	writeAgentFile(t, userAgents, "aliased.md", `---
name: aliased
description: A Claude Code alias model.
model: sonnet
---

Aliased.`)
	writeAgentFile(t, projectAgents, "no-tools.md", `---
name: no-tools
description: An empty allowlist.
tools: []
---

Nothing.`)

	thread := sessionsdomain.ThreadOptions{Model: "provider/thread", CustomInstructions: "siblings"}
	tests := []struct {
		name    string
		agent   string
		want    sessionsdomain.ThreadOptions
		wantErr string
	}{
		{
			name:  "a project agent with a model pins it and keeps its tools for the worker",
			agent: "reviewer",
			want: sessionsdomain.ThreadOptions{
				Agent: "reviewer", Model: "deepseek/deepseek-v4-pro", PinModel: true,
				SystemPrompt: "You review diffs.", CustomInstructions: "siblings",
				Tools: []string{ToolGrep, ToolRead, "project_tool"}, DisallowedTools: []string{ToolBash},
			},
		},
		{
			name:  "an inherit agent keeps the thread's model and leaves disallowedTools to the worker",
			agent: "helper",
			want: sessionsdomain.ThreadOptions{
				Agent: "helper", Model: "provider/thread", SystemPrompt: "You help.", CustomInstructions: "siblings",
				DisallowedTools: []string{ToolBash},
			},
		},
		{
			name:  "a bare alias model inherits",
			agent: "aliased",
			want:  sessionsdomain.ThreadOptions{Agent: "aliased", Model: "provider/thread", SystemPrompt: "Aliased.", CustomInstructions: "siblings"},
		},
		{
			name:    "an unknown agent names the available ones",
			agent:   "ghost",
			wantErr: `unknown agent "ghost"; available agents: aliased, helper, reviewer`,
		},
		{
			name:    "an empty allowlist is invalid instead of granting every tool",
			agent:   "no-tools",
			wantErr: `unknown agent "no-tools"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := thread
			opts.Agent = tt.agent
			got, err := ResolveThreadAgent(proj, opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("resolved %+v, want %+v", got, tt.want)
			}
		})
	}
}
