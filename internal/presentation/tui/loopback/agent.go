package loopback

import (
	"context"
	"encoding/json"
	"slices"

	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	render "github.com/inference-gateway/cli/internal/platform/render"
	headless "github.com/inference-gateway/cli/internal/presentation/headless"
	runview "github.com/inference-gateway/cli/internal/presentation/tui/runview"
)

// Agent is the chat TUI's agent for a local chat rendered through AG-UI: the
// in-process agent's events are written as a run by the encoder the serve
// worker uses and read back by the reducer a daemon chat uses, so both chats
// share one mapping. Everything but the run stream goes straight to the agent.
type Agent struct {
	agentdomain.AgentService
	repo    convdomain.ConversationRepository
	publish []headless.Publish
}

// New wraps agent, whose runs the TUI then reads back from AG-UI frames.
func New(agent agentdomain.AgentService, repo convdomain.ConversationRepository, publish ...headless.Publish) *Agent {
	return &Agent{AgentService: agent, repo: repo, publish: publish}
}

// RunWithStream runs the request on the wrapped agent and streams what its
// AG-UI run reduces to. The stream closes once the run wrote its terminal event.
func (a *Agent) RunWithStream(ctx context.Context, req *agentdomain.AgentRequest) (<-chan agentdomain.ChatEvent, error) {
	events, err := a.AgentService.RunWithStream(ctx, req)
	if err != nil {
		return nil, err
	}
	frames := &frames{
		ctx:       ctx,
		out:       make(chan agentdomain.ChatEvent, 1000),
		reducer:   runview.New(req.RequestID),
		approvals: make(chan ipc.ApprovalResponse, 1),
		questions: make(chan ipc.UserQuestionResponse, 1),
		plans:     make(chan ipc.PlanApprovalResponse, 1),
	}
	encoder := headless.NewRunEncoder(frames, headless.RunEncoderDeps{
		Model: req.Model, Repo: a.repo, Publish: a.publish,
		Approvals: frames.approvals, Questions: frames.questions, Plans: frames.plans,
	})
	go func() {
		defer close(frames.out)
		encoder.Start(a.repo.GetCurrentConversationID(), uuid.New().String())
		for event := range events {
			encoder.Handle(event)
		}
		_ = encoder.Finish()
	}()
	return frames.out, nil
}

// frames is where the encoder writes one run: each frame is reduced as it is
// written, and a suspended run is resumed with the user's answers.
type frames struct {
	ctx       context.Context
	out       chan agentdomain.ChatEvent
	reducer   *runview.Reducer
	approvals chan ipc.ApprovalResponse
	questions chan ipc.UserQuestionResponse
	plans     chan ipc.PlanApprovalResponse
}

// Write takes one frame the encoder wrote and hands the TUI what it reduces to.
func (f *frames) Write(frame []byte) (int, error) {
	events, end := f.reducer.Decode(frame)
	for _, event := range events {
		select {
		case f.out <- event:
		case <-f.ctx.Done():
			return len(frame), nil
		}
	}
	if end == runview.Suspended {
		go f.answer(slices.Clone(f.reducer.Prompts()))
	}
	return len(frame), nil
}

// answer waits for the user's answer to every prompt the run suspended on and
// hands each to the encoder, which resumes the run with it.
func (f *frames) answer(prompts []runview.Prompt) {
	for _, prompt := range prompts {
		switch e := prompt.Event.(type) {
		case agentdomain.ToolApprovalRequestedEvent:
			f.approvals <- approval(e)
		case agentdomain.UserQuestionRequestedEvent:
			f.questions <- answers(e)
		case agentdomain.PlanApprovalRequestedEvent:
			action, ok := <-e.ResponseChan
			if !ok {
				action = agentdomain.PlanApprovalReject
			}
			f.plans <- ipc.PlanApprovalResponse{PlanID: e.PlanID, Action: render.PlanActionName(action)}
		}
	}
}

// approval is the user's decision on a tool call in its wire form. A prompt
// the TUI dismissed rejects the call.
func approval(e agentdomain.ToolApprovalRequestedEvent) ipc.ApprovalResponse {
	response := ipc.ApprovalResponse{ToolCallID: e.ToolCall.ID}
	action, ok := <-e.ResponseChan
	if !ok {
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
func answers(e agentdomain.UserQuestionRequestedEvent) ipc.UserQuestionResponse {
	response := ipc.UserQuestionResponse{ToolCallID: e.ToolCallID}
	picked, ok := <-e.ResponseChan
	if !ok {
		response.Cancelled = true
		return response
	}
	response.Answers, _ = json.Marshal(picked)
	return response
}
