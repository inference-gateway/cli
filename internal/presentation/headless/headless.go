package headless

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computer "github.com/inference-gateway/cli/internal/computer"
	computerinfra "github.com/inference-gateway/cli/internal/computer/infrastructure"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	gateway "github.com/inference-gateway/cli/internal/gateway"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	models "github.com/inference-gateway/cli/internal/platform/models"
	render "github.com/inference-gateway/cli/internal/platform/render"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	mcpdomain "github.com/inference-gateway/cli/internal/protocols/mcp/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// fileRefPattern matches @file references in the task description.
var fileRefPattern = regexp.MustCompile(`@([^\s]+)`)

// Services is the slice of the composition root a headless run uses.
// cmd/headless supplies the *container.ServiceContainer.
type Services interface {
	RouteBrowserRequests(request func(ctx context.Context, id string, frame json.RawMessage) (json.RawMessage, error))
	SetUINotifier(n agentdomain.UINotifier)
	Shutdown(ctx context.Context) error
	StartScreenshotServer(sessionID string) *computerinfra.ScreenshotServer
	GetGatewaySupervisor() *gateway.Supervisor
	GetAgentSupervisor() a2adomain.AgentSupervisor
	GetAgentService() agentdomain.AgentService
	GetMCPSupervisor() mcpdomain.Supervisor
	GetToolRegistry() *tools.Registry
	GetToolService() agentdomain.ToolService
	GetFileService() agentdomain.FileService
	GetImageService() agentdomain.ImageService
	GetModelService() convdomain.ModelService
	GetConversationRepository() convdomain.ConversationRepository
	GetMessageQueue() convdomain.MessageQueue
	GetSessionRollover() convdomain.SessionRollover
	GetStateStore() *statemanager.Store
	GetShortcutRegistry() *shortcuts.Registry
	GetBackgroundTaskRegistry() scheddomain.BackgroundTaskRegistry
	GetTelemetryRecorder() *telemetry.Recorder
	NewPanel(out io.Writer) *Panel
}

// Options carries the headless command's flag values.
type Options struct {
	Model           string
	Task            string
	Files           []string
	NoSave          bool
	SessionID       string
	RequireApproval bool
	Heartbeat       bool
	Remote          bool
	ResultFile      string
	Format          string
	Mode            string
	Serve           bool
	KeepAlive       bool
}

// resolveAgentMode picks the coding mode for a headless run: the --mode flag
// wins, then INFER_AGENT_MODE, then the subagent-inherited mode
// (INFER_SUBAGENT_AGENT_MODE). An invalid --mode is a hard error; unparseable
// env vars fall through to the next source.
func resolveAgentMode(flag string) (agentdomain.AgentMode, error) {
	if strings.TrimSpace(flag) != "" {
		mode, ok := agentdomain.ParseAgentMode(flag)
		if !ok {
			return agentdomain.AgentModeStandard, fmt.Errorf("invalid --mode %q: must be one of standard, plan, auto, auto-with-judge", flag)
		}
		return mode, nil
	}
	if mode, ok := agentdomain.ParseAgentMode(os.Getenv("INFER_AGENT_MODE")); ok {
		return mode, nil
	}
	return scheddomain.InheritedAgentMode(), nil
}

