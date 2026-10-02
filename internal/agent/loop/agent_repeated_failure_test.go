package loop

import (
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func TestTrackRepeatedFailure(t *testing.T) {
	s := &Agent{}
	tc := sdk.ChatCompletionMessageToolCall{
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Read",
			Arguments: `{"file_path":"/nope/reminders.go"}`,
		},
	}
	failed := convdomain.ConversationEntry{ToolExecution: &agentdomain.ToolExecutionResult{Success: false}}
	ok := convdomain.ConversationEntry{ToolExecution: &agentdomain.ToolExecutionResult{Success: true}}

	s.trackRepeatedFailure(tc, failed)
	if name, n := s.takeRepeatedFailure(); name != "" || n != 0 {
		t.Fatal("1st failure should not warn")
	}
	s.trackRepeatedFailure(tc, failed)
	if name, n := s.takeRepeatedFailure(); name != "" || n != 0 {
		t.Fatal("2nd failure should not warn")
	}
	s.trackRepeatedFailure(tc, failed)
	name, n := s.takeRepeatedFailure()
	if name != "Read" || n != 3 {
		t.Fatalf("3rd failure should warn (name=Read, n=3), got name=%q n=%d", name, n)
	}

	paged := tc
	paged.Function.Arguments = `{ "offset": 40, "file_path":"/nope/reminders.go", "limit":20 }`
	s.trackRepeatedFailure(paged, failed)
	if name, n := s.takeRepeatedFailure(); name != "Read" || n != 4 {
		t.Fatalf("offset/limit/key-order noise should keep counting (name=Read, n=4), got name=%q n=%d", name, n)
	}

	tc2 := tc
	tc2.Function.Arguments = `{"file_path":"/other.go"}`
	s.trackRepeatedFailure(tc2, failed)
	if name, n := s.takeRepeatedFailure(); name != "" || n != 0 {
		t.Fatal("different args should start a fresh count")
	}

	s.trackRepeatedFailure(tc, ok)
	if name, n := s.takeRepeatedFailure(); name != "" || n != 0 {
		t.Fatal("count should reset after success")
	}
	s.trackRepeatedFailure(tc, failed)
	if name, n := s.takeRepeatedFailure(); name != "" || n != 0 {
		t.Fatal("count should reset after success (1st failure again)")
	}
}

func TestRepeatedFailureKey(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		a, b     string
		wantSame bool
	}{
		{"read ignores offset limit order and whitespace", "Read", `{"file_path":"/a.go","offset":1}`, `{ "limit": 5, "file_path": "/a.go" }`, true},
		{"read differs by path", "Read", `{"file_path":"/a.go"}`, `{"file_path":"/b.go"}`, false},
		{"other tool ignores key order and whitespace", "Bash", `{"command":"ls","timeout":1}`, `{ "timeout": 1, "command": "ls" }`, true},
		{"other tool differs by command", "Bash", `{"command":"ls"}`, `{"command":"pwd"}`, false},
		{"invalid json falls back to raw", "Bash", `{broken`, `{broken`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ka, kb := repeatedFailureKey(tt.tool, tt.a), repeatedFailureKey(tt.tool, tt.b)
			if (ka == kb) != tt.wantSame {
				t.Fatalf("keys %q and %q: same=%v, want %v", ka, kb, ka == kb, tt.wantSame)
			}
			if !strings.HasPrefix(ka, tt.tool+"\x00") {
				t.Fatalf("key %q should start with the tool name", ka)
			}
		})
	}
}
