package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"
	cobra "github.com/spf13/cobra"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agent "github.com/inference-gateway/cli/internal/agent"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	streamevent "github.com/inference-gateway/cli/internal/streamevent"
)

var agentCmd = &cobra.Command{
	Use:   "agent [task description]",
	Short: "Execute a task using an autonomous agent in background mode",
	Long: `Execute a task using an autonomous agent in background mode. The CLI will work iteratively
until the task is considered complete. Particularly useful for SCM tickets like GitHub issues.

Session Resumption:
  Use --session-id to resume a previous agent session and continue work from where it left off.
  Find session IDs using: infer conversations list

Examples:
  # Start new agent sessions
  infer agent "Please fix the github issue 38"
  infer agent --model "openai/gpt-4" "Implement the feature described in issue #42"
  infer agent "Debug the failing test in PR 15"
  infer agent "Analyze this screenshot" --files screenshot.png
  infer agent "Compare these images" -f image1.png -f image2.png
  infer agent "Review this code and diagram" --files @code.go @diagram.png

  # Resume existing sessions
  infer agent "continue fixing the authentication bug" --session-id abc-123-def
  infer agent "analyze these new error logs" --session-id abc-123 --files error.log
  infer agent "try a different approach" --session-id abc-123 --no-save`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		model, _ := cmd.Flags().GetString("model")
		files, _ := cmd.Flags().GetStringSlice("files")
		noSave, _ := cmd.Flags().GetBool("no-save")
		sessionID, _ := cmd.Flags().GetString("session-id")
		requireApproval, _ := cmd.Flags().GetBool("require-approval")
		heartbeat, _ := cmd.Flags().GetBool("heartbeat")
		remote, _ := cmd.Flags().GetBool("remote")
		resultFile, _ := cmd.Flags().GetString("result-file")
		return RunAgentCommand(Cfg, model, args[0], files, noSave, sessionID, requireApproval, heartbeat, remote, resultFile)
	},
}

// ConversationMessage represents a message in the JSON output conversation
type ConversationMessage struct {
	Role             string                               `json:"role"`
	Content          string                               `json:"content"`
	ReasoningContent string                               `json:"reasoning_content,omitempty"`
	ToolCalls        *[]sdk.ChatCompletionMessageToolCall `json:"tool_calls,omitempty"`
	Tools            []string                             `json:"tools,omitempty"`
	ToolCallID       string                               `json:"tool_call_id,omitempty"`
	ToolExecution    *domain.ToolExecutionResult          `json:"-"`
	TokenUsage       *sdk.CompletionUsage                 `json:"token_usage,omitempty"`
	Timestamp        time.Time                            `json:"timestamp"`
	RequestID        string                               `json:"request_id,omitempty"`
	Internal         bool                                 `json:"-"`
	Images           []domain.ImageAttachment             `json:"images,omitempty"`
}

// AgentSession manages the background execution session
type AgentSession struct {
	agentService          domain.AgentService
	toolService           domain.ToolService
	fileService           domain.FileService
	imageService          domain.ImageService
	model                 string
	agentMode             domain.AgentMode
	conversation          []ConversationMessage
	sessionID             string
	maxTurns              int
	completedTurns        int
	config                *config.Config
	conversationRepo      domain.ConversationRepository
	reminderProvider      domain.SystemReminderProvider
	hookProvider          domain.HookCommandProvider
	firedReminders        map[string]bool
	saveEnabled           bool
	bgWaiter              *services.BackgroundTasksWaiter
	requireApproval       bool
	approvalCh            chan domain.ApprovalResponse
	rolloverManager       *services.SessionRolloverManager
	groupKey              string
	pricingService        domain.PricingService
	totalPromptTokens     int
	totalCompletionTokens int
	totalTokens           int
	requestCount          int
}

// inheritedSubagentMode returns the coding mode a subagent should start in, read
// from INFER_SUBAGENT_AGENT_MODE (set by the Agent tool when spawning from a
// non-Standard parent). It defaults to Standard when unset or unrecognized, so
// top-level `infer chat` / `infer agent` runs are unaffected.
func inheritedSubagentMode() domain.AgentMode {
	if m, ok := domain.ParseAgentMode(os.Getenv(domain.EnvSubagentAgentMode)); ok {
		return m
	}
	return domain.AgentModeStandard
}

