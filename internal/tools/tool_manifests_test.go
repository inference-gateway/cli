package tools

import (
	"maps"
	"slices"
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	avatars "github.com/inference-gateway/cli/internal/avatars"
)

// TestToolManifestsMatchValidation keeps the enums and bounds a manifest shows
// the model in step with the constants the tool validates its arguments with.
func TestToolManifestsMatchValidation(t *testing.T) {
	tests := []struct {
		tool string
		path string
		want any
	}{
		{ToolImageGeneration, "quality.enum", imageQualities},
		{ToolImageGeneration, "size.enum", imageSizes},
		{ToolImageEdit, "quality.enum", imageEditQualities},
		{ToolImageEdit, "size.enum", imageSizes},
		{ToolImageVariation, "size.enum", imageSizes},
		{ToolCreateAvatar, "quality.enum", imageEditQualities},
		{ToolCreateAvatar, "angles.items.enum", slices.Sorted(maps.Keys(avatars.Angles))},
		{ToolSchedule, "operation.enum", []string{scheduleOpCreate, scheduleOpList, scheduleOpGet, scheduleOpUpdate, scheduleOpDelete}},
		{ToolMemory, "operation.enum", []string{OperationRead, OperationWrite, OperationDelete}},
		{ToolMemory, "type.enum", []string{MemoryTypeUser, MemoryTypeFeedback, MemoryTypeProject, MemoryTypeReference}},
		{ToolAskUserQuestion, "questions.minItems", minQuestions},
		{ToolAskUserQuestion, "questions.maxItems", maxQuestions},
		{ToolAskUserQuestion, "questions.items.properties.header.maxLength", maxQuestionHeader},
		{ToolAskUserQuestion, "questions.items.properties.options.minItems", minOptions},
		{ToolAskUserQuestion, "questions.items.properties.options.maxItems", maxOptions},
		{ToolSendSubagentInput, "keys.description", "Interactive subagents only. Named keys to send after the text. Allowed: " + allowedSubagentKeyList},
	}
	for _, tt := range tests {
		t.Run(tt.tool+"."+tt.path, func(t *testing.T) {
			manifest, ok := toolManifests[tt.tool]
			require.True(t, ok, "no manifest for %s", tt.tool)
			assert.Equal(t, tt.want, schemaValue(t, manifest.Parameters["properties"], tt.path))
		})
	}
}

func schemaValue(t *testing.T, node any, path string) any {
	t.Helper()
	for _, key := range strings.Split(path, ".") {
		fields, ok := node.(map[string]any)
		require.True(t, ok, "%s: %q is not an object", path, key)
		node = fields[key]
	}
	return node
}

// TestToolApprovalSettings pins the approval precedence each tool applies: its
// own require_approval setting, then its manifest default, then the global
// tools.safety.require_approval.
func TestToolApprovalSettings(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name   string
		global bool
		setup  func(*config.Config)
		tool   func(*config.Config) agentdomain.ManifestTool
		want   bool
	}{
		{name: "WebSearch inherits global on", global: true, tool: webSearch, want: true},
		{name: "WebSearch inherits global off", global: false, tool: webSearch, want: false},
		{name: "WebSearch setting false beats global on", global: true, setup: func(c *config.Config) { c.Tools.WebSearch.RequireApproval = &no }, tool: webSearch, want: false},
		{name: "WebSearch setting true beats global off", global: false, setup: func(c *config.Config) { c.Tools.WebSearch.RequireApproval = &yes }, tool: webSearch, want: true},
		{name: "TodoWrite needs no approval by default even with global on", global: true, tool: todoWrite, want: false},
		{name: "TodoWrite setting true beats its default", global: false, setup: func(c *config.Config) { c.Tools.TodoWrite.RequireApproval = &yes }, tool: todoWrite, want: true},
		{name: "CreateAvatar needs approval by default even with global off", global: false, tool: createAvatar, want: true},
		{name: "CreateAvatar follows text_to_video.require_approval", global: true, setup: func(c *config.Config) { c.TextToVideo.RequireApproval = &no }, tool: createAvatar, want: false},
		{name: "TextToSpeech needs no approval by default", global: true, tool: textToSpeech, want: false},
		{name: "TextToSpeech opt in via config", global: false, setup: func(c *config.Config) { c.TextToSpeech.RequireApproval = &yes }, tool: textToSpeech, want: true},
		{name: "ApproveSubagent always needs approval", global: false, tool: approveSubagent, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Tools.WebSearch.RequireApproval = nil
			if tt.setup != nil {
				tt.setup(cfg)
			}
			if got := tt.tool(cfg).Manifest().RequiresApproval(tt.global); got != tt.want {
				t.Errorf("RequiresApproval(global=%v) = %v, want %v", tt.global, got, tt.want)
			}
		})
	}
}

func webSearch(cfg *config.Config) agentdomain.ManifestTool { return NewWebSearchTool(cfg) }
func todoWrite(cfg *config.Config) agentdomain.ManifestTool { return NewTodoWriteTool(cfg) }
func createAvatar(cfg *config.Config) agentdomain.ManifestTool {
	return NewCreateAvatarTool(cfg, nil)
}
func textToSpeech(cfg *config.Config) agentdomain.ManifestTool {
	return NewTextToSpeechTool(cfg, nil)
}
func approveSubagent(cfg *config.Config) agentdomain.ManifestTool {
	return NewApproveSubagentTool(cfg, nil)
}
