package handlers

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ChatMessageProcessor handles message processing logic
type ChatMessageProcessor struct {
	handler *ChatHandler
}

// NewChatMessageProcessor creates a new message processor
func NewChatMessageProcessor(handler *ChatHandler) *ChatMessageProcessor {
	return &ChatMessageProcessor{
		handler: handler,
	}
}

// handleUserInput processes user input messages
func (p *ChatMessageProcessor) handleUserInput(
	msg domain.UserInputEvent,
) tea.Cmd {
	if strings.HasPrefix(msg.Content, "/") {
		return p.handler.commandHandler.handleCommand(msg.Content)
	}

	if strings.HasPrefix(msg.Content, "!!") {
		return p.handler.commandHandler.handleToolCommand(msg.Content)
	}

	if strings.HasPrefix(msg.Content, "!") {
		return p.handler.commandHandler.handleBashCommand(msg.Content)
	}

	expandedContent, err := p.expandFileReferences(msg.Content)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to expand file references: %v", err),
				Sticky: false,
			}
		}
	}

	return p.processChatMessage(expandedContent)
}

// ExtractMarkdownSummary extracts the "## Summary" section from markdown content (exposed for testing)
func (p *ChatMessageProcessor) ExtractMarkdownSummary(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	var summaryLines []string
	inSummary := false

	for _, line := range lines {
		if strings.TrimSpace(line) == "## Summary" {
			inSummary = true
			summaryLines = append(summaryLines, line)
			continue
		}

		if inSummary {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "## ") || trimmed == "---" {
				break
			}
			summaryLines = append(summaryLines, line)
		}
	}

	if len(summaryLines) > 1 {
		result := strings.Join(summaryLines, "\n")
		result = strings.TrimRight(result, " \t\n") + "\n"
		return result, true
	}

	return "", false
}

// expandFileReferences expands @filename references with file content
func (p *ChatMessageProcessor) expandFileReferences(content string) (string, error) {
	re := regexp.MustCompile(`@([^\s]+)`)
	matches := re.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		return content, nil
	}

	expandedContent := content
	for _, match := range matches {
		fullMatch := match[0]
		filename := match[1]

		if err := p.handler.fileService.ValidateFile(filename); err != nil {
			continue
		}

		fileContent, err := p.handler.fileService.ReadFile(filename)
		if err != nil {
			continue
		}

		contentToInclude := fileContent
		if strings.HasSuffix(strings.ToLower(filename), ".md") {
			if summaryContent, hasSummary := p.ExtractMarkdownSummary(fileContent); hasSummary {
				contentToInclude = summaryContent
			}
		}

		fileBlock := fmt.Sprintf("File: %s\n```%s\n%s\n```\n", filename, filename, contentToInclude)
		expandedContent = strings.Replace(expandedContent, fullMatch, fileBlock, 1)
	}

	return expandedContent, nil
}

// processChatMessage processes a regular chat message
func (p *ChatMessageProcessor) processChatMessage(
	content string,
) tea.Cmd {
	message := sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(content),
	}

	if p.handler.stateManager.IsAgentBusy() {
		requestID := fmt.Sprintf("queued-%d", time.Now().UnixNano())
		p.handler.messageQueue.Enqueue(message, requestID)

		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Message queued - agent is currently busy",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}
	}

	userEntry := domain.ConversationEntry{
		Message: message,
		Time:    time.Now(),
	}

	if err := p.handler.conversationRepo.AddMessage(userEntry); err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to save message: %v", err),
				Sticky: false,
			}
		}
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: p.handler.conversationRepo.GetMessages(),
			}
		},
	}

	if len(p.handler.conversationRepo.GetMessages()) > 10 {
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Optimizing conversation history (%d messages)...", len(p.handler.conversationRepo.GetMessages())),
				Spinner:    true,
				StatusType: domain.StatusPreparing,
			}
		})
	}

	cmds = append(cmds, p.handler.startChatCompletion())

	return tea.Batch(cmds...)
}