func RunAgentCommand(cfg *config.Config, modelFlag, taskDescription string, files []string, noSave bool, sessionID string, requireApproval, heartbeat, remote bool, resultFile string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			outputAgentError(fmt.Sprintf("agent panic: %v", r))
			err = fmt.Errorf("agent panic: %v", r)
			return
		}
		if err != nil {
			outputAgentError(err.Error())
		}
	}()

	if heartbeat && cfg.Prompts.Agent.SystemPromptHeartbeat != "" {
		cfg.Prompts.Agent.SystemPrompt = cfg.Prompts.Agent.SystemPromptHeartbeat
	}

	if remote && cfg.Prompts.Agent.SystemPromptRemote != "" {
		cfg.Prompts.Agent.SystemPrompt = cfg.Prompts.Agent.SystemPromptRemote
	}

	svc := container.NewServiceContainer(cfg)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	}()

	if err := svc.GetGatewayManager().EnsureStarted(); err != nil {
		return fmt.Errorf(`failed to start inference gateway: %w

Possible solutions:
1. Ensure Docker is running: docker ps
2. Manually start the gateway container: docker run -d --name inference-gateway -p 8080:8080 ghcr.io/inference-gateway/inference-gateway:latest
3. Or disable auto-run in config and start the gateway manually
4. Check gateway logs: docker logs inference-gateway

For more information, visit: https://github.com/inference-gateway/inference-gateway`, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Gateway.Timeout)*time.Second)
	defer cancel()

	models, err := svc.GetModelService().ListModels(ctx)
	if err != nil {
		return fmt.Errorf("inference gateway is not available: %w", err)
	}

	if len(models) == 0 {
		return fmt.Errorf("no models available from inference gateway")
	}

	defaultModel := cfg.Agent.Model
	if cfg.IsClaudeCodeMode() {
		modelFlag = services.CanonicalClaudeModelID(modelFlag)
		defaultModel = services.CanonicalClaudeModelID(defaultModel)
	}

	selectedModel, err := selectModel(models, modelFlag, defaultModel)
	if err != nil {
		return err
	}

	agentService := svc.GetAgentService()
	toolService := svc.GetToolService()
	fileService := svc.GetFileService()
	imageService := svc.GetImageService()
	conversationRepo := svc.GetConversationRepository()
	stateManager := svc.GetStateManager()
	pricingService := svc.GetPricingService()

	agentMode := inheritedSubagentMode()
	stateManager.SetAgentMode(agentMode)

	saveEnabled := !noSave

	newSessionID := uuid.New().String()
	session := &AgentSession{
		agentService:     agentService,
		toolService:      toolService,
		fileService:      fileService,
		imageService:     imageService,
		model:            selectedModel,
		agentMode:        agentMode,
		sessionID:        newSessionID,
		maxTurns:         cfg.Agent.MaxTurns,
		conversation:     []ConversationMessage{},
		config:           cfg,
		conversationRepo: conversationRepo,
		reminderProvider: cfg.Reminders,
		hookProvider:     cfg.Hooks,
		firedReminders:   make(map[string]bool),
		saveEnabled:      saveEnabled,
		bgWaiter: services.NewBackgroundTasksWaiter(
			cfg,
			newSessionID,
			svc.GetBackgroundTaskRegistry(),
			svc.GetMessageQueue(),
			conversationRepo,
		),
		requireApproval: requireApproval,
		approvalCh:      make(chan domain.ApprovalResponse, 1),
		pricingService:  pricingService,
	}

	session.rolloverManager = svc.GetSessionRolloverManager()
	session.groupKey = resolveAndLoadSession(session, session.rolloverManager, sessionID, selectedModel)

	session.maybeRollover()

	err = session.execute(taskDescription, files)
	if resultFile != "" {
		writeSubagentResultFile(resultFile, session, err)
	}
	return err
}

// writeSubagentResultFile atomically writes the session's final assistant
// message and outcome to path, for a parent Agent tool to harvest when the
// subagent runs detached (e.g. in a tmux pane whose stdout the parent doesn't own).
func writeSubagentResultFile(path string, s *AgentSession, runErr error) {
	rf := domain.SubagentResultFile{
		FinalAssistant: s.finalAssistantContent(),
		Success:        runErr == nil,
		SessionID:      s.sessionID,
	}
	if runErr != nil {
		rf.Error = runErr.Error()
	}
	data, mErr := json.Marshal(rf)
	if mErr != nil {
		logger.Warn("failed to marshal subagent result file", "error", mErr)
		return
	}
	tmp := path + ".tmp"
	if wErr := os.WriteFile(tmp, data, 0o644); wErr != nil {
		logger.Warn("failed to write subagent result file", "error", wErr)
		return
	}
	if rErr := os.Rename(tmp, path); rErr != nil {
		logger.Warn("failed to finalize subagent result file", "error", rErr)
	}
}

// finalAssistantContent returns the subagent's substantive answer: the last
// non-empty assistant message that appears BEFORE the first internal
// verification prompt. The loop appends an "anything else?" turn whose answer is
// a trivial "task complete" confirmation - harvesting that instead of the real
// result is what we avoid here. Falls back to the last assistant message overall.
func (s *AgentSession) finalAssistantContent() string {
	limit := len(s.conversation)
	for i, m := range s.conversation {
		if m.Internal && m.Role == "user" {
			limit = i
			break
		}
	}
	if content := lastAssistantBefore(s.conversation, limit); content != "" {
		return content
	}
	return lastAssistantBefore(s.conversation, len(s.conversation))
}

// lastAssistantBefore returns the last non-empty assistant message content in
// conversation[:limit], or "" if none.
func lastAssistantBefore(conversation []ConversationMessage, limit int) string {
	for i := limit - 1; i >= 0; i-- {
		m := conversation[i]
		if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
			return m.Content
		}
	}
	return ""
}

// resolveAndLoadSession resolves --session-id through the rollover manager
// (literal UUIDs pass through unchanged; non-UUID inputs are treated as group
// keys and resolved via .infer/session_groups.json), then loads the resulting
// session. Returns the group key (empty for literal UUIDs).
func resolveAndLoadSession(session *AgentSession, rolloverMgr *services.SessionRolloverManager, sessionID, selectedModel string) string {
	if sessionID == "" {
		logger.Info("starting agent session",
			"session_id", session.sessionID,
			"model", selectedModel)
		session.outputStatusMessage("info", "Starting new agent session", map[string]any{
			"session_id": session.sessionID,
			"model":      selectedModel,
		})
		return ""
	}

	groupKey := ""
	if rolloverMgr != nil {
		if resolved, gk, _ := rolloverMgr.ResolveSessionID(sessionID); resolved != "" {
			sessionID = resolved
			groupKey = gk
		}
	}

	loaded, err := session.initializeSession(sessionID)
	if err != nil {
		logger.Warn("failed to load session, starting fresh", "session_id", sessionID, "error", err)
		session.outputStatusMessage("warning", "Could not load session, starting fresh", map[string]any{
			"session_id": sessionID,
			"error":      err.Error(),
		})
		return groupKey
	}

	if loaded {
		logger.Info("resumed agent session", "session_id", sessionID, "messages", len(session.conversation))
		session.outputStatusMessage("info", "Resumed agent session", map[string]any{
			"session_id":    sessionID,
			"message_count": len(session.conversation),
		})
	}
	return groupKey
}

