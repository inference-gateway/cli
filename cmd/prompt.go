package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
	"github.com/spf13/cobra"
)

var promptCmd = &cobra.Command{
	Use:   "prompt [prompt text]",
	Short: "Execute a one-off prompt task in background mode",
	Long: `Execute a one-off prompt task in background mode. The CLI will work iteratively
until the task is considered complete. Particularly useful for SCM tickets like GitHub issues.

Examples:
  infer prompt "Please fix the github issue 38"
  infer prompt --model "openai/gpt-4" "Implement the feature described in issue #42"
  infer prompt "Debug the failing test in PR 15"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		model, _ := cmd.Flags().GetString("model")
		return runPromptCommand(args[0], model)
	},
}

// ConversationMessage represents a message in the JSON output conversation
type ConversationMessage struct {
	Role       string                               `json:"role"`
	Content    string                               `json:"content"`
	ToolCalls  *[]sdk.ChatCompletionMessageToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                               `json:"tool_call_id,omitempty"`
	TokenUsage *sdk.CompletionUsage                 `json:"token_usage,omitempty"`
	Timestamp  time.Time                            `json:"timestamp"`
	RequestID  string                               `json:"request_id,omitempty"`
}

// PromptSession manages the background execution session
type PromptSession struct {
	services       *container.ServiceContainer
	model          string
	conversation   []ConversationMessage
	sessionID      string
	maxTurns       int
	completedTurns int
}

func runPromptCommand(promptText string, modelFlag string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	services := container.NewServiceContainer(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := services.GetModelService().ListModels(ctx)
	if err != nil {
		return fmt.Errorf("inference gateway is not available: %w", err)
	}

	if len(models) == 0 {
		return fmt.Errorf("no models available from inference gateway")
	}

	selectedModel, err := selectModel(models, modelFlag, cfg.Chat.DefaultModel)
	if err != nil {
		return err
	}

	session := &PromptSession{
		services:     services,
		model:        selectedModel,
		sessionID:    uuid.New().String(),
		maxTurns:     20,
		conversation: []ConversationMessage{},
	}

	logger.Info("Starting prompt session", "session_id", session.sessionID, "model", selectedModel)

	return session.execute(promptText)
}

func (s *PromptSession) execute(promptText string) error {
	s.addMessage(ConversationMessage{
		Role:      "user",
		Content:   promptText,
		Timestamp: time.Now(),
	})

	s.outputMessage(s.conversation[len(s.conversation)-1])

	for s.completedTurns < s.maxTurns {
		if err := s.executeTurn(); err != nil {
			logger.Error("Turn execution failed", "error", err, "turn", s.completedTurns)
			return err
		}

		s.completedTurns++

		if s.isTaskComplete() {
			logger.Info("Task appears to be complete", "turns", s.completedTurns)
			break
		}
	}

	if s.completedTurns >= s.maxTurns {
		logger.Info("Maximum turns reached", "turns", s.completedTurns)
	}

	return nil
}

func (s *PromptSession) executeTurn() error {
	ctx := context.Background()
	requestID := uuid.New().String()

	messages := s.buildSDKMessages()

	response, err := s.services.GetChatService().SendMessageSync(ctx, requestID, s.model, messages)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return s.processSyncResponse(response, requestID)
}

func (s *PromptSession) buildSDKMessages() []sdk.Message {
	var messages []sdk.Message

	for _, msg := range s.conversation {
		var role sdk.MessageRole
		switch msg.Role {
		case "user":
			role = sdk.User
		case "assistant":
			role = sdk.Assistant
		case "tool":
			role = sdk.Tool
		case "system":
			role = sdk.System
		default:
			role = sdk.User
		}

		sdkMsg := sdk.Message{
			Role:    role,
			Content: msg.Content,
		}

		if msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
			sdkMsg.ToolCalls = msg.ToolCalls
		}

		if msg.ToolCallID != "" {
			sdkMsg.ToolCallId = &msg.ToolCallID
		}

		messages = append(messages, sdkMsg)
	}

	return messages
}

func (s *PromptSession) processSyncResponse(response *domain.ChatSyncResponse, requestID string) error {
	if response.Content != "" {
		assistantMsg := ConversationMessage{
			Role:       "assistant",
			Content:    response.Content,
			TokenUsage: response.Usage,
			Timestamp:  time.Now(),
			RequestID:  requestID,
		}
		s.addMessage(assistantMsg)
		s.outputMessage(assistantMsg)
	}

	for _, toolCall := range response.ToolCalls {
		toolCallMsg := ConversationMessage{
			Role:      "assistant",
			Content:   "",
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{toolCall},
			Timestamp: time.Now(),
			RequestID: requestID,
		}
		s.addMessage(toolCallMsg)
		s.outputMessage(toolCallMsg)

		result, err := s.executeToolCall(toolCall.Function.Name, toolCall.Function.Arguments)
		if err != nil {
			logger.Error("Tool execution failed", "tool", toolCall.Function.Name, "error", err)
			continue
		}

		toolResultMsg := ConversationMessage{
			Role:       "tool",
			Content:    s.formatToolResult(result),
			ToolCallID: toolCall.Id,
			Timestamp:  time.Now(),
		}
		s.addMessage(toolResultMsg)
		s.outputMessage(toolResultMsg)
	}

	return nil
}

func (s *PromptSession) executeToolCall(toolName, args string) (*domain.ToolExecutionResult, error) {
	var argsMap map[string]any
	if err := json.Unmarshal([]byte(args), &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	ctx := context.Background()
	return s.services.GetToolService().ExecuteTool(ctx, toolName, argsMap)
}

func (s *PromptSession) formatToolResult(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if !result.Success {
		return fmt.Sprintf("Tool execution failed: %s", result.Error)
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("Result of tool call: %v", result.Data)
	}

	return fmt.Sprintf("Result of tool call: %s", string(resultBytes))
}

func (s *PromptSession) addMessage(msg ConversationMessage) {
	s.conversation = append(s.conversation, msg)
}

func (s *PromptSession) outputMessage(msg ConversationMessage) {
	output, err := json.Marshal(msg)
	if err != nil {
		logger.Error("Failed to marshal message", "error", err)
		return
	}

	fmt.Println(string(output))
}

func (s *PromptSession) isTaskComplete() bool {
	if len(s.conversation) < 2 {
		return false
	}

	lastMsg := s.conversation[len(s.conversation)-1]
	if lastMsg.Role != "assistant" {
		return false
	}

	content := strings.ToLower(lastMsg.Content)

	completionIndicators := []string{
		"task complete",
		"task is complete",
		"issue has been fixed",
		"issue is fixed",
		"problem has been resolved",
		"problem is resolved",
		"implementation complete",
		"implementation is complete",
		"fix has been applied",
		"fix applied",
		"successfully implemented",
		"successfully fixed",
		"done",
		"finished",
		"completed",
	}

	for _, indicator := range completionIndicators {
		if strings.Contains(content, indicator) {
			return true
		}
	}

	githubIssuePattern := regexp.MustCompile(`(?i)(issue\s+#?\d+|github\s+issue\s+#?\d+).*(?:fixed|resolved|completed|closed|done)`)
	if githubIssuePattern.MatchString(content) {
		return true
	}

	if s.completedTurns > 3 && lastMsg.ToolCalls == nil {
		noActionIndicators := []string{
			"no further action",
			"no additional steps",
			"nothing more to do",
			"task appears complete",
		}

		for _, indicator := range noActionIndicators {
			if strings.Contains(content, indicator) {
				return true
			}
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

	return "", fmt.Errorf("no model specified. Please use --model flag or set a default model with 'infer config set-model <model>'")
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
	promptCmd.Flags().StringP("model", "m", "", "Model to use for the prompt (e.g., openai/gpt-4)")
	rootCmd.AddCommand(promptCmd)
}
