package formatting

import (
	"slices"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func entry(t *testing.T, role, content string) convdomain.ConversationEntry {
	t.Helper()
	return convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.MessageRole(role), Content: textContent(t, content)},
	}
}

func TestFormatConversationToLines(t *testing.T) {
	assistantWithModel := entry(t, "assistant", "hi there")
	assistantWithModel.Model = "openai/gpt-4o"

	hidden := entry(t, "user", "secret")
	hidden.Hidden = true

	tests := []struct {
		name         string
		conversation []convdomain.ConversationEntry
		want         []string
	}{
		{
			name:         "empty conversation",
			conversation: nil,
			want:         nil,
		},
		{
			name:         "user entry",
			conversation: []convdomain.ConversationEntry{entry(t, "user", "hello")},
			want:         []string{"> You: hello", ""},
		},
		{
			name:         "assistant without model",
			conversation: []convdomain.ConversationEntry{entry(t, "assistant", "hi")},
			want:         []string{"⏺ Assistant: hi", ""},
		},
		{
			name:         "assistant with model",
			conversation: []convdomain.ConversationEntry{assistantWithModel},
			want:         []string{"⏺ openai/gpt-4o: hi there", ""},
		},
		{
			name:         "tool entry",
			conversation: []convdomain.ConversationEntry{entry(t, "tool", "result")},
			want:         []string{"🔧 Tool: result", ""},
		},
		{
			name:         "unknown role passes through",
			conversation: []convdomain.ConversationEntry{entry(t, "system", "prompt")},
			want:         []string{"system: prompt", ""},
		},
		{
			name:         "hidden entries skipped",
			conversation: []convdomain.ConversationEntry{hidden, entry(t, "user", "visible")},
			want:         []string{"> You: visible", ""},
		},
		{
			name: "multimodal content falls back to ExtractTextFromContent",
			conversation: []convdomain.ConversationEntry{{
				Message: sdk.Message{
					Role:    "user",
					Content: multimodalContent(t, textPart(t, "part one"), textPart(t, "part two")),
				},
			}},
			want: []string{"> You: part one part two", ""},
		},
		{
			name:         "multiline content split with trailing spaces trimmed",
			conversation: []convdomain.ConversationEntry{entry(t, "user", "line one  \nline two")},
			want:         []string{"> You: line one", "line two", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewConversationLineFormatter(80, nil)
			got := f.FormatConversationToLines(tt.conversation)
			if !slices.Equal(got, tt.want) {
				t.Errorf("FormatConversationToLines() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConversationLineFormatterSetWidth(t *testing.T) {
	f := NewConversationLineFormatter(80, nil)
	f.SetWidth(120)
	if f.width != 120 {
		t.Errorf("SetWidth: width = %d, want 120", f.width)
	}
}