// maybeRollover delegates to SessionRolloverManager.MaybeRollover for the
// shared gate-then-perform-then-log path that chat mode also uses (see
// internal/handlers/chat_message_processor.go), then performs the agent-mode
// post-rollover work: emits a status message, swaps s.sessionID, reloads the
// in-memory conversation from the new session file via initializeSession.
//
// Called both at startup (before the turn loop) and at the top of every turn
// iteration. s.completedTurns is intentionally preserved across rollover so
// that --max-turns N stays a hard ceiling and the reminder hooks keep their
// cadence. The idle trigger is structurally a no-op inside an active loop
// because every preceding executeTurn appended a fresh-timestamped message.
func (s *AgentSession) maybeRollover() {
	newID, fired := s.rolloverManager.MaybeRollover(context.Background(), s.model, s.groupKey)
	if !fired {
		return
	}

	s.outputStatusMessage("info", "Rolled over to new session (summary preserved)", map[string]any{
		"previous_session_id": s.sessionID,
		"new_session_id":      newID,
	})
	s.sessionID = newID
	s.conversation = nil
	if _, err := s.initializeSession(newID); err != nil {
		logger.Warn("failed to reload session after rollover", "error", err, "session_id", newID)
	}
}

// fileExpansionResult holds the result of expanding file references
type fileExpansionResult struct {
	content string
	images  []domain.ImageAttachment
}

// expandFileReferences expands @filename references and --files flag inputs
func (s *AgentSession) expandFileReferences(content string, additionalFiles []string) (*fileExpansionResult, error) {
	result := &fileExpansionResult{
		content: content,
		images:  []domain.ImageAttachment{},
	}

	re := regexp.MustCompile(`@([^\s]+)`)
	matches := re.FindAllStringSubmatch(content, -1)

	expandedContent := content
	for _, match := range matches {
		fullMatch := match[0]
		filename := match[1]

		if err := s.fileService.ValidateFile(filename); err != nil {
			logger.Warn("skipping invalid file reference", "filename", filename, "error", err)
			continue
		}

		if s.imageService != nil && s.imageService.IsImageFile(filename) {
			imageAttachment, err := s.imageService.ReadImageFromFile(filename)
			if err != nil {
				logger.Warn("failed to read image file", "filename", filename, "error", err)
				continue
			}
			result.images = append(result.images, *imageAttachment)
			imageRef := fmt.Sprintf("[Image: %s]", filename)
			expandedContent = strings.Replace(expandedContent, fullMatch, imageRef, 1)
			continue
		}

		fileContent, err := s.fileService.ReadFile(filename)
		if err != nil {
			logger.Warn("failed to read file", "filename", filename, "error", err)
			continue
		}

		fileBlock := fmt.Sprintf("File: %s\n```%s\n%s\n```\n", filename, filename, fileContent)
		expandedContent = strings.Replace(expandedContent, fullMatch, fileBlock, 1)
	}

	for _, filename := range additionalFiles {
		if err := s.fileService.ValidateFile(filename); err != nil {
			return nil, fmt.Errorf("invalid file '%s': %w", filename, err)
		}

		if s.imageService != nil && s.imageService.IsImageFile(filename) {
			imageAttachment, err := s.imageService.ReadImageFromFile(filename)
			if err != nil {
				return nil, fmt.Errorf("failed to read image file '%s': %w", filename, err)
			}
			result.images = append(result.images, *imageAttachment)
			continue
		}

		fileContent, err := s.fileService.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read file '%s': %w", filename, err)
		}

		fileBlock := fmt.Sprintf("\n\nFile: %s\n```%s\n%s\n```\n", filename, filename, fileContent)
		expandedContent += fileBlock
	}

	result.content = expandedContent
	return result, nil
}

func (s *AgentSession) execute(taskDescription string, files []string) error {
	defer s.emitSessionStats()

	expansion, err := s.expandFileReferences(taskDescription, files)
	if err != nil {
		return fmt.Errorf("failed to expand file references: %w", err)
	}

	s.addMessage(ConversationMessage{
		Role:      "user",
		Content:   expansion.content,
		Images:    expansion.images,
		Timestamp: time.Now(),
	})

	s.outputMessage(s.conversation[len(s.conversation)-1])

	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	defer monitorCancel()
	s.bgWaiter.Start(monitorCtx)
	defer s.bgWaiter.Stop()

	if s.requireApproval {
		go s.readApprovalResponses()
	}

	consecutiveNoToolCalls := 0

	for s.completedTurns < s.maxTurns {
		s.maybeRollover()

		turn := s.completedTurns + 1
		if s.completedTurns == 0 {
			s.dispatchHooks(domain.HookPreSession, turn)
		}
		s.dispatchHooks(domain.HookPreStream, turn)

		if err := s.executeTurn(); err != nil {
			logger.Error("turn execution failed", "error", err, "turn", s.completedTurns)
			return err
		}

		s.dispatchHooks(domain.HookPostTool, turn)
		s.completedTurns++

		if !s.lastResponseHadNoToolCalls() {
			consecutiveNoToolCalls = 0
			continue
		}

		injected := s.drainBackgroundResults(monitorCtx)
		if injected > 0 {
			consecutiveNoToolCalls = 0
			continue
		}

		consecutiveNoToolCalls++

		if consecutiveNoToolCalls >= 2 {
			logger.Info("task appears complete (no more tool calls)", "turns", s.completedTurns)
			break
		}

		verifyMsg := ConversationMessage{
			Role: "user",
			Content: `<system-reminder>
This is an automated check, not a message from the user. If more work is needed to fully address the user's last request, continue now. If everything is already done, stop without replying - do NOT announce completion, summarize, or otherwise mention this check to the user.
</system-reminder>`,
			Timestamp: time.Now(),
			Internal:  true,
		}
		s.addMessage(verifyMsg)
	}

	if s.completedTurns >= s.maxTurns {
		logger.Info("maximum turns reached", "turns", s.completedTurns)
	}

	s.dispatchHooks(domain.HookPostSession, s.completedTurns)

	s.waitForBackgroundTasks(monitorCtx)

	logger.Info("agent session completed", "turns", s.completedTurns)
	return nil
}

