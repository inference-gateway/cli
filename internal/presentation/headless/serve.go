package headless

import (
	"cmp"
	"context"
	"os"
	"time"

	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computer "github.com/inference-gateway/cli/internal/computer"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// serve runs the headless --serve worker: one agent run per run input read on
// stdin, each rendered as its own AG-UI run on stdout, until stdin closes and
// the queue is empty. Mid-turn messages stay queued for the running turn to
// drain, and panel frames are answered on stdout whenever they arrive.
func serve(ctx context.Context, svc Services, notifications uiBridge, turn agentdomain.AgentRequest, history []convdomain.ConversationEntry, notes *startupNotes) {
	ctl := newHeadlessControl(svc.GetAgentService(), svc.GetMessageQueue(), turn.RequestID)
	ctl.browser = newStdioBrowser(os.Stdout)
	ctl.panel = svc.NewPanel(os.Stdout)
	svc.RouteBrowserRequests(ctl.browser.Request)
	go ctl.readLines(os.Stdin)

	svc.GetTelemetryRecorder().SetConversationID(turn.RequestID)
	for ctl.awaitTurn() {
		runServeTurn(ctx, svc, ctl, notifications, turn, history, notes)
		history = nil
	}
}

// runServeTurn moves the queued messages into the conversation and renders one
// agent run over it as one AG-UI run. A run that fails to start still renders
// as RUN_STARTED then RUN_ERROR, so every turn keeps the one-run contract.
func runServeTurn(ctx context.Context, svc Services, ctl *headlessControl, notifications uiBridge, req agentdomain.AgentRequest, history []convdomain.ConversationEntry, notes *startupNotes) {
	repo := svc.GetConversationRepository()
	agentService := svc.GetAgentService()
	moveQueuedMessages(svc.GetMessageQueue(), repo)
	req.Messages = convdomain.BuildAgentMessagesFromEntries(repo.GetMessages())
	req.Model = cmp.Or(svc.GetModelService().GetCurrentModel(), req.Model)

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

	outcome := sessionOutcome(encoder.Finish())
	endSpan(outcome)
	rec.RecordSession("headless", outcome, time.Since(started))
}

// moveQueuedMessages persists every queued message into the conversation, the way
// the agent's own queue drain does mid-run, so the next run starts from them.
func moveQueuedMessages(queue convdomain.MessageQueue, repo convdomain.ConversationRepository) {
	for msg := queue.Dequeue(); msg != nil; msg = queue.Dequeue() {
		if err := repo.AddMessage(convdomain.ConversationEntry{Message: msg.Message, Time: time.Now()}); err != nil {
			logger.Warn("failed to persist a queued message", "error", err)
		}
	}
}
