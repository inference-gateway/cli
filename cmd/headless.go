package cmd

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	uuid "github.com/google/uuid"
	cobra "github.com/spf13/cobra"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	computerinfra "github.com/inference-gateway/cli/internal/computer/infrastructure"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	models "github.com/inference-gateway/cli/internal/models"
	render "github.com/inference-gateway/cli/internal/render"
	services "github.com/inference-gateway/cli/internal/services"
	chatcompletion "github.com/inference-gateway/cli/internal/services/chatcompletion"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
)

// startHeadlessScreenshotServer starts the screenshot capture server and
// registers the "screen" frame source so GetLatestFrame is available, exactly
// as interactive chat does. It logs instead of printing: headless stdout
// carries the ag-ui/json protocol stream. Returns nil when streaming is off or
// the server failed to start.
func startHeadlessScreenshotServer(cfg *config.Config, svc *container.ServiceContainer, sessionID string) *computerinfra.ScreenshotServer {
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

// inheritedSubagentMode returns the coding mode a subagent should start in,
// read from INFER_SUBAGENT_AGENT_MODE. Returns Standard when unset or
// unrecognized, so top-level infer headless runs are unaffected.
func inheritedSubagentMode() agentdomain.AgentMode {
	if m, ok := agentdomain.ParseAgentMode(os.Getenv(domain.EnvSubagentAgentMode)); ok {
		return m
	}
	return agentdomain.AgentModeStandard
}

// headlessOptions carries the headless command's flag values.
type headlessOptions struct {
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
}

var headlessCmd = &cobra.Command{
	Use:   "headless [task description]",
	Short: "Execute a task using an autonomous agent in headless mode (non-interactive)",
	Long: `Execute a task in headless (non-interactive) mode. The CLI works
iteratively until the task is considered complete.

Examples:
  infer headless "fix issue #42"
  infer headless --model openai/gpt-4 "implement feature"
  infer headless --files screenshot.png "analyze this"
  infer headless --session-id abc-123 "continue working"

Exit Codes:
  0  task completed
  1  task failed
  2  max turns exhausted`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := headlessOptions{Task: args[0]}
		opts.Model, _ = cmd.Flags().GetString("model")
		opts.Files, _ = cmd.Flags().GetStringSlice("files")
		opts.NoSave, _ = cmd.Flags().GetBool("no-save")
		opts.SessionID, _ = cmd.Flags().GetString("session-id")
		opts.RequireApproval, _ = cmd.Flags().GetBool("require-approval")
		opts.Heartbeat, _ = cmd.Flags().GetBool("heartbeat")
		opts.Remote, _ = cmd.Flags().GetBool("remote")
		opts.ResultFile, _ = cmd.Flags().GetString("result-file")
		opts.Format, _ = cmd.Flags().GetString("format")
		return runHeadless(Cfg, opts)
	},
}

func init() {
	headlessCmd.Flags().StringP("model", "m", "", "Model to use (e.g. openai/gpt-4)")
	headlessCmd.Flags().StringSliceP("files", "f", []string{}, "Files or images to include")
	headlessCmd.Flags().Bool("no-save", false, "Disable saving conversation to database")
	headlessCmd.Flags().String("session-id", "", "Resume an existing session by conversation ID")
	headlessCmd.Flags().Bool("require-approval", false, "Enable IPC tool approval via stdin/stdout")
	headlessCmd.Flags().Bool("heartbeat", false, "Use heartbeat system prompt")
	headlessCmd.Flags().Bool("remote", false, "Use remote-control system prompt")
	headlessCmd.Flags().String("result-file", "", "Write final result JSON to this path")
	headlessCmd.Flags().String("format", "json", "Output format: json, json-pretty, ag-ui, text")
	rootCmd.AddCommand(headlessCmd)
}

func runHeadless(cfg *config.Config, opts headlessOptions) (err error) { //nolint:gocyclo,cyclop,funlen
	switch opts.Format {
	case "json", "json-pretty", "ag-ui", "text":
	default:
		return fmt.Errorf("invalid --format %q (supported: json, json-pretty, ag-ui, text)", opts.Format)
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
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	}()

	if err := svc.GetGatewayManager().EnsureStarted(); err != nil {
		return fmt.Errorf("failed to start inference gateway: %w", err)
	}

	if agentManager := svc.GetAgentManager(); agentManager != nil {
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

	if opts.Heartbeat && cfg.Prompts.Agent.SystemPromptHeartbeat != "" {
		cfg.Prompts.Agent.SystemPrompt = cfg.Prompts.Agent.SystemPromptHeartbeat
	}
	if opts.Remote && cfg.Prompts.Agent.SystemPromptRemote != "" {
		cfg.Prompts.Agent.SystemPrompt = cfg.Prompts.Agent.SystemPromptRemote
	}

	cfg.Tools.Agent.Mode = domain.SubagentModeHeadless

	agentService := svc.GetAgentService()
	conversationRepo := svc.GetConversationRepository()

	svc.GetStateManager().SetAgentMode(inheritedSubagentMode())

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

	if screenshotServer := startHeadlessScreenshotServer(cfg, svc, sessionID); screenshotServer != nil {
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
		history = chatcompletion.BuildAgentMessagesFromEntries(conversationRepo.GetMessages())
	}

	expanded, err := expandFileReferences(opts.Task, opts.Files, svc.GetFileService(), svc.GetImageService(), selectedModel)
	if err != nil {
		return fmt.Errorf("failed to expand file references: %w", err)
	}

	userMsg := sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(expanded),
	}
	if err := conversationRepo.AddMessage(domain.ConversationEntry{Message: userMsg, Time: time.Now()}); err != nil {
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
	var approvals <-chan domain.ApprovalResponse
	if opts.Format != "text" {
		ctl := newHeadlessControl(agentService, svc.GetStateManager(), sessionID)
		go ctl.readLines(os.Stdin)
		approvals = ctl.approvals
		renderEvents = ctl.pumpEvents(events, func() (<-chan agentdomain.ChatEvent, error) {
			return resumeHeadlessRun(ctx, agentService, conversationRepo, req)
		})
	}
	rendered = true
	switch opts.Format {
	case "json":
		err = render.RenderJSON(renderEvents, os.Stdout, approvals, sessionID, selectedModel, cfg, conversationRepo)
	case "json-pretty":
		err = render.RenderJSONPretty(renderEvents, os.Stdout, approvals, sessionID, selectedModel, cfg, conversationRepo)
	case "ag-ui":
		err = render.RenderAGUI(renderEvents, os.Stdout, approvals, sessionID, selectedModel)
	case "text":
		err = render.RenderText(events, os.Stdout)
	}

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

func expandFileReferences(content string, files []string, fileSvc domain.FileService, imageSvc domain.ImageService, model string) (string, error) {
	re := regexp.MustCompile(`@([^\s]+)`)
	matches := re.FindAllStringSubmatch(content, -1)

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
func prepareConversation(ctx context.Context, repo domain.ConversationRepository, sessionID string, resume, noSave bool) []sdk.Message {
	persistentRepo, ok := repo.(*services.PersistentConversationRepository)
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
	return chatcompletion.BuildAgentMessagesFromEntries(persistentRepo.GetMessages())
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
func writeResultFile(path string, repo domain.ConversationRepository, sessionID string, runErr error) {
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
	rf := domain.SubagentResultFile{
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
