package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
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

// fileExpansionResult holds the result of expanding file references
type fileExpansionResult struct {
	content string
	images  []domain.ImageAttachment
}

// handleUserInput processes user input messages
func (p *ChatMessageProcessor) handleUserInput(
	msg domain.UserInputEvent,
) tea.Cmd {
	if strings.HasPrefix(msg.Content, "/") {
		return p.handler.HandleCommand(msg.Content)
	}

	if strings.HasPrefix(msg.Content, "!!") {
		return p.handler.HandleToolCommand(msg.Content)
	}

	if strings.HasPrefix(msg.Content, "!") {
		return p.handler.HandleBashCommand(msg.Content)
	}

	result, err := p.expandFileReferences(msg.Content)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to expand file references: %v", err),
				Sticky: false,
			}
		}
	}

	allImages := append(msg.Images, result.images...)

	var warningCmd tea.Cmd
	if len(allImages) > 0 {
		currentModel := p.handler.modelService.GetCurrentModel()
		if !p.handler.modelService.IsVisionModel(currentModel) {
			warningCmd = func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    fmt.Sprintf("Warning: Model '%s' may not support vision. Images may be ignored.", currentModel),
					Spinner:    false,
					StatusType: domain.StatusDefault,
				}
			}
		}
	}

	chatCmd := p.processChatMessage(result.content, allImages)

	if warningCmd != nil {
		return tea.Batch(warningCmd, chatCmd)
	}
	return chatCmd
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

// expandFileReferences expands @filename references with file content or images
func (p *ChatMessageProcessor) expandFileReferences(content string) (*fileExpansionResult, error) {
	re := regexp.MustCompile(`@([^\s]+)`)
	matches := re.FindAllStringSubmatch(content, -1)

	result := &fileExpansionResult{
		content: content,
		images:  []domain.ImageAttachment{},
	}

	if len(matches) == 0 {
		return result, nil
	}

	expandedContent := content

	for _, match := range matches {
		fullMatch := match[0]
		filename := match[1]

		if err := p.handler.fileService.ValidateFile(filename); err != nil {
			continue
		}

		if p.handler.imageService != nil && p.handler.imageService.IsImageFile(filename) {
			imageAttachment, err := p.handler.imageService.ReadImageFromFile(filename)
			if err != nil {
				continue
			}
			result.images = append(result.images, *imageAttachment)
			imageRef := fmt.Sprintf("[Image: %s]", filename)
			expandedContent = strings.Replace(expandedContent, fullMatch, imageRef, 1)
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

	result.content = expandedContent
	return result, nil
}

// processChatMessage processes a regular chat message with optional image attachments
func (p *ChatMessageProcessor) processChatMessage(
	content string,
	images []domain.ImageAttachment,
) tea.Cmd {
	var message sdk.Message

	if len(images) > 0 {
		var contentParts []sdk.ContentPart

		textPart, err := sdk.NewTextContentPart(content)
		if err != nil {
			return func() tea.Msg {
				return domain.ShowErrorEvent{
					Error:  fmt.Sprintf("Failed to create text content: %v", err),
					Sticky: false,
				}
			}
		}
		contentParts = append(contentParts, textPart)

		for _, img := range images {
			dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)
			imagePart, err := sdk.NewImageContentPart(dataURL, nil)
			if err != nil {
				return func() tea.Msg {
					return domain.ShowErrorEvent{
						Error:  fmt.Sprintf("Failed to create image content: %v", err),
						Sticky: false,
					}
				}
			}
			contentParts = append(contentParts, imagePart)
		}

		message = sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(contentParts),
		}
	} else {
		message = sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(content),
		}
	}

	if p.handler.stateManager.IsAgentBusy() {
		requestID := fmt.Sprintf("queued-%d", time.Now().UnixNano())
		p.handler.messageQueue.Enqueue(message, requestID)
		logger.Info("chat input queued - agent busy",
			"request_id", requestID,
			"queue_size_after_enqueue", p.handler.messageQueue.Size())

		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Message queued - agent is currently busy",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}
	}

	// Auto-rollover BEFORE appending the new user message - otherwise the new
	// message resets the idle clock and never triggers. Mirrors what /compact
	// does manually: produces a summary, starts a new conversation file, and
	// the new user message lands in the new file via AddMessage in the tail
	// helper below.
	if p.shouldRolloverNow() {
		return p.compactThenContinue(message, images)
	}

	return p.appendUserMessageAndStartCompletion(message, images)
}

