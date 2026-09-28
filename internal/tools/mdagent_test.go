package tools

import (
	"os"
	"slices"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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
