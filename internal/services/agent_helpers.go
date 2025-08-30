package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// validateRequest validates the agent request
func (s *AgentServiceImpl) validateRequest(req *domain.AgentRequest) error {
	if len(req.Messages) == 0 {
		return fmt.Errorf("no messages provided")
	}
	if req.Model == "" {
		return fmt.Errorf("no model specified")
	}
	return nil
}

// addSystemPrompt adds system prompt with dynamic sandbox info and returns messages
func (s *AgentServiceImpl) addSystemPrompt(messages []sdk.Message) []sdk.Message {
	var systemMessages []sdk.Message

	baseSystemPrompt := s.config.GetSystemPrompt()
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

// convertToSDKTools converts tool service tools to SDK tools
func (s *AgentServiceImpl) convertToSDKTools() *[]sdk.ChatCompletionTool {
	if s.toolService == nil {
		return nil
	}

	availableTools := s.toolService.ListTools()
	if len(availableTools) == 0 {
		return nil
	}

	sdkTools := make([]sdk.ChatCompletionTool, len(availableTools))
	for i, tool := range availableTools {
		description := tool.Description

		var parameters *sdk.FunctionParameters
		if tool.Parameters != nil {
			if paramMap, ok := tool.Parameters.(map[string]any); ok {
				fp := sdk.FunctionParameters(paramMap)
				parameters = &fp
			}
		}

		sdkTools[i] = sdk.ChatCompletionTool{
			Type: sdk.Function,
			Function: sdk.FunctionObject{
				Name:        tool.Name,
				Description: &description,
				Parameters:  parameters,
			},
		}
	}

	return &sdkTools
}

// generateContentSync generates content synchronously
func (s *AgentServiceImpl) generateContentSync(timeoutCtx context.Context, model string, messages []sdk.Message) (*sdk.CreateChatCompletionResponse, error) {
	provider, modelName, err := s.parseProvider(model)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	providerType := sdk.Provider(provider)

	clientWithTools := s.client
	if tools := s.convertToSDKTools(); tools != nil {
		clientWithTools = s.client.WithTools(tools)
	}

	options := &sdk.CreateChatCompletionRequest{
		MaxTokens: &s.maxTokens,
	}

	response, err := clientWithTools.
		WithOptions(options).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: s.config.ShouldSkipMCPToolOnClient(),
			SkipA2A: s.config.ShouldSkipA2AToolOnClient(),
		}).
		GenerateContent(timeoutCtx, providerType, modelName, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	return response, nil
}

// parseProvider parses provider and model name from model string
func (s *AgentServiceImpl) parseProvider(model string) (string, string, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	provider := parts[0]
	modelName := parts[1]

	// Handle Google's model format: google/models/gemini-2.5-flash -> google, gemini-2.5-flash
	if provider == "google" && strings.HasPrefix(modelName, "models/") {
		modelName = strings.TrimPrefix(modelName, "models/")
	}

	return provider, modelName, nil
}

// sendErrorEvent sends an error event
func (s *AgentServiceImpl) sendErrorEvent(events chan<- domain.ChatEvent, requestID string, err error) {
	events <- domain.ChatErrorEvent{
		RequestID: requestID,
		Timestamp: time.Now(),
		Error:     err,
	}
}
