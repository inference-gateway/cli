package domain

import "testing"

func TestAnyToolFailed(t *testing.T) {
	ok := &ToolExecutionResult{Success: true}
	bad := &ToolExecutionResult{Success: false}

	tests := []struct {
		name    string
		results []ConversationEntry
		want    bool
	}{
		{"empty", nil, false},
		{"all success", []ConversationEntry{{ToolExecution: ok}, {ToolExecution: ok}}, false},
		{"one failure", []ConversationEntry{{ToolExecution: ok}, {ToolExecution: bad}}, true},
		{"nil execution ignored", []ConversationEntry{{ToolExecution: nil}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyToolFailed(tt.results); got != tt.want {
				t.Fatalf("AnyToolFailed = %v, want %v", got, tt.want)
			}
		})
	}
}
