package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

func TestIsCompleteJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid simple object",
			input:    `{"key": "value"}`,
			expected: true,
		},
		{
			name:     "valid object with nested content",
			input:    `{"file_path": "/test/file.txt", "content": "hello world"}`,
			expected: true,
		},
		{
			name:     "valid array",
			input:    `["item1", "item2"]`,
			expected: true,
		},
		{
			name:     "valid object with whitespace",
			input:    `  {"key": "value"}  `,
			expected: true,
		},
		{
			name:     "valid number",
			input:    `42`,
			expected: true,
		},
		{
			name:     "valid string",
			input:    `"hello"`,
			expected: true,
		},
		{
			name:     "valid boolean",
			input:    `true`,
			expected: true,
		},
		{
			name:     "valid null",
			input:    `null`,
			expected: true,
		},
		{
			name:     "incomplete object - missing closing brace",
			input:    `{"key": "value"`,
			expected: false,
		},
		{
			name:     "incomplete object - truncated string",
			input:    `{"file_path": "/test/file.txt", "content": "hello wo`,
			expected: false,
		},
		{
			name:     "incomplete array - missing closing bracket",
			input:    `["item1", "item2"`,
			expected: false,
		},
		{
			name:     "incomplete nested object",
			input:    `{"outer": {"inner": "value"`,
			expected: false,
		},
		{
			name:     "empty string",
			input:    ``,
			expected: false,
		},
		{
			name:     "whitespace only",
			input:    `   `,
			expected: false,
		},
		{
			name:     "incomplete - just opening brace",
			input:    `{`,
			expected: false,
		},
		{
			name:     "incomplete - truncated key",
			input:    `{"file_pa`,
			expected: false,
		},
		{
			name:     "incomplete - missing value",
			input:    `{"key":`,
			expected: false,
		},
		{
			name:     "malformed JSON",
			input:    `{key: "value"}`,
			expected: false,
		},
		{
			name:     "valid complex object with multiline content",
			input:    `{"file_path": "/test.txt", "content": "line1\nline2\nline3"}`,
			expected: true,
		},
		{
			name:     "incomplete with escaped quotes",
			input:    `{"content": "hello \"world`,
			expected: false,
		},
		{
			name:     "valid with escaped quotes",
			input:    `{"content": "hello \"world\""}`,
			expected: true,
		},
		{
			name:     "truncated Write tool call at output token limit",
			input:    `{"file_path": "/home/user/project/src/components/MyComponent.tsx", "content": "import React from 'react';\n\nexport const MyComponent: React.FC = () => {\n  const [state, setState] = React.useState<string>('');\n\n  return (\n    <div className=\"container\">\n      <h1>My Component</h1>\n      <p>This is a test component that demonstrates`,
			expected: false,
		},
		{
			name:     "complete Write tool call for large file",
			input:    `{"file_path": "/home/user/project/src/components/MyComponent.tsx", "content": "import React from 'react';\n\nexport const MyComponent: React.FC = () => {\n  return <div>Hello</div>;\n};\n"}`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCompleteJSON(tt.input)
			if result != tt.expected {
				t.Errorf("isCompleteJSON(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsGitRepository(t *testing.T) {
	result := isGitRepository()
	t.Logf("isGitRepository() = %v", result)
}

func TestGetGitRepositoryName(t *testing.T) {
	tests := []struct {
		name        string
		remoteURL   string
		expected    string
		description string
	}{
		{
			name:        "HTTPS GitHub URL",
			remoteURL:   "https://github.com/inference-gateway/cli.git",
			expected:    "inference-gateway/cli",
			description: "Should extract owner/repo from HTTPS URL",
		},
		{
			name:        "HTTPS GitHub URL without .git",
			remoteURL:   "https://github.com/inference-gateway/cli",
			expected:    "inference-gateway/cli",
			description: "Should extract owner/repo from HTTPS URL without .git extension",
		},
		{
			name:        "SSH GitHub URL",
			remoteURL:   "git@github.com:inference-gateway/cli.git",
			expected:    "inference-gateway/cli",
			description: "Should extract owner/repo from SSH URL",
		},
		{
			name:        "SSH GitHub URL without .git",
			remoteURL:   "git@github.com:inference-gateway/cli",
			expected:    "inference-gateway/cli",
			description: "Should extract owner/repo from SSH URL without .git extension",
		},
		{
			name:        "HTTPS GitLab URL",
			remoteURL:   "https://gitlab.com/myorg/myproject.git",
			expected:    "myorg/myproject",
			description: "Should extract owner/repo from GitLab HTTPS URL",
		},
		{
			name:        "SSH GitLab URL",
			remoteURL:   "git@gitlab.com:myorg/myproject.git",
			expected:    "myorg/myproject",
			description: "Should extract owner/repo from GitLab SSH URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Testing URL pattern: %s", tt.remoteURL)
			t.Logf("Expected result: %s", tt.expected)
			t.Logf("Description: %s", tt.description)
		})
	}
}

