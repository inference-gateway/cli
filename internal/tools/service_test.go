package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	models "github.com/inference-gateway/cli/internal/platform/models"
)

func toolNamesForMode(svc *Service, mode agentdomain.AgentMode) []string {
	defs := svc.ListToolsForMode(mode)
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	return names
}

func TestListToolsForMode_ReadOnly(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc := NewService(cfg, registry)
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
	registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc := NewService(cfg, registry)

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
	registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, &schedmocks.FakeBackgroundTaskRegistry{}, nil)
	svc := NewService(cfg, registry)

	if !slices.Contains(toolNamesForMode(svc, agentdomain.AgentModePlan), ToolAgent) {
		t.Error("plan mode must advertise the Agent tool")
	}
	if slices.Contains(toolNamesForMode(svc, agentdomain.AgentModeReadOnly), ToolAgent) {
		t.Error("read-only mode must not advertise the Agent tool (subagents cannot spawn subagents)")
	}
}

// TestExecuteTool_ModeGuard verifies execution-time mode enforcement: a mode
// rejects the tools it does not make available, and a context without a mode
// fails open.
func TestExecuteTool_ModeGuard(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc := NewService(cfg, registry)

	tests := []struct {
		name    string
		mode    agentdomain.AgentMode
		hasMode bool
		tool    string
		wantErr string
	}{
		{"plan rejects Write", agentdomain.AgentModePlan, true, ToolWrite, "disabled in plan mode"},
		{"plan rejects Bash", agentdomain.AgentModePlan, true, ToolBash, "disabled in plan mode"},
		{"plan error lists the plan tools", agentdomain.AgentModePlan, true, ToolWrite, "use one of " + ToolAskUserQuestion + ", " + ToolGrep + ", " + ToolRead},
		{"plan allows ListSubagents", agentdomain.AgentModePlan, true, ToolListSubagents, ""},
		{"plan allows CloseSubagent", agentdomain.AgentModePlan, true, ToolCloseSubagent, ""},
		{"readonly rejects ListSubagents", agentdomain.AgentModeReadOnly, true, ToolListSubagents, "not available in readonly mode"},
		{"standard rejects RequestPlanApproval", agentdomain.AgentModeStandard, true, ToolRequestPlanApproval, "not available in standard mode"},
		{"readonly rejects Write", agentdomain.AgentModeReadOnly, true, ToolWrite, "not available in readonly mode"},
		{"readonly allows Read", agentdomain.AgentModeReadOnly, true, ToolRead, ""},
		{"standard allows AskUserQuestion", agentdomain.AgentModeStandard, true, ToolAskUserQuestion, ""},
		{"auto allows AskUserQuestion", agentdomain.AgentModeAutoAccept, true, ToolAskUserQuestion, ""},
		{"no mode fails open", agentdomain.AgentModeStandard, false, ToolWrite, ""},
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
	registry := NewRegistry(cfg, &agentdomainmocks.FakeImageService{}, nil, nil, nil, nil, nil, &agentdomainmocks.FakeImageAnnotator{}, nil, nil)
	svc := NewService(cfg, registry)

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
