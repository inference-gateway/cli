package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/shared"
	"github.com/inference-gateway/sdk"
)

// InMemoryConversationRepository implements ConversationRepository using in-memory storage
type InMemoryConversationRepository struct {
	messages         []domain.ConversationEntry
	mutex            sync.RWMutex
	sessionStats     domain.SessionTokenStats
	formatterService *ToolFormatterService
}

// NewInMemoryConversationRepository creates a new in-memory conversation repository
func NewInMemoryConversationRepository(formatterService *ToolFormatterService) *InMemoryConversationRepository {
	return &InMemoryConversationRepository{
		messages:         make([]domain.ConversationEntry, 0),
		formatterService: formatterService,
	}
}

// formatToolCall formats a tool call for display using the formatter service
func (r *InMemoryConversationRepository) formatToolCall(toolCall sdk.ChatCompletionMessageToolCall) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("%s()", toolCall.Function.Name)
	}

	if r.formatterService != nil {
		return r.formatterService.FormatToolCall(toolCall.Function.Name, args)
	}

	return shared.FormatToolCall(toolCall.Function.Name, args)
}

func (r *InMemoryConversationRepository) AddMessage(msg domain.ConversationEntry) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if msg.Time.IsZero() {
		msg.Time = time.Now()
	}

	r.messages = append(r.messages, msg)
	return nil
}

func (r *InMemoryConversationRepository) GetMessages() []domain.ConversationEntry {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	result := make([]domain.ConversationEntry, len(r.messages))
	copy(result, r.messages)
	return result
}

func (r *InMemoryConversationRepository) Clear() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.messages = r.messages[:0]
	r.sessionStats = domain.SessionTokenStats{}
	return nil
}

func (r *InMemoryConversationRepository) ClearExceptFirstUserMessage() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	var firstUserMessage *domain.ConversationEntry
	for i := range r.messages {
		if r.messages[i].Message.Role == sdk.User {
			firstUserMessage = &r.messages[i]
			break
		}
	}

	if firstUserMessage == nil {
		r.messages = r.messages[:0]
		r.sessionStats = domain.SessionTokenStats{}
		return nil
	}

	r.messages = []domain.ConversationEntry{*firstUserMessage}
	r.sessionStats = domain.SessionTokenStats{}
	return nil
}

func (r *InMemoryConversationRepository) Export(format domain.ExportFormat) ([]byte, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	switch format {
	case domain.ExportJSON:
		filteredMessages := r.filterHiddenMessages()
		return json.MarshalIndent(filteredMessages, "", "  ")

	case domain.ExportMarkdown:
		return r.exportMarkdown(), nil

	case domain.ExportText:
		return r.exportText(), nil

	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
}

func (r *InMemoryConversationRepository) GetMessageCount() int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return len(r.messages)
}

func (r *InMemoryConversationRepository) UpdateLastMessage(content string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if len(r.messages) == 0 {
		return fmt.Errorf("no messages to update")
	}

	lastIndex := len(r.messages) - 1
	r.messages[lastIndex].Message.Content = sdk.NewMessageContent(content)
	r.messages[lastIndex].Time = time.Now()

	return nil
}

func (r *InMemoryConversationRepository) UpdateLastMessageToolCalls(toolCalls *[]sdk.ChatCompletionMessageToolCall) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if len(r.messages) == 0 {
		return fmt.Errorf("no messages to update")
	}

	lastIndex := len(r.messages) - 1
	if r.messages[lastIndex].Message.Role != sdk.Assistant {
		return fmt.Errorf("last message is not from assistant")
	}

	r.messages[lastIndex].Message.ToolCalls = toolCalls
	r.messages[lastIndex].Time = time.Now()

	return nil
}