func TestGetGitBranch(t *testing.T) {
	result := getGitBranch()
	t.Logf("getGitBranch() = %q", result)
}

func TestGetGitMainBranch(t *testing.T) {
	result := getGitMainBranch()
	t.Logf("getGitMainBranch() = %q", result)
}

func TestGetRecentCommits(t *testing.T) {
	commits := getRecentCommits(5)
	t.Logf("getRecentCommits(5) returned %d commits", len(commits))
	for i, commit := range commits {
		t.Logf("Commit %d: %s", i+1, commit)
	}
}

// twoStubSkills returns the pair of distinctive skills used by the
// buildSkillsInfo formatting case.
func twoStubSkills() []agentdomain.Skill {
	return []agentdomain.Skill{
		{Name: "pdf-helper", Description: "Extract text from PDFs.", Path: "/abs/path/.infer/skills/pdf-helper/SKILL.md", Scope: agentdomain.SkillScopeProject},
		{Name: "diagrams", Description: "Render mermaid diagrams.", Path: "/home/me/.infer/skills/diagrams/SKILL.md", Scope: agentdomain.SkillScopeUser},
	}
}

// manyStubSkills returns four long-description skills so the char cap kicks in.
func manyStubSkills() []agentdomain.Skill {
	var skills []agentdomain.Skill
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		skills = append(skills, agentdomain.Skill{
			Name:        name,
			Description: strings.Repeat("x", 200),
			Path:        "/abs/.infer/skills/" + name + "/SKILL.md",
			Scope:       agentdomain.SkillScopeProject,
		})
	}
	return skills
}

func skillsCapConfig(maxChars int) *config.Config {
	return &config.Config{Agent: config.AgentConfig{Skills: config.AgentSkillsConfig{Enabled: true, MaxChars: maxChars}}}
}

