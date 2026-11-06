package loopback

import (
	"context"
	"encoding/json"

	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	headless "github.com/inference-gateway/cli/internal/presentation/headless"
	runview "github.com/inference-gateway/cli/internal/presentation/tui/runview"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// Agent is the chat TUI's agent for a local chat rendered through AG-UI: the
// in-process agent's events are written as a run by the encoder the serve
// worker uses and read back by the reducer a daemon chat uses, so both chats
// share one mapping. Everything but the run stream goes straight to the agent.
type Agent struct {
	agentdomain.AgentService
	repo convdomain.ConversationRepository
	jobs func() []scheddomain.TrackedJob
}

// New wraps agent, whose runs the TUI then reads back from AG-UI frames. jobs
// snapshots the background tasks, whose patch hands over a step's tool results.
func New(agent agentdomain.AgentService, repo convdomain.ConversationRepository, jobs func() []scheddomain.TrackedJob) *Agent {
	return &Agent{AgentService: agent, repo: repo, jobs: jobs}
}

// RunWithStream runs the request on the wrapped agent and streams what its
// AG-UI run reduces to, until the run wrote its terminal event. ponytail: a plan
// skips the run, since the agent does not wait on it and the chat restarts the
// run itself. It becomes an interrupt once the handlers stop deciding plans.
func (a *Agent) RunWithStream(ctx context.Context, req *agentdomain.AgentRequest) (<-chan agentdomain.ChatEvent, error) {
	events, err := a.AgentService.RunWithStream(ctx, req)
	if err != nil {
		return nil, err
	}
	sink := &frames{
		out:       make(chan agentdomain.ChatEvent, 1000),
		reducer:   runview.New(req.RequestID),
		approvals: make(chan ipc.ApprovalResponse, 1),
		questions: make(chan ipc.UserQuestionResponse, 1),
	}
	encoder := headless.NewRunEncoder(sink, headless.RunEncoderDeps{
		Model: req.Model, Repo: a.repo, Jobs: a.jobs, Quiet: true,
		Approvals: sink.approvals, Questions: sink.questions,
	})
	go func() {
		defer close(sink.out)
		encoder.Start(a.repo.GetCurrentConversationID(), uuid.New().String())
		for event := range events {
			switch e := event.(type) {
			case agentdomain.PlanApprovalRequestedEvent:
				sink.out <- e
			case agentdomain.ChatCompleteEvent:
				encoder.Handle(withoutLimits(e))
			default:
				encoder.Handle(event)
			}
		}
		_ = encoder.Finish()
	}()
	return sink.out, nil
}

// withoutLimits is a completion without the limit that forced it. A local chat
// completes such a run, where a serve worker fails it for its exit code.
func withoutLimits(e agentdomain.ChatCompleteEvent) agentdomain.ChatCompleteEvent {
	e.MaxTurnsReached, e.MaxDurationReached = false, false
	return e
}

// frames is where the encoder writes one run: each frame is reduced as it is
// written, and a suspended run is resumed with the user's answers. released
// closes once the encoder moved past the prompts that are open.
type frames struct {
	out       chan agentdomain.ChatEvent
	reducer   *runview.Reducer
	approvals chan ipc.ApprovalResponse
	questions chan ipc.UserQuestionResponse
	released  chan struct{}
}

// Write takes one frame the encoder wrote and hands the TUI what it reduces to.
func (f *frames) Write(frame []byte) (int, error) {
	events, end := f.reducer.Decode(frame)
	for _, event := range events {
		f.out <- event
	}
	if end != runview.Open && f.released != nil {
		close(f.released)
		f.released = nil
	}
	if end == runview.Suspended {
		f.released = make(chan struct{})
		go f.answer(f.reducer.Prompts(), f.released)
	}
	return len(frame), nil
}

// answer waits for the user's answer to every prompt the run suspended on and
// hands each to the encoder, which resumes the run with it. Once the encoder
// moved past the prompts, because the agent stopped waiting, nobody needs one.
func (f *frames) answer(prompts []runview.Prompt, released <-chan struct{}) {
	for _, prompt := range prompts {
		switch e := prompt.Event.(type) {
		case agentdomain.ToolApprovalRequestedEvent:
			select {
			case action, answered := <-e.ResponseChan:
				f.approvals <- approval(e, action, answered)
			case <-released:
			}
		case agentdomain.UserQuestionRequestedEvent:
			select {
			case picked, answered := <-e.ResponseChan:
				f.questions <- answers(e, picked, answered)
			case <-released:
			}
		}
	}
}

// approval is the user's decision on a tool call in its wire form. A prompt
// the TUI dismissed rejects the call.
func approval(e agentdomain.ToolApprovalRequestedEvent, action agentdomain.ApprovalAction, answered bool) ipc.ApprovalResponse {
	response := ipc.ApprovalResponse{ToolCallID: e.ToolCall.ID}
	if !answered {
		return response
	}
	response.Approved = action != agentdomain.ApprovalReject
	if action == agentdomain.ApprovalAutoAccept {
		response.Scope = "always"
	}
	return response
}

// answers is the user's answers to a question form in their wire form. A form
// the TUI dismissed is cancelled.
func answers(e agentdomain.UserQuestionRequestedEvent, picked []agentdomain.UserQuestionAnswer, answered bool) ipc.UserQuestionResponse {
	response := ipc.UserQuestionResponse{ToolCallID: e.ToolCallID}
	if !answered {
		response.Cancelled = true
		return response
	}
	response.Answers, _ = json.Marshal(picked)
	return response
}
