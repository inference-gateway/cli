package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	uuid "github.com/google/uuid"
	cobra "github.com/spf13/cobra"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	models "github.com/inference-gateway/cli/internal/models"
	render "github.com/inference-gateway/cli/internal/render"
	services "github.com/inference-gateway/cli/internal/services"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
)

// inheritedSubagentMode returns the coding mode a subagent should start in,
// read from INFER_SUBAGENT_AGENT_MODE. Returns Standard when unset or
// unrecognized, so top-level infer headless runs are unaffected.
func inheritedSubagentMode() domain.AgentMode {
	if m, ok := domain.ParseAgentMode(os.Getenv(domain.EnvSubagentAgentMode)); ok {
		return m
	}
	return domain.AgentModeStandard
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
	headlessCmd.Flags().String("format", "json", "Output format: json, ag-ui, text")
	rootCmd.AddCommand(headlessCmd)
}

func runHeadless(cfg *config.Config, opts headlessOptions) (err error) {
	switch opts.Format {
	case "json", "ag-ui", "text":
	default:
		return fmt.Errorf("invalid --format %q (supported: json, ag-ui, text)", opts.Format)
	}

	svc := container.NewServiceContainer(cfg)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	}()

	if err := svc.GetGatewayManager().EnsureStarted(); err != nil {
		return fmt.Errorf("failed to start inference gateway: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Gateway.Timeout)*time.Second)
	defer cancel()

	availModels, err := svc.GetModelService().ListModels(ctx)
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

	agentService := svc.GetAgentService()
	conversationRepo := svc.GetConversationRepository()

	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	if !opts.NoSave {
		if persistentRepo, ok := conversationRepo.(*services.PersistentConversationRepository); ok {
			persistentRepo.SetConversationID(sessionID)
		}
	}

	// Expand @file references.
	expanded, err := expandFileReferences(opts.Task, opts.Files, svc.GetFileService(), svc.GetImageService(), selectedModel)
	if err != nil {
		return fmt.Errorf("failed to expand file references: %w", err)
	}

	req := &domain.AgentRequest{
		RequestID: sessionID,
		Model:     selectedModel,
		Messages: []sdk.Message{{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(expanded),
		}},
		ApprovalBrokerAttached: opts.RequireApproval,
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

	switch opts.Format {
	case "json":
		var stdin io.Reader
		if opts.RequireApproval {
			stdin = os.Stdin
		}
		err = render.RenderJSON(events, os.Stdout, stdin, sessionID, selectedModel, cfg, conversationRepo)
	case "ag-ui":
		err = render.RenderAGUI(events, os.Stdout, sessionID, selectedModel)
	case "text":
		err = render.RenderText(events, os.Stdout)
	}

	endSessionSpan(sessionOutcome(err))
	rec.RecordSession("headless", sessionOutcome(err), time.Since(sessionStart))

	if opts.ResultFile != "" && err == nil {
		writeResultFile(opts.ResultFile, conversationRepo)
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
			expanded = strings.ReplaceAll(expanded, fullMatch, domain.ImageFileRef(filename, models.SupportsVision(model)))
			continue
		}

		fileContent, err := fileSvc.ReadFile(filename)
		if err != nil {
			logger.Warn("failed to read file", "filename", filename, "error", err)
			continue
		}
		fileBlock := fmt.Sprintf("File: %s\n```%s\n%s\n```\n", filename, filename, fileContent)
		expanded = strings.ReplaceAll(expanded, fullMatch, fileBlock)
	}

	for _, filename := range files {
		if err := fileSvc.ValidateFile(filename); err != nil {
			return "", fmt.Errorf("invalid file %q: %w", filename, err)
		}
		if imageSvc != nil && imageSvc.IsImageFile(filename) {
			expanded += "\n\n" + domain.ImageFileRef(filename, models.SupportsVision(model))
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

func sessionOutcome(err error) string {
	switch err {
	case nil:
		return telemetry.RunSuccess
	case context.Canceled, context.DeadlineExceeded:
		return telemetry.RunStoppedEarly
	default:
		return telemetry.RunFailed
	}
}

func writeResultFile(path string, repo domain.ConversationRepository) {
	if path == "" {
		return
	}
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
		Success:        true,
	}
	data, _ := json.Marshal(rf)
	tmp := path + ".tmp"
	_ = os.WriteFile(tmp, data, 0o644)
	_ = os.Rename(tmp, path)
}