// waitForBackgroundTasks is the post-loop final-wait. Ensures we never exit
// `infer agent` with in-flight background work. If draining surfaces new
// completion messages, runs one final integration turn.
func (s *AgentSession) waitForBackgroundTasks(ctx context.Context) {
	if !s.bgWaiter.HasPendingTasks() {
		return
	}

	injected := s.drainBackgroundResults(ctx)
	if injected == 0 {
		return
	}

	logger.Info("running final integration turn after background task drain",
		"injected", injected)
	if err := s.executeTurn(); err != nil {
		logger.Error("final integration turn failed", "error", err)
	}
}

// drainBackgroundResults blocks on the waiter until background tasks complete
// (or the per-session timeout fires), then materializes the drained payloads
// as internal user messages on the conversation. Returns the number of
// messages injected.
func (s *AgentSession) drainBackgroundResults(ctx context.Context) int {
	drained := s.bgWaiter.WaitAndDrain(ctx)
	if len(drained) == 0 {
		return 0
	}

	for _, d := range drained {
		msg := ConversationMessage{
			Role:      "user",
			Content:   d.Content,
			Timestamp: d.Timestamp,
			Internal:  true,
		}
		s.addMessage(msg)
		s.outputMessage(msg)
	}

	logger.Info("injected background task results into conversation", "count", len(drained))
	return len(drained)
}

func (s *AgentSession) executeTurn() error {
	ctx := context.Background()
	requestID := uuid.New().String()

	messages := s.buildSDKMessages()

	req := &domain.AgentRequest{
		RequestID: requestID,
		Model:     s.model,
		Messages:  messages,
	}

	response, err := s.agentService.Run(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return s.processSyncResponse(response, requestID)
}

func (s *AgentSession) buildSDKMessages() []sdk.Message {
	var messages []sdk.Message

	for _, msg := range s.conversation {
		content := s.buildMessageContent(msg)

		sdkMsg := sdk.Message{
			Role:    sdk.MessageRole(msg.Role),
			Content: content,
		}

		if msg.ReasoningContent != "" {
			reasoning := msg.ReasoningContent
			sdkMsg.Reasoning = &reasoning
			sdkMsg.ReasoningContent = &reasoning
		}

		if msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
			sdkMsg.ToolCalls = msg.ToolCalls
		}

		if msg.ToolCallID != "" {
			sdkMsg.ToolCallID = &msg.ToolCallID
		}

		messages = append(messages, sdkMsg)
	}

	repaired, synthetics := services.EnsureToolCallsClosed(messages)
	if len(synthetics) > 0 {
		logger.Warn("resumed conversation had orphan tool_calls; patched in-memory",
			"count", len(synthetics))
	}
	return repaired
}

func (s *AgentSession) buildMessageContent(msg ConversationMessage) sdk.MessageContent {
	if len(msg.Images) == 0 {
		return sdk.NewMessageContent(msg.Content)
	}

	contentParts := s.buildContentParts(msg)
	if contentParts == nil {
		return sdk.NewMessageContent(msg.Content)
	}

	return sdk.NewMessageContent(contentParts)
}

func (s *AgentSession) buildContentParts(msg ConversationMessage) []sdk.ContentPart {
	textPart, err := sdk.NewTextContentPart(msg.Content)
	if err != nil {
		logger.Warn("failed to create text content part", "error", err)
		return nil
	}

	contentParts := []sdk.ContentPart{textPart}

	for _, img := range msg.Images {
		dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)
		imagePart, err := sdk.NewImageContentPart(dataURL, nil)
		if err != nil {
			logger.Warn("failed to create image content part", "filename", img.Filename, "error", err)
			continue
		}
		contentParts = append(contentParts, imagePart)
	}

	return contentParts
}

func (s *AgentSession) processSyncResponse(response *domain.ChatSyncResponse, requestID string) error {
	if response.Usage != nil {
		s.totalPromptTokens += int(response.Usage.PromptTokens)
		s.totalCompletionTokens += int(response.Usage.CompletionTokens)
		s.totalTokens += int(response.Usage.TotalTokens)
		s.requestCount++
	}

	if response.Content == "" && len(response.ToolCalls) == 0 {
		return nil
	}

	assistantMsg := ConversationMessage{
		Role:             "assistant",
		Content:          response.Content,
		ReasoningContent: response.ReasoningContent,
		TokenUsage:       response.Usage,
		Timestamp:        time.Now(),
		RequestID:        requestID,
	}

	if len(response.ToolCalls) > 0 {
		assistantMsg.ToolCalls = &response.ToolCalls
	}

	s.addMessage(assistantMsg)
	s.outputMessage(assistantMsg)

	if s.saveEnabled && s.conversationRepo != nil && response.Usage != nil {
		go func() {
			if err := s.conversationRepo.AddTokenUsage(
				s.model,
				int(response.Usage.PromptTokens),
				int(response.Usage.CompletionTokens),
				int(response.Usage.TotalTokens),
			); err != nil {
				logger.Warn("failed to track token usage", "error", err)
			}
		}()
	}

	if len(response.ToolCalls) == 0 {
		return nil
	}

	toolResults := s.executeToolCalls(response.ToolCalls)

	for _, result := range toolResults {
		s.addMessage(result)
		s.outputMessage(result)
	}

	return nil
}

