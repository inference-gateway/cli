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

var errRewindWhileBusy = errors.New("the session is running a turn or has one queued")

// conversationRewoundFrame answers rewind_conversation with the messages the
// conversation keeps, or with why it was left as it was.
type conversationRewoundFrame struct {
	Type     string         `json:"type"`
	ID       string         `json:"id"`
	Messages []agui.Message `json:"messages"`
	Error    string         `json:"error,omitempty"`
}

// rewinds answers rewind_conversation from the worker's conversation.
// holdTurns keeps the next turn from starting until release runs and reports
// false while one is running or queued, since a turn writes to the entries a
// rewind drops.
type rewinds struct {
	write     frameWriter
	repo      convdomain.ConversationRepository
	holdTurns func() (release func(), idle bool)
}

func (r *rewinds) rewind(id string, prompt convdomain.PromptRef) {
	messages, err := r.rewindTo(prompt)
	if err != nil {
		r.write(conversationRewoundFrame{Type: outboundConversationRewound, ID: id, Error: err.Error()})
		return
	}
	r.write(conversationRewoundFrame{Type: outboundConversationRewound, ID: id, Messages: messages})
}

// rewindTo drops the prompt and everything after it between two turns, and
// returns the messages left before the next turn can add to them.
func (r *rewinds) rewindTo(prompt convdomain.PromptRef) ([]agui.Message, error) {
	if r.holdTurns != nil {
		release, idle := r.holdTurns()
		if !idle {
			return nil, errRewindWhileBusy
		}
		defer release()
	}
	if err := r.repo.RewindToPrompt(prompt); err != nil {
		return nil, err
	}
	return snapshotMessages(r.repo.GetMessages()), nil
}
