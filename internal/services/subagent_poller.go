package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// PaneInspector probes an interactive subagent without importing the tools
// package (which would create an import cycle), returning a domain.PaneObservation
// (result-file message, pane liveness, and pending-approval signal). The concrete
// tmux+sidecar implementation is constructed in package agent (tools.NewPaneInspector)
// and injected via SetPaneInspector.
type PaneInspector func(ctx context.Context, paneID, sessionID string) domain.PaneObservation

// SubagentPoller drives the per-subagent monitor goroutines that watch
// in-flight local subagents (spawned by the Agent tool) for completion and
// emit their results onto the shared message queue. It is the behavior layer
// over the data held in domain.SubagentTracker - the subagent analogue of
// A2ATaskPoller.
type SubagentPoller struct {
	tracker          domain.SubagentTracker
	eventChan        chan<- domain.ChatEvent
	messageQueue     domain.MessageQueue
	requestID        string
	conversationRepo domain.ConversationRepository
	mu               sync.RWMutex
	activeMonitors   map[string]context.CancelFunc
	stopChan         chan struct{}
	stopped          bool
	agentEventChan   chan<- domain.AgentEvent
	paneInspector    PaneInspector
	// notifiedApprovals records, per subagent id, the approval summary already
	// surfaced to the main agent - so a pending approval is announced once (not
	// every poll) and a fresh approval re-announces. It lives on the poller (not a
	// monitor goroutine) so it survives a monitor re-spawn after SendSubagentInput.
	notifiedApprovals map[string]string

	// Interactive-pane completion-heuristic tunables (overridable in tests).
	interactivePollInterval time.Duration
	interactiveGrace        time.Duration
	interactiveStableNeeded int
}

// NewSubagentPoller constructs a poller over the given subagent tracker.
func NewSubagentPoller(
	tracker domain.SubagentTracker,
	eventChan chan<- domain.ChatEvent,
	messageQueue domain.MessageQueue,
	requestID string,
	conversationRepo domain.ConversationRepository,
) *SubagentPoller {
	return &SubagentPoller{
		tracker:                 tracker,
		eventChan:               eventChan,
		messageQueue:            messageQueue,
		requestID:               requestID,
		conversationRepo:        conversationRepo,
		activeMonitors:          make(map[string]context.CancelFunc),
		notifiedApprovals:       make(map[string]string),
		stopChan:                make(chan struct{}),
		interactivePollInterval: 2 * time.Second,
		interactiveGrace:        4 * time.Second,
		interactiveStableNeeded: 3,
	}
}

// SetAgentEventChannel registers the agent's event channel so the poller can
// wake the agent loop when a subagent terminates. nil disables the wake-up.
func (p *SubagentPoller) SetAgentEventChannel(ch chan<- domain.AgentEvent) {
	p.mu.Lock()
	p.agentEventChan = ch
	p.mu.Unlock()
}

// SetPaneInspector enables watching interactive subagents for completion by
// polling their tmux panes. It is set only in chat mode (headless one-shot runs
// leave it nil, so interactive subagents are not watched there - they are
// fire-and-forget and must not block the CLI from exiting). Call before Start.
func (p *SubagentPoller) SetPaneInspector(fn PaneInspector) {
	p.mu.Lock()
	p.paneInspector = fn
	p.mu.Unlock()
}

// Start runs the polling loop until ctx is cancelled or Stop is called.
func (p *SubagentPoller) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.stopAllMonitors()
			return
		case <-p.stopChan:
			p.stopAllMonitors()
			return
		case <-ticker.C:
			p.checkForNewSubagents(ctx)
		}
	}
}

// Stop terminates the polling loop.
func (p *SubagentPoller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopped {
		p.stopped = true
		close(p.stopChan)
	}
}

func (p *SubagentPoller) checkForNewSubagents(ctx context.Context) {
	if p.tracker == nil {
		return
	}
	p.mu.RLock()
	canWatchInteractive := p.paneInspector != nil
	p.mu.RUnlock()

	for _, state := range p.tracker.GetAllSubagents() {
		if state.Mode == domain.SubagentModeInteractive {
			if !canWatchInteractive || state.Status != domain.SubagentRunning {
				continue
			}
		}

		p.mu.RLock()
		_, monitoring := p.activeMonitors[state.ID]
		p.mu.RUnlock()
		if monitoring {
			continue
		}
		p.monitor(ctx, state)
	}
}

