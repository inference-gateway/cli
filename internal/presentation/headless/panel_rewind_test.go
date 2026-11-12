package headless

import (
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// rewindPanel is a panel over a conversation of two turns.
func rewindPanel(t *testing.T) (*Panel, lineSink, convdomain.ConversationRepository) {
	t.Helper()
	deps := panelDeps()
	turns := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("first")},
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("first reply")},
		{Role: sdk.User, Content: sdk.NewMessageContent("second")},
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("second reply")},
	}
	for _, message := range turns {
		if err := deps.Conversations.AddMessage(convdomain.ConversationEntry{Message: message, Time: time.Now()}); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	p, sink := startPanel(deps)
	return p, sink, deps.Conversations
}

func TestPanelRewindAnswersWithTheMessagesItKept(t *testing.T) {
	p, sink, repo := rewindPanel(t)

	if !handle(t, p, map[string]any{"type": "rewind_conversation", "id": "conv-1", "content": "second", "occurrence": 1}) {
		t.Fatal("the panel did not consume rewind_conversation")
	}
	frame := readFrame(t, sink, "conversation_rewound")
	if frame["id"] != "conv-1" || frame["error"] != nil {
		t.Fatalf("rewound = %v", frame)
	}
	messages := frame["messages"].([]any)
	if len(messages) != 2 || len(repo.GetMessages()) != 2 {
		t.Fatalf("messages = %v, want the first turn", messages)
	}
	last := messages[1].(map[string]any)
	if last["role"] != "assistant" || last["content"] != "first reply" {
		t.Fatalf("the last kept message = %v", last)
	}
}

func TestPanelRewindSaysWhyItLeftTheConversation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		busy    bool
		reason  error
	}{
		{name: "a turn is running or queued", content: "second", busy: true, reason: errRewindWhileBusy},
		{name: "the conversation has no such prompt", content: "never sent", reason: convdomain.ErrPromptNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, sink, repo := rewindPanel(t)
			p.rewinds.holdTurns = func() (func(), bool) { return func() {}, !tt.busy }

			handle(t, p, map[string]any{"type": "rewind_conversation", "id": "conv-1", "content": tt.content})
			frame := readFrame(t, sink, "conversation_rewound")
			if frame["error"] != tt.reason.Error() {
				t.Fatalf("error = %v, want %v", frame["error"], tt.reason)
			}
			if got := len(repo.GetMessages()); got != 4 {
				t.Fatalf("a refused rewind dropped entries, %d left", got)
			}
		})
	}
}