// Run executes one headless task. newServices builds the composition root; it
// is called only after the flags validate, so a bad --format or --mode never
// starts any service.
func Run(cfg *config.Config, opts Options, newServices func() Services) (err error) { //nolint:gocyclo,cyclop,funlen,gocognit
	if err := validateOptions(opts); err != nil {
		return err
	}

	mode, err := resolveAgentMode(opts.Mode)
	if err != nil {
		return err
	}
	if mode == agentdomain.AgentModeAutoWithJudge && cfg.Judge.ResolveModel(cfg.Agent.Model) == "" {
		return fmt.Errorf("auto-with-judge mode selected but no judge model is resolvable: set judge.model in %s or agent.model", config.DefaultJudgePath)
	}

	rendered := false
	defer func() {
		if r := recover(); r != nil {
			logger.Error("headless run panic", "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("agent panic: %v", r)
			rendered = false
		}
		if err != nil && !rendered {
			emitPreRunError(os.Stdout, opts.Format, err)
		}
	}()

	svc := newServices()
	notifications := make(uiBridge, 8)
	svc.SetUINotifier(notifications)
	shutdown := sync.OnceFunc(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	})
	defer shutdown()
	utils.OnShutdownSignal(shutdown)

	if err := svc.GetGatewaySupervisor().EnsureStarted(); err != nil {
		return fmt.Errorf("failed to start inference gateway: %w", err)
	}

	// The notes buffer the local agents' boot progress; the first run the
	// process opens drains them as its activity entries.
	notes := newStartupNotes()
	if agentSupervisor := svc.GetAgentSupervisor(); agentSupervisor != nil && !isBashTask(opts.Task) {
		startLocalAgents(agentSupervisor, cfg, opts.Format, notes)
	}

	if mcpSupervisor := svc.GetMCPSupervisor(); mcpSupervisor != nil {
		svc.GetToolRegistry().RegisterTools(mcpSupervisor.DiscoverTools(context.Background()))
	}

	listCtx, listCancel := context.WithTimeout(context.Background(), time.Duration(cfg.Gateway.Timeout)*time.Second)
	availModels, err := svc.GetModelService().ListModels(listCtx)
	listCancel()
	if err != nil {
		return fmt.Errorf("inference gateway not available: %w", err)
	}
	if len(availModels) == 0 {
		return fmt.Errorf("no models available from inference gateway")
	}

	selectedModel, err := selectModel(availModels, opts.Model, defaultModel(cfg, opts, availModels))
	if err != nil {
		return err
	}

	if err := svc.GetModelService().SelectModel(selectedModel); err != nil {
		logger.Warn("failed to record the selected model", "model", selectedModel, "error", err)
	}

	if opts.Heartbeat && cfg.Prompts.Agent.SystemPromptHeartbeat != "" {
		cfg.Prompts.Agent.SystemPrompt = cfg.Prompts.Agent.SystemPromptHeartbeat
	}
	if opts.Remote && cfg.Prompts.Agent.SystemPromptRemote != "" {
		cfg.Prompts.Agent.SystemPrompt = cfg.Prompts.Agent.SystemPromptRemote
	}

	cfg.Tools.Agent.Mode = scheddomain.SubagentModeHeadless

	agentService := svc.GetAgentService()
	conversationRepo := svc.GetConversationRepository()

	svc.GetStateStore().SetAgentMode(mode)

	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	groupKey := ""
	rolloverMgr := svc.GetSessionRollover()
	if rolloverMgr != nil {
		if resolved, gk, _ := rolloverMgr.ResolveSessionID(sessionID); resolved != "" {
			sessionID = resolved
			groupKey = gk
		}
	}

	if screenshotServer := svc.StartScreenshotServer(sessionID); screenshotServer != nil {
		defer func() {
			if stopErr := screenshotServer.Stop(); stopErr != nil {
				logger.Error("failed to stop screenshot server", "error", stopErr)
			}
		}()
	}

	ctx := context.Background()

	resumedEntries := prepareConversation(ctx, conversationRepo, sessionID, opts.SessionID != "", opts.NoSave)
	history := convdomain.BuildAgentMessagesFromEntries(resumedEntries)

	if newID, fired := rolloverMgr.MaybeRollover(ctx, selectedModel, groupKey); fired {
		logger.Info("rolled over to new session (summary preserved)",
			"previous_session_id", sessionID, "new_session_id", newID)
		sessionID = newID
		resumedEntries = conversationRepo.GetMessages()
		history = convdomain.BuildAgentMessagesFromEntries(resumedEntries)
	}

	if opts.Serve {
		rendered = true
		serve(ctx, svc, notifications, agentdomain.AgentRequest{
			RequestID:                  sessionID,
			Model:                      selectedModel,
			ApprovalBrokerAttached:     opts.RequireApproval,
			UserQuestionBrokerAttached: true,
			GroupKey:                   groupKey,
		}, resumedEntries, notes)
		return nil
	}

	deps := shortcuts.Deps{SessionID: sessionID}
	if rolloverMgr != nil {
		deps.Compact = func(ctx context.Context) (string, error) {
			return compactSession(ctx, rolloverMgr, selectedModel, groupKey)
		}
	}

	if direct, derr := runDirect(ctx, opts, svc.GetToolService(), conversationRepo, sessionID, selectedModel, cfg); direct {
		rendered = true
		return derr
	}

	out, handled, err := shortcuts.Run(ctx, svc.GetShortcutRegistry(), opts.Task, deps)
	if err != nil {
		return err
	}

	task := opts.Task
	switch {
	case out.Prompt != "":
		task = out.Prompt
		if out.Model != "" {
			selectedModel = out.Model
		}
	case handled:
		rendered = true
		err = emitCommandResult(opts.Format, conversationRepo, sessionID, selectedModel, cfg, out.Text)
		if opts.ResultFile != "" {
			writeResultFile(opts.ResultFile, conversationRepo, sessionID, err)
		}
		return err
	}

	expanded, images, err := expandFileReferences(task, opts.Files, svc.GetFileService(), svc.GetImageService(), selectedModel)
	if err != nil {
		return fmt.Errorf("failed to expand file references: %w", err)
	}

	userMsg, err := userMessage(expanded, images)
	if err != nil {
		return fmt.Errorf("failed to build user message: %w", err)
	}
	if err := conversationRepo.AddMessage(convdomain.ConversationEntry{Message: userMsg, Time: time.Now()}); err != nil {
		logger.Warn("failed to persist user task message", "error", err)
	}

	req := &agentdomain.AgentRequest{
		RequestID:                  sessionID,
		Model:                      selectedModel,
		Messages:                   append(history, userMsg),
		ApprovalBrokerAttached:     opts.RequireApproval,
		UserQuestionBrokerAttached: questionsAnswerable(opts),
		GroupKey:                   groupKey,
	}

	rec := svc.GetTelemetryRecorder()
	rec.SetConversationID(sessionID)
	sessionStart := time.Now()
	endSessionSpan := rec.StartSession("headless")

	events, err := agentService.RunWithStream(ctx, req)
	if err != nil {
		endSessionSpan(telemetry.RunFailed)
		return fmt.Errorf("failed to run agent: %w", err)
	}

	var ctl *headlessControl
	if opts.Format != "text" {
		ctl = newHeadlessControl(agentService, svc.GetMessageQueue(), sessionID)
		go ctl.readLines(os.Stdin)
	}
	rendered = true
	err = renderStream(opts.Format, notifications.merge(events), ctl, sessionID, selectedModel, cfg, conversationRepo, resumedEntries, svc.GetBackgroundTaskRegistry().Snapshot, notes)

	endSessionSpan(sessionOutcome(err))
	rec.RecordSession("headless", sessionOutcome(err), time.Since(sessionStart))

	if opts.KeepAlive {
		err = keepAlive(ctx, svc, ctl, notifications, opts, sessionID, selectedModel, cfg, groupKey, err)
	}
	if opts.ResultFile != "" {
		writeResultFile(opts.ResultFile, conversationRepo, sessionID, err)
	}
	return err
}

