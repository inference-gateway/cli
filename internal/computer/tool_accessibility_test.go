package computer

import (
	"strings"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

func TestParseAccessibilityActions(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		kind    computerdomain.ActionKind
		wantErr bool
	}{
		{"observe", map[string]any{"action": "accessibility", "target": "frontmost"}, computerdomain.ActionAccessibility, false},
		{"press", map[string]any{"action": "press", "target": "frontmost", "label": "Save"}, computerdomain.ActionPress, false},
		{"press missing label", map[string]any{"action": "press"}, computerdomain.ActionPress, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := parseAction(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAction() error = %v, wantErr %v", err, tt.wantErr)
			}
			if action.Kind != tt.kind {
				t.Errorf("Kind = %q, want %q", action.Kind, tt.kind)
			}
		})
	}
}

func TestComputerToolFormatsCompactAccessibilityTree(t *testing.T) {
	tool := &ComputerTool{}
	result := &agentdomain.ToolExecutionResult{
		Success: true,
		Data: &computerdomain.Observation{
			Message: "accessibility tree for frontmost",
			Elements: []computerdomain.UIElement{{
				Role: "button", Label: "Save", State: "enabled actions=press", BBox: [4]int{10, 20, 30, 40},
			}},
		},
	}

	formatted := tool.FormatForLLM(result)
	for _, wanted := range []string{`"role":"button"`, `"label":"Save"`, `"state":"enabled actions=press"`, `"bbox":[10,20,30,40]`} {
		if !strings.Contains(formatted, wanted) {
			t.Errorf("FormatForLLM() = %q, want %q", formatted, wanted)
		}
	}
}