func (p *SubagentPoller) monitor(ctx context.Context, state *domain.SubagentState) {
	p.mu.Lock()
	if _, exists := p.activeMonitors[state.ID]; exists {
		p.mu.Unlock()
		return
	}
	monitorCtx, cancel := context.WithCancel(ctx)
	p.activeMonitors[state.ID] = cancel
	p.mu.Unlock()

	go p.monitorSingle(monitorCtx, state)
}

func (p *SubagentPoller) monitorSingle(ctx context.Context, state *domain.SubagentState) {
	defer func() {
		p.mu.Lock()
		delete(p.activeMonitors, state.ID)
		p.mu.Unlock()
	}()

	p.emitSubmitted(state)

	if state.Mode == domain.SubagentModeInteractive {
		p.monitorInteractive(ctx, state)
		return
	}

	select {
	case <-ctx.Done():
		return

	case result := <-state.ResultChan:
		p.finish(state, result)

	case err := <-state.ErrorChan:
		errorMsg := ""
		if err != nil {
			errorMsg = err.Error()
		}
		p.finish(state, &domain.ToolExecutionResult{
			ToolName: "Agent",
			Success:  false,
			Error:    errorMsg,
		})
	}
}

// finish delivers the subagent outcome: enqueue the result onto the
// conversation, emit the completion/failure event, wake the agent loop, and
// remove the subagent from tracking.
func (p *SubagentPoller) finish(state *domain.SubagentState, result *domain.ToolExecutionResult) {
	if result == nil {
		result = &domain.ToolExecutionResult{ToolName: "Agent", Success: false, Error: "subagent produced no result"}
	}

	if !state.Silent {
		p.addResultToMessageQueue(state, result)
	}
	p.emitCompletion(state, result)
	_ = p.tracker.RemoveSubagent(state.ID)
}

func (p *SubagentPoller) emitSubmitted(state *domain.SubagentState) {
	p.emit(domain.SubagentSubmittedEvent{
		RequestID:  p.requestID,
		Timestamp:  time.Now(),
		SubagentID: state.ID,
		Label:      state.Label,
	})
}

func (p *SubagentPoller) emitCompletion(state *domain.SubagentState, result *domain.ToolExecutionResult) {
	if result.Success {
		p.emit(domain.SubagentCompletedEvent{
			RequestID:  p.requestID,
			Timestamp:  time.Now(),
			SubagentID: state.ID,
			Label:      state.Label,
			Result:     *result,
		})
		return
	}
	p.emit(domain.SubagentFailedEvent{
		RequestID:  p.requestID,
		Timestamp:  time.Now(),
		SubagentID: state.ID,
		Label:      state.Label,
		Result:     *result,
		Error:      result.Error,
	})
}

func (p *SubagentPoller) emit(event domain.ChatEvent) {
	if p.eventChan == nil {
		return
	}
	select {
	case p.eventChan <- event:
	default:
		logger.Warn("dropped subagent UI event - chat event channel full", "event", fmt.Sprintf("%T", event))
	}
}

// addResultToMessageQueue lands the subagent result on the shared message
// queue (as a user-role message the model will read on its next turn) and
// wakes the agent event loop.
func (p *SubagentPoller) addResultToMessageQueue(state *domain.SubagentState, result *domain.ToolExecutionResult) {
	if result == nil || p.messageQueue == nil {
		return
	}

	formatted := result.Error
	if p.conversationRepo != nil {
		formatted = p.conversationRepo.FormatToolResultForLLM(result)
	}

	label := state.Label
	if label == "" {
		label = state.ID
	}
	verb := "Completed"
	if !result.Success {
		verb = "Failed"
	}
	p.enqueueAndWake(state, fmt.Sprintf("[Subagent %s: %s]\n\n%s", verb, label, formatted))
}

// enqueueAndWake lands a user-role message on the shared queue (the model reads
// it on its next turn), emits the UI event, and wakes the agent event loop.
func (p *SubagentPoller) enqueueAndWake(state *domain.SubagentState, content string) {
	if p.messageQueue == nil {
		return
	}
	message := sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(content),
	}
	p.messageQueue.Enqueue(message, p.requestID)

	p.emit(domain.MessageQueuedEvent{
		RequestID: p.requestID,
		Timestamp: time.Now(),
		Message:   message,
	})

	p.mu.RLock()
	agentCh := p.agentEventChan
	p.mu.RUnlock()
	if agentCh != nil {
		select {
		case agentCh <- domain.MessageReceivedEvent{}:
		default:
			logger.Warn("agent event channel full - subagent wake-up dropped", "subagent_id", state.ID)
		}
	}
}

