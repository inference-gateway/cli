package tools

import (
	"context"
	"fmt"
	"time"

	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// AgentCardTool handles fetching detailed agent capabilities
type AgentCardTool struct {
	config           *config.Config
	a2aDirectService domain.A2ADirectService
}

// AgentCardResult represents the result of an agent card operation
type AgentCardResult struct {
	Operation string         `json:"operation"`
	AgentName string         `json:"agent_name"`
	Card      *adk.AgentCard `json:"card,omitempty"`
	Message   string         `json:"message"`
	Success   bool           `json:"success"`
	Duration  time.Duration  `json:"duration,omitempty"`
}

// NewAgentCardTool creates a new agent card tool
func NewAgentCardTool(cfg *config.Config, a2aDirectService domain.A2ADirectService) *AgentCardTool {
	return &AgentCardTool{
		config:           cfg,
		a2aDirectService: a2aDirectService,
	}
}

// Definition returns the tool definition for the LLM
func (t *AgentCardTool) Definition() sdk.ChatCompletionTool {
	description := "Fetch detailed agent capabilities and API schema from Agent-to-Agent (A2A) servers. Provides comprehensive information about what an agent can do, its supported skills, communication methods, and security requirements."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "AgentCard",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the A2A agent to fetch the card for",
					},
				},
				"required": []string{"agent_name"},
			},
		},
	}
}

// Execute runs the tool with given arguments
func (t *AgentCardTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.config.IsA2ADirectEnabled() {
		return &domain.ToolExecutionResult{
			ToolName:  "AgentCard",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A direct connections are disabled in configuration",
			Data: AgentCardResult{
				Operation: "get_card",
				Success:   false,
				Message:   "A2A direct connections are disabled",
			},
		}, nil
	}

	if t.a2aDirectService == nil {
		return &domain.ToolExecutionResult{
			ToolName:  "AgentCard",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A direct service not available",
			Data: AgentCardResult{
				Operation: "get_card",
				Success:   false,
				Message:   "A2A direct service not initialized",
			},
		}, nil
	}

	agentName, ok := args["agent_name"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_name parameter is required and must be a string")
	}

	card, err := t.a2aDirectService.GetAgentCard(ctx, agentName)
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to fetch agent card: %v", err))
	}

	logger.Debug("Agent card fetched via tool", "agent_name", agentName, "card_name", card.Name)

	return &domain.ToolExecutionResult{
		ToolName:  "AgentCard",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: AgentCardResult{
			Operation: "get_card",
			AgentName: agentName,
			Card:      card,
			Success:   true,
			Message:   fmt.Sprintf("Successfully fetched agent card for '%s'", agentName),
		},
	}, nil
}