// questionsAnswerable reports whether a stdin broker can answer the
// AskUserQuestion tool. A keep-alive run's stdin is held open by a parent that
// only sends messages, so a question there would wait until the parent hangs up.
func questionsAnswerable(opts Options) bool {
	return opts.Format != "text" && !opts.KeepAlive
}

// keepAlive reports the task turn and then runs every run_agent_input frame
// the parent sends as a further turn, reporting each, until stdin closes. It
// returns the last turn's error.
func keepAlive(ctx context.Context, svc Services, ctl *headlessControl, notifications uiBridge, opts Options, sessionID, model string, cfg *config.Config, groupKey string, err error) error {
	repo := svc.GetConversationRepository()
	emitTurnLine(os.Stdout, repo, svc.GetMessageQueue(), sessionID, err)
	for ctl.awaitTurn() {
		err = runFollowUpTurn(ctx, svc, ctl, notifications, opts, sessionID, model, cfg, groupKey)
		emitTurnLine(os.Stdout, repo, svc.GetMessageQueue(), sessionID, err)
	}
	return err
}

// runFollowUpTurn moves the queued messages into the conversation and runs one
// agent turn over it in the run's format, the way runServeTurn does in AG-UI.
func runFollowUpTurn(ctx context.Context, svc Services, ctl *headlessControl, notifications uiBridge, opts Options, sessionID, model string, cfg *config.Config, groupKey string) error {
	repo := svc.GetConversationRepository()
	moveQueuedMessages(svc.GetMessageQueue(), repo)
	req := &agentdomain.AgentRequest{
		RequestID:                  sessionID,
		Model:                      model,
		Messages:                   convdomain.BuildAgentMessagesFromEntries(repo.GetMessages()),
		ApprovalBrokerAttached:     opts.RequireApproval,
		UserQuestionBrokerAttached: questionsAnswerable(opts),
		GroupKey:                   groupKey,
	}

	rec := svc.GetTelemetryRecorder()
	started := time.Now()
	endSpan := rec.StartSession("headless")
	events, err := svc.GetAgentService().RunWithStream(ctx, req)
	if err != nil {
		err = fmt.Errorf("failed to run agent: %w", err)
		emitPreRunError(os.Stdout, opts.Format, err)
	} else {
		err = renderStream(opts.Format, notifications.merge(events), ctl, sessionID, model, cfg, repo, nil, svc.GetBackgroundTaskRegistry().Snapshot, nil)
	}
	endSpan(sessionOutcome(err))
	rec.RecordSession("headless", sessionOutcome(err), time.Since(started))
	return err
}

