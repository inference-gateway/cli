package browser

import (
	"errors"
	"reflect"
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
)

type stubRateLimiter struct{ err error }

func (s stubRateLimiter) CheckAndRecord(string) error { return s.err }

func TestCheckRateLimit(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{"allowed", nil, false},
		{"limited", errors.New("rate limit exceeded"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &browserToolBase{name: "BrowserRead", rateLimiter: stubRateLimiter{err: tt.err}}
			if err := b.checkRateLimit(); (err != nil) != tt.wantErr {
				t.Fatalf("checkRateLimit() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestErrorAndSuccessResult(t *testing.T) {
	b := &browserToolBase{name: "BrowserClick"}
	args := map[string]any{"selector": "button"}
	start := time.Now().Add(-time.Millisecond)

	er := b.errorResult(args, start, "boom")
	if er.ToolName != "BrowserClick" || er.Success || er.Error != "boom" || er.Data != nil {
		t.Errorf("errorResult = %+v", er)
	}
	if er.Duration <= 0 {
		t.Errorf("errorResult duration = %v, want > 0", er.Duration)
	}

	data := browserdomain.BrowserToolResult{Action: "click", Selector: "button"}
	sr := b.successResult(args, start, data)
	if sr.ToolName != "BrowserClick" || !sr.Success || sr.Error != "" {
		t.Errorf("successResult = %+v", sr)
	}
	if got, ok := sr.Data.(browserdomain.BrowserToolResult); !ok || !reflect.DeepEqual(got, data) {
		t.Errorf("successResult data = %+v, want %+v", sr.Data, data)
	}
}

func TestFormatPreview(t *testing.T) {
	b := &browserToolBase{name: "BrowserClick"}
	tests := []struct {
		name   string
		result *agentdomain.ToolExecutionResult
		want   string
	}{
		{"nil result", nil, "BrowserClick failed"},
		{"failed result", &agentdomain.ToolExecutionResult{Success: false}, "BrowserClick failed"},
		{
			"success with unexpected data type",
			&agentdomain.ToolExecutionResult{Success: true, Data: "raw"},
			"BrowserClick succeeded",
		},
		{
			"selector preferred as target",
			&agentdomain.ToolExecutionResult{Success: true, Data: browserdomain.BrowserToolResult{
				Action: "click", Selector: "button.submit", URL: "https://example.com",
			}},
			"click button.submit",
		},
		{
			"url fallback when no selector",
			&agentdomain.ToolExecutionResult{Success: true, Data: browserdomain.BrowserToolResult{
				Action: "navigate", URL: "https://example.com",
			}},
			"navigate https://example.com",
		},
		{
			"action alone trimmed",
			&agentdomain.ToolExecutionResult{Success: true, Data: browserdomain.BrowserToolResult{Action: "read"}},
			"read",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.FormatPreview(tt.result); got != tt.want {
				t.Errorf("FormatPreview() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatResultDispatch(t *testing.T) {
	b := &browserToolBase{name: "BrowserRead"}
	result := &agentdomain.ToolExecutionResult{
		Success: true,
		Data:    browserdomain.BrowserToolResult{Action: "read", URL: "https://example.com"},
	}
	if got := b.FormatResult(result, agentdomain.FormatterShort); got != "read https://example.com" {
		t.Errorf("FormatterShort = %q", got)
	}
	if got := b.FormatResult(result, agentdomain.FormatterLLM); got != "Performed read - page: https://example.com" {
		t.Errorf("FormatterLLM = %q", got)
	}
}

func TestBaseDisplayFlags(t *testing.T) {
	b := &browserToolBase{}
	if b.ShouldCollapseArg("selector") {
		t.Error("ShouldCollapseArg should be false")
	}
	if b.ShouldAlwaysExpand() {
		t.Error("ShouldAlwaysExpand should be false")
	}
}

func TestRequireString(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantVal string
		wantErr bool
	}{
		{"valid string", map[string]any{"selector": "button"}, "button", false},
		{"missing key", map[string]any{}, "", true},
		{"nil args", nil, "", true},
		{"wrong type int", map[string]any{"selector": 42}, "", true},
		{"nil value", map[string]any{"selector": nil}, "", true},
		{"empty string", map[string]any{"selector": ""}, "", true},
		{"whitespace only", map[string]any{"selector": "   "}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := requireString(tt.args, "selector")
			if (err != nil) != tt.wantErr {
				t.Fatalf("requireString err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.wantVal {
				t.Errorf("requireString = %q, want %q", got, tt.wantVal)
			}
		})
	}
}

func TestNumberArg(t *testing.T) {
	tests := []struct {
		name   string
		args   map[string]any
		want   float64
		wantOk bool
	}{
		{"float64", map[string]any{"x": 12.5}, 12.5, true},
		{"float32", map[string]any{"x": float32(2)}, 2, true},
		{"int", map[string]any{"x": 7}, 7, true},
		{"int64", map[string]any{"x": int64(9)}, 9, true},
		{"negative float", map[string]any{"x": -3.0}, -3, true},
		{"zero value present", map[string]any{"x": 0}, 0, true},
		{"string number rejected", map[string]any{"x": "10"}, 0, false},
		{"bool rejected", map[string]any{"x": true}, 0, false},
		{"nil value", map[string]any{"x": nil}, 0, false},
		{"missing key", map[string]any{}, 0, false},
		{"nil args", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := numberArg(tt.args, "x")
			if got != tt.want || ok != tt.wantOk {
				t.Errorf("numberArg = (%v, %v), want (%v, %v)", got, ok, tt.want, tt.wantOk)
			}
		})
	}
}

func TestClickCoords(t *testing.T) {
	tests := []struct {
		name   string
		args   map[string]any
		wantX  float64
		wantY  float64
		wantOk bool
	}{
		{"both floats", map[string]any{"x": 10.0, "y": 20.0}, 10, 20, true},
		{"mixed int and float", map[string]any{"x": 10, "y": 20.5}, 10, 20.5, true},
		{"only x", map[string]any{"x": 10.0}, 10, 0, false},
		{"only y", map[string]any{"y": 20.0}, 0, 20, false},
		{"neither", map[string]any{}, 0, 0, false},
		{"wrong type for y", map[string]any{"x": 10.0, "y": "20"}, 10, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, ok := clickCoords(tt.args)
			if x != tt.wantX || y != tt.wantY || ok != tt.wantOk {
				t.Errorf("clickCoords = (%v, %v, %v), want (%v, %v, %v)", x, y, ok, tt.wantX, tt.wantY, tt.wantOk)
			}
		})
	}
}

func TestFormatForLLMNilResult(t *testing.T) {
	b := &browserToolBase{name: "BrowserClick"}
	if got := b.FormatForLLM(nil); got != "Error: no result" {
		t.Fatalf("FormatForLLM(nil) = %q, want %q", got, "Error: no result")
	}
}