// errorResult creates an error result
func (t *AgentCardTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*domain.ToolExecutionResult, error) {
	agentName := ""
	if name, ok := args["agent_name"].(string); ok {
		agentName = name
	}

	return &domain.ToolExecutionResult{
		ToolName:  "AgentCard",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: AgentCardResult{
			Operation: "get_card",
			AgentName: agentName,
			Success:   false,
			Message:   errorMsg,
		},
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *AgentCardTool) Validate(args map[string]any) error {
	agentName, ok := args["agent_name"].(string)
	if !ok {
		return fmt.Errorf("agent_name parameter is required and must be a string")
	}

	if agentName == "" {
		return fmt.Errorf("agent_name parameter cannot be empty")
	}

	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *AgentCardTool) IsEnabled() bool {
	return t.config.IsA2ADirectEnabled()
}

// FormatResult formats tool execution results for different contexts
func (t *AgentCardTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	if result.Data == nil {
		return result.Error
	}

	data, ok := result.Data.(AgentCardResult)
	if !ok {
		return "Invalid agent card result format"
	}

	switch formatType {
	case domain.FormatterLLM:
		return t.formatForLLM(data)
	case domain.FormatterShort:
		return data.Message
	default:
		return t.formatForUI(data)
	}
}

// formatForLLM formats the result for LLM consumption
func (t *AgentCardTool) formatForLLM(data AgentCardResult) string {
	result := fmt.Sprintf("Agent Card: %s", data.Message)

	if data.Card != nil {
		cardSummary := fmt.Sprintf("\nAgent: %s v%s\nDescription: %s\nSkills: %d\nCapabilities: %+v",
			data.Card.Name,
			data.Card.Version,
			data.Card.Description,
			len(data.Card.Skills),
			data.Card.Capabilities)
		result += cardSummary
	}

	return result
}

// formatForUI formats the result for UI display
func (t *AgentCardTool) formatForUI(data AgentCardResult) string {
	result := fmt.Sprintf("**Agent Card**: %s", data.Message)

	if data.Card == nil {
		return result
	}

	result += fmt.Sprintf("\n🤖 **Agent**: %s v%s", data.Card.Name, data.Card.Version)
	result += fmt.Sprintf("\n📝 **Description**: %s", data.Card.Description)

	result = t.addProviderInfo(result, data.Card)
	result = t.addSkillsInfo(result, data.Card)
	result = t.addDocumentationInfo(result, data.Card)
	result = t.addCapabilitiesInfo(result, data.Card)

	return result
}

func (t *AgentCardTool) addProviderInfo(result string, card *adk.AgentCard) string {
	if card.Provider == nil {
		return result
	}
	return result + fmt.Sprintf("\n🏢 **Provider**: %s", card.Provider.Organization)
}

func (t *AgentCardTool) addSkillsInfo(result string, card *adk.AgentCard) string {
	if len(card.Skills) == 0 {
		return result
	}

	result += fmt.Sprintf("\n⚡ **Skills** (%d):", len(card.Skills))
	for i, skill := range card.Skills {
		if i >= 5 {
			break
		}
		result += fmt.Sprintf("\n  • %s: %s", skill.Name, skill.Description)
	}
	if len(card.Skills) > 5 {
		result += fmt.Sprintf("\n  ... and %d more", len(card.Skills)-5)
	}
	return result
}

func (t *AgentCardTool) addDocumentationInfo(result string, card *adk.AgentCard) string {
	if card.DocumentationURL == nil {
		return result
	}
	return result + fmt.Sprintf("\n📚 **Documentation**: %s", *card.DocumentationURL)
}

func (t *AgentCardTool) addCapabilitiesInfo(result string, card *adk.AgentCard) string {
	caps := card.Capabilities
	result += "\n🔧 **Capabilities**:"

	if caps.Streaming != nil {
		result += fmt.Sprintf(" Streaming: %v", *caps.Streaming)
	}
	if caps.PushNotifications != nil {
		result += fmt.Sprintf(", Push Notifications: %v", *caps.PushNotifications)
	}
	if caps.StateTransitionHistory != nil {
		result += fmt.Sprintf(", State History: %v", *caps.StateTransitionHistory)
	}
	if len(caps.Extensions) > 0 {
		result += fmt.Sprintf(", Extensions: %d", len(caps.Extensions))
	}

	return result
}

// FormatPreview returns a short preview of the result for UI display
func (t *AgentCardTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result.Data == nil {
		return result.Error
	}

	if data, ok := result.Data.(AgentCardResult); ok {
		if data.Card != nil {
			return fmt.Sprintf("Agent Card: %s v%s (%d skills)",
				data.Card.Name, data.Card.Version, len(data.Card.Skills))
		}
		return fmt.Sprintf("Agent Card: %s", data.Message)
	}

	return "Agent card fetch completed"
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *AgentCardTool) ShouldCollapseArg(key string) bool {
	return false // No complex arguments to collapse
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *AgentCardTool) ShouldAlwaysExpand() bool {
	return true // Agent cards contain valuable information worth expanding
}
