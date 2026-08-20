package domain

import (
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
)

func TestCreateTitleFromMessage(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "New Conversation"},
		{name: "whitespace only", content: "   \n\t  ", want: "New Conversation"},
		{name: "short message kept as-is", content: "fix the bug", want: "fix the bug"},
		{name: "surrounding whitespace trimmed", content: "  hello world  ", want: "hello world"},
		{
			name:    "exactly ten words kept",
			content: "one two three four five six seven eight nine ten",
			want:    "one two three four five six seven eight nine ten",
		},
		{
			name:    "truncated to ten words",
			content: "one two three four five six seven eight nine ten eleven twelve",
			want:    "one two three four five six seven eight nine ten",
		},
		{
			name:    "internal whitespace collapsed to single spaces",
			content: "hello\n\nworld\ttabs",
			want:    "hello world tabs",
		},
		{
			name:    "long title truncated to 80 bytes with ellipsis",
			content: strings.Repeat("abcdefghi ", 10), // 10 words x 9 chars = 99 chars joined
			want:    strings.Repeat("abcdefghi ", 7) + "abcdefg" + "...",
		},
		{
			name:    "multibyte within limit stays intact",
			content: "こんにちは 世界",
			want:    "こんにちは 世界",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CreateTitleFromMessage(tt.content))
		})
	}
}

// Current behavior: truncation slices bytes, so a multibyte word longer than 80
// bytes is cut at byte 77 regardless of rune boundaries. The result is always
// 80 bytes ending in "...".
func TestCreateTitleFromMessage_MultibyteTruncation(t *testing.T) {
	title := CreateTitleFromMessage(strings.Repeat("日", 30)) // one 90-byte word
	assert.Len(t, title, 80)
	assert.True(t, strings.HasSuffix(title, "..."))
}
