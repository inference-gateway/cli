package tools

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestMainAgentAllows(t *testing.T) {
	explore := agentdomain.MainAgent{Name: "explore", Tools: []string{ToolRead, ToolGrep}}
	noBash := agentdomain.MainAgent{Name: "no-bash", DisallowedTools: []string{ToolBash}}
	mailer := agentdomain.MainAgent{Name: "mailer", MCPServers: []string{"agentmail"}}
	offline := agentdomain.MainAgent{Name: "offline", MCPServers: []string{}}
	everything := agentdomain.MainAgent{Name: "main"}

	tests := []struct {
		name  string
		agent agentdomain.MainAgent
		tool  string
		want  bool
	}{
		{"a listed tool", explore, ToolRead, true},
		{"an unlisted tool", explore, ToolWrite, false},
		{"a disallowed tool", noBash, ToolBash, false},
		{"anything else without a list", noBash, ToolWrite, true},
		{"an MCP tool of a named server", mailer, "MCP_agentmail_send", true},
		{"an MCP tool of another server", mailer, "MCP_github_issue", false},
		{"a built-in tool beside named servers", mailer, ToolRead, true},
		{"an MCP tool when no server is named", offline, "MCP_agentmail_send", false},
		{"any tool for an unrestricted agent", everything, "MCP_github_issue", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MainAgentAllows(tt.agent, tt.tool); got != tt.want {
				t.Fatalf("MainAgentAllows(%s, %s) = %v, want %v", tt.agent.Name, tt.tool, got, tt.want)
			}
		})
	}
}

func TestExecuteToolRefusesWhatTheMainAgentMayNotCall(t *testing.T) {
	service := &Service{registry: &Registry{}}
	ctx := agentdomain.WithMainAgent(context.Background(), agentdomain.MainAgent{Name: "explore", Tools: []string{ToolRead, ToolGrep}})

	_, err := service.ExecuteTool(ctx, sdk.ChatCompletionMessageToolCallFunction{Name: ToolWrite, Arguments: "{}"})
	if err == nil || !strings.Contains(err.Error(), `Write is not available to agent "explore" - use one of Read, Grep instead`) {
		t.Fatalf("err = %v, want the refusal naming the agent's tools", err)
	}
}
