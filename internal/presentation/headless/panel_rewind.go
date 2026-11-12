package headless

import (
	"errors"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// The panel frames a client rewinds a session's conversation with before it
// resends a prompt: the worker owns the conversation, so a client has no other
// way to drop its messages.
const (
	inboundRewindConversation   = "rewind_conversation"
	outboundConversationRewound = "conversation_rewound"
)

var errRewindDuringTurn = errors.New("the session is running a turn, stop it before rewinding")

// conversationRewoundFrame answers rewind_conversation with the messages the
// conversation keeps, or with why it was left as it was.
type conversationRewoundFrame struct {
	Type     string         `json:"type"`
	ID       string         `json:"id"`
	Messages []agui.Message `json:"messages"`
	Error    string         `json:"error,omitempty"`
}

// rewinds answers rewind_conversation from the worker's conversation. busy
// reports a turn that is running or about to, which writes to the entries a
// rewind would drop.
type rewinds struct {
	write frameWriter
	repo  convdomain.ConversationRepository
	busy  func() bool
}

func (r *rewinds) rewind(id string, prompt convdomain.PromptRef) {
	if err := r.rewindTo(prompt); err != nil {
		r.write(conversationRewoundFrame{Type: outboundConversationRewound, ID: id, Error: err.Error()})
		return
	}
	r.write(conversationRewoundFrame{Type: outboundConversationRewound, ID: id, Messages: snapshotMessages(r.repo.GetMessages())})
}

func (r *rewinds) rewindTo(prompt convdomain.PromptRef) error {
	if r.busy != nil && r.busy() {
		return errRewindDuringTurn
	}
	return r.repo.RewindToPrompt(prompt)
}
