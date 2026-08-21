package infrastructure

import (
	"math"
	"strings"
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestFormatDuration(t *testing.T) {
	f := NewBaseFormatter("Test")
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0ns"},
		{"max ns", 999 * time.Nanosecond, "999ns"},
		{"min us", time.Microsecond, "1µs"},
		{"max us", 999999 * time.Nanosecond, "999µs"},
		{"min ms", time.Millisecond, "1ms"},
		{"max ms", 999 * time.Millisecond, "999ms"},
		{"sub-second rounding", 1500 * time.Microsecond, "1ms"},
		{"min s", time.Second, "1s"},
		{"max s", 59*time.Second + 999*time.Millisecond, "59s"},
		{"min m", time.Minute, "1m"},
		{"m with s", 90 * time.Second, "1m30s"},
		{"max m", 59*time.Minute + 59*time.Second, "59m59s"},
		{"min h", time.Hour, "1h"},
		{"h with m", time.Hour + time.Minute, "1h1m"},
		{"h exact", 2 * time.Hour, "2h"},
		{"h drops seconds", 2*time.Hour + 59*time.Second, "2h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := f.FormatDuration(&agentdomain.ToolExecutionResult{Duration: tt.d})
			if got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestTruncateText(t *testing.T) {
	f := NewBaseFormatter("Test")
	tests := []struct {
		name      string
		text      string
		maxLength int
		want      string
	}{
		{"shorter than max", "abc", 10, "abc"},
		{"exact length", "abcde", 5, "abcde"},
		{"one over", "abcdef", 5, "ab..."},
		{"max 3 and long", "abcdef", 3, "..."},
		{"max 0 and long", "abcdef", 0, "..."},
		{"empty text", "", 0, ""},
		// Byte-based slicing splits the 2-byte é rune mid-sequence: current
		// behavior emits invalid UTF-8, pinned here as a regression guard.
		{"multibyte split", "héllo world", 5, "h\xc3..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.TruncateText(tt.text, tt.maxLength); got != tt.want {
				t.Errorf("TruncateText(%q, %d) = %q, want %q", tt.text, tt.maxLength, got, tt.want)
			}
		})
	}
}

func TestCollapseArgValue(t *testing.T) {
	f := NewBaseFormatter("Test")
	tests := []struct {
		name      string
		value     any
		maxLength int
		want      string
	}{
		{"short string", "abc", 10, "abc"},
		{"exact length", "abcde", 5, "abcde"},
		{"truncated", "abcdefgh", 6, "abc..."},
		{"max 3", "abcdefgh", 3, "..."},
		{"non-string value", 12345, 4, "1..."},
		// Same rune-splitting behavior as TruncateText.
		{"multibyte split", "héllo world", 5, "h\xc3..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.collapseArgValue(tt.value, tt.maxLength); got != tt.want {
				t.Errorf("collapseArgValue(%v, %d) = %q, want %q", tt.value, tt.maxLength, got, tt.want)
			}
		})
	}
}

func TestGetDomainFromURL(t *testing.T) {
	f := NewBaseFormatter("Test")
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"https with path", "https://example.com/a/b", "example.com"},
		{"http", "http://example.com", "example.com"},
		{"no scheme", "example.com/path", "example.com"},
		{"with port", "https://example.com:8080/x", "example.com:8080"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.GetDomainFromURL(tt.url); got != tt.want {
				t.Errorf("GetDomainFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestGetFileName(t *testing.T) {
	f := NewBaseFormatter("Test")
	tests := []struct {
		name string
		path string
		want string
	}{
		{"nested path", "a/b/c.txt", "c.txt"},
		{"bare filename", "file.txt", "file.txt"},
		{"trailing slash", "a/b/", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.GetFileName(tt.path); got != tt.want {
				t.Errorf("GetFileName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestJoinArgs(t *testing.T) {
	f := NewBaseFormatter("Test")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"empty", nil, ""},
		{"one", []string{"a=1"}, "a=1"},
		{"many", []string{"a=1", "b=2", "c=3"}, "a=1, b=2, c=3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.joinArgs(tt.args); got != tt.want {
				t.Errorf("joinArgs(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestFormatToolCall(t *testing.T) {
	longVal := strings.Repeat("x", 60)
	base := NewBaseFormatter("Read")
	custom := NewCustomFormatter("Write", func(key string) bool { return key == "content" })
	nilFunc := NewCustomFormatter("Edit", nil)

	tests := []struct {
		name string
		call func() string
		want string
	}{
		{"base no args", func() string { return base.FormatToolCall(nil, false) }, "Read()"},
		{"base sorted keys", func() string {
			return base.FormatToolCall(map[string]any{"b": 2, "a": 1}, false)
		}, "Read(a=1, b=2)"},
		// BaseFormatter never collapses (ShouldCollapseArg is always false).
		{"base long value kept", func() string {
			return base.FormatToolCall(map[string]any{"path": longVal}, false)
		}, "Read(path=" + longVal + ")"},
		{"custom no args", func() string { return custom.FormatToolCall(map[string]any{}, false) }, "Write()"},
		{"custom collapses matched key", func() string {
			return custom.FormatToolCall(map[string]any{"content": longVal, "path": "f.go"}, false)
		}, "Write(content=..., path=f.go)"},
		{"custom expanded keeps value", func() string {
			return custom.FormatToolCall(map[string]any{"content": "abc"}, true)
		}, "Write(content=abc)"},
		{"nil collapse func falls back", func() string {
			return nilFunc.FormatToolCall(map[string]any{"content": "abc"}, false)
		}, "Edit(content=abc)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.call(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatStatusIcon(t *testing.T) {
	f := NewBaseFormatter("Test")
	if got := f.FormatStatusIcon(true); got != "✓" {
		t.Errorf("FormatStatusIcon(true) = %q, want ✓", got)
	}
	if got := f.FormatStatusIcon(false); got != "✗" {
		t.Errorf("FormatStatusIcon(false) = %q, want ✗", got)
	}
}

func TestFormatAsJSON(t *testing.T) {
	f := NewBaseFormatter("Test")
	if got := f.FormatAsJSON(map[string]any{"a": 1}); got != "{\n  \"a\": 1\n}" {
		t.Errorf("FormatAsJSON(map) = %q", got)
	}
	// NaN is not JSON-marshalable, so it falls back to %+v.
	if got := f.FormatAsJSON(math.NaN()); got != "NaN" {
		t.Errorf("FormatAsJSON(NaN) = %q, want NaN", got)
	}
}

func TestFormatExpandedFailureAndCustomCollapse(t *testing.T) {
	f := NewCustomFormatter("Write", func(key string) bool { return key == "content" })
	result := &agentdomain.ToolExecutionResult{
		ToolName:  "Write",
		Success:   false,
		Duration:  2 * time.Second,
		Error:     "disk full",
		Arguments: map[string]any{"content": strings.Repeat("x", 60), "path": "f.go"},
	}

	out := f.FormatExpanded(result, "")

	for _, want := range []string{
		"Write(content=..., path=f.go)",
		"Duration: 2s",
		"✗ Failed",
		"Error: disk full",
		"content: ...",
		"path: f.go",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expanded output missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Result:") || strings.Contains(out, "Metadata:") {
		t.Errorf("empty sections must be omitted:\n%s", out)
	}
}
