package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	browser "github.com/inference-gateway/cli/internal/browser"
	computer "github.com/inference-gateway/cli/internal/computer"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	a2a "github.com/inference-gateway/cli/internal/protocols/a2a"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files in testdata/")

const builtinToolCount = 49

// toolDefinitionsSnapshot is what the built-in tools expose: the definitions
// sent to the model, the tools offered per agent mode, and whether a call
// needs approval per mode with the global require_approval on and off.
type toolDefinitionsSnapshot struct {
	Definitions []sdk.ChatCompletionTool   `json:"definitions"`
	Modes       map[string][]string        `json:"modes"`
	Approval    map[string]map[string]bool `json:"approval"`
}

// TestToolDefinitionsGolden pins every built-in tool's definition JSON, the
// per-mode tool lists and the approval outcomes, so moving the definitions
// around cannot change what the model sees or what needs approval.
func TestToolDefinitionsGolden(t *testing.T) {
	goldenPath, err := filepath.Abs(filepath.Join("testdata", "tool_definitions.golden.json"))
	if err != nil {
		t.Fatalf("resolve golden path: %v", err)
	}
	isolateToolEnvironment(t)
	cfg := goldenConfig()
	registry := goldenRegistry(cfg)
	svc := NewLLMToolServiceWithRegistry(cfg, registry)

	defs := registry.GetToolDefinitions()
	if len(defs) != builtinToolCount {
		t.Fatalf("expected %d built-in tools, got %d", builtinToolCount, len(defs))
	}

	snapshot := toolDefinitionsSnapshot{
		Definitions: defs,
		Modes:       map[string][]string{},
		Approval:    map[string]map[string]bool{},
	}
	for _, mode := range []agentdomain.AgentMode{agentdomain.AgentModeStandard, agentdomain.AgentModePlan, agentdomain.AgentModeReadOnly} {
		snapshot.Modes[mode.ModeKey()] = toolNamesForMode(svc, mode)
	}
	configs := map[string]*config.Config{"configured": cfg, "no_tool_overrides": withoutToolApprovalOverrides(cfg)}
	for cfgName, approvalCfg := range configs {
		for _, mode := range []agentdomain.AgentMode{agentdomain.AgentModeStandard, agentdomain.AgentModePlan} {
			for _, global := range []bool{true, false} {
				key := fmt.Sprintf("%s/%s/global_%t", cfgName, mode.ModeKey(), global)
				snapshot.Approval[key] = approvalOutcomes(t, approvalCfg, mode, global, defs)
			}
		}
	}

	got, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	got = append(got, '\n')
	assertGolden(t, goldenPath, got)
}

func isolateToolEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(tools.SubagentToolsEnv, "")
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	t.Chdir(t.TempDir())
}

func goldenConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.Tools.Schedule.Enabled = true
	cfg.TextToSpeech.Enabled = true
	cfg.TextToMusic.Enabled = true
	cfg.TextToSFX.Enabled = true
	cfg.TextToVideo.Enabled = true
	cfg.TextToVideo.CreateAvatar = true

	cfg.Memory = *config.DefaultMemoryConfig()
	cfg.Memory.Enabled = true

	cfg.BrowserUse = *config.DefaultBrowserUseConfig()
	cfg.BrowserUse.Enabled = true
	for _, tool := range []*config.BrowserToolConfig{
		&cfg.BrowserUse.Tools.Navigate, &cfg.BrowserUse.Tools.Click, &cfg.BrowserUse.Tools.Type,
		&cfg.BrowserUse.Tools.Read, &cfg.BrowserUse.Tools.Screenshot, &cfg.BrowserUse.Tools.Tabs,
	} {
		tool.Enabled = true
	}

	cfg.ComputerUse = *config.DefaultComputerUseConfig()
	cfg.ComputerUse.Enabled = true
	cfg.ComputerUse.Recording.Enabled = true
	return cfg
}