// executeToolCall runs a single tool. Headless normally runs in standard mode (a
// restricted allow-list) with off-list/mutating actions gated upstream by
// approval_behaviour; when spawned by the Agent tool from a non-Standard parent
// it inherits that mode (s.agentMode, from INFER_SUBAGENT_AGENT_MODE) instead.
// The mode is carried on the context so the Bash tool resolves the right
// per-mode allow-list. approved is set only after an explicit IPC approval,
// marking the context so an off-list but user-approved command is allowed to run.
func (s *AgentSession) executeToolCall(toolName, args string, approved bool) (*domain.ToolExecutionResult, error) {
	var argsMap map[string]any
	if err := json.Unmarshal([]byte(args), &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	ctx := domain.WithModel(domain.WithAgentMode(domain.WithSessionID(context.Background(), s.sessionID), s.agentMode), s.model)
	if approved {
		ctx = domain.WithToolApproved(ctx)
	}

	toolCall := sdk.ChatCompletionMessageToolCallFunction{
		Name:      toolName,
		Arguments: args,
	}
	return s.toolService.ExecuteTool(ctx, toolCall)
}

func (s *AgentSession) executeToolCallsParallel(toolCalls []sdk.ChatCompletionMessageToolCall) []ConversationMessage {
	if len(toolCalls) == 0 {
		return []ConversationMessage{}
	}

	logger.Info("executing tool calls in parallel", "count", len(toolCalls), "max_concurrent", s.config.Agent.MaxConcurrentTools)

	results := make([]ConversationMessage, len(toolCalls))
	semaphore := make(chan struct{}, s.config.Agent.MaxConcurrentTools)

	var wg sync.WaitGroup
	for i, toolCall := range toolCalls {
		wg.Add(1)
		go func(index int, tc sdk.ChatCompletionMessageToolCall) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			result, err := s.executeToolCall(tc.Function.Name, tc.Function.Arguments, false)
			if err != nil {
				logger.Error("tool execution failed", "tool", tc.Function.Name, "error", err)
				errorResult := &domain.ToolExecutionResult{
					ToolName: tc.Function.Name,
					Success:  false,
					Error:    err.Error(),
				}
				results[index] = ConversationMessage{
					Role:          "tool",
					Content:       fmt.Sprintf("Tool execution failed: %s", err.Error()),
					ToolCallID:    tc.ID,
					ToolExecution: errorResult,
					Timestamp:     time.Now(),
				}
				return
			}

			results[index] = ConversationMessage{
				Role:          "tool",
				Content:       s.formatToolResult(result),
				ToolCallID:    tc.ID,
				ToolExecution: result,
				Timestamp:     time.Now(),
			}
		}(i, toolCall)
	}

	wg.Wait()
	return results
}

// readApprovalResponses reads JSON approval responses from stdin and dispatches them to approvalCh.
func (s *AgentSession) readApprovalResponses() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp domain.ApprovalResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			logger.Warn("failed to parse approval response from stdin", "error", err)
			continue
		}
		if resp.Type != "approval_response" {
			continue
		}

		s.approvalCh <- resp
	}

	if err := scanner.Err(); err != nil {
		logger.Warn("error reading approval responses from stdin", "error", err)
	}
}

// toolResultMessage builds the conversation message for a finished tool call,
// formatting either the successful result or the execution error.
func (s *AgentSession) toolResultMessage(tc sdk.ChatCompletionMessageToolCall, result *domain.ToolExecutionResult, err error) ConversationMessage {
	if err != nil {
		return ConversationMessage{
			Role:       "tool",
			Content:    fmt.Sprintf("Tool execution failed: %s", err.Error()),
			ToolCallID: tc.ID,
			ToolExecution: &domain.ToolExecutionResult{
				ToolName: tc.Function.Name,
				Success:  false,
				Error:    err.Error(),
			},
			Timestamp: time.Now(),
		}
	}
	return ConversationMessage{
		Role:          "tool",
		Content:       s.formatToolResult(result),
		ToolCallID:    tc.ID,
		ToolExecution: result,
		Timestamp:     time.Now(),
	}
}

// toolRejectedMessage builds a non-executed tool message (rejected by the user or
// blocked by policy) carrying a reason the model can act on.
func (s *AgentSession) toolRejectedMessage(tc sdk.ChatCompletionMessageToolCall, content, errStr string) ConversationMessage {
	return ConversationMessage{
		Role:       "tool",
		Content:    content,
		ToolCallID: tc.ID,
		ToolExecution: &domain.ToolExecutionResult{
			ToolName: tc.Function.Name,
			Success:  false,
			Error:    errStr,
			Rejected: true,
		},
		Timestamp: time.Now(),
	}
}

// executeToolCalls runs a turn's tool calls. Tools that don't need approval run in
// parallel; tools that do are handled per tools.safety.approval_behaviour (via
// config.ResolveApprovalDelivery): delivered over IPC when an approval broker is
// attached (--require-approval, e.g. the channel manager) or blocked with a reason
// otherwise (CI/heartbeat). The agent is headless, so an interactive TUI prompt is
// never an option here - require-approval/ipc without a broker resolve to block,
// which is what keeps unattended runs from executing anything off the allow-list.
func (s *AgentSession) executeToolCalls(toolCalls []sdk.ChatCompletionMessageToolCall) []ConversationMessage {
	if len(toolCalls) == 0 {
		return []ConversationMessage{}
	}

	results := make([]ConversationMessage, len(toolCalls))

	type indexedCall struct {
		index int
		call  sdk.ChatCompletionMessageToolCall
	}
	var approvalRequired, autoApproved []indexedCall
	for i, tc := range toolCalls {
		if s.isToolApprovalRequired(tc) {
			approvalRequired = append(approvalRequired, indexedCall{i, tc})
		} else {
			autoApproved = append(autoApproved, indexedCall{i, tc})
		}
	}

	if len(autoApproved) > 0 {
		autoCalls := make([]sdk.ChatCompletionMessageToolCall, len(autoApproved))
		for j, ic := range autoApproved {
			autoCalls[j] = ic.call
		}
		autoResults := s.executeToolCallsParallel(autoCalls)
		for j, ic := range autoApproved {
			results[ic.index] = autoResults[j]
		}
	}

	for _, ic := range approvalRequired {
		results[ic.index] = s.deliverApprovalRequiredTool(ic.call)
	}

	return results
}