func TestBuildSkillsInfo(t *testing.T) {
	tests := []struct {
		name         string
		svc          *AgentServiceImpl
		wantEmpty    bool
		wantContains []string
		wantAbsent   []string
		minPaths     int
		exactPaths   int
	}{
		{
			name:       "nil service",
			svc:        &AgentServiceImpl{},
			wantEmpty:  true,
			exactPaths: -1,
		},
		{
			name:       "empty list",
			svc:        &AgentServiceImpl{skillsService: &agentdomainmocks.FakeSkillsService{}},
			wantEmpty:  true,
			exactPaths: -1,
		},
		{
			name: "formats skills",
			svc: func() *AgentServiceImpl {
				fake := &agentdomainmocks.FakeSkillsService{}
				fake.ListReturns(twoStubSkills())
				return &AgentServiceImpl{skillsService: fake}
			}(),
			wantContains: []string{
				"AVAILABLE SKILLS:",
				"Read tool",
				"pdf-helper",
				"Extract text from PDFs.",
				"/abs/path/.infer/skills/pdf-helper/SKILL.md",
				"diagrams",
				"Render mermaid diagrams.",
				"/home/me/.infer/skills/diagrams/SKILL.md",
				"project",
				"user",
			},
			minPaths:   2,
			exactPaths: -1,
		},
		{
			name: "caps rendered list at max chars",
			svc: func() *AgentServiceImpl {
				fake := &agentdomainmocks.FakeSkillsService{}
				fake.ListReturns(manyStubSkills())
				return &AgentServiceImpl{config: skillsCapConfig(700), skillsService: fake}
			}(),
			wantContains: []string{"/abs/.infer/skills/alpha/SKILL.md", "more skills not expanded", "infer skills search"},
			wantAbsent:   []string{"/abs/.infer/skills/delta/SKILL.md", "delta"},
			exactPaths:   1,
		},
		{
			name: "no cap when max chars is zero",
			svc: func() *AgentServiceImpl {
				fake := &agentdomainmocks.FakeSkillsService{}
				fake.ListReturns(manyStubSkills())
				return &AgentServiceImpl{config: skillsCapConfig(0), skillsService: fake}
			}(),
			wantAbsent: []string{"more skills not expanded"},
			exactPaths: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.svc.buildSkillsInfo()

			if tt.wantEmpty {
				require.Empty(t, got)
			}
			for _, want := range tt.wantContains {
				require.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				require.NotContains(t, got, absent)
			}
			if tt.minPaths > 0 {
				require.GreaterOrEqual(t, strings.Count(got, "Path: "), tt.minPaths)
			}
			if tt.exactPaths >= 0 {
				require.Equal(t, tt.exactPaths, strings.Count(got, "Path: "))
			}
		})
	}
}

// TestBuildSkillsInfo_LargeCatalogStaysBounded guards the section against a
// catalog that grows without limit. This string rides in the static system
// prompt, so anything O(catalog size) here is paid on every request and sits in
// the KV-cache prefix.
func TestBuildSkillsInfo_LargeCatalogStaysBounded(t *testing.T) {
	const maxChars = 4000

	all := []agentdomain.Skill{{
		Name:        "local-one",
		Description: "The one skill that is actually on disk.",
		Path:        "/abs/.infer/skills/local-one/SKILL.md",
		Scope:       agentdomain.SkillScopeProject,
	}}
	for i := 0; i < 1000; i++ {
		all = append(all, agentdomain.Skill{
			Name:        fmt.Sprintf("catalog-skill-%04d", i),
			Description: strings.Repeat("y", 200),
			Scope:       agentdomain.SkillScopeCatalog,
		})
	}

	fake := &agentdomainmocks.FakeSkillsService{}
	fake.ListReturns(all)
	got := (&AgentServiceImpl{config: skillsCapConfig(maxChars), skillsService: fake}).buildSkillsInfo()

	// The tail line is written after the cap check, so allow one line of slack -
	// what must not happen is growth proportional to the catalog.
	require.Less(t, len(got), maxChars+200,
		"skills section must stay within its char budget regardless of catalog size")
	require.Contains(t, got, "more skills not expanded")
	require.NotContains(t, got, "catalog-skill-0999",
		"omitted skills must be counted, never named")

	// The one actionable skill outranks 1000 path-less catalog entries.
	require.Contains(t, got, "/abs/.infer/skills/local-one/SKILL.md")
}

// toolDef builds an sdk.ChatCompletionTool with the given name and (optional)
// description, mirroring what tools register via Definition().
func toolDef(name, description string) sdk.ChatCompletionTool {
	fn := sdk.FunctionObject{Name: name}
	if description != "" {
		fn.Description = &description
	}
	return sdk.ChatCompletionTool{Type: sdk.ChatCompletionToolType("function"), Function: fn}
}

