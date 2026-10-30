package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tools "github.com/inference-gateway/cli/internal/tools"
)

func readCall(id, args string) sdk.Message {
	return toolCallMsg(id, tools.ToolRead, args)
}

func skillCall(id, name string) sdk.Message {
	return toolCallMsg(id, tools.ToolSkill, `{"name":"`+name+`"}`)
}

func toolCallMsg(id, name, args string) sdk.Message {
	return sdk.Message{
		Role:    sdk.Assistant,
		Content: sdk.NewMessageContent(""),
		ToolCalls: &[]sdk.ChatCompletionMessageToolCall{{
			ID:       id,
			Type:     sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{Name: name, Arguments: args},
		}},
	}
}

func toolResultMsg(id string) sdk.Message {
	return sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("result"), ToolCallID: &id}
}

// answered pairs every tool call in msgs with a tool result so it counts as a
// completed load.
func answered(msgs ...sdk.Message) []sdk.Message {
	var out []sdk.Message
	for _, m := range msgs {
		out = append(out, m)
		if m.ToolCalls != nil {
			for _, tc := range *m.ToolCalls {
				out = append(out, toolResultMsg(tc.ID))
			}
		}
	}
	return out
}

func skillsFake(skills ...agentdomain.Skill) *agentdomainmocks.FakeSkillsService {
	fake := &agentdomainmocks.FakeSkillsService{}
	fake.ListReturns(skills)
	fake.GetStub = func(name string) (agentdomain.Skill, bool) {
		for _, sk := range skills {
			if sk.Name == name {
				return sk, true
			}
		}
		return agentdomain.Skill{}, false
	}
	return fake
}

func TestSkillLoads(t *testing.T) {
	tests := []struct {
		name     string
		messages []sdk.Message
		want     []string
	}{
		{
			name:     "skill call counts by name",
			messages: []sdk.Message{skillCall("1", "go")},
			want:     []string{"go"},
		},
		{
			name:     "plugin-qualified skill call is reduced to the bare name",
			messages: []sdk.Message{skillCall("1", "ponytail:ponytail")},
			want:     []string{"ponytail"},
		},
		{
			name:     "a Read of the SKILL.md path is not a load",
			messages: []sdk.Message{readCall("1", `{"file_path":"/s/go/SKILL.md"}`)},
			want:     nil,
		},
		{
			name:     "malformed arguments are ignored",
			messages: []sdk.Message{toolCallMsg("1", tools.ToolSkill, `{"name":`)},
			want:     nil,
		},
		{
			name: "most recent first, deduplicated",
			messages: []sdk.Message{
				skillCall("1", "a"),
				skillCall("2", "b"),
				skillCall("3", "a"),
			},
			want: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, skillLoads(answered(tt.messages...)))
		})
	}
	t.Run("an unanswered call is not a load", func(t *testing.T) {
		assert.Nil(t, skillLoads([]sdk.Message{skillCall("1", "go")}))
	})
}

func TestLoadedSkills(t *testing.T) {
	fake := skillsFake(
		agentdomain.Skill{Name: "go", Path: "/s/go/SKILL.md"},
		agentdomain.Skill{Name: "ddd", Path: "/s/ddd/SKILL.md"},
		agentdomain.Skill{Name: "catalog", Path: ""},
	)
	messages := answered(
		userMsg("/go"),
		skillCall("1", "go"),
		skillCall("2", "ddd"),
		skillCall("3", "catalog"),
		skillCall("4", "go"),
	)

	got := loadedSkills(fake, messages)

	require.Len(t, got, 2, "a skill counts once however often it was loaded, a catalog skill without a path never")
	assert.Equal(t, "go", got[0].Name, "most recently loaded first")
	assert.Equal(t, "ddd", got[1].Name)
	assert.Nil(t, loadedSkills(nil, messages))
}

