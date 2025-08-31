package services

import (
	"fmt"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// processToolCallDeltas processes a list of tool call deltas
func (s *AgentServiceImpl) accumulateToolCalls(deltas []sdk.ChatCompletionMessageToolCallChunk) map[string]*sdk.ChatCompletionMessageToolCall { // nolint:unused
	iterationToolCallsMap := make(map[string]*sdk.ChatCompletionMessageToolCall, 5)
	for _, deltaToolCall := range deltas {
		key := fmt.Sprintf("%d", deltaToolCall.Index)

		if iterationToolCallsMap[key] == nil {
			iterationToolCallsMap[key] = &sdk.ChatCompletionMessageToolCall{
				Id:   deltaToolCall.ID,
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "",
					Arguments: "",
				},
			}
		}

		toolCall := iterationToolCallsMap[key]
		if deltaToolCall.ID != "" {
			toolCall.Id = deltaToolCall.ID
		}
		if deltaToolCall.Function.Name != "" {
			toolCall.Function.Name += deltaToolCall.Function.Name
		}
		if deltaToolCall.Function.Arguments != "" {
			toolCall.Function.Arguments += deltaToolCall.Function.Arguments
		}
	}

	return iterationToolCallsMap
}

// storeAssistantMessage stores an assistant message to conversation history
func (s *AgentServiceImpl) storeAssistantMessage(requestID, content string, toolCalls []sdk.ChatCompletionMessageToolCall, timestamp time.Time) { // nolint:unused
	if s.conversationRepo == nil {
		return
	}

	message := sdk.Message{
		Role:    sdk.Assistant,
		Content: content,
	}

	if len(toolCalls) > 0 {
		message.ToolCalls = &toolCalls
	}

	entry := domain.ConversationEntry{
		Message: message,
		Model:   "",
		Time:    timestamp,
	}

	if err := s.conversationRepo.AddMessage(entry); err != nil {
		logger.Error("failed to store assistant message", "error", err)
	}

}

// addSystemPrompt adds system prompt with dynamic sandbox info and returns messages
func (s *AgentServiceImpl) addSystemPrompt(messages []sdk.Message) []sdk.Message {
	var systemMessages []sdk.Message

	baseSystemPrompt := s.config.GetAgentConfig().SystemPrompt
	if baseSystemPrompt != "" {
		currentTime := time.Now().Format("Monday, January 2, 2006 at 3:04 PM MST")

		sandboxInfo := s.buildSandboxInfo()

		systemPromptWithSandbox := fmt.Sprintf("%s\n\n%s\n\nCurrent date and time: %s",
			baseSystemPrompt, sandboxInfo, currentTime)

		systemMessages = append(systemMessages, sdk.Message{
			Role:    sdk.System,
			Content: systemPromptWithSandbox,
		})
	}

	if len(systemMessages) > 0 {
		messages = append(systemMessages, messages...)
	}
	return messages
}

// buildSandboxInfo creates dynamic sandbox information for the system prompt
func (s *AgentServiceImpl) buildSandboxInfo() string {
	sandboxDirs := s.config.GetSandboxDirectories()
	protectedPaths := s.config.GetProtectedPaths()

	var sandboxInfo strings.Builder
	sandboxInfo.WriteString("SANDBOX RESTRICTIONS:\n")

	if len(sandboxDirs) > 0 {
		sandboxInfo.WriteString("You are restricted to work within these allowed directories:\n")
		for _, dir := range sandboxDirs {
			sandboxInfo.WriteString(fmt.Sprintf("- %s\n", dir))
		}
		sandboxInfo.WriteString("\n")
	}

	if len(protectedPaths) > 0 {
		sandboxInfo.WriteString("You MUST NOT attempt to access these protected paths:\n")
		for _, path := range protectedPaths {
			sandboxInfo.WriteString(fmt.Sprintf("- %s\n", path))
		}
	}

	return sandboxInfo.String()
}

// validateRequest validates the agent request
func (s *AgentServiceImpl) validateRequest(req *domain.AgentRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if req.RequestID == "" {
		return fmt.Errorf("no request ID provided")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("no messages provided")
	}
	if req.Model == "" {
		return fmt.Errorf("no model specified")
	}
	return nil
}

// parseProvider parses provider and model name from model string
func (s *AgentServiceImpl) parseProvider(model string) (string, string, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	return parts[0], parts[1], nil
}

// sendErrorEvent sends an error event
func (s *AgentServiceImpl) sendErrorEvent(events chan<- domain.ChatEvent, requestID string, err error) { // nolint:unused
	events <- domain.ChatErrorEvent{
		RequestID: requestID,
		Timestamp: time.Now(),
		Error:     err,
	}
}

// shouldInjectSystemReminder checks if a system reminder should be injected
func (s *AgentServiceImpl) shouldInjectSystemReminder() bool { // nolint:unused
	// cfg, ok := s.config.(*config.Config)
	// if !ok {
	// 	return false
	// }

	// if !cfg.Agent.SystemReminders.Enabled {
	// 	return false
	// }

	// interval := cfg.Agent.SystemReminders.Interval
	// if interval <= 0 {
	// 	interval = 4
	// }

	// return s.assistantMessageCounter > 0 && s.assistantMessageCounter%interval == 0
	return false // TODO - refactor this
}

// getSystemReminderMessage returns the system reminder message to inject
func (s *AgentServiceImpl) getSystemReminderMessage() sdk.Message { // nolint:unused
	cfg, ok := s.config.(*config.Config)
	if !ok {
		return sdk.Message{}
	}

	reminderText := cfg.Agent.SystemReminders.ReminderText
	if reminderText == "" {
		reminderText = `<system-reminder>
This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware. If you are working on tasks that would benefit from a todo list please use the TodoWrite tool to create one. If not, please feel free to ignore. Again do not mention this message to the user.
</system-reminder>`
	}

	return sdk.Message{
		Role:    sdk.User,
		Content: reminderText,
	}
}
