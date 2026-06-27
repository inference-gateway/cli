package tools

import (
	"encoding/json"
	"strings"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// freshListShellsData mirrors the Data map produced by ListShellsTool.Execute for a
// non-empty shell list (shell_count is an int, output_size an int64, exit_code a *int,
// shells a []map[string]any). The unused completed_at field is omitted. The reloaded
// variant in the round-trip test runs this through JSON, turning the numbers into
// float64 and the slice into []any — the shape that used to panic FormatResult.
func freshListShellsData() map[string]any {
	exitZero := 0
	return map[string]any{
		"shell_count": 2,
		"shells": []map[string]any{
			{
				"shell_id":    "shell-1",
				"command":     "sleep 100",
				"state":       domain.ShellStateRunning.String(),
				"started_at":  "12:00:00",
				"elapsed":     "5s",
				"output_size": int64(1234),
				"exit_code":   (*int)(nil), // running shell: no exit code
			},
			{
				"shell_id":    "shell-2",
				"command":     "echo done",
				"state":       domain.ShellStateCompleted.String(),
				"started_at":  "12:01:00",
				"elapsed":     "1s",
				"output_size": int64(56),
				"exit_code":   &exitZero,
			},
		},
	}
}

func jsonRoundTrip(t *testing.T, in *domain.ToolExecutionResult) *domain.ToolExecutionResult {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out domain.ToolExecutionResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &out
}

// TestListShellsTool_FormatAfterJSONRoundTrip is the regression for the panic
// "interface conversion: interface {} is float64, not int" that crashed the TUI when a
// saved conversation containing a ListShells result was reloaded. Formatting must work
// on both the fresh result and the JSON-reloaded one.
func TestListShellsTool_FormatAfterJSONRoundTrip(t *testing.T) {
	fresh := &domain.ToolExecutionResult{ToolName: "ListShells", Success: true, Data: freshListShellsData()}

	cases := map[string]*domain.ToolExecutionResult{
		"fresh":    fresh,
		"reloaded": jsonRoundTrip(t, fresh),
	}
	tool := &ListShellsTool{}

	for name, result := range cases {
		t.Run(name, func(t *testing.T) {
			// A panic here (the bug) fails the test loudly with the offending stack.
			if got := tool.FormatPreview(result); got != "Found 2 background shell(s)" {
				t.Errorf("FormatPreview = %q, want %q", got, "Found 2 background shell(s)")
			}

			ui := tool.FormatResult(result, domain.FormatterUI)
			for _, want := range []string{"Background Shells (2):", "1234 bytes", "56 bytes"} {
				if !strings.Contains(ui, want) {
					t.Errorf("FormatterUI output missing %q:\n%s", want, ui)
				}
			}

			llm := tool.FormatResult(result, domain.FormatterLLM)
			for _, want := range []string{"Found 2 background shell(s):", "Output Size: 1234 bytes", "Exit Code: 0"} {
				if !strings.Contains(llm, want) {
					t.Errorf("FormatterLLM output missing %q:\n%s", want, llm)
				}
			}
			// Only the completed shell has an exit code; the running one must not print one.
			if n := strings.Count(llm, "Exit Code:"); n != 1 {
				t.Errorf("FormatterLLM rendered %d exit codes, want 1:\n%s", n, llm)
			}
		})
	}
}

func TestListShellsTool_FormatEmptyAfterJSONRoundTrip(t *testing.T) {
	fresh := &domain.ToolExecutionResult{
		ToolName: "ListShells",
		Success:  true,
		Data: map[string]any{
			"shell_count": 0,
			"message":     "No background shells are currently running or tracked.",
		},
	}
	tool := &ListShellsTool{}

	for name, result := range map[string]*domain.ToolExecutionResult{"fresh": fresh, "reloaded": jsonRoundTrip(t, fresh)} {
		t.Run(name, func(t *testing.T) {
			if got := tool.FormatPreview(result); got != "No background shells running" {
				t.Errorf("FormatPreview = %q, want %q", got, "No background shells running")
			}
			if got := tool.FormatResult(result, domain.FormatterUI); got != "No background shells running" {
				t.Errorf("FormatterUI = %q, want %q", got, "No background shells running")
			}
			if got := tool.FormatResult(result, domain.FormatterLLM); got != "No background shells are currently running or tracked." {
				t.Errorf("FormatterLLM = %q", got)
			}
		})
	}
}

func TestToInt(t *testing.T) {
	five := 5
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"int", 7, 7},
		{"int64", int64(8), 8},
		{"int32", int32(9), 9},
		{"float64", float64(10), 10},
		{"float64-truncates", 10.9, 10},
		{"float32", float32(11), 11},
		{"ptr-int", &five, 5},
		{"nil-ptr-int", (*int)(nil), 0},
		{"nil", nil, 0},
		{"non-numeric", "nope", 0},
	}
	for _, tc := range cases {
		if got := toInt(tc.in); got != tc.want {
			t.Errorf("%s: toInt(%v [%T]) = %d, want %d", tc.name, tc.in, tc.in, got, tc.want)
		}
	}
}

func TestAsMapSlice(t *testing.T) {
	if got := asMapSlice([]map[string]any{{"a": 1}, {"a": 2}}); len(got) != 2 {
		t.Errorf("fresh []map[string]any: len = %d, want 2", len(got))
	}
	// JSON round-trip shape: []any whose elements are map[string]any.
	if got := asMapSlice([]any{map[string]any{"a": 1.0}, map[string]any{"a": 2.0}}); len(got) != 2 {
		t.Errorf("[]any of maps: len = %d, want 2", len(got))
	}
	// Non-map elements are skipped.
	if got := asMapSlice([]any{map[string]any{"a": 1}, "not-a-map", 42}); len(got) != 1 {
		t.Errorf("mixed slice: len = %d, want 1", len(got))
	}
	if got := asMapSlice("nope"); got != nil {
		t.Errorf("non-slice: got %v, want nil", got)
	}
	if got := asMapSlice(nil); got != nil {
		t.Errorf("nil: got %v, want nil", got)
	}
}