func TestBuildToolsInfo(t *testing.T) {
	long := strings.Repeat("x", 200)
	tests := []struct {
		name         string
		noService    bool
		tools        []sdk.ChatCompletionTool
		stateMode    *agentdomain.AgentMode
		wantEmpty    bool
		wantContains []string
		wantAbsent   []string
		wantModeArg  *agentdomain.AgentMode
	}{
		{
			name:      "nil service returns empty",
			noService: true,
			wantEmpty: true,
		},
		{
			name:      "empty tool list returns empty",
			wantEmpty: true,
		},
		{
			name: "formats roster with first description line",
			tools: []sdk.ChatCompletionTool{
				toolDef("Read", "Read a file from disk.\nSupports line ranges."),
				toolDef("Grep", "Search file contents with a regex."),
				toolDef("Bare", ""),
			},
			wantContains: []string{
				"AVAILABLE TOOLS:",
				"- Read: Read a file from disk.",
				"- Grep: Search file contents with a regex.",
				"- Bare\n",
			},
			wantAbsent: []string{"Supports line ranges.", "- Bare:"},
		},
		{
			name:         "truncates long description",
			tools:        []sdk.ChatCompletionTool{toolDef("Big", long)},
			wantContains: []string{"..."},
			wantAbsent:   []string{long},
		},
		{
			name:        "defaults to standard mode without state manager",
			tools:       []sdk.ChatCompletionTool{toolDef("Read", "Read a file.")},
			wantModeArg: ptr(agentdomain.AgentModeStandard),
		},
		{
			name:        "uses current agent mode from state manager",
			tools:       []sdk.ChatCompletionTool{toolDef("Read", "Read a file.")},
			stateMode:   ptr(agentdomain.AgentModePlan),
			wantModeArg: ptr(agentdomain.AgentModePlan),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &AgentServiceImpl{}
			fake := &agentdomainmocks.FakeToolService{}
			if !tt.noService {
				fake.ListToolsForModeReturns(tt.tools)
				s.toolService = fake
			}
			if tt.stateMode != nil {
				sm := statemanager.NewStateManager(false)
				sm.SetAgentMode(*tt.stateMode)
				s.stateManager = sm
			}

			got := s.buildToolsInfo()

			if tt.wantEmpty {
				require.Empty(t, got)
			}
			for _, want := range tt.wantContains {
				require.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				require.NotContains(t, got, absent)
			}
			if tt.wantModeArg != nil {
				require.Equal(t, 1, fake.ListToolsForModeCallCount())
				require.Equal(t, *tt.wantModeArg, fake.ListToolsForModeArgsForCall(0))
			}
		})
	}
}

func userMsg(text string) sdk.Message {
	return sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(text)}
}

func assistantMsg(text string) sdk.Message {
	return sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent(text)}
}

// activeSkillsAgent returns an agent whose skills service knows foo/bar, each
// with distinctive metadata (description + path).
func activeSkillsAgent() *AgentServiceImpl {
	fake := &agentdomainmocks.FakeSkillsService{}
	skills := []agentdomain.Skill{
		{Name: "foo", Description: "FOO_DESC", Path: "/abs/.infer/skills/foo/SKILL.md", Scope: agentdomain.SkillScopeProject},
		{Name: "bar", Description: "BAR_DESC", Path: "/home/me/.infer/skills/bar/SKILL.md", Scope: agentdomain.SkillScopeUser},
	}
	get := func(name string) (agentdomain.Skill, bool) {
		for _, sk := range skills {
			if sk.Name == name {
				return sk, true
			}
		}
		return agentdomain.Skill{}, false
	}
	fake.GetStub = get
	fake.DiscoverStub = func(_ context.Context, name string) (agentdomain.Skill, bool) { return get(name) }
	return &AgentServiceImpl{
		skillsService: fake,
	}
}

