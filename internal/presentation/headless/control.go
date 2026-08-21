package headless

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"time"

	ipc "github.com/inference-gateway/cli/internal/platform/ipc"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

const resumeContinuePrompt = "Please continue from where you left off."

// headlessControl is the single reader of the headless process's stdin. It
// splits the IPC line stream into approval responses for the renderer and
// computer_use_control actions, mirroring what the chat approval coordinator
// does: pause cancels the in-flight request and sets the paused state; resume
// restarts the run with a hidden continue message. Control actions surface on
// ctrlEvents as the same domain events the renderers already handle. Both
// channels close on stdin EOF.
//
// The reader goroutine applies pause immediately (cancellation must not wait
// on the event pump), but only the pump clears the paused state — it is the
// goroutine that decides whether an ended stream restarts, and clearing the
// flag anywhere else races that decision.
type headlessControl struct {
	agentService agentdomain.AgentService
	pauseState   agentdomain.ComputerUsePauseManager
	sessionID    string
	approvals    chan ipc.ApprovalResponse
	ctrlEvents   chan agentdomain.ChatEvent
}

func newHeadlessControl(agentService agentdomain.AgentService, pauseState agentdomain.ComputerUsePauseManager, sessionID string) *headlessControl {
	return &headlessControl{
		agentService: agentService,
		pauseState:   pauseState,
		sessionID:    sessionID,
		approvals:    make(chan ipc.ApprovalResponse, 4),
		ctrlEvents:   make(chan agentdomain.ChatEvent, 4),
	}
}

func (c *headlessControl) readLines(in io.Reader) {
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		c.dispatchLine(scanner.Bytes())
	}
	close(c.approvals)
	close(c.ctrlEvents)
}

func (c *headlessControl) dispatchLine(line []byte) {
	var resp ipc.ApprovalResponse
	if json.Unmarshal(line, &resp) == nil && resp.Type == "approval_response" {
		c.approvals <- resp
		return
	}
	var ctrl ipc.ComputerUseControlMessage
	if json.Unmarshal(line, &ctrl) != nil || ctrl.Type != "computer_use_control" {
		return
	}
	switch ctrl.Action {
	case "pause":
		_ = c.agentService.CancelRequest(c.sessionID)
		c.pauseState.SetComputerUsePaused(true, c.sessionID)
		c.ctrlEvents <- agentdomain.ComputerUsePausedEvent{RequestID: c.sessionID, Timestamp: time.Now()}
	case "resume":
		c.ctrlEvents <- agentdomain.ComputerUseResumedEvent{RequestID: c.sessionID, Timestamp: time.Now()}
	default:
		logger.Warn("ignoring unknown computer_use_control action", "action", ctrl.Action)
	}
}

// pumpEvents merges agent stream events and control events into one channel
// for the renderer, which is called exactly once. When a run's stream closes
// while paused, the pump waits for the resume control event and continues
// with the stream that resume() returns — the headless mirror of chat mode's
// restart-on-resume. The pump goroutine owns closing the returned channel.
func (c *headlessControl) pumpEvents(stream <-chan agentdomain.ChatEvent, resume func() (<-chan agentdomain.ChatEvent, error)) <-chan agentdomain.ChatEvent {
	merged := make(chan agentdomain.ChatEvent)
	go func() {
		defer close(merged)
		ctrlEvents := (<-chan agentdomain.ChatEvent)(c.ctrlEvents)
		pendingResume := false
		for {
			select {
			case ev, ok := <-stream:
				if !ok {
					pendingResume = c.drainControlEvents(merged, ctrlEvents, pendingResume)
					if pendingResume {
						pendingResume = false
						stream = c.startResume(merged, resume)
					} else {
						stream = c.awaitResume(merged, ctrlEvents, resume)
					}
					if stream == nil {
						return
					}
					continue
				}
				merged <- ev
			case ev, ok := <-ctrlEvents:
				if !ok {
					ctrlEvents = nil
					continue
				}
				merged <- ev
				pendingResume = c.noteControlEvent(ev, pendingResume)
			}
		}
	}()
	return merged
}

// noteControlEvent updates the pump's resume bookkeeping for one forwarded
// control event. A resume only counts while paused, so a stray resume with no
// preceding pause cannot restart a normally-completed run; counting it also
// clears the paused state, un-blocking RunWithStream's paused fail-fast for
// the restart.
func (c *headlessControl) noteControlEvent(ev agentdomain.ChatEvent, pendingResume bool) bool {
	switch ev.(type) {
	case agentdomain.ComputerUsePausedEvent:
		return false
	case agentdomain.ComputerUseResumedEvent:
		if c.pauseState.IsComputerUsePaused() {
			c.pauseState.ClearComputerUsePauseState()
			return true
		}
	}
	return pendingResume
}

// drainControlEvents forwards every already-queued control event before the
// pump decides whether the ended stream should resume, so a resume that
// raced the cancelled stream's close is not lost. Returns the updated
// pending-resume flag.
func (c *headlessControl) drainControlEvents(merged chan<- agentdomain.ChatEvent, ctrlEvents <-chan agentdomain.ChatEvent, pendingResume bool) bool {
	for {
		select {
		case ev, ok := <-ctrlEvents:
			if !ok {
				return pendingResume
			}
			merged <- ev
			pendingResume = c.noteControlEvent(ev, pendingResume)
		default:
			return pendingResume
		}
	}
}

// awaitResume forwards control events while the session is paused and returns
// the resumed run's stream, or nil when rendering should end: not paused,
// stdin EOF, or the resumed run failing to start.
func (c *headlessControl) awaitResume(merged chan<- agentdomain.ChatEvent, ctrlEvents <-chan agentdomain.ChatEvent, resume func() (<-chan agentdomain.ChatEvent, error)) <-chan agentdomain.ChatEvent {
	if ctrlEvents == nil || !c.pauseState.IsComputerUsePaused() {
		return nil
	}
	for ev := range ctrlEvents {
		merged <- ev
		if c.noteControlEvent(ev, false) {
			return c.startResume(merged, resume)
		}
	}
	return nil
}

// startResume starts the resumed run, surfacing a failure to start as an
// error event so hosts see it instead of a silent end of stream.
func (c *headlessControl) startResume(merged chan<- agentdomain.ChatEvent, resume func() (<-chan agentdomain.ChatEvent, error)) <-chan agentdomain.ChatEvent {
	next, err := resume()
	if err != nil {
		merged <- agentdomain.ChatErrorEvent{RequestID: c.sessionID, Timestamp: time.Now(), Error: err}
		return nil
	}
	return next
}

// resumeRun appends the hidden continue message the chat coordinator
// uses on resume and starts a new agent run over the full conversation.
func resumeRun(ctx context.Context, agentService agentdomain.AgentService, repo convdomain.ConversationRepository, req *agentdomain.AgentRequest) (<-chan agentdomain.ChatEvent, error) {
	entry := convdomain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(resumeContinuePrompt),
		},
		Time:   time.Now(),
		Hidden: true,
	}
	if err := repo.AddMessage(entry); err != nil {
		return nil, err
	}
	next := *req
	next.Messages = conversation.BuildAgentMessagesFromEntries(repo.GetMessages())
	return agentService.RunWithStream(ctx, &next)
}
