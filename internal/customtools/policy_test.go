package customtools

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agent "github.com/inference-gateway/cli/internal/agent"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
)

type fixedMode agentdomain.AgentMode

func (m fixedMode) GetAgentMode() agentdomain.AgentMode   { return agentdomain.AgentMode(m) }
func (m fixedMode) SetAgentMode(agentdomain.AgentMode)    {}
func (m fixedMode) CycleAgentMode() agentdomain.AgentMode { return agentdomain.AgentMode(m) }

// newToolService registers Echo (default modes, inherited approval) and Peek
// (offered in plan, never needs approval) next to the built-in tools.
func newToolService(t *testing.T, requireApproval bool) (*config.Config, *agent.LLMToolService) {
	t.Helper()
	dir := t.TempDir()
	peek := strings.Replace(echoManifest, "name: Echo", "name: Peek", 1) +
		"modes:\n  - standard\n  - auto\n  - auto-with-judge\n  - plan\n  - readonly\nrequire_approval: false\n"
	for name, content := range map[string]string{"Echo.yaml": echoManifest, "Peek.yaml": peek} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Tools: config.ToolsConfig{
		Enabled:   true,
		CustomDir: dir,
		Safety:    config.SafetyConfig{RequireApproval: requireApproval},
	}}
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	registry.RegisterTools(NewTools(cfg, tools.ToolNames()))
	return cfg, agent.NewLLMToolServiceWithRegistry(cfg, registry)
}

func TestCustomTools_ModesGateListingAndExecution(t *testing.T) {
	_, service := newToolService(t, true)
	listed := func(mode agentdomain.AgentMode) []string {
		var names []string
		for _, def := range service.ListToolsForMode(mode) {
			names = append(names, def.Function.Name)
		}
		return names
	}

	if standard := listed(agentdomain.AgentModeStandard); !slices.Contains(standard, "Echo") || !slices.Contains(standard, "Peek") {
		t.Errorf("standard mode lists %v, want Echo and Peek", standard)
	}
	if plan := listed(agentdomain.AgentModePlan); slices.Contains(plan, "Echo") || !slices.Contains(plan, "Peek") {
		t.Errorf("plan mode lists %v, want Peek but not Echo", plan)
	}

	ctx := agentdomain.WithAgentMode(context.Background(), agentdomain.AgentModePlan)
	_, err := service.ExecuteTool(ctx, sdk.ChatCompletionMessageToolCallFunction{Name: "Echo", Arguments: `{"text":"hi"}`})
	if err == nil || !strings.Contains(err.Error(), "tool not allowed: Echo") {
		t.Errorf("ExecuteTool in plan mode = %v, want Echo refused", err)
	}
}

func TestCustomTools_RequireApproval(t *testing.T) {
	tests := []struct {
		name            string
		tool            string
		globalApproval  bool
		wantApprovalReq bool
	}{
		{name: "omitted inherits global true", tool: "Echo", globalApproval: true, wantApprovalReq: true},
		{name: "omitted inherits global false", tool: "Echo", globalApproval: false, wantApprovalReq: false},
		{name: "explicit false wins over global", tool: "Peek", globalApproval: true, wantApprovalReq: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, service := newToolService(t, tt.globalApproval)
			policy := agent.NewStandardApprovalPolicy(cfg, fixedMode(agentdomain.AgentModeStandard), service)
			call := &sdk.ChatCompletionMessageToolCall{Function: sdk.ChatCompletionMessageToolCallFunction{Name: tt.tool, Arguments: `{"text":"hi"}`}}
			if got := policy.ShouldRequireApproval(context.Background(), call, true); got != tt.wantApprovalReq {
				t.Errorf("ShouldRequireApproval(%s) = %v, want %v", tt.tool, got, tt.wantApprovalReq)
			}
		})
	}
}

func TestCustomTools_ProjectToolsAlwaysNeedApproval(t *testing.T) {
	project := t.TempDir()
	t.Chdir(project)
	peek := strings.Replace(echoManifest, "name: Echo", "name: Peek", 1) +
		"modes:\n  - standard\n  - auto\n  - auto-with-judge\n  - plan\n  - readonly\nrequire_approval: false\n"
	writeFiles(t, filepath.Join(project, ".agents", "tools"), map[string]string{"Peek.yaml": peek})
	cfg := &config.Config{Tools: config.ToolsConfig{
		Enabled:   true,
		CustomDir: t.TempDir(),
		Safety:    config.SafetyConfig{RequireApproval: false},
	}}
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	registry.RegisterTools(NewTools(cfg, tools.ToolNames()))
	service := agent.NewLLMToolServiceWithRegistry(cfg, registry)

	for mode, want := range map[agentdomain.AgentMode]bool{
		agentdomain.AgentModeStandard:   true,
		agentdomain.AgentModeReadOnly:   true,
		agentdomain.AgentModePlan:       true,
		agentdomain.AgentModeAutoAccept: false,
	} {
		policy := agent.NewStandardApprovalPolicy(cfg, fixedMode(mode), service)
		call := &sdk.ChatCompletionMessageToolCall{Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Peek", Arguments: `{"text":"hi"}`}}
		if got := policy.ShouldRequireApproval(context.Background(), call, true); got != want {
			t.Errorf("%s mode: ShouldRequireApproval = %v, want %v despite require_approval: false", mode.ModeKey(), got, want)
		}
	}
}