func TestBuildActiveSkillInfo(t *testing.T) {
	tests := []struct {
		name         string
		nilService   bool
		messages     []sdk.Message
		wantEmpty    bool
		wantContains []string
		wantFooCount int
	}{
		{
			name:       "nil service returns empty",
			nilService: true,
			messages:   []sdk.Message{userMsg("/foo")},
			wantEmpty:  true,
		},
		{
			name:      "no trigger returns empty",
			messages:  []sdk.Message{userMsg("just a normal message about foo")},
			wantEmpty: true,
		},
		{
			name:     "slash trigger injects metadata not body",
			messages: []sdk.Message{userMsg("/foo please do the thing")},
			wantContains: []string{
				"ACTIVE SKILL ",
				"Read tool",
				"foo (project): FOO_DESC",
				"Path: /abs/.infer/skills/foo/SKILL.md",
			},
		},
		{
			name:         "phrase trigger with article",
			messages:     []sdk.Message{userMsg("use the bar skill now")},
			wantContains: []string{"bar (user): BAR_DESC"},
		},
		{
			name:         "phrase trigger without article",
			messages:     []sdk.Message{userMsg("use bar skill")},
			wantContains: []string{"bar (user): BAR_DESC"},
		},
		{
			name:         "phrase trigger is case-insensitive",
			messages:     []sdk.Message{userMsg("Please Use The Bar Skill")},
			wantContains: []string{"bar (user): BAR_DESC"},
		},
		{
			name:         "two skills use plural header",
			messages:     []sdk.Message{userMsg("/foo and use the bar skill")},
			wantContains: []string{"ACTIVE SKILLS ", "FOO_DESC", "BAR_DESC"},
		},
		{
			name:         "dedupes across messages",
			messages:     []sdk.Message{userMsg("/foo"), userMsg("/foo again")},
			wantFooCount: 1,
		},
		{
			name:      "unknown token ignored",
			messages:  []sdk.Message{userMsg("/unknown-skill do it")},
			wantEmpty: true,
		},
		{
			name:      "only user messages scanned",
			messages:  []sdk.Message{assistantMsg("you could /foo here")},
			wantEmpty: true,
		},
		{
			name:         "adjacent slash tokens both trigger",
			messages:     []sdk.Message{userMsg("/foo /bar")},
			wantContains: []string{"FOO_DESC", "BAR_DESC"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := activeSkillsAgent()
			if tt.nilService {
				s = &AgentServiceImpl{}
			}

			got := s.buildActiveSkillInfo(tt.messages, false)

			if tt.wantEmpty {
				require.Empty(t, got)
			}
			for _, want := range tt.wantContains {
				require.Contains(t, got, want)
			}
			if tt.wantFooCount > 0 {
				require.Equal(t, tt.wantFooCount, strings.Count(got, "FOO_DESC"))
			}
		})
	}
}

func TestFilterMemoryIndex(t *testing.T) {
	index := "# Memory Index\n\n" +
		"- [global-fact](global-fact.md) - a global fact\n" +
		"- [inference-gateway-cli/build](inference-gateway-cli/build.md) - build steps\n" +
		"- [other-proj/x](other-proj/x.md) - other\n" +
		"- [zeta/y](zeta/y.md) - another\n" +
		"- unparsable entry line\n"

	got := filterMemoryIndex(index, "inference-gateway-cli")
	for _, want := range []string{
		"](global-fact.md)",
		"](inference-gateway-cli/build.md)",
		"- unparsable entry line",
		"other projects with memories",
		"other-proj, zeta",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("filtered index missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "](other-proj/x.md)") || strings.Contains(got, "](zeta/y.md)") {
		t.Errorf("foreign entries must be collapsed:\n%s", got)
	}

	got = filterMemoryIndex(index, "")
	if strings.Contains(got, "](inference-gateway-cli/build.md)") {
		t.Errorf("empty project slug must keep only globals:\n%s", got)
	}
	if !strings.Contains(got, "inference-gateway-cli, other-proj, zeta") {
		t.Errorf("all projects must be summarized (sorted):\n%s", got)
	}

	onlyGlobal := "# Memory Index\n\n- [a](a.md) - a\n"
	if got := filterMemoryIndex(onlyGlobal, "p"); strings.Contains(got, "other projects") {
		t.Errorf("no foreign projects must mean no summary line:\n%s", got)
	}
}

