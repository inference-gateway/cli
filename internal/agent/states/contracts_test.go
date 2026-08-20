package states

import (
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestAnyToolFailed(t *testing.T) {
	ok := &agentdomain.ToolExecutionResult{Success: true}
	bad := &agentdomain.ToolExecutionResult{Success: false}

	tests := []struct {
		name    string
		results []domain.ConversationEntry
		want    bool
	}{
		{"empty", nil, false},
		{"all success", []domain.ConversationEntry{{ToolExecution: ok}, {ToolExecution: ok}}, false},
		{"one failure", []domain.ConversationEntry{{ToolExecution: ok}, {ToolExecution: bad}}, true},
		{"nil execution ignored", []domain.ConversationEntry{{ToolExecution: nil}}, false},
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
		results []domain.ConversationEntry
		want    bool
	}{
		{"empty", nil, false},
		{"no rejection", []domain.ConversationEntry{{ToolExecution: ok}, {ToolExecution: failed}}, false},
		{"one rejection", []domain.ConversationEntry{{ToolExecution: ok}, {ToolExecution: rejected}}, true},
		{"nil execution ignored", []domain.ConversationEntry{{ToolExecution: nil}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyToolRejected(tt.results); got != tt.want {
				t.Fatalf("AnyToolRejected = %v, want %v", got, tt.want)
			}
		})
	}
}
