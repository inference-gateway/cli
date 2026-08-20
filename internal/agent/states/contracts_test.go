package states

import (
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func TestAnyToolFailed(t *testing.T) {
	ok := &agentdomain.ToolExecutionResult{Success: true}
	bad := &agentdomain.ToolExecutionResult{Success: false}

	tests := []struct {
		name    string
		results []convdomain.ConversationEntry
		want    bool
	}{
		{"empty", nil, false},
		{"all success", []convdomain.ConversationEntry{{ToolExecution: ok}, {ToolExecution: ok}}, false},
		{"one failure", []convdomain.ConversationEntry{{ToolExecution: ok}, {ToolExecution: bad}}, true},
		{"nil execution ignored", []convdomain.ConversationEntry{{ToolExecution: nil}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyToolFailed(tt.results); got != tt.want {
				t.Fatalf("AnyToolFailed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnyToolRejected(t *testing.T) {
	ok := &agentdomain.ToolExecutionResult{Success: true}
	failed := &agentdomain.ToolExecutionResult{Success: false}
	rejected := &agentdomain.ToolExecutionResult{Success: false, Rejected: true}

	tests := []struct {
		name    string
		results []convdomain.ConversationEntry
		want    bool
	}{
		{"empty", nil, false},
		{"no rejection", []convdomain.ConversationEntry{{ToolExecution: ok}, {ToolExecution: failed}}, false},
		{"one rejection", []convdomain.ConversationEntry{{ToolExecution: ok}, {ToolExecution: rejected}}, true},
		{"nil execution ignored", []convdomain.ConversationEntry{{ToolExecution: nil}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyToolRejected(tt.results); got != tt.want {
				t.Fatalf("AnyToolRejected = %v, want %v", got, tt.want)
			}
		})
	}
}
