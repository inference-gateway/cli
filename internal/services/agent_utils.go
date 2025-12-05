package services

import (
	"encoding/json"
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
		if delta.Function.Name != "" && toolCall.Function.Name == "" {
			toolCall.Function.Name = delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			if toolCall.Function.Arguments == "" {
				toolCall.Function.Arguments = delta.Function.Arguments
				continue
			}

			if isCompleteJSON(toolCall.Function.Arguments) {
				continue
			}

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

	baseSystemPrompt := s.getSystemPromptForMode()
	if baseSystemPrompt != "" {
		currentTime := time.Now().Format("Monday, January 2, 2006 at 3:04 PM MST")

		sandboxInfo := s.buildSandboxInfo()

		a2aAgentInfo := s.buildA2AAgentInfo()

		systemPromptWithInfo := fmt.Sprintf("%s\n\n%s%s\n\nCurrent date and time: %s",
			baseSystemPrompt, sandboxInfo, a2aAgentInfo, currentTime)

		systemMessages = append(systemMessages, sdk.Message{
			Role:    sdk.System,
			Content: sdk.NewMessageContent(systemPromptWithInfo),
		})
	}

	if len(systemMessages) > 0 {
		messages = append(systemMessages, messages...)
	}
	return messages
}

// getSystemPromptForMode returns the appropriate system prompt based on current agent mode
func (s *AgentServiceImpl) getSystemPromptForMode() string {
	agentConfig := s.config.GetAgentConfig()

	if s.stateManager == nil {
		return agentConfig.SystemPrompt
	}

	mode := s.stateManager.GetAgentMode()
	switch mode {
	case domain.AgentModePlan:
		if agentConfig.SystemPromptPlan != "" {
			return agentConfig.SystemPromptPlan
		}
		return agentConfig.SystemPrompt

	case domain.AgentModeAutoAccept:
		return agentConfig.SystemPrompt

	case domain.AgentModeStandard:
		return agentConfig.SystemPrompt

	default:
		return agentConfig.SystemPrompt
	}
}

// buildA2AAgentInfo creates dynamic A2A agent information for the system prompt
func (s *AgentServiceImpl) buildA2AAgentInfo() string {
	if s.a2aAgentService == nil {
		return ""
	}

	urls := s.a2aAgentService.GetConfiguredAgents()
	if len(urls) == 0 {
		return ""
	}

	agentInfo := "\n\nAvailable A2A Agents:\n"
	for _, url := range urls {
		agentInfo += fmt.Sprintf("- %s\n", url)
	}
	agentInfo += "\nYou can delegate tasks to these agents using the A2A_SubmitTask tool."
	return agentInfo
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
		Content: sdk.NewMessageContent(reminderText),
	}
}

// isCompleteJSON checks if a string is a complete, valid JSON
func isCompleteJSON(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	var js any
	return json.Unmarshal([]byte(s), &js) == nil
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// getTruncationRecoveryGuidance returns tool-specific guidance when a tool call is truncated
func getTruncationRecoveryGuidance(toolName string) string {
	switch toolName {
	case "Write":
		return "YOU MUST use a different approach: " +
			"1. First create an EMPTY or MINIMAL file using Write with just a skeleton/placeholder. " +
			"2. Then use the Edit tool to add content in small chunks (20-30 lines per Edit call). " +
			"3. Repeat Edit calls until the file is complete. " +
			"DO NOT attempt to Write the full content again - it will fail the same way."
	case "Edit":
		return "YOUR EDIT WAS TOO LARGE. YOU MUST: " +
			"1. Break your edit into SMALLER chunks (10-20 lines maximum per Edit call). " +
			"2. Use a shorter, more precise old_string to match. " +
			"3. Make multiple smaller Edit calls instead of one large edit. " +
			"DO NOT retry with the same large edit - it will fail again."
	case "Bash":
		return "Your command output or arguments were too large. " +
			"Try breaking the command into smaller parts or redirecting output to a file."
	default:
		return "The tool arguments were too large. " +
			"Try breaking your request into smaller, incremental operations."
	}
}