// exportMarkdown exports conversation as markdown
func (r *InMemoryConversationRepository) exportMarkdown() []byte {
	var content strings.Builder

	filteredMessages := r.filterHiddenMessages()

	content.WriteString("# Chat Session Export\n\n")
	content.WriteString(fmt.Sprintf("**Date:** %s\n", time.Now().Format("2006-01-02 15:04:05")))
	content.WriteString(fmt.Sprintf("**Total Messages:** %d\n", len(filteredMessages)))

	if r.sessionStats.RequestCount > 0 {
		content.WriteString(fmt.Sprintf("**Total Input Tokens:** %d\n", r.sessionStats.TotalInputTokens))
		content.WriteString(fmt.Sprintf("**Total Output Tokens:** %d\n", r.sessionStats.TotalOutputTokens))
		content.WriteString(fmt.Sprintf("**Total Tokens:** %d\n", r.sessionStats.TotalTokens))
		content.WriteString(fmt.Sprintf("**API Requests:** %d\n", r.sessionStats.RequestCount))
	}
	content.WriteString("\n---\n\n")

	for i, entry := range filteredMessages {
		var role string
		switch entry.Message.Role {
		case sdk.User:
			role = "👤 **You**"
		case sdk.Assistant:
			if entry.Model != "" {
				role = fmt.Sprintf("🤖 **Assistant (%s)**", entry.Model)
			} else {
				role = "🤖 **Assistant**"
			}
		case sdk.System:
			role = "⚙️ **System**"
		case sdk.Tool:
			role = "🔧 **Tool Result**"
		default:
			role = fmt.Sprintf("**%s**", string(entry.Message.Role))
		}

		content.WriteString(fmt.Sprintf("## Message %d - %s\n\n", i+1, role))
		content.WriteString(fmt.Sprintf("*%s*\n\n", entry.Time.Format("2006-01-02 15:04:05")))

		contentStr, err := entry.Message.Content.AsMessageContent0()
		if err == nil && contentStr != "" {
			content.WriteString(contentStr)
			content.WriteString("\n\n")
		}

		if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 {
			content.WriteString("### Tool Calls\n\n")
			for _, toolCall := range *entry.Message.ToolCalls {
				content.WriteString(fmt.Sprintf("**Tool:** %s\n\n", r.formatToolCall(toolCall)))
				if toolCall.Function.Arguments != "" {
					content.WriteString("**Arguments:**\n```json\n")
					content.WriteString(toolCall.Function.Arguments)
					content.WriteString("\n```\n\n")
				}
			}
		}

		if entry.Message.ToolCallId != nil {
			content.WriteString(fmt.Sprintf("*Tool Call ID: %s*\n\n", *entry.Message.ToolCallId))
		}

		content.WriteString("---\n\n")
	}

	content.WriteString(fmt.Sprintf("*Exported on %s using Inference Gateway CLI*\n", time.Now().Format("2006-01-02 15:04:05")))

	return []byte(content.String())
}

// exportText exports conversation as plain text
func (r *InMemoryConversationRepository) exportText() []byte {
	var content strings.Builder

	filteredMessages := r.filterHiddenMessages()

	content.WriteString("Chat Session Export\n")
	content.WriteString("===================\n\n")

	for _, entry := range filteredMessages {
		var role string
		switch entry.Message.Role {
		case sdk.User:
			role = "You"
		case sdk.Assistant:
			if entry.Model != "" {
				role = fmt.Sprintf("Assistant (%s)", entry.Model)
			} else {
				role = "Assistant"
			}
		case sdk.System:
			role = "System"
		case sdk.Tool:
			role = "Tool"
		default:
			role = string(entry.Message.Role)
		}

		content.WriteString(fmt.Sprintf("[%s] %s: %s\n\n",
			entry.Time.Format("15:04:05"), role, entry.Message.Content))
	}

	return []byte(content.String())
}

// filterHiddenMessages returns a copy of messages with hidden messages filtered out
func (r *InMemoryConversationRepository) filterHiddenMessages() []domain.ConversationEntry {
	filtered := make([]domain.ConversationEntry, 0, len(r.messages))
	for _, entry := range r.messages {
		if !entry.Hidden {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// AddTokenUsage adds token usage from a single API call to session totals
func (r *InMemoryConversationRepository) AddTokenUsage(inputTokens, outputTokens, totalTokens int) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.sessionStats.TotalInputTokens += inputTokens
	r.sessionStats.TotalOutputTokens += outputTokens
	r.sessionStats.TotalTokens += totalTokens
	r.sessionStats.RequestCount++

	return nil
}

// GetSessionTokens returns the accumulated token statistics for the session
func (r *InMemoryConversationRepository) GetSessionTokens() domain.SessionTokenStats {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.sessionStats
}

// FormatToolResultForLLM formats tool execution results for LLM consumption
func (r *InMemoryConversationRepository) FormatToolResultForLLM(result *domain.ToolExecutionResult) string {
	if r.formatterService != nil {
		return r.formatterService.FormatToolResultForLLM(result)
	}
	if result.Success {
		return "Tool executed successfully"
	}
	return "Tool execution failed"
}

// FormatToolResultForUI formats tool execution results for UI display
func (r *InMemoryConversationRepository) FormatToolResultForUI(result *domain.ToolExecutionResult, terminalWidth int) string {
	if r.formatterService != nil {
		return r.formatterService.FormatToolResultForUI(result, terminalWidth)
	}
	if result.Success {
		return "Tool executed successfully"
	}
	return "Tool execution failed"
}

// FormatToolResultExpanded formats expanded tool execution results
func (r *InMemoryConversationRepository) FormatToolResultExpanded(result *domain.ToolExecutionResult, terminalWidth int) string {
	if r.formatterService != nil {
		return r.formatterService.FormatToolResultExpanded(result, terminalWidth)
	}
	if result.Success {
		return "Tool executed successfully"
	}
	return "Tool execution failed"
}
