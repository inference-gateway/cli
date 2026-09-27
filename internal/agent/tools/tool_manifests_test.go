package tools

import (
	"maps"
	"slices"
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

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
		{"ImageGeneration", "quality.enum", imageQualities},
		{"ImageGeneration", "size.enum", imageSizes},
		{"ImageEdit", "quality.enum", imageEditQualities},
		{"ImageEdit", "size.enum", imageSizes},
		{"ImageVariation", "size.enum", imageSizes},
		{"CreateAvatar", "quality.enum", imageEditQualities},
		{"CreateAvatar", "angles.items.enum", slices.Sorted(maps.Keys(avatars.Angles))},
		{"Schedule", "operation.enum", []string{scheduleOpCreate, scheduleOpList, scheduleOpGet, scheduleOpUpdate, scheduleOpDelete}},
		{"Memory", "operation.enum", []string{OperationRead, OperationWrite, OperationDelete}},
		{"Memory", "type.enum", []string{MemoryTypeUser, MemoryTypeFeedback, MemoryTypeProject, MemoryTypeReference}},
		{"AskUserQuestion", "questions.minItems", minQuestions},
		{"AskUserQuestion", "questions.maxItems", maxQuestions},
		{"AskUserQuestion", "questions.items.properties.header.maxLength", maxQuestionHeader},
		{"AskUserQuestion", "questions.items.properties.options.minItems", minOptions},
		{"AskUserQuestion", "questions.items.properties.options.maxItems", maxOptions},
		{"SendSubagentInput", "keys.description", "Named keys to send after the text. Allowed: " + allowedSubagentKeyList},
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
