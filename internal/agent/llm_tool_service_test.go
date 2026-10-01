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
	models "github.com/inference-gateway/cli/internal/platform/models"
	tools "github.com/inference-gateway/cli/internal/tools"
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
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
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

func TestListToolsForMode_AskUserQuestionModes(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc := NewLLMToolServiceWithRegistry(cfg, registry)

	for _, mode := range []agentdomain.AgentMode{
		agentdomain.AgentModePlan,
		agentdomain.AgentModeStandard,
		agentdomain.AgentModeAutoAccept,
		agentdomain.AgentModeAutoWithJudge,
	} {
		if !slices.Contains(toolNamesForMode(svc, mode), "AskUserQuestion") {
			t.Errorf("expected AskUserQuestion to be advertised in %s mode", mode)
		}
	}
	if slices.Contains(toolNamesForMode(svc, agentdomain.AgentModeReadOnly), "AskUserQuestion") {
		t.Error("expected AskUserQuestion to be excluded from read-only mode")
	}
}

// TestListToolsForMode_PlanOffersAgent: plan mode's read-only exploration
// subagents are the research path for a large area, so the Agent tool must be
// advertised there - and only there, not in read-only subagent mode.
func TestListToolsForMode_PlanOffersAgent(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := goldenRegistry(cfg)
	svc := NewLLMToolServiceWithRegistry(cfg, registry)

	if !slices.Contains(toolNamesForMode(svc, agentdomain.AgentModePlan), tools.ToolAgent) {
		t.Error("plan mode must advertise the Agent tool")
	}
	if slices.Contains(toolNamesForMode(svc, agentdomain.AgentModeReadOnly), tools.ToolAgent) {
		t.Error("read-only mode must not advertise the Agent tool (subagents cannot spawn subagents)")
	}
}

// TestExecuteTool_ModeGuard verifies execution-time mode enforcement: a mode
// rejects the tools it does not make available, and a context without a mode
// fails open.
func TestExecuteTool_ModeGuard(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc := NewLLMToolServiceWithRegistry(cfg, registry)

	tests := []struct {
		name    string
		mode    agentdomain.AgentMode
		hasMode bool
		tool    string
		wantErr string
	}{
		{"plan rejects Write", agentdomain.AgentModePlan, true, tools.ToolWrite, "disabled in plan mode"},
		{"plan rejects Bash", agentdomain.AgentModePlan, true, tools.ToolBash, "disabled in plan mode"},
		{"standard rejects RequestPlanApproval", agentdomain.AgentModeStandard, true, tools.ToolRequestPlanApproval, "not available in standard mode"},
		{"readonly rejects Write", agentdomain.AgentModeReadOnly, true, tools.ToolWrite, "not available in readonly mode"},
		{"readonly allows Read", agentdomain.AgentModeReadOnly, true, tools.ToolRead, ""},
		{"standard allows AskUserQuestion", agentdomain.AgentModeStandard, true, tools.ToolAskUserQuestion, ""},
		{"auto allows AskUserQuestion", agentdomain.AgentModeAutoAccept, true, tools.ToolAskUserQuestion, ""},
		{"no mode fails open", agentdomain.AgentModeStandard, false, tools.ToolWrite, ""},
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

// TestListToolsOffersImageDecodeToEveryModel: vision models need it to look
// at files on disk (it attaches the image), text models get a description.
func TestListToolsOffersImageDecodeToEveryModel(t *testing.T) {
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
	registry := tools.NewRegistry(cfg, &agentdomainmocks.FakeImageService{}, nil, nil, nil, nil, nil, &agentdomainmocks.FakeImageAnnotator{}, nil, nil)
	svc := NewLLMToolServiceWithRegistry(cfg, registry)

	names := func() []string {
		defs := svc.ListTools()
		out := make([]string, 0, len(defs))
		for _, d := range defs {
			out = append(out, d.Function.Name)
		}
		return out
	}

	if !slices.Contains(names(), "ImageDecode") {
		t.Error("ImageDecode must be advertised regardless of the model's vision support")
	}
	if !svc.IsToolEnabled("ImageDecode") {
		t.Error("ImageDecode must be executable")
	}
}
