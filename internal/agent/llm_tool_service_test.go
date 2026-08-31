package agent

import (
	"context"
	"slices"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	models "github.com/inference-gateway/cli/internal/platform/models"
)

func toolNamesForMode(svc *LLMToolService, mode agentdomain.AgentMode) []string {
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
	names := toolNamesForMode(svc, agentdomain.AgentModeReadOnly)

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

	if !slices.Contains(toolNamesForMode(svc, agentdomain.AgentModePlan), "AskUserQuestion") {
		t.Error("expected AskUserQuestion to be available in plan mode")
	}
	if slices.Contains(toolNamesForMode(svc, agentdomain.AgentModeStandard), "AskUserQuestion") {
		t.Error("expected AskUserQuestion to be excluded from standard mode")
	}
	if slices.Contains(toolNamesForMode(svc, agentdomain.AgentModeAutoAccept), "AskUserQuestion") {
		t.Error("expected AskUserQuestion to be excluded from auto-accept mode")
	}
}

// TestExecuteTool_ModeGuard verifies execution-time mode enforcement: plan
// mode rejects tools outside the plan allow-set, other modes reject plan-only
// tools, and a context without a mode fails open.
func TestExecuteTool_ModeGuard(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil)
	svc := NewLLMToolServiceWithRegistry(cfg, registry)

	tests := []struct {
		name    string
		mode    agentdomain.AgentMode
		hasMode bool
		tool    string
		wantErr string
	}{
		{"plan rejects Write", agentdomain.AgentModePlan, true, "Write", "disabled in plan mode"},
		{"plan rejects Bash", agentdomain.AgentModePlan, true, "Bash", "disabled in plan mode"},
		{"standard rejects RequestPlanApproval", agentdomain.AgentModeStandard, true, "RequestPlanApproval", "only available in plan mode"},
		{"auto rejects AskUserQuestion", agentdomain.AgentModeAutoAccept, true, "AskUserQuestion", "only available in plan mode"},
		{"no mode fails open", agentdomain.AgentModeStandard, false, "Write", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.hasMode {
				ctx = agentdomain.WithAgentMode(ctx, tt.mode)
			}
			_, err := svc.ExecuteTool(ctx, sdk.ChatCompletionMessageToolCallFunction{Name: tt.tool, Arguments: "{}"})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ExecuteTool(%s) err = %v, want containing %q", tt.tool, err, tt.wantErr)
				}
				return
			}
			if err != nil && strings.Contains(err.Error(), "tool not allowed") {
				t.Fatalf("ExecuteTool(%s) without mode must not hit the mode guard, got %v", tt.tool, err)
			}
		})
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
	registry := tools.NewRegistry(cfg, &agentdomainmocks.FakeImageService{}, nil, nil, nil, &agentdomainmocks.FakeImageAnnotator{}, nil, nil)
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
	if !svc.IsToolEnabled("ImageDecode") {
		t.Error("ImageDecode must stay executable for vision models (hidden, not disabled)")
	}

	current = "deepseek/deepseek-v4-flash"
	if !slices.Contains(names(), "ImageDecode") {
		t.Error("text-only model must keep ImageDecode")
	}
}
