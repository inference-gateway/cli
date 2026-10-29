package daemonthread

import (
	"context"
	"errors"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	headless "github.com/inference-gateway/cli/internal/presentation/headless"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// errNoEffort refuses a reasoning effort change the worker cannot take.
var errNoEffort = errors.New("the reasoning effort is set by the daemon worker's config in daemon mode")

// Agent is the chat TUI's agent in daemon mode: each turn runs on the thread's
// worker, wherever it runs, and comes back decoded into the chat events the
// TUI renders. The TUI's conversation is a mirror of the worker's, written as
// events arrive and reconciled from the worker when a turn ends.
type Agent struct {
	thread *Thread
	repo   convdomain.ConversationRepository
	queue  convdomain.MessageQueue
	modes  agentdomain.AgentModeState
	models convdomain.ModelService

	mu        sync.Mutex
	modeFor   string
	syncedFor agentdomain.AgentMode
}

// NewAgent builds the daemon-mode agent over thread, mirroring into repo.
func NewAgent(thread *Thread, repo convdomain.ConversationRepository, queue convdomain.MessageQueue, modes agentdomain.AgentModeState, models convdomain.ModelService) *Agent {
	return &Agent{thread: thread, repo: repo, queue: queue, modes: modes, models: models}
}

// runInputFrame starts a run with new messages or answers a suspended one.
type runInputFrame struct {
	Type  string   `json:"type"`
	Input runInput `json:"input"`
}

type runInput struct {
	ThreadID string         `json:"threadId,omitempty"`
	RunID    string         `json:"runId,omitempty"`
	Messages []inputMessage `json:"messages,omitempty"`
	Resume   []resumeEntry  `json:"resume,omitempty"`
}

type inputMessage struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type inputPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	Filename string `json:"filename,omitempty"`
}

type resumeEntry struct {
	InterruptID string `json:"interruptId"`
	Status      string `json:"status"`
	Payload     any    `json:"payload,omitempty"`
}

// RunWithStream sends the user's new messages to the thread, or with none
// follows the run another client started, and streams the run's events. The
// stream ends once the run finished and the conversation was reconciled.
func (a *Agent) RunWithStream(ctx context.Context, req *agentdomain.AgentRequest) (<-chan agentdomain.ChatEvent, error) {
	release := a.thread.Consume()
	sent, err := a.prepare(ctx)
	if err != nil {
		release()
		return nil, err
	}
	events := make(chan agentdomain.ChatEvent, 1000)
	go func() {
		defer release()
		defer close(events)
		a.pump(ctx, req.RequestID, sent, events)
	}()
	return events, nil
}

// prepare follows the conversation the TUI holds, syncs the agent mode and
// sends the messages the worker does not have yet, reporting whether it sent
// any.
func (a *Agent) prepare(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	id := a.repo.GetCurrentConversationID()
	if id == "" {
		return false, nil
	}
	if !a.thread.Follows(id) {
		if err := a.thread.Follow(ctx, id, a.threadOptions()); err != nil {
			return false, err
		}
	}
	if err := a.syncMode(ctx); err != nil {
		return false, err
	}
	a.moveQueued(ctx)
	messages := a.unsent(id)
	if len(messages) == 0 {
		return false, nil
	}
	a.thread.DiscardSettled()
	if err := a.thread.Send(runInputFrame{Type: "run_agent_input", Input: runInput{ThreadID: id, RunID: uuid.NewString(), Messages: messages}}); err != nil {
		return false, err
	}
	a.thread.SetSynced(id, a.repo.GetMessageCount())
	return true, nil
}

// threadOptions are the options a thread the TUI opens starts its worker with.
func (a *Agent) threadOptions() sessionsdomain.ThreadOptions {
	return sessionsdomain.ThreadOptions{Model: a.models.GetCurrentModel(), Mode: a.modes.GetAgentMode().ModeKey()}
}

