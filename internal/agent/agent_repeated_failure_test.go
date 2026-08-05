package agent

import (
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestTrackRepeatedFailure(t *testing.T) {
	s := &AgentServiceImpl{}
	tc := sdk.ChatCompletionMessageToolCall{
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Read",
			Arguments: `{"file_path":"/nope/reminders.go"}`,
		},
	}
	failed := domain.ConversationEntry{ToolExecution: &domain.ToolExecutionResult{Success: false}}
	ok := domain.ConversationEntry{ToolExecution: &domain.ToolExecutionResult{Success: true}}

	if s.trackRepeatedFailure(tc, failed) != "" {
		t.Fatal("1st failure should not warn")
	}
	if s.trackRepeatedFailure(tc, failed) != "" {
		t.Fatal("2nd failure should not warn")
	}
	note := s.trackRepeatedFailure(tc, failed)
	if !strings.Contains(note, "<system-reminder>") || !strings.Contains(note, "3 times") {
		t.Fatalf("3rd failure should warn, got: %q", note)
	}

	// different args = different key
	tc2 := tc
	tc2.Function.Arguments = `{"file_path":"/other.go"}`
	if s.trackRepeatedFailure(tc2, failed) != "" {
		t.Fatal("different args should start a fresh count")
	}

	// success resets
	s.trackRepeatedFailure(tc, ok)
	if s.trackRepeatedFailure(tc, failed) != "" {
		t.Fatal("count should reset after success")
	}
}
