package agent

import (
	"testing"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func TestTrackRepeatedFailure(t *testing.T) {
	s := &AgentServiceImpl{}
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