// syncMode tells the followed thread's worker the TUI's agent mode when it
// changed or the thread did.
func (a *Agent) syncMode(ctx context.Context) error {
	mode, id := a.modes.GetAgentMode(), a.thread.Live()
	a.mu.Lock()
	synced := a.modeFor == id && a.syncedFor == mode
	a.mu.Unlock()
	if synced {
		return nil
	}
	frame := map[string]string{"type": "set_mode", "mode": mode.ModeKey(), "project_dir": a.thread.opts.ProjectDir}
	if _, err := a.thread.Request(ctx, frame, "mode"); err != nil {
		return err
	}
	a.mu.Lock()
	a.modeFor, a.syncedFor = id, mode
	a.mu.Unlock()
	return nil
}

// moveQueued moves the messages the user queued while the TUI was busy into
// the conversation, so the turn sends them.
func (a *Agent) moveQueued(ctx context.Context) {
	drained, err := a.queue.Drain(ctx)
	if err != nil {
		logger.Warn("daemon chat could not drain the message queue", "error", err)
		return
	}
	for _, queued := range drained {
		if err := a.repo.AddMessage(convdomain.ConversationEntry{Message: queued.Message, Time: time.Now()}); err != nil {
			logger.Warn("daemon chat could not add a queued message", "error", err)
		}
	}
}

// unsent is the user's messages of conversation id the worker does not have.
func (a *Agent) unsent(id string) []inputMessage {
	entries := a.repo.GetMessages()
	synced := min(a.thread.Synced(id), len(entries))
	var messages []inputMessage
	for _, entry := range entries[synced:] {
		if entry.Message.Role == sdk.User {
			messages = append(messages, inputMessage{ID: uuid.NewString(), Role: string(sdk.User), Content: userContent(entry)})
		}
	}
	return messages
}

// userContent is a user entry as run input content: its text, with its
// images as parts when it carries any.
func userContent(entry convdomain.ConversationEntry) any {
	text := formatting.ExtractTextFromContent(entry.Message.Content, nil)
	if len(entry.Images) == 0 {
		return text
	}
	parts := []inputPart{{Type: "text", Text: text}}
	for _, image := range entry.Images {
		parts = append(parts, inputPart{Type: "image", MimeType: image.MimeType, Data: image.Data, Filename: image.Filename})
	}
	return parts
}

// pump decodes the run frames into events, mirrors them and answers the
// prompts the run suspends on, until the run ends. A follow-only request with
// nothing running ends at once.
func (a *Agent) pump(ctx context.Context, requestID string, sent bool, out chan<- agentdomain.ChatEvent) {
	if !sent && !a.thread.Running() && !a.thread.Pending() {
		a.reconcile()
		return
	}
	decoder := headless.NewRunDecoder(requestID)
	for {
		frame, err := a.thread.Next(ctx)
		if err != nil {
			out <- agentdomain.ChatErrorEvent{RequestID: requestID, Timestamp: time.Now(), Error: err}
			return
		}
		switch Marker(frame) {
		case markerOffline:
			continue
		case markerReconnected:
			if !a.thread.Running() {
				a.reconcile()
				return
			}
			continue
		}
		events, end := decoder.Decode(frame)
		for _, event := range events {
			a.mirror(event)
		}
		if end == headless.RunFinished || end == headless.RunCancelled || end == headless.RunFailed {
			a.reconcile()
		}
		for _, event := range events {
			out <- event
		}
		switch end {
		case headless.RunSuspended:
			go a.answer(decoder.Prompts())
		case headless.RunFinished, headless.RunCancelled, headless.RunFailed:
			return
		}
	}
}