func TestBuildMemoryInfo_TruncatesAtLineBoundary(t *testing.T) {
	dir := t.TempDir()
	var src strings.Builder
	src.WriteString("# Memory Index\n\n")
	for i := range 30 {
		fmt.Fprintf(&src, "- [fact-%02d](fact-%02d.md) - description of fact number %02d\n", i, i, i)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.MemoryIndexFileName), []byte(src.String()), 0o600))

	cfg := &config.Config{Memory: config.MemoryConfig{Enabled: true, Dir: dir, MaxChars: 500}}
	s := &AgentServiceImpl{config: cfg}

	got := s.buildMemoryInfo(1)

	require.Contains(t, got, "memory index truncated")
	for line := range strings.SplitSeq(got, "\n") {
		if strings.HasPrefix(line, "- [") {
			require.Contains(t, src.String(), line+"\n", "truncation left a partial entry: %q", line)
		}
	}
}

func TestBuildAgentsMDInfo(t *testing.T) {
	newSvc := func(enabled bool, maxChars int) *AgentServiceImpl {
		cfg := &config.Config{}
		cfg.Agent.AgentsMD = config.AgentsMDConfig{Enabled: enabled, MaxChars: maxChars}
		return &AgentServiceImpl{config: cfg}
	}

	t.Run("disabled returns empty", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("AGENTS.md", []byte("rules"), 0o644))
		require.Empty(t, newSvc(false, 0).buildAgentsMDInfo())
	})

	t.Run("missing file returns empty", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.Empty(t, newSvc(true, 0).buildAgentsMDInfo())
	})

	t.Run("empty file returns empty", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("AGENTS.md", []byte("  \n"), 0o644))
		require.Empty(t, newSvc(true, 0).buildAgentsMDInfo())
	})

	t.Run("injects content with header", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("AGENTS.md", []byte("# Rules\nBe lazy."), 0o644))
		got := newSvc(true, 0).buildAgentsMDInfo()
		require.True(t, strings.HasPrefix(got, "PROJECT INSTRUCTIONS (AGENTS.md):\n"))
		require.Contains(t, got, "Be lazy.")
	})

	t.Run("caps content with truncation marker", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("AGENTS.md", []byte(strings.Repeat("x", 100)), 0o644))
		got := newSvc(true, 10).buildAgentsMDInfo()
		require.Contains(t, got, strings.Repeat("x", 10))
		require.NotContains(t, got, strings.Repeat("x", 11))
		require.Contains(t, got, "[truncated at 10 chars]")
	})
}

func TestBuildProjectTreeInfo(t *testing.T) {
	t.Run("injects tree with real file names when enabled", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("known_file.go", []byte("package x"), 0o644))

		cfg := &config.Config{}
		cfg.Tools.Enabled = true
		cfg.Agent.Context.TreeEnabled = true
		cfg.Agent.Context.GitContextRefreshTurns = 10
		s := &AgentServiceImpl{config: cfg}

		got := s.buildProjectTreeInfo(0)
		require.Contains(t, got, "PROJECT STRUCTURE")
		require.Contains(t, got, "known_file.go")
	})

	t.Run("empty when disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Tools.Enabled = true
		cfg.Agent.Context.TreeEnabled = false
		s := &AgentServiceImpl{config: cfg}

		require.Empty(t, s.buildProjectTreeInfo(0))
	})
}

func TestBuildSystemPrompt_AgentsMDAfterCustomInstructions(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("AGENTS.md", []byte("project rules here"), 0o644))

	cfg := &config.Config{}
	cfg.Prompts.Agent.SystemPrompt = "base prompt"
	cfg.Prompts.Agent.CustomInstructions = "custom instructions here"
	cfg.Agent.AgentsMD = config.AgentsMDConfig{Enabled: true, MaxChars: config.DefaultInstructionsMaxChars}
	s := &AgentServiceImpl{config: cfg}

	got := s.BuildSystemPrompt()
	base := strings.Index(got, "base prompt")
	custom := strings.Index(got, "custom instructions here")
	agentsMD := strings.Index(got, "PROJECT INSTRUCTIONS (AGENTS.md):\nproject rules here")
	require.GreaterOrEqual(t, base, 0)
	require.Greater(t, custom, base)
	require.Greater(t, agentsMD, custom)
}

