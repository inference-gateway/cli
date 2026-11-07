package headless

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computer "github.com/inference-gateway/cli/internal/computer"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	directexec "github.com/inference-gateway/cli/internal/presentation/tui/directexec"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// serve runs the headless --serve worker: one agent run per run input read on
// stdin, each rendered as its own AG-UI run on stdout, until stdin closes and
// the queue is empty. Mid-turn messages stay queued for the running turn to
// drain, and panel frames are answered on stdout whenever they arrive.
func serve(ctx context.Context, svc Services, notifications uiBridge, turn agentdomain.AgentRequest, notes *startupNotes) {
	ctl := newHeadlessControl(svc.GetAgentService(), svc.GetMessageQueue(), newReferences(svc), turn.RequestID)
	ctl.browser = newStdioBrowser(os.Stdout)
	ctl.panel = svc.NewPanel(os.Stdout)
	ctl.panel.screen.wake = ctl.wakeTurns
	ctl.panel.screen.resolve = ctl.rejectComputerApprovals
	svc.SetUINotifier(drainWaker{UINotifier: notifications, drained: ctl.drained})
	svc.RouteBrowserRequests(ctl.browser.Request)
	go ctl.readLines(os.Stdin)

	svc.GetTelemetryRecorder().SetConversationID(turn.RequestID)
	for ctl.awaitTurn() {
		runServeTurn(ctx, svc, ctl, notifications, turn, notes)
	}
}

// runServeTurn moves the queued messages into the conversation and renders one
// agent run over it as one AG-UI run. A run that fails to start still renders
// as RUN_STARTED then RUN_ERROR, so every turn keeps the one-run contract. A
// plan the user accepted during the turn runs next, in a fresh session.
func runServeTurn(ctx context.Context, svc Services, ctl *headlessControl, notifications uiBridge, req agentdomain.AgentRequest, notes *startupNotes) {
	for {
		planID, execute, runErr := serveTurn(ctx, svc, ctl, notifications, req, notes)
		reportSubagentTurn(os.Stdout, svc.GetConversationRepository(), svc.GetMessageQueue(), req.RequestID, runErr)
		if !execute {
			return
		}
		if err := conversation.StartPlanExecution(svc.GetConversationRepository(), planID); err != nil {
			logger.Error("failed to start the accepted plan's session", "error", err, "plan_id", planID)
			return
		}
	}
}

// serveTurn renders the turn as one run, a direct call or an agent run, and
// returns the plan the user accepted during it and the error it ended with.
func serveTurn(ctx context.Context, svc Services, ctl *headlessControl, notifications uiBridge, req agentdomain.AgentRequest, notes *startupNotes) (string, bool, error) {
	repo := svc.GetConversationRepository()
	agentService := svc.GetAgentService()
	req.Model = cmp.Or(svc.GetModelService().GetCurrentModel(), req.Model)
	drained := ctl.nextTurn(ctx)
	if fn, direct, err := queuedDirectCall(drained); direct {
		task, _ := drained[0].Message.Content.AsMessageContent0()
		return "", false, runServeDirect(ctx, svc.GetToolService(), repo, svc.GetStateStore().GetAgentMode(), ctl, req, task, fn, err)
	}
	persistQueuedMessages(drained, repo)
	history := repo.GetMessages()
	req.Messages = convdomain.BuildAgentMessagesFromEntries(repo.GetMessages())

	rec := svc.GetTelemetryRecorder()
	started := time.Now()
	endSpan := rec.StartSession("headless")

	encoder := NewRunEncoder(os.Stdout, RunEncoderDeps{
		Model: req.Model, Repo: repo, History: history, Jobs: svc.GetBackgroundTaskRegistry().Snapshot,
		Approvals: ctl.approvals, Questions: ctl.questions, Plans: ctl.plans, Modes: svc.GetStateStore(),
		Interrupt: ctl, Startup: notes, Publish: []Publish{computer.PublishedEvent},
		SuggestNextStep: nextStepSuggestion(ctx, svc.GetMessageQueue(), svc.GetNextStepSuggestionRunner().GenerateNextStepSuggestion),
	})
	encoder.Start(liveThreadID(repo, req.RequestID), uuid.New().String())
	events, err := agentService.RunWithStream(ctx, &req)
	if err != nil {
		encoder.Handle(agentdomain.ChatErrorEvent{RequestID: req.RequestID, Timestamp: time.Now(), Error: err})
	} else {
		for event := range notifications.merge(events) {
			encoder.Handle(event)
		}
	}

	runErr := encoder.Finish()
	outcome := sessionOutcome(runErr)
	endSpan(outcome)
	rec.RecordSession("headless", outcome, time.Since(started))
	planID, execute := encoder.AcceptedPlan()
	return planID, execute, runErr
}

// nextStepSuggestion yields the turn's suggestion under the worker's context,
// and nothing when a queued message or a parent agent takes the next step.
func nextStepSuggestion(ctx context.Context, queue convdomain.MessageQueue, generate func(context.Context) string) func() string {
	return func() string {
		if !queue.IsEmpty() || os.Getenv(scheddomain.EnvSubagentParentSession) != "" {
			return ""
		}
		return generate(ctx)
	}
}

