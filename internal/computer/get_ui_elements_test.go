package computer

import (
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func axTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.ComputerUse = *config.DefaultComputerUseConfig()
	cfg.ComputerUse.Enabled = true
	return cfg
}

func TestGetUIElementsValidate(t *testing.T) {
	tool := NewGetUIElementsTool(axTestConfig(t))
	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"no target", map[string]any{}, false},
		{"frontmost", map[string]any{"target": "frontmost"}, false},
		{"dock", map[string]any{"target": "dock"}, false},
		{"menubar", map[string]any{"target": "menubar"}, false},
		{"invalid", map[string]any{"target": "taskbar"}, true},
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

func TestGetUIElementsFormatForLLM(t *testing.T) {
	tool := NewGetUIElementsTool(axTestConfig(t))

	annotation := &agentdomain.ImageAnnotation{
		Summary: "2 pressable UI elements of dock, coordinates in the 1024x768 frame space",
		Elements: []agentdomain.AnnotatedElement{
			{Index: 1, Label: "dock item", Text: "Finder", BBox: [4]int{0, 700, 40, 740}},
			{Index: 2, Label: "dock item", Text: "Safari", BBox: [4]int{50, 700, 90, 740}},
		},
	}
	result := &agentdomain.ToolExecutionResult{
		ToolName: "GetUIElements",
		Success:  true,
		Data: map[string]any{
			"target":  "dock",
			"count":   2,
			"message": agentdomain.AnnotationText(annotation) + "\nPress an element by its title with PressUIElement, or click its center coordinate with MouseClick.",
		},
	}

	got := tool.FormatForLLM(result)
	for _, want := range []string{
		"Frame summary: 2 pressable UI elements",
		`1. dock item "Finder" - center (20,720) bbox [0,700,40,740]`,
		"PressUIElement",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatForLLM() missing %q in:\n%s", want, got)
		}
	}

	if got := tool.FormatPreview(result); got != "2 UI elements (dock)" {
		t.Errorf("FormatPreview() = %q", got)
	}
}

func TestGetUIElementsDegradeResult(t *testing.T) {
	tool := NewGetUIElementsTool(axTestConfig(t))
	result := axDegradeResult("GetUIElements", axNoProviderMessage)
	if !result.Success {
		t.Fatal("degrade result must be a success (never a hard failure)")
	}
	if got := tool.FormatForLLM(result); !strings.Contains(got, "GetLatestFrame") {
		t.Errorf("degrade message should steer to GetLatestFrame, got %q", got)
	}
}

func TestGetUIElementsEnabled(t *testing.T) {
	cfg := axTestConfig(t)
	tool := NewGetUIElementsTool(cfg)
	if !tool.IsEnabled() {
		t.Fatal("expected enabled by default")
	}
	cfg.ComputerUse.Tools.GetUIElements.Enabled = false
	if tool.IsEnabled() {
		t.Fatal("expected disabled when tool config is off")
	}
}