// mirror writes what an event adds to the conversation the way the
// in-process agent would, so the TUI renders it before the reconcile.
func (a *Agent) mirror(event agentdomain.ChatEvent) {
	var entries []convdomain.ConversationEntry
	switch e := event.(type) {
	case agentdomain.ChatCompleteEvent:
		if e.Cancelled || e.Message == "" && e.ReasoningContent == "" && len(e.ToolCalls) == 0 {
			return
		}
		entry := convdomain.ConversationEntry{
			Message:          sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent(e.Message)},
			ReasoningContent: e.ReasoningContent, Model: a.models.GetCurrentModel(), Time: time.Now(),
		}
		if len(e.ToolCalls) > 0 {
			calls := e.ToolCalls
			entry.Message.ToolCalls = &calls
		}
		entries = append(entries, entry)
	case agentdomain.ToolExecutionCompletedEvent:
		for _, result := range e.Results {
			a.repo.RemovePendingToolCallByID(result.ToolCallID)
			id := result.ToolCallID
			entries = append(entries, convdomain.ConversationEntry{
				Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent(a.repo.FormatToolResultForLLM(result)), ToolCallID: &id},
				ToolExecution: result, Time: time.Now(),
			})
		}
	case agentdomain.MessageQueuedEvent:
		entries = append(entries, convdomain.ConversationEntry{Message: e.Message, Time: time.Now()})
	}
	for _, entry := range entries {
		if err := a.repo.AddMessage(entry); err != nil {
			logger.Warn("daemon chat could not mirror an entry", "error", err)
		}
	}
	a.thread.SetSynced(a.repo.GetCurrentConversationID(), a.repo.GetMessageCount())
}

// reconcile replaces the mirror with the conversation as the worker stored
// it and has the TUI render it. A rollover or an accepted plan moved the
// thread on to the conversation its last run named.
func (a *Agent) reconcile() {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	id := a.thread.Live()
	if id == "" {
		return
	}
	if err := a.repo.LoadConversation(ctx, id); err != nil {
		logger.Warn("daemon chat could not reconcile the conversation", "conversation_id", id, "error", err)
	}
	a.thread.SetSynced(id, a.repo.GetMessageCount())
	a.thread.post(tui.UpdateHistoryEvent{History: a.repo.GetMessages()})
}

// answer waits for the user's answer to every prompt the run suspended on and
// resumes the run with them, after the agent mode an answer may have changed.
func (a *Agent) answer(prompts []headless.Prompt) {
	entries := make([]resumeEntry, 0, len(prompts))
	for _, prompt := range prompts {
		entries = append(entries, await(prompt))
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if err := a.syncMode(ctx); err != nil {
		logger.Warn("daemon chat could not sync the agent mode before resuming", "error", err)
	}
	if err := a.thread.Send(runInputFrame{Type: "run_agent_input", Input: runInput{Resume: entries}}); err != nil {
		logger.Warn("daemon chat could not resume the run", "error", err)
	}
}

// await turns the user's answer to one prompt into its resume entry. A
// dismissed prompt, or one the TUI cannot show, is cancelled.
func await(prompt headless.Prompt) resumeEntry {
	cancelled := resumeEntry{InterruptID: prompt.InterruptID, Status: "cancelled"}
	resolved := resumeEntry{InterruptID: prompt.InterruptID, Status: "resolved"}
	switch e := prompt.Event.(type) {
	case agentdomain.ToolApprovalRequestedEvent:
		if action, ok := <-e.ResponseChan; ok && action != agentdomain.ApprovalReject {
			return resolved
		}
	case agentdomain.UserQuestionRequestedEvent:
		if answers, ok := <-e.ResponseChan; ok {
			resolved.Payload = map[string]any{"answers": answers}
			return resolved
		}
	case agentdomain.PlanApprovalRequestedEvent:
		if action, ok := <-e.ResponseChan; ok {
			resolved.Payload = map[string]any{"action": planAction(action)}
			return resolved
		}
	}
	return cancelled
}

// planAction is the wire name of a plan decision.
func planAction(action agentdomain.PlanApprovalAction) string {
	switch action {
	case agentdomain.PlanApprovalAccept:
		return ipc.PlanAccept
	case agentdomain.PlanApprovalAcceptStandard:
		return ipc.PlanAcceptStandard
	}
	return ipc.PlanReject
}

// CancelRequest interrupts the thread's open run.
func (a *Agent) CancelRequest(string) error {
	return a.thread.Send(map[string]string{"type": "interrupt"})
}

func (a *Agent) GetMetrics(string) *agentdomain.ChatMetrics { return nil }

func (a *Agent) BuildSystemPrompt() string { return "" }

func (a *Agent) SetReasoningEffort(string) error { return errNoEffort }

func (a *Agent) GetReasoningEffort() string { return "" }