// liveThreadID is the conversation a run renders as its thread: the one the
// repo holds now, which a rollover or an accepted plan moves on from the
// session's first id. Cancellation keeps using the session id.
func liveThreadID(repo convdomain.ConversationRepository, sessionID string) string {
	return cmp.Or(repo.GetCurrentConversationID(), sessionID)
}

// reportSubagentTurn tells the parent Agent tool following this thread that a
// turn ended, as the CUSTOM subagent_turn event carrying what the keep-alive
// turn line carries. A worker no parent spawned reports nothing.
func reportSubagentTurn(w io.Writer, repo convdomain.ConversationRepository, queue convdomain.MessageQueue, sessionID string, runErr error) {
	if os.Getenv(scheddomain.EnvSubagentParentSession) == "" {
		return
	}
	line := scheddomain.SubagentTurnLine{Type: scheddomain.SubagentTurnLineType, SubagentResultFile: turnResult(repo, sessionID, runErr)}
	line.Done = queue.IsEmpty()
	agui.WriteCustom(w, scheddomain.SubagentTurnLineType, line)
}

// queuedDirectCall reports whether the drained turn is one `!cmd` or
// `!!Tool(...)` message, which runs through the tool service without a model
// the way a one-shot headless task does. Any other turn goes to the agent.
func queuedDirectCall(drained []convdomain.QueuedMessage) (fn sdk.ChatCompletionMessageToolCallFunction, direct bool, err error) {
	if len(drained) != 1 {
		return fn, false, nil
	}
	text, terr := drained[0].Message.Content.AsMessageContent0()
	if terr != nil {
		return fn, false, nil
	}
	return directCall(text)
}

// isDirectMessage reports whether a queued message is a `!cmd` or `!!Tool(...)`.
func isDirectMessage(message convdomain.QueuedMessage) bool {
	_, direct, _ := queuedDirectCall([]convdomain.QueuedMessage{message})
	return direct
}

// runServeDirect renders a direct task as one run of its own on the live
// conversation: the tool call and its result, or why the task cannot run. An
// interrupt stops the command. It returns the error the turn ended with.
func runServeDirect(ctx context.Context, toolService agentdomain.ToolService, repo convdomain.ConversationRepository, mode agentdomain.AgentMode, ctl *headlessControl, req agentdomain.AgentRequest, task string, fn sdk.ChatCompletionMessageToolCallFunction, err error) error {
	threadID := liveThreadID(repo, req.RequestID)
	if err == nil {
		err = directCallRefusal(toolService, mode, task, fn)
	}
	if err != nil {
		encoder := NewRunEncoder(os.Stdout, RunEncoderDeps{Model: req.Model, Repo: repo, Interrupt: ctl})
		encoder.Start(threadID, uuid.New().String())
		encoder.Handle(agentdomain.ChatErrorEvent{RequestID: req.RequestID, Timestamp: time.Now(), Error: err})
		return encoder.Finish()
	}
	ctx, release := ctl.interruptible(ctx)
	defer release()
	if err := runDirectCall(ctx, "ag-ui", toolService, repo, threadID, req.Model, nil, task, fn); err != nil {
		logger.Warn("direct task failed", "error", err, "session_id", req.RequestID)
		return err
	}
	return nil
}

// directCallRefusal says why a direct task cannot run: its tool is off, or a
// `!!Tool(...)` names one the agent mode does not allow, as the chat TUI refuses.
func directCallRefusal(toolService agentdomain.ToolService, mode agentdomain.AgentMode, task string, fn sdk.ChatCompletionMessageToolCallFunction) error {
	if !toolService.IsToolEnabled(fn.Name) {
		return fmt.Errorf("tool %s is not enabled", fn.Name)
	}
	if !isBashTask(task) && !directexec.ToolAvailableInMode(toolService, mode, fn.Name) {
		return fmt.Errorf("tool %s is not available in %s", fn.Name, mode.DisplayName())
	}
	return nil
}

// moveQueuedMessages persists every queued message into the conversation, the way
// the agent's own queue drain does mid-run, so the next run starts from them.
func moveQueuedMessages(ctx context.Context, queue convdomain.MessageQueue, repo convdomain.ConversationRepository) {
	persistQueuedMessages(drainQueue(ctx, queue), repo)
}

// drainQueue takes every queued message off the queue, logging a backend failure.
func drainQueue(ctx context.Context, queue convdomain.MessageQueue) []convdomain.QueuedMessage {
	drained, err := queue.Drain(ctx)
	if err != nil {
		logger.Warn("failed to drain the message queue", "error", err)
	}
	return drained
}

// persistQueuedMessages appends drained messages to the conversation in order.
func persistQueuedMessages(drained []convdomain.QueuedMessage, repo convdomain.ConversationRepository) {
	for _, msg := range drained {
		if err := repo.AddMessage(convdomain.ConversationEntry{Message: msg.Message, Time: time.Now()}); err != nil {
			logger.Warn("failed to persist a queued message", "error", err)
		}
	}
}