// shouldRolloverNow is a cheap pre-check on the synchronous Update path so
// that the vast majority of user messages (where no rollover is due) skip
// the async dispatch entirely. The real ShouldRollover/PerformRollover run
// inside MaybeRollover on the goroutine; a false negative here just means
// we skip a one-message rollover that would otherwise fire, and the next
// message will catch it.
func (p *ChatMessageProcessor) shouldRolloverNow() bool {
	return p.handler.sessionRolloverManager != nil &&
		p.handler.sessionRolloverManager.ShouldRollover(p.handler.modelService.GetCurrentModel())
}

// compactThenContinue runs the rollover asynchronously so the Bubble Tea
// Update loop stays responsive (the summary LLM call takes a few seconds and
// would otherwise freeze the UI with no spinner). The flow:
//
//  1. SetChatPending so IsAgentBusy() queues any further user input arriving
//     while the rollover is in flight.
//  2. Emit a "Compacting conversation..." status (spinner on) for the user.
//  3. Run MaybeRollover on a goroutine via a tea.Cmd; on completion dispatch
//     a RolloverCompletedEvent that the chat handler routes back into
//     appendUserMessageAndStartCompletion to resume the deferred work.
func (p *ChatMessageProcessor) compactThenContinue(message sdk.Message, images []domain.ImageAttachment) tea.Cmd {
	p.handler.stateManager.SetChatPending()
	logger.Info("chat rollover: deferring user message, kicking off async MaybeRollover",
		"queue_size_before", p.handler.messageQueue.Size(),
		"agent_busy_now", p.handler.stateManager.IsAgentBusy())

	statusCmd := func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Compacting conversation...",
			Spinner:    true,
			StatusType: domain.StatusPreparing,
		}
	}

	rolloverCmd := func() tea.Msg {
		newID, fired := p.handler.sessionRolloverManager.MaybeRollover(
			context.Background(),
			p.handler.modelService.GetCurrentModel(),
			"",
		)
		logger.Info("chat rollover: MaybeRollover returned",
			"fired", fired,
			"new_session_id", newID,
			"queue_size_after", p.handler.messageQueue.Size())
		return domain.RolloverCompletedEvent{
			Message: message,
			Images:  images,
		}
	}

	return tea.Batch(statusCmd, rolloverCmd)
}

// appendUserMessageAndStartCompletion is the synchronous tail of
// processChatMessage. It persists the user message, fires the history
// refresh and optional optimization status, and kicks off the chat
// completion. Called directly when no rollover is due, and via
// HandleRolloverCompletedEvent after an async rollover finishes.
func (p *ChatMessageProcessor) appendUserMessageAndStartCompletion(message sdk.Message, images []domain.ImageAttachment) tea.Cmd {
	userEntry := domain.ConversationEntry{
		Message: message,
		Time:    time.Now(),
		Images:  images,
	}

	if err := p.handler.conversationRepo.AddMessage(userEntry); err != nil {
		logger.Error("chat: failed to AddMessage in appendUserMessageAndStartCompletion", "error", err)
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to save message: %v", err),
				Sticky: false,
			}
		}
	}

	logger.Info("chat: AddMessage + startChatCompletion",
		"repo_messages_after_add", len(p.handler.conversationRepo.GetMessages()),
		"queue_size", p.handler.messageQueue.Size())

	p.handler.stateManager.SetChatPending()

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