// validateOptions rejects flag combinations before any service starts.
func validateOptions(opts Options) error {
	switch opts.Format {
	case "json", "json-pretty", "ag-ui", "text":
	default:
		return fmt.Errorf("invalid --format %q (supported: json, json-pretty, ag-ui, text)", opts.Format)
	}
	switch {
	case opts.Serve && opts.Task != "":
		return errors.New("--serve takes no task, send run_agent_input frames on stdin instead")
	case opts.Serve && opts.Format != "ag-ui":
		return fmt.Errorf("--serve streams AG-UI runs, so it needs --format ag-ui (got %q)", opts.Format)
	case !opts.Serve && opts.Task == "":
		return errors.New("a task is required unless --serve is set")
	case opts.KeepAlive && opts.Serve:
		return errors.New("--keep-alive runs a task then reads further turns, --serve reads every turn: pick one")
	case opts.KeepAlive && opts.Format == "text":
		return errors.New("--keep-alive reads run_agent_input frames on stdin, which --format text does not")
	}
	return nil
}

// defaultModel is the model a run falls back to without --model. A serve
// worker also answers panel requests before any thread picks a model, so it
// takes the gateway's first model rather than failing to boot.
func defaultModel(cfg *config.Config, opts Options, available []string) string {
	if opts.Serve {
		return cmp.Or(cfg.Agent.Model, available[0])
	}
	return cfg.Agent.Model
}

func selectModel(models []string, modelFlag, defaultModel string) (string, error) {
	if modelFlag != "" {
		for _, m := range models {
			if m == modelFlag {
				return m, nil
			}
		}
		return "", fmt.Errorf("model %q not available. Available: %v", modelFlag, models)
	}
	if defaultModel != "" {
		for _, m := range models {
			if m == defaultModel {
				return m, nil
			}
		}
		return "", fmt.Errorf("default model %q not available. Available: %v", defaultModel, models)
	}
	return "", fmt.Errorf("no model specified; use --model or set agent.model in config")
}

