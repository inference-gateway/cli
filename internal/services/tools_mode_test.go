package services

import (
	"slices"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
	mocksdomain "github.com/inference-gateway/cli/tests/mocks/domain"
)

func toolNamesForMode(svc *LLMToolService, mode domain.AgentMode) []string {
	defs := svc.ListToolsForMode(mode)
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	return names
}

func TestListToolsForMode_ReadOnly(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil)
	svc := NewLLMToolServiceWithRegistry(cfg, registry)
	names := toolNamesForMode(svc, domain.AgentModeReadOnly)

	for _, want := range []string{"Read", "Grep", "Tree"} {
		if !slices.Contains(names, want) {
			t.Errorf("ReadOnly mode should include %s; got %v", want, names)
		}
	}
	for _, forbidden := range []string{"Bash", "Write", "Edit", "MultiEdit", "Delete"} {
		if slices.Contains(names, forbidden) {
			t.Errorf("ReadOnly mode must exclude mutating tool %s; got %v", forbidden, names)
		}
	}
}

func TestListToolsForMode_AskUserQuestionPlanOnly(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil)
	svc := NewLLMToolServiceWithRegistry(cfg, registry)

	if !slices.Contains(toolNamesForMode(svc, domain.AgentModePlan), "AskUserQuestion") {
		t.Error("expected AskUserQuestion to be available in plan mode")
	}
	if slices.Contains(toolNamesForMode(svc, domain.AgentModeStandard), "AskUserQuestion") {
		t.Error("expected AskUserQuestion to be excluded from standard mode")
	}
	if slices.Contains(toolNamesForMode(svc, domain.AgentModeAutoAccept), "AskUserQuestion") {
		t.Error("expected AskUserQuestion to be excluded from auto-accept mode")
	}
}

// TestListToolsHidesImageDecodeForVisionModels verifies per-model tool
// filtering: vision-capable models (image in input) see images natively, so
// ImageDecode is not advertised to them; text-only models keep it.
func TestListToolsHidesImageDecodeForVisionModels(t *testing.T) {
	visionMods := sdk.ModelModalities{
		Input:  []sdk.Modality{sdk.ModalityText, sdk.ModalityImage},
		Output: []sdk.Modality{sdk.ModalityText},
	}
	textMods := sdk.ModelModalities{
		Input:  []sdk.Modality{sdk.ModalityText},
		Output: []sdk.Modality{sdk.ModalityText},
	}
	models.SetGatewayModalities(map[string]sdk.ModelModalities{
		"anthropic/claude-haiku-4-5": visionMods,
		"deepseek/deepseek-v4-flash": textMods,
	})
	defer models.SetGatewayModalities(nil)

	cfg := config.DefaultConfig()
	cfg.Vision.Annotator.Enabled = true
	cfg.Vision.Annotator.Model = "openai/qwen3-vl-2b"
	registry := tools.NewRegistry(cfg, &mocksdomain.FakeImageService{}, nil, nil, nil, &mocksdomain.FakeImageAnnotator{}, nil, nil)
	svc := NewLLMToolServiceWithRegistry(cfg, registry)

	current := "anthropic/claude-haiku-4-5"
	svc.SetCurrentModelFn(func() string { return current })

	names := func() []string {
		defs := svc.ListTools()
		out := make([]string, 0, len(defs))
		for _, d := range defs {
			out = append(out, d.Function.Name)
		}
		return out
	}

	if slices.Contains(names(), "ImageDecode") {
		t.Error("vision model must not be offered ImageDecode")
	}

	current = "deepseek/deepseek-v4-flash"
	if !slices.Contains(names(), "ImageDecode") {
		t.Error("text-only model must keep ImageDecode")
	}
}
