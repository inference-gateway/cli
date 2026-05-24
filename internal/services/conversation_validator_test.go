package services_test

import (
	"testing"

	services "github.com/inference-gateway/cli/internal/services"
	sdk "github.com/inference-gateway/sdk"
)

func assistantWithToolCalls(ids ...string) sdk.Message {
	calls := make([]sdk.ChatCompletionMessageToolCall, 0, len(ids))
	for _, id := range ids {
		calls = append(calls, sdk.ChatCompletionMessageToolCall{
			ID:   id,
			Type: sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "Tool_" + id,
				Arguments: "{}",
			},
		})
	}
	return sdk.Message{
		Role:      sdk.Assistant,
		Content:   sdk.NewMessageContent("calling tools"),
		ToolCalls: &calls,
	}
}

func toolResponse(id, content string) sdk.Message {
	idCopy := id
	return sdk.Message{
		Role:       sdk.Tool,
		Content:    sdk.NewMessageContent(content),
		ToolCallID: &idCopy,
	}
}

func userMsg(content string) sdk.Message {
	return sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(content)}
}

func TestEnsureToolCallsClosed_EmptyConversation(t *testing.T) {
	repaired, synthetics := services.EnsureToolCallsClosed(nil)
	if len(repaired) != 0 {
		t.Errorf("expected empty repaired, got %d", len(repaired))
	}
	if len(synthetics) != 0 {
		t.Errorf("expected no synthetics, got %d", len(synthetics))
	}
}

func TestEnsureToolCallsClosed_NoToolCalls(t *testing.T) {
	conv := []sdk.Message{
		userMsg("hi"),
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("hello")},
		userMsg("bye"),
	}
	repaired, synthetics := services.EnsureToolCallsClosed(conv)
	if len(synthetics) != 0 {
		t.Errorf("expected no synthetics, got %d", len(synthetics))
	}
	if len(repaired) != len(conv) {
		t.Errorf("expected len %d, got %d", len(conv), len(repaired))
	}
}

func TestEnsureToolCallsClosed_AllToolCallsResolved(t *testing.T) {
	conv := []sdk.Message{
		userMsg("read files"),
		assistantWithToolCalls("a", "b", "c"),
		toolResponse("a", "result-a"),
		toolResponse("b", "result-b"),
		toolResponse("c", "result-c"),
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("done")},
	}
	repaired, synthetics := services.EnsureToolCallsClosed(conv)
	if len(synthetics) != 0 {
		t.Errorf("expected no synthetics, got %d", len(synthetics))
	}
	if len(repaired) != len(conv) {
		t.Errorf("expected len %d, got %d", len(conv), len(repaired))
	}
}

func TestEnsureToolCallsClosed_OnePartialResolved(t *testing.T) {
	conv := []sdk.Message{
		userMsg("read 3 files"),
		assistantWithToolCalls("a", "b", "c"),
		toolResponse("b", "result-b"),
		userMsg("hi"),
	}
	repaired, synthetics := services.EnsureToolCallsClosed(conv)
	if len(synthetics) != 2 {
		t.Fatalf("expected 2 synthetics, got %d", len(synthetics))
	}
	if synthetics[0].ToolCallID != "a" || synthetics[1].ToolCallID != "c" {
		t.Errorf("expected synthetics for [a, c] in order, got [%s, %s]",
			synthetics[0].ToolCallID, synthetics[1].ToolCallID)
	}

	if len(repaired) != 6 {
		t.Fatalf("expected len 6, got %d", len(repaired))
	}
	if repaired[2].Role != sdk.Tool || repaired[2].ToolCallID == nil || *repaired[2].ToolCallID != "b" {
		t.Errorf("expected real response b at index 2, got role=%s id=%v", repaired[2].Role, repaired[2].ToolCallID)
	}
	if repaired[3].Role != sdk.Tool || repaired[3].ToolCallID == nil || *repaired[3].ToolCallID != "a" {
		t.Errorf("expected synthetic a at index 3, got role=%s id=%v", repaired[3].Role, repaired[3].ToolCallID)
	}
	if repaired[4].Role != sdk.Tool || repaired[4].ToolCallID == nil || *repaired[4].ToolCallID != "c" {
		t.Errorf("expected synthetic c at index 4, got role=%s id=%v", repaired[4].Role, repaired[4].ToolCallID)
	}
	if repaired[5].Role != sdk.User {
		t.Errorf("expected user msg at index 5, got role=%s", repaired[5].Role)
	}
}

func TestEnsureToolCallsClosed_NoneResolved(t *testing.T) {
	conv := []sdk.Message{
		userMsg("read files"),
		assistantWithToolCalls("a", "b", "c"),
		userMsg("hi"),
	}
	repaired, synthetics := services.EnsureToolCallsClosed(conv)
	if len(synthetics) != 3 {
		t.Fatalf("expected 3 synthetics, got %d", len(synthetics))
	}
	want := []string{"a", "b", "c"}
	for i, s := range synthetics {
		if s.ToolCallID != want[i] {
			t.Errorf("synthetics[%d].ToolCallID = %s, want %s", i, s.ToolCallID, want[i])
		}
		body, err := s.Message.Content.AsMessageContent0()
		if err != nil || body != services.CancelledToolResponseContent {
			t.Errorf("synthetics[%d].Message.Content = %q (err=%v), want %q",
				i, body, err, services.CancelledToolResponseContent)
		}
	}
	if len(repaired) != 6 {
		t.Fatalf("expected len 6, got %d", len(repaired))
	}
	if repaired[5].Role != sdk.User {
		t.Errorf("user message should land after synthetics, got role=%s at index 5", repaired[5].Role)
	}
}

