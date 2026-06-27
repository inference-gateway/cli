package services

import (
	"slices"
	"testing"

	config "github.com/inference-gateway/cli/config"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func toolNamesForMode(svc *LLMToolService, mode domain.AgentMode) []string {
	defs := svc.ListToolsForMode(mode)
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	return names
}

func TestListToolsForMode_AskUserQuestionPlanOnly(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil)
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
