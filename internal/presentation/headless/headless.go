package headless

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	computerinfra "github.com/inference-gateway/cli/internal/computer/infrastructure"
	container "github.com/inference-gateway/cli/internal/container"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	models "github.com/inference-gateway/cli/internal/platform/models"
	render "github.com/inference-gateway/cli/internal/platform/render"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// fileRefPattern matches @file references in the task description.
var fileRefPattern = regexp.MustCompile(`@([^\s]+)`)

// startScreenshotServer starts the screenshot capture server and
// registers the "screen" frame source so GetLatestFrame is available, exactly
// as interactive chat does. It logs instead of printing: headless stdout
// carries the ag-ui/json protocol stream. Returns nil when streaming is off or
// the server failed to start.
func startScreenshotServer(cfg *config.Config, svc *container.ServiceContainer, sessionID string) *computerinfra.ScreenshotServer {
	if !cfg.ComputerUse.Enabled || !cfg.ComputerUse.Screenshot.StreamingEnabled {
		return nil
	}
	screenshotServer := computerinfra.NewScreenshotServer(cfg, svc.GetImageService(), sessionID)
	if err := screenshotServer.Start(); err != nil {
		logger.Warn("failed to start screenshot server", "error", err)
		return nil
	}
	svc.GetToolRegistry().RegisterFrameSource("screen", screenshotServer)
	return screenshotServer
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

// Run is the composition root for headless mode: it builds the service
// container directly, so this presentation package intentionally depends
// on internal/container (the depguard leaf rule only polices imports into
// presentation, not out of it). cmd/headless.go stays thin flag plumbing.
func Run(cfg *config.Config, opts Options) (err error) { //nolint:gocyclo,cyclop,funlen
	switch opts.Format {
	case "json", "json-pretty", "ag-ui", "text":
	default:
		return fmt.Errorf("invalid --format %q (supported: json, json-pretty, ag-ui, text)", opts.Format)
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
			render.EmitPreRunError(os.Stdout, opts.Format, err)
		}
	}()

	svc := container.NewServiceContainer(cfg)
	svc.StartExtensionBridge()
	shutdown := sync.OnceFunc(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	})
	defer shutdown()
	utils.OnShutdownSignal(shutdown)

	if err := svc.GetGatewayManager().EnsureStarted(); err != nil {
		return fmt.Errorf("failed to start inference gateway: %w", err)
	}

	if agentManager := svc.GetAgentManager(); agentManager != nil {
		if err := agentManager.StartAgents(context.Background()); err != nil {
			logger.Warn("failed to start agents in background", "error", err)
		}
		readyTimeout := time.Duration(cmp.Or(cfg.A2A.AgentsReadyTimeoutSec, 600)) * time.Second
		waitCtx, waitCancel := context.WithTimeout(context.Background(), readyTimeout)
		agentManager.WaitForAgentsReady(waitCtx)
		waitCancel()
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

	selectedModel, err := selectModel(availModels, opts.Model, cfg.Agent.Model)
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

	svc.GetStateManager().SetAgentMode(mode)

	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	groupKey := ""
	rolloverMgr := svc.GetSessionRolloverManager()
	if rolloverMgr != nil {
		if resolved, gk, _ := rolloverMgr.ResolveSessionID(sessionID); resolved != "" {
			sessionID = resolved
			groupKey = gk
		}
	}

	if screenshotServer := startScreenshotServer(cfg, svc, sessionID); screenshotServer != nil {
		defer func() {
			if stopErr := screenshotServer.Stop(); stopErr != nil {
				logger.Error("failed to stop screenshot server", "error", stopErr)
			}
		}()
	}

	ctx := context.Background()

	history := prepareConversation(ctx, conversationRepo, sessionID, opts.SessionID != "", opts.NoSave)

	if newID, fired := rolloverMgr.MaybeRollover(ctx, selectedModel, groupKey); fired {
		logger.Info("rolled over to new session (summary preserved)",
			"previous_session_id", sessionID, "new_session_id", newID)
		sessionID = newID
		history = conversation.BuildAgentMessagesFromEntries(conversationRepo.GetMessages())
	}

	deps := shortcuts.Deps{SessionID: sessionID}
	if rolloverMgr != nil {
		deps.Compact = func(ctx context.Context) (string, error) {
			return compactSession(ctx, rolloverMgr, selectedModel, groupKey)
		}
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

	expanded, err := expandFileReferences(task, opts.Files, svc.GetFileService(), svc.GetImageService(), selectedModel)
	if err != nil {
		return fmt.Errorf("failed to expand file references: %w", err)
	}

	userMsg := sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(expanded),
	}
	if err := conversationRepo.AddMessage(convdomain.ConversationEntry{Message: userMsg, Time: time.Now()}); err != nil {
		logger.Warn("failed to persist user task message", "error", err)
	}

	req := &agentdomain.AgentRequest{
		RequestID:              sessionID,
		Model:                  selectedModel,
		Messages:               append(history, userMsg),
		ApprovalBrokerAttached: opts.RequireApproval,
		GroupKey:               groupKey,
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

	renderEvents := events
	var approvals <-chan ipc.ApprovalResponse
	if opts.Format != "text" {
		ctl := newHeadlessControl(agentService, svc.GetStateManager(), sessionID)
		go ctl.readLines(os.Stdin)
		approvals = ctl.approvals
		renderEvents = ctl.pumpEvents(events, func() (<-chan agentdomain.ChatEvent, error) {
			return resumeRun(ctx, agentService, conversationRepo, req)
		})
	}
	rendered = true
	err = renderStream(opts.Format, renderEvents, approvals, sessionID, selectedModel, cfg, conversationRepo)

	endSessionSpan(sessionOutcome(err))
	rec.RecordSession("headless", sessionOutcome(err), time.Since(sessionStart))

	if opts.ResultFile != "" {
		writeResultFile(opts.ResultFile, conversationRepo, sessionID, err)
	}
	return err
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
// same contract whichever produced the events.
func renderStream(format string, events <-chan agentdomain.ChatEvent, approvals <-chan ipc.ApprovalResponse, sessionID, model string, cfg *config.Config, repo convdomain.ConversationRepository) error {
	switch format {
	case "json":
		return render.RenderJSON(events, os.Stdout, approvals, sessionID, model, cfg, repo)
	case "json-pretty":
		return render.RenderJSONPretty(events, os.Stdout, approvals, sessionID, model, cfg, repo)
	case "ag-ui":
		return render.RenderAGUI(events, os.Stdout, approvals, sessionID, model)
	default:
		return render.RenderText(events, os.Stdout)
	}
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

	return renderStream(format, events, nil, sessionID, model, cfg, repo)
}

// compactSession is /compact outside the TUI: the rollover manager already runs
// the same optimizer-summarise-reseed the chat handler does.
func compactSession(ctx context.Context, mgr *conversation.SessionRolloverManager, model, groupKey string) (string, error) {
	newID, err := mgr.PerformRollover(ctx, model, groupKey)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Compacted the conversation into a new session with a summary: %s", newID), nil
}

func expandFileReferences(content string, files []string, fileSvc agentdomain.FileService, imageSvc agentdomain.ImageService, model string) (string, error) {
	matches := fileRefPattern.FindAllStringSubmatch(content, -1)

	expanded := content
	for _, match := range matches {
		fullMatch := match[0]
		filename := match[1]

		if err := fileSvc.ValidateFile(filename); err != nil {
			logger.Warn("skipping invalid file reference", "filename", filename, "error", err)
			continue
		}

		if imageSvc != nil && imageSvc.IsImageFile(filename) {
			expanded = strings.Replace(expanded, fullMatch, agentdomain.ImageFileRef(filename, models.SupportsVision(model)), 1)
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
			return "", fmt.Errorf("invalid file %q: %w", filename, err)
		}
		if imageSvc != nil && imageSvc.IsImageFile(filename) {
			expanded += "\n\n" + agentdomain.ImageFileRef(filename, models.SupportsVision(model))
			continue
		}
		fileContent, err := fileSvc.ReadFile(filename)
		if err != nil {
			return "", fmt.Errorf("failed to read file %q: %w", filename, err)
		}
		expanded += fmt.Sprintf("\n\nFile: %s\n```%s\n%s\n```\n", filename, filename, fileContent)
	}
	return expanded, nil
}

// prepareConversation points the persistent repository at the session,
// honours --no-save, and returns prior history when resuming an existing
// --session-id (empty when starting fresh or storage is not persistent).
func prepareConversation(ctx context.Context, repo convdomain.ConversationRepository, sessionID string, resume, noSave bool) []sdk.Message {
	persistentRepo, ok := repo.(*conversation.PersistentConversationRepository)
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
		logger.Warn("could not load conversation for --session-id, starting fresh", "session_id", sessionID, "error", err)
		return nil
	}
	return conversation.BuildAgentMessagesFromEntries(persistentRepo.GetMessages())
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

// writeResultFile atomically writes the run's outcome and final assistant
// message to path, for a parent Agent tool to harvest - on failure too, so
// the parent gets the partial answer and error detail instead of silence.
func writeResultFile(path string, repo convdomain.ConversationRepository, sessionID string, runErr error) {
	entries := repo.GetMessages()
	content := ""
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Message.Role == sdk.Assistant {
			if c, err := e.Message.Content.AsMessageContent0(); err == nil && c != "" {
				content = c
				break
			}
		}
	}
	rf := scheddomain.SubagentResultFile{
		FinalAssistant: content,
		Success:        runErr == nil,
		SessionID:      sessionID,
	}
	if runErr != nil {
		rf.Error = runErr.Error()
	}
	data, _ := json.Marshal(rf)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		logger.Warn("failed to write result file", "path", tmp, "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		logger.Warn("failed to rename result file", "path", path, "error", err)
	}
}
