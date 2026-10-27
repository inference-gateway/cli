package headless

import (
	"cmp"
	"context"
	"io"
	"os"
	"time"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computer "github.com/inference-gateway/cli/internal/computer"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// serve runs the headless --serve worker: one agent run per run input read on
// stdin, each rendered as its own AG-UI run on stdout, until stdin closes and
// the queue is empty. Mid-turn messages stay queued for the running turn to
// drain, and panel frames are answered on stdout whenever they arrive.
func serve(ctx context.Context, svc Services, notifications uiBridge, turn agentdomain.AgentRequest, notes *startupNotes) {
	ctl := newHeadlessControl(svc.GetAgentService(), svc.GetMessageQueue(), turn.RequestID)
	ctl.browser = newStdioBrowser(os.Stdout)
	ctl.panel = svc.NewPanel(os.Stdout)
	svc.RouteBrowserRequests(ctl.browser.Request)
	go ctl.readLines(os.Stdin)

	svc.GetTelemetryRecorder().SetConversationID(turn.RequestID)
	for ctl.awaitTurn() {
		runServeTurn(ctx, svc, ctl, notifications, turn, notes)
	}
}

// runServeTurn moves the queued messages into the conversation and renders one
// agent run over it as one AG-UI run. A run that fails to start still renders
// as RUN_STARTED then RUN_ERROR, so every turn keeps the one-run contract.
func runServeTurn(ctx context.Context, svc Services, ctl *headlessControl, notifications uiBridge, req agentdomain.AgentRequest, notes *startupNotes) {
	repo := svc.GetConversationRepository()
	agentService := svc.GetAgentService()
	req.Model = cmp.Or(svc.GetModelService().GetCurrentModel(), req.Model)
	drained := drainQueue(ctx, svc.GetMessageQueue())
	if fn, direct, err := queuedDirectCall(drained); direct {
		reportSubagentTurn(os.Stdout, svc.GetConversationRepository(), svc.GetMessageQueue(), req.RequestID, runServeDirect(ctx, svc, ctl, req, fn, err))
		return
	}
	persistQueuedMessages(drained, repo)
	history := repo.GetMessages()
	req.Messages = convdomain.BuildAgentMessagesFromEntries(repo.GetMessages())

	rec := svc.GetTelemetryRecorder()
	started := time.Now()
	endSpan := rec.StartSession("headless")

	encoder := NewRunEncoder(os.Stdout, RunEncoderDeps{
		Model: req.Model, Repo: repo, History: history, Jobs: svc.GetBackgroundTaskRegistry().Snapshot,
		Approvals: ctl.approvals, Questions: ctl.questions,
		Interrupt: ctl, Startup: notes, Publish: []Publish{computer.PublishedEvent},
	})
	encoder.Start(req.RequestID, uuid.New().String())
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
	reportSubagentTurn(os.Stdout, svc.GetConversationRepository(), svc.GetMessageQueue(), req.RequestID, runErr)
}

// reportSubagentTurn tells the parent Agent tool following this thread that a
// turn ended, as the CUSTOM subagent_turn event carrying what the keep-alive
// turn line carries. A worker no parent spawned reports nothing.
func reportSubagentTurn(w io.Writer, repo convdomain.ConversationRepository, queue convdomain.MessageQueue, sessionID string, runErr error) {
	if os.Getenv(parentSessionEnv) == "" {
		return
	}
	line := scheddomain.SubagentTurnLine{Type: scheddomain.SubagentTurnLineType, SubagentResultFile: turnResult(repo, sessionID, runErr)}
	line.Done = queue.IsEmpty()
	agui.WriteCustom(w, scheddomain.SubagentTurnLineType, line)
}

// parentSessionEnv names the session that spawned this worker as a subagent.
const parentSessionEnv = "INFER_PARENT_SESSION_ID"

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

// runServeDirect renders a direct task as one run of its own: the tool call
// and its result, or the parse error the task failed on. It returns the error
// the turn ended with.
func runServeDirect(ctx context.Context, svc Services, ctl *headlessControl, req agentdomain.AgentRequest, fn sdk.ChatCompletionMessageToolCallFunction, err error) error {
	repo := svc.GetConversationRepository()
	if err != nil {
		encoder := NewRunEncoder(os.Stdout, RunEncoderDeps{Model: req.Model, Repo: repo, Interrupt: ctl})
		encoder.Start(req.RequestID, uuid.New().String())
		encoder.Handle(agentdomain.ChatErrorEvent{RequestID: req.RequestID, Timestamp: time.Now(), Error: err})
		return encoder.Finish()
	}
	if err := runDirectCall(ctx, "ag-ui", svc.GetToolService(), repo, req.RequestID, req.Model, nil, fn); err != nil {
		logger.Warn("direct task failed", "error", err, "session_id", req.RequestID)
		return err
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
