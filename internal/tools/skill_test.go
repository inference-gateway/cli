package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestSkillTool_Execute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("---\nname: go\n---\n# Go\nbe boring\n"), 0o644))

	local := agentdomain.Skill{Name: "go", Path: path, Scope: agentdomain.SkillScopeProject}
	catalog := agentdomain.Skill{Name: "remote", Scope: agentdomain.SkillScopeCatalog}
	fake := &agentdomainmocks.FakeSkillsService{}
	fake.GetStub = func(name string) (agentdomain.Skill, bool) {
		switch name {
		case "go":
			return local, true
		case "remote":
			return catalog, true
		}
		return agentdomain.Skill{}, false
	}
	fake.DiscoverStub = func(_ context.Context, name string) (agentdomain.Skill, bool) {
		return agentdomain.Skill{Name: name, Path: path, Scope: agentdomain.SkillScopeCatalog}, true
	}
	cfg := config.DefaultConfig()
	tool := NewSkillTool(cfg, fake)

	tests := []struct {
		name        string
		args        map[string]any
		ctx         context.Context
		wantOK      bool
		wantContain string
	}{
		{"loads a local skill", map[string]any{"name": "go"}, context.Background(), true, "be boring"},
		{"accepts a plugin-qualified name", map[string]any{"name": "plugin:go"}, context.Background(), true, "be boring"},
		{"unknown skill fails", map[string]any{"name": "nope"}, context.Background(), false, "unknown skill"},
		{"catalog skill is installed outside chat", map[string]any{"name": "remote"}, context.Background(), true, "be boring"},
		{"catalog skill is not installed in chat", map[string]any{"name": "remote"}, agentdomain.WithChatHandler(context.Background(), &agentdomainmocks.FakeBashDetachChannelHolder{}), false, "ask the user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tool.Execute(tt.ctx, tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOK, res.Success)
			if tt.wantOK {
				assert.Contains(t, tool.FormatForLLM(res), tt.wantContain)
				assert.Contains(t, tool.FormatForLLM(res), "Skill: ")
				assert.NotContains(t, tool.FormatForLLM(res), "name: go", "frontmatter is stripped")
			} else {
				assert.Contains(t, res.Error, tt.wantContain)
			}
		})
	}

	assert.Error(t, tool.Validate(map[string]any{}))
	assert.NoError(t, tool.Validate(map[string]any{"name": "go"}))
	assert.True(t, tool.IsEnabled())
}