// deliverApprovalRequiredTool handles one tool call that needs approval per the
// configured approval_behaviour: request IPC approval (and run it on approval), or
// block it with an actionable reason when no approver is reachable.
func (s *AgentSession) deliverApprovalRequiredTool(tc sdk.ChatCompletionMessageToolCall) ConversationMessage {
	behaviour := s.config.ApprovalBehaviourFor(tc.Function.Name)
	if config.ResolveApprovalDelivery(behaviour, s.requireApproval, false) != config.ApprovalBehaviourIPC {
		reason := fmt.Sprintf(
			"Blocked: %q was not auto-approved and no approver is available in this run "+
				"(tools.safety.approval_behaviour=%s). Do not retry the same call - use an allowed "+
				"command or tool, or stop and tell the user exactly what you need and why.",
			tc.Function.Name, behaviour)
		logger.Info("tool blocked (no approver reachable)", "tool", tc.Function.Name, "behaviour", behaviour)
		return s.toolRejectedMessage(tc, reason, reason)
	}

	s.outputApprovalRequest(tc)
	approved := false
	select {
	case resp := <-s.approvalCh:
		approved = resp.Approved
	case <-time.After(5 * time.Minute):
		logger.Warn("approval timeout for tool", "tool", tc.Function.Name)
	}
	if !approved {
		return s.toolRejectedMessage(tc,
			fmt.Sprintf("Tool '%s' was rejected by the user.", tc.Function.Name),
			"tool execution rejected by user")
	}

	result, err := s.executeToolCall(tc.Function.Name, tc.Function.Arguments, true)
	return s.toolResultMessage(tc, result, err)
}

// isToolApprovalRequired checks if a tool requires user approval based on config.
func (s *AgentSession) isToolApprovalRequired(tc sdk.ChatCompletionMessageToolCall) bool {
	// Auto-accept bypasses approval entirely, matching chat YOLO mode. A headless
	// run only reaches this mode when spawned by the Agent tool from an
	// auto-accept parent (via INFER_SUBAGENT_AGENT_MODE); top-level runs stay
	// Standard and fall through to the per-mode gating below.
	if s.agentMode == domain.AgentModeAutoAccept {
		return false
	}
	if tc.Function.Name == "Bash" {
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return true
		}
		command, ok := args["command"].(string)
		if !ok {
			return true
		}
		// An allowed command (per the session's mode allow-list) runs without
		// approval; an off-list one needs approval and is then delivered per
		// tools.safety.approval_behaviour (IPC when a broker is attached, else
		// blocked) by deliverApprovalRequiredTool.
		if s.config.IsBashCommandAllowed(command, s.agentMode.AllowedlistKey()) {
			return false
		}
	}
	return s.config.IsApprovalRequired(tc.Function.Name)
}

// outputAgentError emits an agent_error JSON line on stdout so the channels
// manager can forward the failure to the user. Long messages are truncated to
// stay within channel transport limits (Telegram caps messages at ~4096 chars).
func outputAgentError(message string) {
	const maxLen = 3500
	if len(message) > maxLen {
		message = message[:maxLen] + "…"
	}
	msg := domain.AgentErrorMessage{Type: "agent_error", Message: message}
	out, err := json.Marshal(msg)
	if err != nil {
		logger.Error("failed to marshal agent error", "error", err)
		return
	}
	if _, werr := os.Stdout.Write(append(out, '\n')); werr != nil {
		logger.Error("failed to write agent error to stdout", "error", werr)
	}
}

// outputApprovalRequest writes an approval request JSON line to stdout for the channel manager.
func (s *AgentSession) outputApprovalRequest(tc sdk.ChatCompletionMessageToolCall) {
	req := domain.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   tc.Function.Name,
		ToolArgs:   tc.Function.Arguments,
		ToolCallID: tc.ID,
	}
	output, err := json.Marshal(req)
	if err != nil {
		logger.Error("failed to marshal approval request", "error", err)
		return
	}
	fmt.Println(string(output))
}

func (s *AgentSession) formatToolResult(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if !result.Success {
		detail := result.Error
		if detail == "" && result.Data != nil {
			if b, err := json.Marshal(result.Data); err == nil {
				detail = string(b)
			}
		}
		return fmt.Sprintf("Tool execution failed: %s", detail)
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("Result of tool call: %v", result.Data)
	}

	return fmt.Sprintf("Result of tool call: %s", string(resultBytes))
}

// convertToConversationEntry converts a ConversationMessage to domain.ConversationEntry
func (s *AgentSession) convertToConversationEntry(msg ConversationMessage) domain.ConversationEntry {
	var sdkMsg sdk.Message
	sdkMsg.Role = sdk.MessageRole(msg.Role)
	sdkMsg.Content = sdk.NewMessageContent(msg.Content)

	if msg.ReasoningContent != "" {
		reasoning := msg.ReasoningContent
		sdkMsg.Reasoning = &reasoning
		sdkMsg.ReasoningContent = &reasoning
	}

	if msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
		sdkMsg.ToolCalls = msg.ToolCalls
	}

	if msg.ToolCallID != "" {
		sdkMsg.ToolCallID = &msg.ToolCallID
	}

	entry := domain.ConversationEntry{
		Message:          sdkMsg,
		Model:            s.model,
		Time:             msg.Timestamp,
		Images:           msg.Images,
		Hidden:           msg.Internal,
		ReasoningContent: msg.ReasoningContent,
		ToolExecution:    msg.ToolExecution,
	}

	return entry
}