// renderStream writes an event stream in the requested --format. Both an agent
// run and a slash command's output go through it, so every format keeps the
// same contract whichever produced the events. A nil control is an unattended
// stream: gated calls are rejected and questions dismissed at once.
func renderStream(format string, events <-chan agentdomain.ChatEvent, ctl *headlessControl, sessionID, model string, cfg *config.Config, repo convdomain.ConversationRepository, history []convdomain.ConversationEntry, jobs func() []scheddomain.TrackedJob, notes *startupNotes) error {
	var approvals <-chan ipc.ApprovalResponse
	var questions <-chan ipc.UserQuestionResponse
	var interrupts Interrupts
	if ctl != nil {
		approvals, questions, interrupts = ctl.approvals, ctl.questions, ctl
	}
	switch format {
	case "json":
		return render.RenderJSON(events, os.Stdout, approvals, questions, sessionID, model, cfg, repo)
	case "json-pretty":
		return render.RenderJSONPretty(events, os.Stdout, approvals, questions, sessionID, model, cfg, repo)
	case "ag-ui":
		return renderAGUI(events, os.Stdout, approvals, questions, interrupts, sessionID, model, repo, history, jobs, notes, computer.PublishedEvent)
	default:
		return render.RenderText(events, os.Stdout)
	}
}

// agentStartupEmitter returns the format's agent-status emitter: AG-UI buffers
// the notes for the first run's activity entries, render's agent_status lines
// write for the other machine formats.
func agentStartupEmitter(w io.Writer, notes *startupNotes, format string) func(name, state, message string, done, total int) {
	if format == "ag-ui" {
		return startupEmitter(notes)
	}
	return render.AgentStartupEmitter(w, format)
}

// emitPreRunError reports a failure that happened before the event stream
// started in the format's machine shape: an AG-UI RUN_ERROR event for ag-ui,
// render's agent_error lines for the JSON formats.
func emitPreRunError(w io.Writer, format string, err error) {
	if format == "ag-ui" {
		emitAGUIRunError(w, err)
		return
	}
	render.EmitPreRunError(w, format, err)
}

// startLocalAgents starts run:true agents and blocks until they settle, streaming
// each agent's startup state (image pull progress, container start, health) to
// stdout as agent_status lines so a client can show what the wait is for. The
// callbacks are removed once the wait ends so later liveness probes never write
// into the run's event stream.
func startLocalAgents(agentSupervisor a2adomain.AgentSupervisor, cfg *config.Config, format string, notes *startupNotes) {
	if emit := agentStartupEmitter(os.Stdout, notes, format); emit != nil {
		agentSupervisor.SetStatusCallback(func(name string, state a2adomain.AgentState, message, _, _ string) {
			emit(name, state.String(), message, 0, 0)
		})
		agentSupervisor.SetPullProgressCallback(func(name string, done, total int) {
			emit(name, a2adomain.AgentStatePullingImage.String(), "Pulling image", done, total)
		})
	}
	if err := agentSupervisor.StartAgents(context.Background()); err != nil {
		logger.Warn("failed to start agents in background", "error", err)
	}
	readyTimeout := time.Duration(cmp.Or(cfg.A2A.AgentsReadyTimeoutSec, 600)) * time.Second
	waitCtx, waitCancel := context.WithTimeout(context.Background(), readyTimeout)
	agentSupervisor.WaitForAgentsReady(waitCtx)
	waitCancel()
	agentSupervisor.SetStatusCallback(nil)
	agentSupervisor.SetPullProgressCallback(nil)
}