func TestEnsureToolCallsClosed_Idempotent(t *testing.T) {
	conv := []sdk.Message{
		userMsg("read files"),
		assistantWithToolCalls("a", "b"),
		userMsg("hi"),
	}
	once, synth1 := services.EnsureToolCallsClosed(conv)
	if len(synth1) != 2 {
		t.Fatalf("first pass expected 2 synthetics, got %d", len(synth1))
	}
	twice, synth2 := services.EnsureToolCallsClosed(once)
	if len(synth2) != 0 {
		t.Errorf("second pass expected 0 synthetics, got %d", len(synth2))
	}
	if len(twice) != len(once) {
		t.Errorf("second pass changed conversation length: %d → %d", len(once), len(twice))
	}
}

func TestEnsureToolCallsClosed_MultipleAssistantMessages(t *testing.T) {
	conv := []sdk.Message{
		userMsg("first"),
		assistantWithToolCalls("a1"),
		userMsg("second"),
		assistantWithToolCalls("b1", "b2"),
		toolResponse("b2", "ok"),
		userMsg("third"),
	}
	repaired, synthetics := services.EnsureToolCallsClosed(conv)
	if len(synthetics) != 2 {
		t.Fatalf("expected 2 synthetics across 2 assistant turns, got %d", len(synthetics))
	}
	if synthetics[0].ToolCallID != "a1" {
		t.Errorf("first synthetic should close a1, got %s", synthetics[0].ToolCallID)
	}
	if synthetics[1].ToolCallID != "b1" {
		t.Errorf("second synthetic should close b1, got %s", synthetics[1].ToolCallID)
	}

	if repaired[2].Role != sdk.Tool || *repaired[2].ToolCallID != "a1" {
		t.Errorf("synthetic for a1 should land at index 2, got role=%s id=%v",
			repaired[2].Role, repaired[2].ToolCallID)
	}
	if repaired[3].Role != sdk.User {
		t.Errorf("'second' user msg should land at index 3, got role=%s", repaired[3].Role)
	}
}

func TestEnsureToolCallsClosed_ReasoningPreserved(t *testing.T) {
	reasoning := "I should read these files"
	asst := assistantWithToolCalls("a", "b")
	asst.Reasoning = &reasoning
	asst.ReasoningContent = &reasoning

	conv := []sdk.Message{userMsg("read"), asst, userMsg("hi")}
	repaired, _ := services.EnsureToolCallsClosed(conv)

	if repaired[1].Reasoning == nil || *repaired[1].Reasoning != reasoning {
		t.Errorf("Reasoning lost: got %v", repaired[1].Reasoning)
	}
	if repaired[1].ReasoningContent == nil || *repaired[1].ReasoningContent != reasoning {
		t.Errorf("ReasoningContent lost: got %v", repaired[1].ReasoningContent)
	}
	if repaired[1].ToolCalls == nil || len(*repaired[1].ToolCalls) != 2 {
		t.Errorf("ToolCalls altered on assistant message")
	}
}

func TestEnsureToolCallsClosed_NilToolCallIDIgnored(t *testing.T) {
	conv := []sdk.Message{
		userMsg("read"),
		assistantWithToolCalls("a"),
		{Role: sdk.Tool, Content: sdk.NewMessageContent("orphan-tool"), ToolCallID: nil},
		userMsg("hi"),
	}
	_, synthetics := services.EnsureToolCallsClosed(conv)
	if len(synthetics) != 1 {
		t.Fatalf("expected 1 synthetic (the nil-ID tool message doesn't satisfy 'a'), got %d", len(synthetics))
	}
	if synthetics[0].ToolCallID != "a" {
		t.Errorf("expected synthetic for a, got %s", synthetics[0].ToolCallID)
	}
}

func TestEnsureToolCallsClosed_PreservesToolNameForUI(t *testing.T) {
	calls := []sdk.ChatCompletionMessageToolCall{
		{
			ID:   "tc1",
			Type: sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "Read",
				Arguments: `{"file_path":"/x"}`,
			},
		},
	}
	conv := []sdk.Message{
		userMsg("read x"),
		{Role: sdk.Assistant, Content: sdk.NewMessageContent(""), ToolCalls: &calls},
		userMsg("hi"),
	}
	_, synthetics := services.EnsureToolCallsClosed(conv)
	if len(synthetics) != 1 || synthetics[0].ToolName != "Read" {
		t.Errorf("expected synthetic with ToolName=Read, got %+v", synthetics)
	}
}

func TestEnsureToolCallsClosed_EmptyIDSkipped(t *testing.T) {
	calls := []sdk.ChatCompletionMessageToolCall{
		{ID: "", Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "X"}},
		{ID: "good", Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Y"}},
	}
	conv := []sdk.Message{
		userMsg("u"),
		{Role: sdk.Assistant, Content: sdk.NewMessageContent(""), ToolCalls: &calls},
		userMsg("v"),
	}
	_, synthetics := services.EnsureToolCallsClosed(conv)
	if len(synthetics) != 1 || synthetics[0].ToolCallID != "good" {
		t.Errorf("empty-ID tool_call should be skipped, only 'good' synthesized; got %+v", synthetics)
	}
}