// monitorInteractive watches an interactive subagent's tmux pane and notifies
// the main agent when the subagent finishes its turn - so the agent waits for
// the notification instead of polling. Detection is heuristic and costs no LLM
// tokens (it polls tmux): after a short grace window (so the launch-time input
// prompt is not mistaken for completion), the subagent is "done" when the pane
// is idle (back at the input prompt, or its process exited) AND its output has
// been unchanged across several consecutive polls; or when the pane is gone.
func (p *SubagentPoller) monitorInteractive(ctx context.Context, state *domain.SubagentState) {
	p.mu.RLock()
	inspect := p.paneInspector
	p.mu.RUnlock()
	if inspect == nil {
		return
	}

	ticker := time.NewTicker(p.interactivePollInterval)
	defer ticker.Stop()

	stableTicks := 0
	prevScreen := ""
	started := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			obs := inspect(ctx, state.PaneID, state.SessionID)
			if obs.Harvested != "" {
				p.finishInteractive(state, obs.Harvested)
				return
			}

			if obs.Gone || obs.Dead {
				p.finishInteractive(state, "")
				return
			}

			p.handleApprovalSignal(state, obs)
			if obs.AwaitingApproval {
				stableTicks = 0
				prevScreen = obs.Screen
				continue
			}
			if time.Since(started) < p.interactiveGrace {
				prevScreen = obs.Screen
				continue
			}

			if obs.Screen == prevScreen {
				stableTicks++
			} else {
				stableTicks = 0
			}
			prevScreen = obs.Screen
			if stableTicks >= p.interactiveStableNeeded {
				p.finishInteractive(state, "")
				return
			}
		}
	}
}

// handleApprovalSignal notifies the main agent (once per distinct approval) when
// an interactive subagent is blocked on a tool-approval prompt, and forgets the
// record when the prompt clears so a later approval re-notifies. The notify-once
// state lives on the poller (not this goroutine) so it survives a monitor
// re-spawn triggered by SendSubagentInput.
func (p *SubagentPoller) handleApprovalSignal(state *domain.SubagentState, obs domain.PaneObservation) {
	p.mu.Lock()
	if !obs.AwaitingApproval {
		delete(p.notifiedApprovals, state.ID)
		p.mu.Unlock()
		return
	}
	if p.notifiedApprovals[state.ID] == obs.ApprovalSummary {
		p.mu.Unlock()
		return
	}
	p.notifiedApprovals[state.ID] = obs.ApprovalSummary
	p.mu.Unlock()

	label := state.Label
	if label == "" {
		label = state.ID
	}
	content := fmt.Sprintf("[Subagent Awaiting Approval: %s]", label)
	if summary := strings.TrimSpace(obs.ApprovalSummary); summary != "" {
		content += "\n\n" + summary
	}
	content += fmt.Sprintf("\n\nThis subagent is blocked waiting to run the above. Review it, then respond with ApproveSubagent(subagent_id=%q, decision=\"approve\") or decision=\"reject\".", state.ID)
	p.enqueueAndWake(state, content)
}

// finishInteractive notifies the main agent that an interactive subagent has
// finished its turn, folding its last assistant message into the conversation.
// It marks the subagent completed but KEEPS the record and pane so the user can
// read it and CloseSubagent can clean it up later; checkForNewSubagents skips
// non-running subagents, so a completed one is never re-watched or re-notified.
// message is the subagent's real last assistant message, or "" when none was
// harvestable - in which case the notification is a bare header (no pane chrome).
func (p *SubagentPoller) finishInteractive(state *domain.SubagentState, message string) {
	if p.tracker == nil || p.tracker.GetSubagent(state.ID) == nil {
		return
	}
	_ = p.tracker.SetSubagentStatus(state.ID, domain.SubagentCompleted)

	label := state.Label
	if label == "" {
		label = state.ID
	}
	content := fmt.Sprintf("[Subagent Completed: %s]", label)
	if body := strings.TrimSpace(message); body != "" {
		content += "\n\n" + body
	} else {
		content += "\n\n(No final message was captured - the subagent ended its turn without output or is waiting for input. Use ReadSubagentScreen to inspect it, SendSubagentInput to re-prompt it, or CloseSubagent to stop it. Do not assume it failed or produced nothing.)"
	}
	p.enqueueAndWake(state, content)
	p.emitCompletion(state, &domain.ToolExecutionResult{ToolName: "Agent", Success: true})
}

func (p *SubagentPoller) stopAllMonitors() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, cancel := range p.activeMonitors {
		cancel()
	}
	p.activeMonitors = make(map[string]context.CancelFunc)

	if p.tracker != nil {
		for _, state := range p.tracker.GetAllSubagents() {
			if state.CancelFunc != nil {
				state.CancelFunc()
			}
		}
	}
}
