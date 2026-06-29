package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

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
		tracker:          tracker,
		eventChan:        eventChan,
		messageQueue:     messageQueue,
		requestID:        requestID,
		conversationRepo: conversationRepo,
		activeMonitors:   make(map[string]context.CancelFunc),
		stopChan:         make(chan struct{}),
	}
}

// SetAgentEventChannel registers the agent's event channel so the poller can
// wake the agent loop when a subagent terminates. nil disables the wake-up.
func (p *SubagentPoller) SetAgentEventChannel(ch chan<- domain.AgentEvent) {
	p.mu.Lock()
	p.agentEventChan = ch
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
	for _, state := range p.tracker.GetAllSubagents() {
		// Interactive subagents are managed synchronously by the ListSubagents /
		// GetSubagentResult / CloseSubagent tools and have no ResultChan-based
		// completion. Monitoring them here would re-emit a "submitted" event every
		// turn and block a monitor goroutine on a channel nobody sends to.
		if state.Mode == domain.SubagentModeInteractive {
			continue
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

	// Silent (synchronous wait-all) subagents are tracked only to drive the
	// live tree; the Agent tool returns their aggregated result directly, so
	// don't also inject it onto the conversation queue.
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
	content := fmt.Sprintf("[Subagent %s: %s]\n\n%s", verb, label, formatted)

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