func TestBuildSystemPrompt_PluginInstructionsAfterAgentsMD(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("AGENTS.md", []byte("project rules"), 0o644))

	pluginsDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(pluginsDir, "ponytail"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "ponytail", config.PluginAgentsMDName), []byte("be lazy"), 0o644))

	cfg := &config.Config{}
	cfg.Prompts.Agent.SystemPrompt = "base prompt"
	cfg.Agent.AgentsMD = config.AgentsMDConfig{Enabled: true, MaxChars: config.DefaultInstructionsMaxChars, MaxLines: config.DefaultInstructionsMaxLines}
	cfg.Plugins = *config.DefaultPluginsConfig()
	cfg.Plugins.Dir = pluginsDir
	cfg.Plugins.Plugins = []config.PluginEntry{{Name: "ponytail", Enabled: true}}
	s := &AgentServiceImpl{config: cfg}

	got := s.BuildSystemPrompt()
	project := strings.Index(got, "PROJECT INSTRUCTIONS (AGENTS.md):\nproject rules")
	plugin := strings.Index(got, "PLUGIN INSTRUCTIONS (ponytail):\nbe lazy")
	require.GreaterOrEqual(t, project, 0)
	require.Greater(t, plugin, project)
}

func TestBuildSystemPrompt_ExcludesVolatileSections(t *testing.T) {
	t.Chdir(t.TempDir())
	memDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(memDir, config.MemoryIndexFileName), []byte("- [fact](fact.md) - a fact\n"), 0o600))

	cfg := &config.Config{}
	cfg.Prompts.Agent.SystemPrompt = "base prompt"
	cfg.Agent.SystemPromptWithDefaults = true
	cfg.Agent.Context = config.AgentContextConfig{GitContextEnabled: true, TreeEnabled: true, GitContextRefreshTurns: 10}
	cfg.Memory = config.MemoryConfig{Enabled: true, Dir: memDir}
	cfg.Tools.Enabled = true
	s := &AgentServiceImpl{config: cfg}

	got := s.BuildSystemPrompt()

	require.NotContains(t, got, "Current date:")
	require.NotContains(t, got, "GIT REPOSITORY CONTEXT")
	require.NotContains(t, got, "PROJECT STRUCTURE")
	require.NotContains(t, got, "PERSISTENT MEMORY INDEX")
	require.Equal(t, got, s.BuildSystemPrompt(), "system prompt must be byte-identical across calls")
}

func TestVolatileTailMessage(t *testing.T) {
	newSvc := func(memoryEnabled bool, withDefaults bool) *AgentServiceImpl {
		memDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(memDir, config.MemoryIndexFileName), []byte("- [fact](fact.md) - a fact\n"), 0o600))
		cfg := &config.Config{}
		cfg.Prompts.Agent.SystemPrompt = "base prompt"
		cfg.Agent.SystemPromptWithDefaults = withDefaults
		cfg.Memory = config.MemoryConfig{Enabled: memoryEnabled, Dir: memDir}
		return &AgentServiceImpl{config: cfg}
	}

	tailContent := func(t *testing.T, msg sdk.Message) string {
		t.Helper()
		content, err := msg.Content.AsMessageContent0()
		require.NoError(t, err)
		return content
	}

	t.Run("carries volatile sections and date", func(t *testing.T) {
		msg, ok := newSvc(true, true).volatileTailMessage(nil, true)
		require.True(t, ok)
		require.Equal(t, sdk.User, msg.Role)
		content := tailContent(t, msg)
		require.True(t, strings.HasPrefix(content, "<system-reminder>\n"))
		require.True(t, strings.HasSuffix(content, "\n</system-reminder>"))
		require.Contains(t, content, "PERSISTENT MEMORY INDEX")
		require.Contains(t, content, "Current date:")
	})

	t.Run("disabled section is absent", func(t *testing.T) {
		msg, ok := newSvc(false, true).volatileTailMessage(nil, true)
		require.True(t, ok)
		require.NotContains(t, tailContent(t, msg), "PERSISTENT MEMORY INDEX")
	})

	t.Run("defaults off keeps only the date", func(t *testing.T) {
		msg, ok := newSvc(true, false).volatileTailMessage(nil, true)
		require.True(t, ok)
		content := tailContent(t, msg)
		require.NotContains(t, content, "PERSISTENT MEMORY INDEX")
		require.Contains(t, content, "Current date:")
	})

	t.Run("no system prompt yields no tail", func(t *testing.T) {
		s := newSvc(true, true)
		s.config.Prompts.Agent.SystemPrompt = ""
		_, ok := s.volatileTailMessage(nil, true)
		require.False(t, ok)
	})

	t.Run("open tool calls still yield a tail", func(t *testing.T) {
		toolCalls := []sdk.ChatCompletionMessageToolCall{{ID: "call_1"}}
		messages := []sdk.Message{
			{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
			{Role: sdk.Assistant, ToolCalls: &toolCalls},
		}

		_, ok := newSvc(true, true).volatileTailMessage(messages, true)
		require.True(t, ok)
	})
}

