package services

import (
	"fmt"
	"strings"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// accumulateToolCalls processes multiple tool call deltas and stores them in the agent's toolCallsMap
func (s *AgentServiceImpl) accumulateToolCalls(deltas []sdk.ChatCompletionMessageToolCallChunk) {
	s.toolCallsMux.Lock()
	defer s.toolCallsMux.Unlock()

	for _, delta := range deltas {
		key := fmt.Sprintf("%d", delta.Index)

		if s.toolCallsMap[key] == nil {
			s.toolCallsMap[key] = &sdk.ChatCompletionMessageToolCall{
				Id:   delta.ID,
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "",
					Arguments: "",
				},
			}
		}

		toolCall := s.toolCallsMap[key]
		if delta.ID != "" {
			toolCall.Id = delta.ID
		}
		if delta.Function.Name != "" {
			toolCall.Function.Name += delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			toolCall.Function.Arguments += delta.Function.Arguments
		}
	}
}

// getAccumulatedToolCalls returns a copy of all accumulated tool calls and clears the map
func (s *AgentServiceImpl) getAccumulatedToolCalls() map[string]*sdk.ChatCompletionMessageToolCall {
	s.toolCallsMux.Lock()
	defer s.toolCallsMux.Unlock()

	result := make(map[string]*sdk.ChatCompletionMessageToolCall)
	for k, v := range s.toolCallsMap {
		result[k] = v
	}

	s.toolCallsMap = make(map[string]*sdk.ChatCompletionMessageToolCall)
	return result
}

// clearToolCallsMap resets the tool calls map for the next iteration
func (s *AgentServiceImpl) clearToolCallsMap() {
	s.toolCallsMux.Lock()
	defer s.toolCallsMux.Unlock()

	s.toolCallsMap = make(map[string]*sdk.ChatCompletionMessageToolCall)
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

// shouldInjectSystemReminder checks if a system reminder should be injected
func (s *AgentServiceImpl) shouldInjectSystemReminder(turns int) bool {
	cfg := s.config.GetAgentConfig()

	if !cfg.SystemReminders.Enabled {
		return false
	}

	interval := cfg.SystemReminders.Interval
	if interval <= 0 {
		interval = 4
	}

	return turns > 0 && turns%interval == 0
}

// getSystemReminderMessage returns the system reminder message to inject
func (s *AgentServiceImpl) getSystemReminderMessage() sdk.Message {
	cfg := s.config.GetAgentConfig()

	reminderText := cfg.SystemReminders.ReminderText
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
