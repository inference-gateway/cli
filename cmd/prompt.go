package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/inference-gateway/cli/internal/ui"
	sdk "github.com/inference-gateway/sdk"
	"github.com/spf13/cobra"
)

var promptCmd = &cobra.Command{
	Use:   "prompt [prompt_text]",
	Short: "Execute a one-off prompt in background mode",
	Long: `Execute a one-off prompt that runs in background mode until the task is complete.
This command can work with URLs (including GitHub issues) using the Fetch tool.

Examples:
  infer prompt "Please analyze https://github.com/owner/repo/issues/123"
  infer prompt "Help me understand this issue: https://github.com/owner/repo/issues/456"
  infer prompt "Optimize the database queries in the user service"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		promptText := args[0]
		return executeBackgroundPrompt(promptText)
	},
}

// BackgroundExecutor handles background execution of prompts
type BackgroundExecutor struct {
	services      *container.ServiceContainer
	maxIterations int
}

// executeBackgroundPrompt executes a prompt in background mode
func executeBackgroundPrompt(promptText string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	serviceContainer := container.NewServiceContainer(cfg)

	executor := &BackgroundExecutor{
		services:      serviceContainer,
		maxIterations: 10,
	}

	logger.Info("background_execution_starting", "prompt_text", promptText)
	return executor.Execute(promptText)
}

// Execute runs the background prompt execution
func (e *BackgroundExecutor) Execute(promptText string) error {
	ctx := context.Background()

	model, err := e.selectModelRobust(ctx)
	if err != nil {
		return fmt.Errorf("failed to select model: %w", err)
	}

	logger.Info("model_selected", "model", model)

	// Use system prompt from config
	cfg := e.services.GetConfig()
	systemPrompt := cfg.Chat.SystemPrompt

	logger.Debug("background_execution_started",
		"model", model,
		"prompt_text", promptText,
		"system_prompt", systemPrompt)

	return e.executeIteratively(ctx, model, systemPrompt, promptText)
}



// selectModelRobust selects the configured default model only
func (e *BackgroundExecutor) selectModelRobust(ctx context.Context) (string, error) {
	cfg := e.services.GetConfig()

	if cfg.Chat.DefaultModel == "" {
		return "", fmt.Errorf("no default model configured in .infer/config.yaml")
	}

	if err := e.services.GetModelService().SelectModel(cfg.Chat.DefaultModel); err != nil {
		return "", fmt.Errorf("failed to select configured default model '%s': %w", cfg.Chat.DefaultModel, err)
	}

	return cfg.Chat.DefaultModel, nil
}


// sendMessageDirectWithToolCalls sends a message and returns both content and tool calls
func (e *BackgroundExecutor) sendMessageDirectWithToolCalls(ctx context.Context, model string, messages []sdk.Message) (string, []sdk.ChatCompletionMessageToolCall, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid model format, expected 'provider/model'")
	}
	provider := parts[0]
	modelName := parts[1]

	cfg := e.services.GetConfig()
	client := sdk.NewClient(&sdk.ClientOptions{
		BaseURL: strings.TrimSuffix(cfg.Gateway.URL, "/") + "/v1",
		APIKey:  cfg.Gateway.APIKey,
	})

	messages = e.addToolsIfAvailable(messages)

	providerType := sdk.Provider(provider)
	response, err := client.GenerateContent(ctx, providerType, modelName, messages)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate content: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", nil, fmt.Errorf("no choices in response")
	}

	choice := response.Choices[0]
	content := choice.Message.Content
	var toolCalls []sdk.ChatCompletionMessageToolCall

	if choice.Message.ToolCalls != nil {
		toolCalls = *choice.Message.ToolCalls
	}

	return content, toolCalls, nil
}

// addToolsIfAvailable adds tools to messages if tool service is available
func (e *BackgroundExecutor) addToolsIfAvailable(messages []sdk.Message) []sdk.Message {
	toolService := e.services.GetToolService()
	if toolService == nil {
		return messages
	}

	availableTools := toolService.ListTools()
	if len(availableTools) == 0 {
		return messages
	}

	toolsMessage := e.createToolsSystemMessage(availableTools)

	var result []sdk.Message
	systemAdded := false

	for _, msg := range messages {
		if msg.Role == sdk.System && !systemAdded {
			result = append(result, msg, toolsMessage)
			systemAdded = true
		} else {
			result = append(result, msg)
		}
	}

	if !systemAdded {
		result = append([]sdk.Message{toolsMessage}, result...)
	}

	return result
}

// createToolsSystemMessage creates a system message describing available tools
func (e *BackgroundExecutor) createToolsSystemMessage(tools []domain.ToolDefinition) sdk.Message {
	content := "You have access to the following tools:\n\n"

	for _, tool := range tools {
		content += fmt.Sprintf("- **%s**: %s\n", tool.Name, tool.Description)
	}

	content += "\nTo use a tool, respond with a tool call using the proper format. The system will execute the tool and provide you with the results."

	return sdk.Message{
		Role:    sdk.System,
		Content: content,
	}
}

// executeIteratively executes the prompt iteratively until completion
func (e *BackgroundExecutor) executeIteratively(ctx context.Context, model, systemPrompt, promptText string) error {
	messages := []sdk.Message{
		{Role: sdk.System, Content: systemPrompt},
		{Role: sdk.User, Content: promptText},
	}

	for iteration := 1; iteration <= e.maxIterations; iteration++ {
		logger.Info("iteration_starting", "iteration", iteration, "max_iterations", e.maxIterations)

		logger.Debug("sending_message_to_model",
			"iteration", iteration,
			"model", model,
			"message_count", len(messages),
			"messages", messages)

		response, toolCalls, err := e.sendMessageDirectWithToolCalls(ctx, model, messages)
		if err != nil {
			logger.Error("failed_to_send_message", "error", err, "model", model)
			return fmt.Errorf("failed to send message: %w", err)
		}

		logger.Debug("received_assistant_response",
			"iteration", iteration,
			"response_length", len(response),
			"tool_calls_count", len(toolCalls))

		logger.Info("assistant_response", "iteration", iteration, "content", response)

		assistantMsg := sdk.Message{
			Role:    sdk.Assistant,
			Content: response,
		}

		if len(toolCalls) > 0 {
			assistantMsg.ToolCalls = &toolCalls
		}

		messages = append(messages, assistantMsg)

		if len(toolCalls) > 0 {
			logger.Info("processing_tool_calls", "count", len(toolCalls), "iteration", iteration)

			toolResultsProcessed := false
			for _, toolCall := range toolCalls {
				logger.Info("executing_tool", "tool_name", toolCall.Function.Name, "iteration", iteration)

				toolResult, err := e.executeToolCall(ctx, toolCall)
				if err != nil {
					logger.Error("tool_execution_failed", "tool_name", toolCall.Function.Name, "error", err)
					toolResult = fmt.Sprintf("Tool execution failed: %v", err)
				}

				toolResultMsg := sdk.Message{
					Role:       sdk.Tool,
					Content:    toolResult,
					ToolCallId: &toolCall.Id,
				}
				messages = append(messages, toolResultMsg)
				toolResultsProcessed = true

				logger.Info("tool_result", "tool_name", toolCall.Function.Name, "result", toolResult)
			}

			if toolResultsProcessed {
				continue
			}
		}

		if e.isTaskCompleted(response) {
			logger.Info("task_completed", "iteration", iteration)
			return nil
		}

		followUpPrompt := e.generateFollowUpPrompt(response, iteration)
		logger.Debug("generated_follow_up_prompt",
			"iteration", iteration,
			"follow_up_prompt", followUpPrompt)

		messages = append(messages, sdk.Message{
			Role:    sdk.User,
			Content: followUpPrompt,
		})
	}

	logger.Warn("max_iterations_reached", "max_iterations", e.maxIterations)
	return nil
}

// executeToolCall executes a single tool call and returns the result
func (e *BackgroundExecutor) executeToolCall(ctx context.Context, toolCall sdk.ChatCompletionMessageToolCall) (string, error) {
	toolService := e.services.GetToolService()
	if toolService == nil {
		return "", fmt.Errorf("tool service not available")
	}

	var args map[string]interface{}
	if toolCall.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
			return "", fmt.Errorf("failed to parse tool arguments: %w", err)
		}
	}

	result, err := toolService.ExecuteTool(ctx, toolCall.Function.Name, args)
	if err != nil {
		return "", fmt.Errorf("tool execution failed: %w", err)
	}

	return ui.FormatToolResultForLLM(result), nil
}


// isTaskCompleted checks if the task appears to be completed based on the response
func (e *BackgroundExecutor) isTaskCompleted(response string) bool {
	completionIndicators := []string{
		"task completed",
		"solution implemented",
		"issue resolved",
		"implementation complete",
		"problem solved",
		"finished",
		"done",
	}

	responseLower := strings.ToLower(response)
	for _, indicator := range completionIndicators {
		if strings.Contains(responseLower, indicator) {
			return true
		}
	}

	return false
}

// generateFollowUpPrompt generates a follow-up prompt to continue the task
func (e *BackgroundExecutor) generateFollowUpPrompt(response string, iteration int) string {
	prompts := []string{
		"Please continue with the next steps to complete this task.",
		"What additional work is needed to fully resolve this issue?",
		"Are there any remaining steps or considerations for this task?",
		"Please provide any additional implementation details or next steps.",
	}

	promptIndex := (iteration - 1) % len(prompts)
	return prompts[promptIndex]
}

func init() {
	rootCmd.AddCommand(promptCmd)
}