func writeSkill(t *testing.T, dir, name, body string) agentdomain.Skill {
	t.Helper()
	path := filepath.Join(dir, name, "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("---\nname: "+name+"\ndescription: test\n---\n"+body), 0o644))
	return agentdomain.Skill{Name: name, Path: path, Scope: agentdomain.SkillScopeProject}
}

func TestSkillRestorer(t *testing.T) {
	dir := t.TempDir()
	goSkill := writeSkill(t, dir, "go", "# Go\nbe boring\n")
	bigSkill := writeSkill(t, dir, "big", strings.Repeat("x", restoredSkillMaxChars+10))
	missing := agentdomain.Skill{Name: "missing", Path: filepath.Join(dir, "missing", "SKILL.md")}
	restore := SkillRestorer(skillsFake(goSkill, bigSkill, missing))

	t.Run("re-emits a Skill pair for a loaded skill", func(t *testing.T) {
		got := restore(answered(skillCall("1", "go")))

		require.Len(t, got, 2)
		assert.Equal(t, sdk.Assistant, got[0].Role)
		assert.Equal(t, "restored-skill-go", (*got[0].ToolCalls)[0].ID)
		assert.Equal(t, tools.ToolSkill, (*got[0].ToolCalls)[0].Function.Name)
		assert.Equal(t, sdk.Tool, got[1].Role)
		assert.Equal(t, "restored-skill-go", *got[1].ToolCallID)
		text, _ := got[1].Content.AsMessageContent0()
		assert.Contains(t, text, "Skill: go\nPath: "+goSkill.Path)
		assert.Contains(t, text, "be boring")
		assert.NotContains(t, text, "description: test", "frontmatter is stripped from the restored body")
		assert.Equal(t, []string{"go"}, skillLoads(got), "the restored pair must itself count as loaded")
	})

	t.Run("truncates an oversized skill", func(t *testing.T) {
		got := restore(answered(skillCall("1", "big")))

		require.Len(t, got, 2)
		text, _ := got[1].Content.AsMessageContent0()
		assert.Contains(t, text, restoredSkillTruncated)
		assert.Less(t, len(text), restoredSkillMaxChars+500)
	})

	t.Run("skips a missing file and unrelated reads", func(t *testing.T) {
		got := restore(answered(skillCall("1", "missing")))

		assert.Empty(t, got)
	})
}

func TestBuildLoadedSkillsInfo(t *testing.T) {
	goSkill := agentdomain.Skill{Name: "go", Description: "GO_DESC", Path: "/s/go/SKILL.md", Scope: agentdomain.SkillScopeProject}
	svc := &Agent{skillsService: skillsFake(goSkill)}
	messages := answered(userMsg("/go"), skillCall("1", "go"))

	assert.Contains(t, svc.buildLoadedSkillsInfo(messages), "do not load it again: go")
	assert.Empty(t, svc.buildActiveSkillInfo(context.Background(), messages, true), "a loaded skill is no longer told to be loaded")
	assert.Contains(t, svc.buildActiveSkillInfo(context.Background(), []sdk.Message{userMsg("/go")}, true), "Path: /s/go/SKILL.md")
	assert.Empty(t, svc.buildLoadedSkillsInfo([]sdk.Message{userMsg("/go")}))
}

func TestSkillDefinitionRead(t *testing.T) {
	goSkill := agentdomain.Skill{Name: "go", Path: "/s/go/SKILL.md"}
	svc := &Agent{skillsService: skillsFake(goSkill)}

	_, blocked := svc.skillDefinitionRead(tools.ToolRead, map[string]any{"file_path": "/s/go/SKILL.md"})
	assert.True(t, blocked, "a Read of the SKILL.md itself is redirected to the Skill tool")
	_, blocked = svc.skillDefinitionRead(tools.ToolRead, map[string]any{"file_path": "/s/go/references/api.md"})
	assert.False(t, blocked, "a skill's resource files stay readable")
	_, blocked = svc.skillDefinitionRead(tools.ToolSkill, map[string]any{"name": "go"})
	assert.False(t, blocked)
}

func TestAlreadyLoadedSkill(t *testing.T) {
	goSkill := agentdomain.Skill{Name: "go", Path: "/s/go/SKILL.md"}
	repo := &convmocks.FakeConversationRepository{}
	repo.GetMessagesReturns([]convdomain.ConversationEntry{
		{Message: userMsg("/go")},
		{Message: skillCall("1", "go")},
		{Message: toolResultMsg("1")},
	})
	svc := &Agent{skillsService: skillsFake(goSkill), conversationRepo: repo}

	tests := []struct {
		name string
		tool string
		args map[string]any
		want bool
	}{
		{"repeat Skill call of a loaded skill", tools.ToolSkill, map[string]any{"name": "go"}, true},
		{"plugin-qualified repeat call", tools.ToolSkill, map[string]any{"name": "plugin:go"}, true},
		{"Skill call of another skill", tools.ToolSkill, map[string]any{"name": "ddd"}, false},
		{"Read of the SKILL.md path is not a skill load", tools.ToolRead, map[string]any{"file_path": "/s/go/SKILL.md"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sk, got := svc.alreadyLoadedSkill(tt.tool, tt.args)
			assert.Equal(t, tt.want, got)
			if got {
				assert.Equal(t, "go", sk.Name)
				text, _ := svc.alreadyLoadedSkillEntry(sdk.ChatCompletionMessageToolCall{ID: "c"}, sk, tt.args, time.Now()).Message.Content.AsMessageContent0()
				assert.Contains(t, text, "Skill go is already loaded")
			}
		})
	}
}

func TestSkillRestorer_PluginAndCatalogSkills(t *testing.T) {
	dir := t.TempDir()
	plugin := writeSkill(t, dir, "ponytail", "# Ponytail\nbe lazy\n")
	plugin.Scope = agentdomain.SkillScopePlugin
	plugin.PluginName = "ponytail"
	catalog := agentdomain.Skill{Name: "remote", Scope: agentdomain.SkillScopeCatalog}
	svc := skillsFake(plugin, catalog)
	restore := SkillRestorer(svc)

	got := restore(answered(skillCall("1", "ponytail:ponytail"), skillCall("2", "remote")))

	require.Len(t, got, 2, "the plugin skill is restored, the uninstalled catalog skill has no file to restore")
	assert.Contains(t, (*got[0].ToolCalls)[0].Function.Arguments, `"ponytail:ponytail"`, "restored under its qualified name")
	assert.Equal(t, []string{"ponytail"}, skillLoads(got), "and detected again by its bare name")
	assert.Equal(t, "ponytail:ponytail", loadedSkills(svc, got)[0].DisplayName())
}