// withoutToolApprovalOverrides clears every per-tool require_approval setting
// so the snapshot also pins each tool's own default.
func withoutToolApprovalOverrides(cfg *config.Config) *config.Config {
	clone := *cfg
	for _, override := range []**bool{
		&clone.Tools.Bash.RequireApproval, &clone.Tools.Read.RequireApproval, &clone.Tools.Write.RequireApproval,
		&clone.Tools.Edit.RequireApproval, &clone.Tools.MultiEdit.RequireApproval, &clone.Tools.Delete.RequireApproval,
		&clone.Tools.Grep.RequireApproval, &clone.Tools.Tree.RequireApproval, &clone.Tools.WebFetch.RequireApproval,
		&clone.Tools.WebSearch.RequireApproval, &clone.Tools.TodoWrite.RequireApproval, &clone.Tools.Schedule.RequireApproval,
		&clone.Tools.Agent.RequireApproval, &clone.Tools.ImageGeneration.RequireApproval, &clone.Tools.ImageEdit.RequireApproval,
		&clone.Tools.ImageVariation.RequireApproval, &clone.A2A.Tools.QueryAgent.RequireApproval,
		&clone.A2A.Tools.QueryTask.RequireApproval, &clone.A2A.Tools.SubmitTask.RequireApproval,
		&clone.TextToSpeech.RequireApproval, &clone.TextToMusic.RequireApproval, &clone.TextToSFX.RequireApproval,
		&clone.TextToVideo.RequireApproval,
	} {
		*override = nil
	}
	return &clone
}

func goldenRegistry(cfg *config.Config) *tools.Registry {
	annotator := &agentdomainmocks.FakeImageAnnotator{}
	jobs := &schedmocks.FakeBackgroundTaskRegistry{}
	registry := tools.NewRegistry(
		cfg,
		&agentdomainmocks.FakeImageService{},
		&agentdomainmocks.FakeSpeechService{},
		&agentdomainmocks.FakeMusicService{},
		&agentdomainmocks.FakeSoundEffectService{},
		&agentdomainmocks.FakeVideoService{},
		&schedmocks.FakeBackgroundShellService{},
		annotator,
		jobs,
		nil,
	)
	registry.RegisterTools(a2a.NewTools(cfg, a2a.NewTaskTracker(nil), jobs, nil))
	registry.RegisterTools(browser.NewTools(cfg, nil))
	registry.RegisterFrameSource("screen", &agentdomainmocks.FakeFrameSource{})
	registry.RegisterTools(computer.NewTools(cfg, registry, annotator, nil))
	return registry
}

// approvalOutcomes asks the approval policy about every tool in a chat
// session in the given mode, with empty arguments.
func approvalOutcomes(t *testing.T, cfg *config.Config, mode agentdomain.AgentMode, global bool, defs []sdk.ChatCompletionTool) map[string]bool {
	t.Helper()
	modeCfg := *cfg
	modeCfg.Tools.Safety.RequireApproval = global
	stateManager := statemanager.NewStore(false)
	stateManager.SetAgentMode(mode)
	policy := newGoldenApprovalPolicy(&modeCfg, stateManager, goldenRegistry(&modeCfg))

	outcomes := make(map[string]bool, len(defs))
	for _, def := range defs {
		call := &sdk.ChatCompletionMessageToolCall{
			ID:       "golden",
			Type:     sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{Name: def.Function.Name, Arguments: "{}"},
		}
		outcomes[def.Function.Name] = policy.ShouldRequireApproval(context.Background(), call, true)
	}
	return outcomes
}

func newGoldenApprovalPolicy(cfg *config.Config, stateManager agentdomain.AgentModeState, registry *tools.Registry) *StandardApprovalPolicy {
	return NewStandardApprovalPolicy(cfg, stateManager, registry)
}

func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s is out of date; diff it against the output of `go test ./internal/agent -run TestToolDefinitionsGolden -update`", path)
	}
}