// emitCommandResult reports a slash command that answered by itself - /context,
// /clear, /help - as the assistant turn, the way the chat TUI records shortcut
// output in the conversation. No model is called.
func emitCommandResult(format string, repo convdomain.ConversationRepository, sessionID, model string, cfg *config.Config, text string) error {
	if err := repo.AddMessage(convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent(text)},
		Time:    time.Now(),
	}); err != nil {
		logger.Warn("failed to persist shortcut output", "error", err)
	}

	events := make(chan agentdomain.ChatEvent, 2)
	events <- agentdomain.ChatChunkEvent{RequestID: sessionID, Timestamp: time.Now(), Content: text}
	events <- agentdomain.ChatCompleteEvent{RequestID: sessionID, Timestamp: time.Now(), Message: text}
	close(events)

	return renderStream(format, events, nil, sessionID, model, cfg, repo, nil, nil, nil)
}

// compactSession is /compact outside the TUI: the rollover manager already runs
// the same optimizer-summarise-reseed the chat handler does.
func compactSession(ctx context.Context, mgr convdomain.SessionRollover, model, groupKey string) (string, error) {
	newID, err := mgr.PerformRollover(ctx, model, groupKey)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Compacted the conversation into a new session with a summary: %s", newID), nil
}

// expandFileReferences inlines @file references and --files into the task.
// Image files given via --files come back as attachments when the model can
// see images, so they reach it as image parts rather than a path note.
func expandFileReferences(content string, files []string, fileSvc agentdomain.FileService, imageSvc agentdomain.ImageService, model string) (string, []agentdomain.ImageAttachment, error) {
	matches := fileRefPattern.FindAllStringSubmatch(content, -1)
	supportsVision := models.SupportsVision(model)

	expanded := content
	var images []agentdomain.ImageAttachment
	for _, match := range matches {
		fullMatch := match[0]
		filename := match[1]

		if err := fileSvc.ValidateFile(filename); err != nil {
			logger.Warn("skipping invalid file reference", "filename", filename, "error", err)
			continue
		}

		if imageSvc != nil && imageSvc.IsImageFile(filename) {
			expanded = strings.Replace(expanded, fullMatch, agentdomain.ImageFileRef(filename, supportsVision), 1)
			continue
		}

		fileContent, err := fileSvc.ReadFile(filename)
		if err != nil {
			logger.Warn("failed to read file", "filename", filename, "error", err)
			continue
		}
		fileBlock := fmt.Sprintf("File: %s\n```%s\n%s\n```\n", filename, filename, fileContent)
		expanded = strings.Replace(expanded, fullMatch, fileBlock, 1)
	}

	for _, filename := range files {
		if err := fileSvc.ValidateFile(filename); err != nil {
			return "", nil, fmt.Errorf("invalid file %q: %w", filename, err)
		}
		if imageSvc != nil && imageSvc.IsImageFile(filename) {
			if supportsVision {
				img, err := imageSvc.ReadImageFromFile(filename)
				if err != nil {
					return "", nil, fmt.Errorf("failed to read image %q: %w", filename, err)
				}
				images = append(images, *img)
				continue
			}
			expanded += "\n\n" + agentdomain.ImageFileRef(filename, supportsVision)
			continue
		}
		fileContent, err := fileSvc.ReadFile(filename)
		if err != nil {
			return "", nil, fmt.Errorf("failed to read file %q: %w", filename, err)
		}
		expanded += fmt.Sprintf("\n\nFile: %s\n```%s\n%s\n```\n", filename, filename, fileContent)
	}
	return expanded, images, nil
}

// userMessage builds the task message: plain text, or text plus one image
// part per attachment, mirroring how the TUI attaches pasted images.
func userMessage(content string, images []agentdomain.ImageAttachment) (sdk.Message, error) {
	if len(images) == 0 {
		return sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(content)}, nil
	}
	textPart, err := sdk.NewTextContentPart(content)
	if err != nil {
		return sdk.Message{}, err
	}
	parts := []sdk.ContentPart{textPart}
	for _, img := range images {
		imagePart, err := sdk.NewImageContentPart(fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data), nil)
		if err != nil {
			return sdk.Message{}, fmt.Errorf("image %q: %w", img.Filename, err)
		}
		parts = append(parts, imagePart)
	}
	return sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(parts)}, nil
}

