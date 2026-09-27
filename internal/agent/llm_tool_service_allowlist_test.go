package agent

import (
	"slices"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
)

func newAllowlistTestService(t *testing.T) *LLMToolService {
	t.Helper()
	cfg := config.DefaultConfig()
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return NewLLMToolServiceWithRegistry(cfg, registry)
}

// A named Markdown subagent receives its tool allowlist through
// tools.SubagentToolsEnv; isToolEnabled must gate both advertisement and
// execution, so a disallowed tool fails even when the model names it.
func TestSubagentToolAllowlistGates(t *testing.T) {
	t.Setenv(tools.SubagentToolsEnv, "Read,Grep")
	svc := newAllowlistTestService(t)

	for _, allowed := range []string{"Read", "Grep"} {
		if !svc.IsToolEnabled(allowed) {
			t.Errorf("allowed tool %s must be enabled", allowed)
		}
	}
	for _, denied := range []string{"Bash", "Write", "Edit", "Tree", "Agent"} {
		if svc.IsToolEnabled(denied) {
			t.Errorf("tool %s is not in the allowlist and must be disabled", denied)
		}
	}

	names := toolNamesForMode(svc, agentdomain.AgentModeStandard)
	for _, denied := range []string{"Bash", "Write", "Edit"} {
		if slices.Contains(names, denied) {
			t.Errorf("advertised tools must exclude %s; got %v", denied, names)
		}
	}
	if !slices.Contains(names, "Read") {
		t.Errorf("advertised tools must include Read; got %v", names)
	}
}

func TestSubagentToolAllowlistUnsetMeansAll(t *testing.T) {
	svc := newAllowlistTestService(t)
	if !svc.IsToolEnabled("Bash") || !svc.IsToolEnabled("Read") {
		t.Fatalf("without the env var every enabled tool must stay available")
	}
}

func TestSubagentToolAllowlistIgnoresWhitespaceAndEmptyEntries(t *testing.T) {
	t.Setenv(tools.SubagentToolsEnv, " Read , , Grep ")
	svc := newAllowlistTestService(t)
	if !svc.IsToolEnabled("Read") || !svc.IsToolEnabled("Grep") {
		t.Fatalf("entries must be trimmed: Read=%v Grep=%v", svc.IsToolEnabled("Read"), svc.IsToolEnabled("Grep"))
	}
}
