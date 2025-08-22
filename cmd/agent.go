package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent [task description]",
	Short: "Execute a task using an autonomous agent in background mode",
	Long: `Execute a task using an autonomous agent in background mode. The CLI will work iteratively
until the task is considered complete. Particularly useful for SCM tickets like GitHub issues.

Examples:
  infer agent "Please fix the github issue 38"
  infer agent --model "openai/gpt-4" "Implement the feature described in issue #42"
  infer agent "Debug the failing test in PR 15"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		model, _ := cmd.Flags().GetString("model")
		return runAgentCommand(args[0], model)
	},
}

// ConversationMessage represents a message in the JSON output conversation
type ConversationMessage struct {
	Role       string                               `json:"role"`
	Content    string                               `json:"content"`
	ToolCalls  *[]sdk.ChatCompletionMessageToolCall `json:"tool_calls,omitempty"`
	Tools      []string                             `json:"tools,omitempty"`
	ToolCallID string                               `json:"tool_call_id,omitempty"`
	TokenUsage *sdk.CompletionUsage                 `json:"token_usage,omitempty"`
	Timestamp  time.Time                            `json:"timestamp"`
	RequestID  string                               `json:"request_id,omitempty"`
	Internal   bool                                 `json:"-"`
}

// AgentSession manages the background execution session
type AgentSession struct {
	agentService   domain.AgentService
	toolService    domain.ToolService
	model          string
	conversation   []ConversationMessage
	sessionID      string
	maxTurns       int
	completedTurns int
	config         *config.Config
}

func runAgentCommand(taskDescription string, modelFlag string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	services := container.NewServiceContainer(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Gateway.Timeout)*time.Second)
	defer cancel()

	models, err := services.GetModelService().ListModels(ctx)
	if err != nil {
		return fmt.Errorf("inference gateway is not available: %w", err)
	}

	if len(models) == 0 {
		return fmt.Errorf("no models available from inference gateway")
	}

	selectedModel, err := selectModel(models, modelFlag, cfg.Agent.Model)
	if err != nil {
		return err
	}

	agentService := services.GetAgentService()
	toolService := services.GetToolService()

	session := &AgentSession{
		agentService: agentService,
		toolService:  toolService,
		model:        selectedModel,
		sessionID:    uuid.New().String(),
		maxTurns:     20,
		conversation: []ConversationMessage{},
		config:       cfg,
	}

	logger.Info("Starting agent session", "session_id", session.sessionID, "model", selectedModel)

	return session.execute(taskDescription)
}

func (s *AgentSession) execute(taskDescription string) error {
	s.addMessage(ConversationMessage{
		Role:      "user",
		Content:   taskDescription,
		Timestamp: time.Now(),
	})

	s.outputMessage(s.conversation[len(s.conversation)-1])

	consecutiveNoToolCalls := 0

	for s.completedTurns < s.maxTurns {
		if err := s.executeTurn(); err != nil {
			logger.Error("Turn execution failed", "error", err, "turn", s.completedTurns)
			return err
		}

		s.completedTurns++

		if s.lastResponseHadNoToolCalls() {
			consecutiveNoToolCalls++

			if consecutiveNoToolCalls >= 2 {
				logger.Info("Task appears complete (no more tool calls)", "turns", s.completedTurns)
				break
			}

			verifyMsg := ConversationMessage{
				Role:      "user",
				Content:   "Is there anything else that needs to be done to complete this task? If not, simply confirm the task is complete. If there is more work, please continue.",
				Timestamp: time.Now(),
				Internal:  true,
			}
			s.addMessage(verifyMsg)
		} else {
			consecutiveNoToolCalls = 0
		}
	}

	if s.completedTurns >= s.maxTurns {
		logger.Info("Maximum turns reached", "turns", s.completedTurns)
	}

	return nil
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

func (s *AgentSession) processSyncResponse(response *domain.ChatSyncResponse, requestID string) error {
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

func (s *AgentSession) executeToolCall(toolName, args string) (*domain.ToolExecutionResult, error) {
	var argsMap map[string]any
	if err := json.Unmarshal([]byte(args), &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	ctx := context.Background()
	return s.toolService.ExecuteTool(ctx, toolName, argsMap)
}

func (s *AgentSession) formatToolResult(result *domain.ToolExecutionResult) string {
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

func (s *AgentSession) addMessage(msg ConversationMessage) {
	s.conversation = append(s.conversation, msg)
}

func (s *AgentSession) outputMessage(msg ConversationMessage) {
	if msg.Role == "system" || msg.Internal {
		return
	}

	logMsg := msg

	if !s.config.Agent.VerboseTools && msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
		toolNames := make([]string, len(*msg.ToolCalls))
		for i, toolCall := range *msg.ToolCalls {
			toolNames[i] = toolCall.Function.Name
		}
		logMsg.ToolCalls = nil
		logMsg.Tools = toolNames
	}

	output, err := json.Marshal(logMsg)
	if err != nil {
		logger.Error("Failed to marshal message", "error", err)
		return
	}

	fmt.Println(string(output))
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
	agentCmd.Flags().StringP("model", "m", "", "Model to use for the agent (e.g., openai/gpt-4)")
	rootCmd.AddCommand(agentCmd)
}