// prepareConversation points the persistent repository at the session,
// honours --no-save, and returns the entries of an existing --session-id
// (nil when starting fresh: no --session-id, an unknown ID, or no storage).
func prepareConversation(ctx context.Context, repo convdomain.ConversationRepository, sessionID string, resume, noSave bool) []convdomain.ConversationEntry {
	persistentRepo, ok := repo.(convdomain.PersistentConversationRepository)
	if !ok {
		return nil
	}
	persistentRepo.SetConversationID(sessionID)
	if noSave {
		persistentRepo.SetAutoSave(false)
	}
	if !resume {
		return nil
	}
	if err := persistentRepo.LoadConversation(ctx, sessionID); err != nil {
		if errors.Is(err, convdomain.ErrConversationNotFound) {
			logger.Debug("--session-id not found, starting a new session with this ID", "session_id", sessionID)
		} else {
			logger.Warn("could not load conversation for --session-id, starting fresh", "session_id", sessionID, "error", err)
		}
		return nil
	}
	return repo.GetMessages()
}

func sessionOutcome(err error) string {
	switch {
	case err == nil:
		return telemetry.RunSuccess
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, agentdomain.ErrMaxTurnsReached):
		return telemetry.RunStoppedEarly
	default:
		return telemetry.RunFailed
	}
}

// subagentRunStats tallies the run's tool outcomes and token usage for the
// result file.
func subagentRunStats(repo convdomain.ConversationRepository) *scheddomain.SubagentRunStats {
	succeeded, failed := convdomain.ToolOutcomes(repo.GetMessages())
	tokens := repo.GetSessionTokens()
	return &scheddomain.SubagentRunStats{
		ToolsSucceeded: succeeded,
		ToolsFailed:    failed,
		InputTokens:    tokens.TotalInputTokens,
		OutputTokens:   tokens.TotalOutputTokens,
		CachedTokens:   tokens.TotalCachedTokens,
	}
}

// turnResult records a turn's outcome and final assistant message - on failure
// too, so the parent gets the partial answer and error detail instead of silence.
func turnResult(repo convdomain.ConversationRepository, sessionID string, runErr error) scheddomain.SubagentResultFile {
	rf := scheddomain.SubagentResultFile{
		FinalAssistant: convdomain.LastAssistantText(repo.GetMessages()),
		Success:        runErr == nil,
		SessionID:      sessionID,
		Stats:          subagentRunStats(repo),
	}
	if runErr != nil {
		rf.Error = runErr.Error()
	}
	return rf
}

// writeResultFile writes the run's outcome at path for a parent Agent tool to
// harvest from a detached run whose stdout it does not own.
func writeResultFile(path string, repo convdomain.ConversationRepository, sessionID string, runErr error) {
	if err := scheddomain.WriteSubagentResultFile(path, turnResult(repo, sessionID, runErr)); err != nil {
		logger.Warn("failed to write result file", "path", path, "error", err)
	}
}

// emitTurnLine reports one finished keep-alive turn on stdout. Done is false
// when a message queued meanwhile means the next turn starts right away.
func emitTurnLine(w io.Writer, repo convdomain.ConversationRepository, queue convdomain.MessageQueue, sessionID string, runErr error) {
	line := scheddomain.SubagentTurnLine{Type: scheddomain.SubagentTurnLineType, SubagentResultFile: turnResult(repo, sessionID, runErr)}
	line.Done = queue.IsEmpty()
	data, err := json.Marshal(line)
	if err != nil {
		logger.Warn("failed to encode the turn line", "error", err)
		return
	}
	_, _ = fmt.Fprintln(w, string(data))
}