// convertFromConversationEntry converts domain.ConversationEntry back to ConversationMessage
// This is the reverse of convertToConversationEntry, used for loading saved conversations
func (s *AgentSession) convertFromConversationEntry(entry domain.ConversationEntry) ConversationMessage {
	msg := ConversationMessage{
		Role:      string(entry.Message.Role),
		Timestamp: entry.Time,
		Images:    entry.Images,
		Internal:  entry.Hidden,
	}

	if contentStr, err := entry.Message.Content.AsMessageContent0(); err == nil {
		msg.Content = contentStr
	}

	switch {
	case entry.ReasoningContent != "":
		msg.ReasoningContent = entry.ReasoningContent
	case entry.Message.ReasoningContent != nil && *entry.Message.ReasoningContent != "":
		msg.ReasoningContent = *entry.Message.ReasoningContent
	case entry.Message.Reasoning != nil && *entry.Message.Reasoning != "":
		msg.ReasoningContent = *entry.Message.Reasoning
	}

	if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 {
		msg.ToolCalls = entry.Message.ToolCalls
	}

	if entry.Message.ToolCallID != nil {
		msg.ToolCallID = *entry.Message.ToolCallID
	}

	if entry.ToolExecution != nil {
		msg.ToolExecution = entry.ToolExecution
	}

	return msg
}

// dispatchHooks runs the actions attached to a hook point for the synchronous
// `infer agent` loop, mirroring AgentServiceImpl.dispatchHooks: system-reminder
// injection (text action) and command hooks (executable action, #270), both via
// the same shared providers so the two callers behave identically. The loop
// wires only the clean-boundary points (pre_session, pre_stream, post_tool,
// post_session); the mid-turn points (post_stream, pre_tool) and queue_drain are
// event-driven-only. Single-goroutine, so no mutex. A headless run IS the
// session, so the per-request turn doubles as the session turn.
func (s *AgentSession) dispatchHooks(hook domain.HookPoint, turn int) {
	s.injectDueReminders(hook, turn)
	agent.RunCommandHooks(context.Background(), s.config, s.hookProvider, s.agentMode.AllowedlistKey(), hook, turn, s.sessionID)
}

// injectDueReminders appends any reminders due at the hook point as internal user
// messages. Skipped while awaiting tool results (a user message would orphan the
// pending tool_calls) - that guard is reminder-specific and must not block
// command hooks, which is why it lives here rather than in dispatchHooks.
func (s *AgentSession) injectDueReminders(hook domain.HookPoint, turn int) {
	provider := s.reminderProvider
	if provider == nil && s.config != nil {
		provider = s.config.Reminders
	}
	if provider == nil {
		return
	}
	if s.firedReminders == nil {
		s.firedReminders = make(map[string]bool)
	}

	if n := len(s.conversation); n > 0 {
		last := s.conversation[n-1]
		if last.Role == "assistant" && last.ToolCalls != nil && len(*last.ToolCalls) > 0 {
			return
		}
	}

	q := domain.ReminderQuery{
		Hook:        hook,
		Turn:        turn,
		SessionTurn: turn,
		MaxTurns:    s.maxTurns,
		Fired:       s.firedReminders,
	}
	for _, r := range provider.RemindersDue(q) {
		s.addMessage(ConversationMessage{
			Role:      "user",
			Content:   r.Text,
			Timestamp: time.Now(),
			Internal:  true,
		})
		logger.Info("system reminder injected",
			"turn", turn,
			"hook", string(hook),
			"name", r.Name,
			"reminder_chars", len(r.Text),
		)
		streamevent.EmitDebugMessage("user", r.Text, "system_reminder", map[string]any{
			"turn":         turn,
			"session_turn": turn,
			"hook":         string(hook),
			"name":         r.Name,
		})
		s.firedReminders[r.Name] = true
	}
}

func (s *AgentSession) addMessage(msg ConversationMessage) {
	s.conversation = append(s.conversation, msg)

	if s.saveEnabled && s.conversationRepo != nil {
		entry := s.convertToConversationEntry(msg)

		go func() {
			if err := s.conversationRepo.AddMessage(entry); err != nil {
				logger.Warn("failed to persist agent message", "error", err, "role", msg.Role)
			}
		}()
	}
}

// initializeSession sets up the session with the given ID and attempts to load
// existing conversation history. Returns (true, nil) if history was loaded,
// (false, nil) if starting fresh with the provided ID, or (false, err) on error.
func (s *AgentSession) initializeSession(sessionID string) (bool, error) {
	s.sessionID = sessionID

	persistentRepo, ok := s.conversationRepo.(*services.PersistentConversationRepository)
	if !ok {
		return false, nil
	}

	persistentRepo.SetConversationID(sessionID)

	ctx := context.Background()
	if err := persistentRepo.LoadConversation(ctx, sessionID); err != nil {
		return false, nil
	}

	entries := persistentRepo.GetMessages()
	if len(entries) == 0 {
		return false, nil
	}

	s.conversation = make([]ConversationMessage, 0, len(entries))
	for _, entry := range entries {
		msg := s.convertFromConversationEntry(entry)
		s.conversation = append(s.conversation, msg)
	}

	logger.Info("loaded conversation history",
		"session_id", sessionID,
		"message_count", len(entries))

	return true, nil
}

