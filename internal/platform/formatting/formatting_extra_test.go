package formatting

import (
	"testing"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestGetResponsiveWidth(t *testing.T) {
	tests := []struct {
		name          string
		terminalWidth int
		want          int
	}{
		{"zero width floors at min", 0, 40},
		{"negative width floors at min", -10, 40},
		{"tiny terminal floors at min", 20, 40},
		{"exactly at floor boundary", 46, 40},
		{"just above floor", 47, 41},
		{"typical terminal", 80, 74},
		{"wide terminal has no cap", 300, 294},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetResponsiveWidth(tt.terminalWidth); got != tt.want {
				t.Errorf("GetResponsiveWidth(%d) = %d, want %d", tt.terminalWidth, got, tt.want)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  string
	}{
		{"zero width returns unchanged", "hello world", 0, "hello world"},
		{"negative width returns unchanged", "hello world", -5, "hello world"},
		{"fits within width", "hello", 10, "hello"},
		{"wraps at word boundary", "hello world", 5, "hello\nworld"},
		{"empty string", "", 10, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WrapText(tt.text, tt.width); got != tt.want {
				t.Errorf("WrapText(%q, %d) = %q, want %q", tt.text, tt.width, got, tt.want)
			}
		})
	}
}

func TestFormatToolCall(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		want     string
	}{
		{"nil args", "Bash", nil, "Bash()"},
		{"empty args", "Bash", map[string]any{}, "Bash()"},
		{"single arg", "Read", map[string]any{"file_path": "/tmp/x"}, "Read(file_path=/tmp/x)"},
		{
			"multiple args sorted by key",
			"Edit",
			map[string]any{"new": "b", "file": "/f", "old": "a"},
			"Edit(file=/f, new=b, old=a)",
		},
		{"non-string values", "Click", map[string]any{"x": 10, "y": 2.5}, "Click(x=10, y=2.5)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatToolCall(tt.toolName, tt.args); got != tt.want {
				t.Errorf("FormatToolCall(%q, %v) = %q, want %q", tt.toolName, tt.args, got, tt.want)
			}
		})
	}
}

func TestFormatMessageANSIWrapping(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) string
		want string
	}{
		{"success is green", FormatSuccess, "\033[32mok\033[0m"},
		{"warning is yellow", FormatWarning, "\033[33mok\033[0m"},
		{"error is red", FormatErrorCLI, "\033[31mok\033[0m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn("ok"); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func textContent(t *testing.T, s string) sdk.MessageContent {
	t.Helper()
	var c sdk.MessageContent
	if err := c.FromMessageContent0(s); err != nil {
		t.Fatal(err)
	}
	return c
}

func multimodalContent(t *testing.T, parts ...sdk.ContentPart) sdk.MessageContent {
	t.Helper()
	if parts == nil {
		parts = []sdk.ContentPart{} // nil marshals to JSON null, not []
	}
	var c sdk.MessageContent
	if err := c.FromMessageContent1(parts); err != nil {
		t.Fatal(err)
	}
	return c
}

func textPart(t *testing.T, s string) sdk.ContentPart {
	t.Helper()
	var p sdk.ContentPart
	if err := p.FromTextContentPart(sdk.TextContentPart{Type: "text", Text: s}); err != nil {
		t.Fatal(err)
	}
	return p
}

func imagePart(t *testing.T) sdk.ContentPart {
	t.Helper()
	var p sdk.ContentPart
	if err := p.FromImageContentPart(sdk.ImageContentPart{
		Type:     "image_url",
		ImageURL: sdk.ImageURL{URL: "data:image/png;base64,x"},
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractTextFromContent(t *testing.T) {
	tests := []struct {
		name    string
		content sdk.MessageContent
		images  []agentdomain.ImageAttachment
		want    string
	}{
		{
			name:    "simple text content",
			content: textContent(t, "hello world"),
			want:    "hello world",
		},
		{
			name:    "empty text content",
			content: textContent(t, ""),
			want:    "",
		},
		{
			name:    "multimodal text parts joined",
			content: multimodalContent(t, textPart(t, "one"), textPart(t, "two")),
			want:    "one two",
		},
		{
			// Documents current behavior: image parts also unmarshal cleanly
			// into TextContentPart (Text ""), so they join as empty strings
			// instead of "[Image N]" labels.
			name:    "multimodal text plus image part",
			content: multimodalContent(t, textPart(t, "hello"), imagePart(t)),
			want:    "hello ",
		},
		{
			name:    "invalid content without images",
			content: sdk.MessageContent{},
			want:    "[error extracting content]",
		},
		{
			name:    "invalid content with image attachments",
			content: sdk.MessageContent{},
			images: []agentdomain.ImageAttachment{
				{Data: "a", MimeType: "image/png"},
				{Data: "b", MimeType: "image/png"},
			},
			want: "[Image 1] [Image 2]",
		},
		{
			name:    "empty multimodal with image attachments",
			content: multimodalContent(t),
			images:  []agentdomain.ImageAttachment{{Data: "a", MimeType: "image/png"}},
			want:    "[Image 1]",
		},
		{
			name:    "empty multimodal without images",
			content: multimodalContent(t),
			want:    "[empty message]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractTextFromContent(tt.content, tt.images); got != tt.want {
				t.Errorf("ExtractTextFromContent() = %q, want %q", got, tt.want)
			}
		})
	}
}
