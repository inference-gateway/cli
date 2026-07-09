package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// stubSkillsService implements domain.SkillsService for testing the
// system-prompt injection without depending on the real package's filesystem
// scan.
type stubSkillsService struct {
	skills []domain.Skill
}

func (s *stubSkillsService) Load(_ context.Context) error    { return nil }
func (s *stubSkillsService) List() []domain.Skill            { return s.skills }
func (s *stubSkillsService) Errors() []domain.SkillLoadError { return nil }

func (s *stubSkillsService) Get(name string) (domain.Skill, bool) {
	for _, sk := range s.skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return domain.Skill{}, false
}

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

// TestIsCompleteJSON_LargeFileSimulation tests the scenario where an LLM
// hits output token limits while generating large file content
func TestIsCompleteJSON_LargeFileSimulation(t *testing.T) {
	// Simulate a truncated Write tool call that would occur when
	// DeepSeek or another LLM hits output token limits
	incompleteWriteCall := `{"file_path": "/home/user/project/src/components/MyComponent.tsx", "content": "import React from 'react';\n\nexport const MyComponent: React.FC = () => {\n  const [state, setState] = React.useState<string>('');\n\n  return (\n    <div className=\"container\">\n      <h1>My Component</h1>\n      <p>This is a test component that demonstrates`

	if isCompleteJSON(incompleteWriteCall) {
		t.Error("Expected incomplete JSON to return false - this simulates the DeepSeek token limit issue")
	}

	// A complete version should pass
	completeWriteCall := `{"file_path": "/home/user/project/src/components/MyComponent.tsx", "content": "import React from 'react';\n\nexport const MyComponent: React.FC = () => {\n  return <div>Hello</div>;\n};\n"}`

	if !isCompleteJSON(completeWriteCall) {
		t.Error("Expected complete JSON to return true")
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

func TestBuildSkillsInfo_NilService(t *testing.T) {
	s := &AgentServiceImpl{}
	require.Empty(t, s.buildSkillsInfo())
}

func TestBuildSkillsInfo_EmptyList(t *testing.T) {
	s := &AgentServiceImpl{skillsService: &stubSkillsService{}}
	require.Empty(t, s.buildSkillsInfo())
}

func TestBuildSkillsInfo_FormatsSkills(t *testing.T) {
	s := &AgentServiceImpl{
		skillsService: &stubSkillsService{
			skills: []domain.Skill{
				{
					Name:        "pdf-helper",
					Description: "Extract text from PDFs.",
					Path:        "/abs/path/.infer/skills/pdf-helper/SKILL.md",
					Scope:       domain.SkillScopeProject,
				},
				{
					Name:        "diagrams",
					Description: "Render mermaid diagrams.",
					Path:        "/home/me/.infer/skills/diagrams/SKILL.md",
					Scope:       domain.SkillScopeUser,
				},
			},
		},
	}

	got := s.buildSkillsInfo()

	require.Contains(t, got, "AVAILABLE SKILLS:")
	require.Contains(t, got, "Read tool")
	for _, want := range []string{
		"pdf-helper",
		"Extract text from PDFs.",
		"/abs/path/.infer/skills/pdf-helper/SKILL.md",
		"diagrams",
		"Render mermaid diagrams.",
		"/home/me/.infer/skills/diagrams/SKILL.md",
		"project",
		"user",
	} {
		require.Contains(t, got, want)
	}
	require.GreaterOrEqual(t, strings.Count(got, "Path: "), 2)
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

func TestBuildToolsInfo_NilService(t *testing.T) {
	s := &AgentServiceImpl{}
	require.Empty(t, s.buildToolsInfo())
}

func TestBuildToolsInfo_EmptyList(t *testing.T) {
	fake := &domainmocks.FakeToolService{}
	fake.ListToolsForModeReturns(nil)
	s := &AgentServiceImpl{toolService: fake}
	require.Empty(t, s.buildToolsInfo())
}

func TestBuildToolsInfo_FormatsRoster(t *testing.T) {
	fake := &domainmocks.FakeToolService{}
	fake.ListToolsForModeReturns([]sdk.ChatCompletionTool{
		toolDef("Read", "Read a file from disk.\nSupports line ranges."),
		toolDef("Grep", "Search file contents with a regex."),
		toolDef("Bare", ""),
	})
	s := &AgentServiceImpl{toolService: fake}

	got := s.buildToolsInfo()

	require.Contains(t, got, "AVAILABLE TOOLS:")
	require.Contains(t, got, "- Read: Read a file from disk.")
	require.NotContains(t, got, "Supports line ranges.")
	require.Contains(t, got, "- Grep: Search file contents with a regex.")
	require.Contains(t, got, "- Bare\n")
	require.NotContains(t, got, "- Bare:")
}

func TestBuildToolsInfo_TruncatesLongDescription(t *testing.T) {
	long := strings.Repeat("x", 200)
	fake := &domainmocks.FakeToolService{}
	fake.ListToolsForModeReturns([]sdk.ChatCompletionTool{toolDef("Big", long)})
	s := &AgentServiceImpl{toolService: fake}

	got := s.buildToolsInfo()

	require.Contains(t, got, "...")
	require.NotContains(t, got, long)
}

func TestBuildToolsInfo_DefaultsToStandardModeWhenNoStateManager(t *testing.T) {
	fake := &domainmocks.FakeToolService{}
	fake.ListToolsForModeReturns([]sdk.ChatCompletionTool{toolDef("Read", "Read a file.")})
	s := &AgentServiceImpl{toolService: fake}

	s.buildToolsInfo()

	require.Equal(t, 1, fake.ListToolsForModeCallCount())
	require.Equal(t, domain.AgentModeStandard, fake.ListToolsForModeArgsForCall(0))
}

func TestBuildToolsInfo_UsesCurrentAgentMode(t *testing.T) {
	fake := &domainmocks.FakeToolService{}
	fake.ListToolsForModeReturns([]sdk.ChatCompletionTool{toolDef("Read", "Read a file.")})
	sm := &domainmocks.FakeStateManager{}
	sm.GetAgentModeReturns(domain.AgentModePlan)
	s := &AgentServiceImpl{toolService: fake, stateManager: sm}

	s.buildToolsInfo()

	require.Equal(t, domain.AgentModePlan, fake.ListToolsForModeArgsForCall(0))
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
	return &AgentServiceImpl{
		skillsService: &stubSkillsService{
			skills: []domain.Skill{
				{Name: "foo", Description: "FOO_DESC", Path: "/abs/.infer/skills/foo/SKILL.md", Scope: domain.SkillScopeProject},
				{Name: "bar", Description: "BAR_DESC", Path: "/home/me/.infer/skills/bar/SKILL.md", Scope: domain.SkillScopeUser},
			},
		},
	}
}

func TestBuildActiveSkillInfo_NilService(t *testing.T) {
	s := &AgentServiceImpl{}
	require.Empty(t, s.buildActiveSkillInfo([]sdk.Message{userMsg("/foo")}))
}

func TestBuildActiveSkillInfo_NoTrigger(t *testing.T) {
	s := activeSkillsAgent()
	require.Empty(t, s.buildActiveSkillInfo([]sdk.Message{userMsg("just a normal message about foo")}))
}

// The deterministic path injects only metadata (description + path) and points
// the model at the Read tool - it never inlines the SKILL.md body.
func TestBuildActiveSkillInfo_SlashTriggerInjectsMetadataNotBody(t *testing.T) {
	s := activeSkillsAgent()
	got := s.buildActiveSkillInfo([]sdk.Message{userMsg("/foo please do the thing")})
	require.Contains(t, got, "ACTIVE SKILL ")
	require.Contains(t, got, "Read tool")
	require.Contains(t, got, "foo (project): FOO_DESC")
	require.Contains(t, got, "Path: /abs/.infer/skills/foo/SKILL.md")
}

func TestBuildActiveSkillInfo_PhraseTriggerCaseInsensitive(t *testing.T) {
	for _, text := range []string{"use the bar skill now", "use bar skill", "Please Use The Bar Skill"} {
		s := activeSkillsAgent()
		got := s.buildActiveSkillInfo([]sdk.Message{userMsg(text)})
		require.Contains(t, got, "bar (user): BAR_DESC", "text: %q", text)
	}
}

func TestBuildActiveSkillInfo_TwoSkillsPluralHeader(t *testing.T) {
	s := activeSkillsAgent()
	got := s.buildActiveSkillInfo([]sdk.Message{userMsg("/foo and use the bar skill")})
	require.Contains(t, got, "ACTIVE SKILLS ")
	require.Contains(t, got, "FOO_DESC")
	require.Contains(t, got, "BAR_DESC")
}

func TestBuildActiveSkillInfo_DedupesAcrossMessages(t *testing.T) {
	s := activeSkillsAgent()
	got := s.buildActiveSkillInfo([]sdk.Message{userMsg("/foo"), userMsg("/foo again")})
	require.Equal(t, 1, strings.Count(got, "FOO_DESC"))
}

func TestBuildActiveSkillInfo_UnknownTokenIgnored(t *testing.T) {
	s := activeSkillsAgent()
	require.Empty(t, s.buildActiveSkillInfo([]sdk.Message{userMsg("/unknown-skill do it")}))
}

func TestBuildActiveSkillInfo_OnlyUserMessagesScanned(t *testing.T) {
	s := activeSkillsAgent()
	require.Empty(t, s.buildActiveSkillInfo([]sdk.Message{assistantMsg("you could /foo here")}))
}

func TestBuildActiveSkillInfo_AdjacentSlashTokens(t *testing.T) {
	s := activeSkillsAgent()
	got := s.buildActiveSkillInfo([]sdk.Message{userMsg("/foo /bar")})
	require.Contains(t, got, "FOO_DESC")
	require.Contains(t, got, "BAR_DESC")
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

func TestBuildSkillsInfo_CapsRenderedList(t *testing.T) {
	var skills []domain.Skill
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		skills = append(skills, domain.Skill{
			Name:        name,
			Description: strings.Repeat("x", 200),
			Path:        "/abs/.infer/skills/" + name + "/SKILL.md",
			Scope:       domain.SkillScopeProject,
		})
	}
	cfg := &config.Config{Agent: config.AgentConfig{Skills: config.AgentSkillsConfig{Enabled: true, MaxChars: 700}}}
	s := &AgentServiceImpl{config: cfg, skillsService: &stubSkillsService{skills: skills}}

	got := s.buildSkillsInfo()

	require.Contains(t, got, "/abs/.infer/skills/alpha/SKILL.md")
	require.Contains(t, got, "more skills not expanded")
	require.Contains(t, got, "delta")
	require.NotContains(t, got, "/abs/.infer/skills/delta/SKILL.md")
	require.Equal(t, 1, strings.Count(got, "Path: "))

	cfg.Agent.Skills.MaxChars = 0
	got = s.buildSkillsInfo()
	require.NotContains(t, got, "more skills not expanded")
	require.Equal(t, 4, strings.Count(got, "Path: "))
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

func TestBuildSystemPromptText_AgentsMDAfterCustomInstructions(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("AGENTS.md", []byte("project rules here"), 0o644))

	cfg := &config.Config{}
	cfg.Prompts.Agent.SystemPrompt = "base prompt"
	cfg.Prompts.Agent.CustomInstructions = "custom instructions here"
	cfg.Agent.AgentsMD = config.AgentsMDConfig{Enabled: true, MaxChars: config.DefaultInstructionsMaxChars}
	s := &AgentServiceImpl{config: cfg}

	got := s.buildSystemPromptText(nil)
	base := strings.Index(got, "base prompt")
	custom := strings.Index(got, "custom instructions here")
	agentsMD := strings.Index(got, "PROJECT INSTRUCTIONS (AGENTS.md):\nproject rules here")
	require.GreaterOrEqual(t, base, 0)
	require.Greater(t, custom, base)
	require.Greater(t, agentsMD, custom)
}

func TestBuildSystemPromptText_PluginInstructionsAfterAgentsMD(t *testing.T) {
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

	got := s.buildSystemPromptText(nil)
	project := strings.Index(got, "PROJECT INSTRUCTIONS (AGENTS.md):\nproject rules")
	plugin := strings.Index(got, "PLUGIN INSTRUCTIONS (ponytail):\nbe lazy")
	require.GreaterOrEqual(t, project, 0)
	require.Greater(t, plugin, project)
}