// TestBuildSkillsInfo_CatalogEntryIsCacheStable locks in that installing a
// catalog skill mid-session does not rewrite the static system prompt. The
// AVAILABLE SKILLS section rides in message[0]; if the freshly-downloaded path
// leaked into it, every conversation would lose its KV-cache prefix the moment
// a skill was fetched.
func TestBuildSkillsInfo_CatalogEntryIsCacheStable(t *testing.T) {
	notInstalled := agentdomain.Skill{
		Name:        "rust",
		Description: "Idiomatic Rust.",
		Scope:       agentdomain.SkillScopeCatalog,
	}
	installed := notInstalled
	installed.Path = "/abs/.infer/tmp/skills/rust/SKILL.md"

	render := func(sk agentdomain.Skill) string {
		fake := &agentdomainmocks.FakeSkillsService{}
		fake.ListReturns([]agentdomain.Skill{sk})
		return (&AgentServiceImpl{skillsService: fake}).buildSkillsInfo()
	}

	before, after := render(notInstalled), render(installed)
	require.Equal(t, before, after, "installing a catalog skill must not change the static system prompt")
	require.Contains(t, before, "rust")
	require.NotContains(t, after, installed.Path, "the concrete path belongs in the volatile tail, not the system prompt")
}

// TestBuildActiveSkillInfo_CatalogInstallIsHeadlessOnly locks in the approval
// split: a headless run installs a not-yet-downloaded skill itself, while chat
// leaves it alone because the user approves the install at the input layer.
func TestBuildActiveSkillInfo_CatalogInstallIsHeadlessOnly(t *testing.T) {
	newAgent := func() (*AgentServiceImpl, *agentdomainmocks.FakeSkillsService) {
		fake := &agentdomainmocks.FakeSkillsService{}
		fake.GetReturns(agentdomain.Skill{Name: "rust", Description: "RUST_DESC", Scope: agentdomain.SkillScopeCatalog}, true)
		fake.DiscoverReturns(agentdomain.Skill{
			Name:        "rust",
			Description: "RUST_DESC",
			Path:        "/abs/.infer/tmp/skills/rust/SKILL.md",
			Scope:       agentdomain.SkillScopeCatalog,
		}, true)
		return &AgentServiceImpl{skillsService: fake}, fake
	}

	messages := []sdk.Message{userMsg("/rust do a thing")}

	chatAgent, chatFake := newAgent()
	require.Empty(t, chatAgent.buildActiveSkillInfo(messages, true))
	require.Zero(t, chatFake.DiscoverCallCount(), "chat must not download without approval")

	headlessAgent, headlessFake := newAgent()
	got := headlessAgent.buildActiveSkillInfo(messages, false)
	require.Contains(t, got, "/abs/.infer/tmp/skills/rust/SKILL.md")
	require.Equal(t, 1, headlessFake.DiscoverCallCount())
}
