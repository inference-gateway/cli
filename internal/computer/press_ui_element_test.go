package computer

import (
	"strings"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestPressUIElementValidate(t *testing.T) {
	tool := NewPressUIElementTool(axTestConfig(t))
	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"label only", map[string]any{"label": "Finder"}, false},
		{"label and target", map[string]any{"label": "Finder", "target": "dock"}, false},
		{"missing label", map[string]any{"target": "dock"}, true},
		{"empty label", map[string]any{"label": ""}, true},
		{"invalid target", map[string]any{"label": "Finder", "target": "desktop"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%v) err = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestPressUIElementFormat(t *testing.T) {
	tool := NewPressUIElementTool(axTestConfig(t))
	result := &domain.ToolExecutionResult{
		ToolName: "PressUIElement",
		Success:  true,
		Data: map[string]any{
			"label":   "Finder",
			"target":  "dock",
			"message": `Pressed UI element "Finder" in dock`,
		},
	}
	if got := tool.FormatForLLM(result); !strings.Contains(got, `Pressed UI element "Finder"`) {
		t.Errorf("FormatForLLM() = %q", got)
	}
	if got := tool.FormatPreview(result); got != `Pressed "Finder"` {
		t.Errorf("FormatPreview() = %q", got)
	}
}
