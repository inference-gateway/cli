package headless

import (
	"context"
	"encoding/json"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// The panel frames a person driving a session's screen from the web
// terminal's Screen view use: screen_control takes it over or hands it back,
// screen_input lands one pointer, wheel or keyboard input, and both reply as
// one screen_result carrying the request id.
const (
	inboundScreenInput   = "screen_input"
	inboundScreenControl = "screen_control"
	outboundScreenResult = "screen_result"

	screenActionTakeOver = "take_over"
	screenActionHandBack = "hand_back"
)

// handBackMessage is the user message a hand-back queues, so the agent's next
// turn starts from a fresh screenshot of what the person left on the screen.
// The inputs themselves are the person's, and the transcript never replays them.
const handBackMessage = "I took over the screen and handed it back, continue from what you see now."

// screenResultFrame answers one screen_control or screen_input with what the
// worker's screen control did: the screen's held state after the call and
// what failed, if it came to that.
type screenResultFrame struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Held  bool   `json:"held"`
	Error string `json:"error,omitempty"`
}

// screenUnit answers the Screen view's control and input frames on the
// worker's screen control.
type screenUnit struct {
	write   frameWriter
	control computerdomain.ScreenControl
	queue   convdomain.MessageQueue

	// resolve is the serve wiring's hook: it resolves as rejected the open
	// Computer-call approval a take-over decides for the agent. nil elsewhere.
	resolve func()
	// wake is the serve wiring's hook: it wakes the turn loop for the message
	// a hand-back queues. nil elsewhere.
	wake func()
}

func newScreenUnit(write frameWriter, deps PanelDeps) *screenUnit {
	return &screenUnit{write: write, control: deps.Screen, queue: deps.Queue}
}

// held reports the screen's held state, false for a worker without a wired
// screen control.
func (s *screenUnit) held() bool {
	return s.control != nil && s.control.Held()
}

// controlFrame answers screen_control: take over or hand back.
func (s *screenUnit) controlFrame(id string, line []byte) {
	var frame struct {
		Action string `json:"action"`
	}
	if s.control == nil {
		s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Error: "this worker has no screen control"})
		return
	}
	if json.Unmarshal(line, &frame) != nil {
		s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Held: s.held(), Error: "unreadable screen action"})
		return
	}
	switch frame.Action {
	case screenActionTakeOver:
		s.takeOver(id)
	case screenActionHandBack:
		s.handBack(id)
	default:
		s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Held: s.held(), Error: "unknown screen action " + frame.Action})
	}
}

// takeOver claims the screen for the person. It answers the agent's open
// Computer-call approval as rejected: the person at the screen decides, not
// the approval box.
func (s *screenUnit) takeOver(id string) {
	if err := s.control.TakeOver(); err != nil {
		s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Held: s.held(), Error: err.Error()})
		return
	}
	s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Held: true})
	if s.resolve != nil {
		s.resolve()
	}
}

// handBack releases the screen to the agent. A hand-back that released a held
// screen queues the continue message and wakes the turn loop, so the agent's
// next turn starts from a fresh screenshot of what the person left behind.
func (s *screenUnit) handBack(id string) {
	wasHeld := s.held()
	s.control.HandBack()
	s.write(screenResultFrame{Type: outboundScreenResult, ID: id})
	if !wasHeld {
		return
	}
	s.queueContinueMessage()
	if s.wake != nil {
		s.wake()
	}
}

// queueContinueMessage lands the hand-back's continue message on the message
// queue the turn loop drains, the way a run input does.
func (s *screenUnit) queueContinueMessage() {
	if s.queue == nil {
		return
	}
	message := userMessageText(handBackMessage)
	if err := s.queue.Enqueue(context.Background(), message, convdomain.QueueSourceStdin, ipc.UserMessageRequestID); err != nil {
		logger.Error("failed to enqueue the hand-back continue message", "error", err)
	}
}

// input lands one person input on the held screen. An input for a screen
// nobody holds fails, so a stale tab cannot drive the screen it handed back.
func (s *screenUnit) input(id string, line []byte) {
	var frame struct {
		Input computerdomain.ScreenInput `json:"input"`
	}
	if s.control == nil {
		s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Error: "this worker has no screen control"})
		return
	}
	if json.Unmarshal(line, &frame) != nil {
		s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Held: s.held(), Error: "unreadable screen input"})
		return
	}
	if !s.held() {
		s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Error: "the screen is not held"})
		return
	}
	if err := s.control.Drive(context.Background(), frame.Input); err != nil {
		s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Held: s.held(), Error: err.Error()})
		return
	}
	s.write(screenResultFrame{Type: outboundScreenResult, ID: id, Held: true})
}
