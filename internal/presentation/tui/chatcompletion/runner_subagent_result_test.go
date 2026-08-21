package chatcompletion

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

func assistantEntries(answer string) []convdomain.ConversationEntry {
	return []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("do the task")}},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent(answer)}},
	}
}

func runnerWithMessages(entries []convdomain.ConversationEntry) *Runner {
	repo := &convmocks.FakeConversationRepository{}
	repo.GetMessagesReturns(entries)
	return &Runner{conversationRepo: repo}
}

func readResultFile(t *testing.T, path string) scheddomain.SubagentResultFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected a result file: %v", err)
	}
	var rf scheddomain.SubagentResultFile
	if err := json.Unmarshal(data, &rf); err != nil {
		t.Fatalf("result file not valid JSON: %v", err)
	}
	return rf
}

// A completed turn writes the last assistant message taken from the CONVERSATION
// (ChatCompleteEvent.Message is never populated, which was the bug).
func TestRunner_writeSubagentResultFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "result.json")
	t.Setenv(scheddomain.EnvSubagentResultFile, path)

	r := runnerWithMessages(assistantEntries("the answer"))
	r.writeSubagentResultFile(agentdomain.ChatCompleteEvent{})
	if rf := readResultFile(t, path); rf.FinalAssistant != "the answer" || !rf.Success {
		t.Fatalf("unexpected result file: %+v", rf)
	}

	// Gated: pending tool calls, a cancelled turn, and an empty last message must
	// NOT write (the turn isn't a final answer).
	mustNotWrite := func(name string, rr *Runner, msg agentdomain.ChatCompleteEvent) {
		_ = os.Remove(path)
		rr.writeSubagentResultFile(msg)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s: result file must not be written", name)
		}
	}
	mustNotWrite("pending tool calls", r, agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{{ID: "t"}}})
	mustNotWrite("cancelled", r, agentdomain.ChatCompleteEvent{Cancelled: true})
	mustNotWrite("empty answer", runnerWithMessages(assistantEntries("   ")), agentdomain.ChatCompleteEvent{})
}

// An errored turn writes a failure result so the parent harvests the error rather
// than falling back to pane chrome.
func TestRunner_writeSubagentResultFileError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "result.json")
	t.Setenv(scheddomain.EnvSubagentResultFile, path)

	runnerWithMessages(assistantEntries("partial")).writeSubagentResultFileError(errors.New("boom"))
	rf := readResultFile(t, path)
	if rf.Success {
		t.Fatalf("error write must be Success=false: %+v", rf)
	}
	if rf.Error != "boom" {
		t.Fatalf("error not recorded: %+v", rf)
	}
}

// A normal chat (no env var) writes nothing.
func TestRunner_writeSubagentResultFile_EnvUnset(t *testing.T) {
	t.Setenv(scheddomain.EnvSubagentResultFile, "")
	runnerWithMessages(assistantEntries("x")).writeSubagentResultFile(agentdomain.ChatCompleteEvent{})
}