func (s *AgentSession) outputMessage(msg ConversationMessage) {
	if msg.Role == "system" || msg.Internal {
		return
	}

	logMsg := msg

	if !s.config.Agent.VerboseTools && msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
		summaries := make([]string, len(*msg.ToolCalls))
		for i, toolCall := range *msg.ToolCalls {
			summaries[i] = formatToolCallSummary(toolCall.Function.Name, toolCall.Function.Arguments)
		}
		logMsg.Tools = summaries
	}

	output, err := json.Marshal(logMsg)
	if err != nil {
		logger.Error("failed to marshal message", "error", err)
		return
	}

	fmt.Println(string(output))
}

// formatToolCallSummary builds a one-line "Name(key=val, key=val)" preview of
// a tool call, with each value truncated and newlines stripped so it renders
// cleanly in remote channels (Telegram, etc.). When arguments cannot be
// parsed or are empty, the bare tool name is returned.
func formatToolCallSummary(name, argsJSON string) string {
	if argsJSON == "" || argsJSON == "{}" {
		return name
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || len(args) == 0 {
		return name
	}

	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	const maxValueChars = 60
	const maxTotalChars = 200
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		raw := fmt.Sprintf("%v", args[k])
		raw = strings.ReplaceAll(raw, "\n", " ")
		raw = strings.ReplaceAll(raw, "\r", " ")
		raw = strings.TrimSpace(raw)
		if len(raw) > maxValueChars {
			raw = raw[:maxValueChars-1] + "…"
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, raw))
	}

	summary := strings.Join(parts, ", ")
	if len(summary) > maxTotalChars {
		summary = summary[:maxTotalChars-1] + "…"
	}
	return fmt.Sprintf("%s(%s)", name, summary)
}

// outputStatusMessage outputs a structured JSON status message
func (s *AgentSession) outputStatusMessage(messageType, message string, metadata map[string]any) {
	statusMsg := map[string]any{
		"type":      messageType,
		"message":   message,
		"timestamp": time.Now(),
	}

	for k, v := range metadata {
		statusMsg[k] = v
	}

	output, err := json.Marshal(statusMsg)
	if err != nil {
		logger.Error("failed to marshal status message", "error", err)
		return
	}

	fmt.Println(string(output))
}

// emitSessionStats emits a single structured session_stats JSON line summarizing token usage and
// dollar cost for this run. Token counts come from session-local accumulators (independent of
// --no-save); cost is computed via the shared PricingService so the pricing table is never
// duplicated. Invoked via defer in execute() so it fires on completion, early error, and panic
// unwind. Suppressed when no usage-bearing request occurred.
func (s *AgentSession) emitSessionStats() {
	if s.requestCount == 0 {
		return
	}

	var inputCost, outputCost, totalCost float64
	if s.pricingService != nil {
		inputCost, outputCost, totalCost = s.pricingService.CalculateCost(
			s.model, s.totalPromptTokens, s.totalCompletionTokens)
	}

	currency := "USD"
	if s.config != nil && s.config.Pricing.Currency != "" {
		currency = s.config.Pricing.Currency
	}

	s.outputStatusMessage("session_stats", "Session complete", map[string]any{
		"model":             s.model,
		"prompt_tokens":     s.totalPromptTokens,
		"completion_tokens": s.totalCompletionTokens,
		"total_tokens":      s.totalTokens,
		"requests":          s.requestCount,
		"cost": map[string]any{
			"input":    inputCost,
			"output":   outputCost,
			"total":    totalCost,
			"currency": currency,
		},
	})
}

func (s *AgentSession) lastResponseHadNoToolCalls() bool {
	if len(s.conversation) < 2 {
		return false
	}

	for i := len(s.conversation) - 1; i >= 0; i-- {
		msg := s.conversation[i]
		if msg.Role == "assistant" {
			return msg.ToolCalls == nil || len(*msg.ToolCalls) == 0
		}
	}

	return false
}

func selectModel(models []string, modelFlag, defaultModel string) (string, error) {
	if modelFlag != "" {
		if !isModelAvailable(models, modelFlag) {
			return "", fmt.Errorf("model '%s' is not available. Available models: %v", modelFlag, models)
		}
		return modelFlag, nil
	}

	if defaultModel != "" {
		if !isModelAvailable(models, defaultModel) {
			return "", fmt.Errorf("default model '%s' is not available. Available models: %v", defaultModel, models)
		}
		return defaultModel, nil
	}

	return "", fmt.Errorf("no model specified. Please use --model flag or set a default model with 'infer config set agent.model <model>'")
}

func isModelAvailable(models []string, targetModel string) bool {
	for _, model := range models {
		if model == targetModel {
			return true
		}
	}
	return false
}

func init() {
	agentCmd.Flags().StringP("model", "m", "", "Model to use for the agent (e.g., openai/gpt-4)")
	agentCmd.Flags().StringSliceP("files", "f", []string{}, "Files or images to include (e.g., -f image.png -f code.go)")
	agentCmd.Flags().Bool("no-save", false, "Disable saving conversation to database")
	agentCmd.Flags().String("session-id", "", "Resume an existing agent session by conversation ID")
	agentCmd.Flags().Bool("require-approval", false, "Enable IPC-based tool approval via stdin/stdout (used by channel manager)")
	agentCmd.Flags().Bool("heartbeat", false, "Run with the heartbeat system prompt (used by the heartbeat service)")
	agentCmd.Flags().Bool("remote", false, "Run with the remote-control system prompt (used by the channels-manager daemon)")
	agentCmd.Flags().String("result-file", "", "Write the final assistant message and outcome as JSON to this path on exit (used by the Agent tool to harvest detached subagents)")
	rootCmd.AddCommand(agentCmd)
}
