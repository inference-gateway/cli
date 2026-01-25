package adapters

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// ClaudeCodeClient is a wrapper around the official Claude Code CLI
// It implements the SDKClient interface by spawning the claude process
type ClaudeCodeClient struct {
	config         *config.ClaudeCodeConfig
	stateManager   domain.StateManager
	tools          *[]sdk.ChatCompletionTool
	options        *sdk.CreateChatCompletionRequest
	middlewareOpts *sdk.MiddlewareOptions
	wg             *sync.WaitGroup
}

// NewClaudeCodeClient creates a new Claude Code CLI client
func NewClaudeCodeClient(cfg *config.ClaudeCodeConfig, stateManager domain.StateManager) domain.SDKClient {
	return &ClaudeCodeClient{
		config:       cfg,
		stateManager: stateManager,
		wg:           &sync.WaitGroup{},
	}
}

// WithOptions sets the chat completion request options
func (c *ClaudeCodeClient) WithOptions(opts *sdk.CreateChatCompletionRequest) domain.SDKClient {
	clone := *c
	clone.options = opts
	return &clone
}

// WithTools sets the tools for the chat completion
func (c *ClaudeCodeClient) WithTools(tools *[]sdk.ChatCompletionTool) domain.SDKClient {
	clone := *c
	clone.tools = tools
	return &clone
}

// WithMiddlewareOptions sets middleware options (not used in Claude Code mode)
func (c *ClaudeCodeClient) WithMiddlewareOptions(opts *sdk.MiddlewareOptions) domain.SDKClient {
	clone := *c
	clone.middlewareOpts = opts
	return &clone
}

// GenerateContent makes a non-streaming request to Claude Code CLI
func (c *ClaudeCodeClient) GenerateContent(
	ctx context.Context,
	provider sdk.Provider,
	model string,
	messages []sdk.Message,
) (*sdk.CreateChatCompletionResponse, error) {
	logger.Debug(fmt.Sprintf("ClaudeCodeClient.GenerateContent called with model: %s", model))

	eventChan, err := c.GenerateContentStream(ctx, provider, model, messages)
	if err != nil {
		return nil, err
	}

	content, toolCallsMap, err := c.processEvents(eventChan)
	if err != nil {
		return nil, err
	}

	return c.buildResponse(content, toolCallsMap), nil
}

// GenerateContentStream makes a streaming request to Claude Code CLI
func (c *ClaudeCodeClient) GenerateContentStream(
	ctx context.Context,
	provider sdk.Provider,
	model string,
	messages []sdk.Message,
) (<-chan sdk.SSEvent, error) {
	logger.Debug(fmt.Sprintf("claudeCodeClient.GenerateContentStream called with model: %s", model))

	args := c.buildArgs(model)

	cmd := exec.CommandContext(ctx, c.config.CLIPath, args...)
	cmd.Env = c.buildEnv()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, c.wrapError(err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, c.wrapError(err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, c.wrapError(err)
	}

	logger.Debug(fmt.Sprintf("starting claude cli process: %s %v", c.config.CLIPath, args))
	if err := cmd.Start(); err != nil {
		return nil, c.wrapError(err)
	}

	filteredMessages := c.filterMessages(messages)
	messagesJSON, err := json.Marshal(filteredMessages)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal messages: %w", err)
	}

	logger.Debug(fmt.Sprintf("writing %d bytes of messages to stdin", len(messagesJSON)))
	if _, err := stdin.Write(messagesJSON); err != nil {
		return nil, fmt.Errorf("failed to write to stdin: %w", err)
	}
	_ = stdin.Close()

	events := make(chan sdk.SSEvent, 100)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.streamOutput(ctx, stdout, stderr, events, cmd)
	}()

	return events, nil
}

// buildArgs constructs the command-line arguments for the Claude CLI
func (c *ClaudeCodeClient) buildArgs(model string) []string {
	permissionMode := c.getPermissionMode()
	maxTurns := c.config.MaxTurns

	args := []string{
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", fmt.Sprintf("%d", maxTurns),
		"--model", model,
		"--permission-mode", permissionMode,
		"-p",
	}

	if c.tools != nil && len(*c.tools) > 0 {
		args = append(args, "--disallowedTools", "all")
	}

	return args
}

// getPermissionMode maps Infer agent mode to Claude Code permission mode
func (c *ClaudeCodeClient) getPermissionMode() string {
	if c.stateManager == nil {
		return "default"
	}

	mode := c.stateManager.GetAgentMode()
	switch mode {
	case domain.AgentModeStandard:
		return "default"
	case domain.AgentModeAutoAccept:
		return "bypassPermissions"
	case domain.AgentModePlan:
		return "plan"
	default:
		return "default"
	}
}

// buildEnv constructs the environment variables for the Claude CLI process
func (c *ClaudeCodeClient) buildEnv() []string {
	env := os.Environ()

	env = append(env, fmt.Sprintf("CLAUDE_CODE_MAX_OUTPUT_TOKENS=%d", c.config.MaxOutputTokens))
	env = append(env, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")
	env = append(env, "DISABLE_NON_ESSENTIAL_MODEL_CALLS=1")

	if c.config.ThinkingBudget > 0 {
		env = append(env, fmt.Sprintf("MAX_THINKING_TOKENS=%d", c.config.ThinkingBudget))
	}

	env = filterEnv(env, "ANTHROPIC_API_KEY")

	return env
}

// streamOutput reads stdout and stderr from the Claude CLI process and sends events
func (c *ClaudeCodeClient) streamOutput(
	ctx context.Context,
	stdout io.Reader,
	stderr io.Reader,
	events chan<- sdk.SSEvent,
	cmd *exec.Cmd,
) {
	defer close(events)

	stderrBuf := &bytes.Buffer{}
	var stderrErr error
	var stderrDone sync.WaitGroup
	stderrDone.Add(1)
	go func() {
		defer stderrDone.Done()
		_, stderrErr = io.Copy(stderrBuf, stderr)
		if stderrErr != nil {
			logger.Debug(fmt.Sprintf("error reading stderr: %v", stderrErr))
		}
	}()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			logger.Debug("context cancelled, stopping stream")
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if line == "done" || line == `"done"` {
			logger.Debug("Claude CLI completed successfully")
			break
		}

		logger.Debug(fmt.Sprintf("claude CLI output: %s", line))

		var msg ClaudeCodeMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			logger.Debug(fmt.Sprintf("Failed to parse JSON line: %v", err))
			continue
		}

		for _, event := range c.transformMessage(msg) {
			events <- event
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Debug(fmt.Sprintf("scanner error: %v", err))
		errMsg := []byte(err.Error())
		eventType := sdk.SSEventEvent("error")
		events <- sdk.SSEvent{
			Event: &eventType,
			Data:  &errMsg,
		}
	}

	if err := cmd.Wait(); err != nil {
		stderrDone.Wait()

		stderrOutput := stderrBuf.String()
		logger.Debug(fmt.Sprintf("Claude CLI process error: %v, stderr: %s", err, stderrOutput))

		if stderrOutput != "" {
			errMsg := []byte(fmt.Sprintf("Claude CLI error: %s", stderrOutput))
			eventType := sdk.SSEventEvent("error")
			events <- sdk.SSEvent{
				Event: &eventType,
				Data:  &errMsg,
			}
		}
	}
}

// transformMessage converts Claude Code CLI output to SDK SSEvent format
// Returns a slice of events since a single message can contain multiple content blocks
func (c *ClaudeCodeClient) createDeltaEvent(delta map[string]any) sdk.SSEvent {
	streamResponse := map[string]any{
		"id":      "chatcmpl-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   "",
		"choices": delta["choices"],
	}

	responseBytes, _ := json.Marshal(streamResponse)
	logger.Debug(fmt.Sprintf("SSE event data: %s", string(responseBytes)))
	eventType := sdk.SSEventEvent("content_block_delta")
	return sdk.SSEvent{Event: &eventType, Data: &responseBytes}
}

func (c *ClaudeCodeClient) transformMessage(msg ClaudeCodeMessage) []sdk.SSEvent {
	logger.Debug(fmt.Sprintf("transforming message type: %s, subtype: %s", msg.Type, msg.Subtype))

	var events []sdk.SSEvent

	switch msg.Type {
	case "assistant":
		var assistantMsg AssistantMessage
		if err := json.Unmarshal(msg.Message, &assistantMsg); err != nil {
			logger.Debug(fmt.Sprintf("failed to unmarshal assistant message: %v, raw: %s", err, string(msg.Message)))
			return events
		}

		logger.Debug(fmt.Sprintf("assistant message content blocks: %d", len(assistantMsg.Content)))

		var lastBlockType string
		for _, block := range assistantMsg.Content {
			switch block.Type {
			case "text":
				logger.Debug(fmt.Sprintf("text block with %d chars: '%s'", len(block.Text), block.Text))

				content := block.Text
				if lastBlockType == "text" && content != "" {
					content = "\n" + content
				}

				response := map[string]any{
					"choices": []map[string]any{
						{
							"delta": map[string]any{
								"content": content,
							},
							"index": 0,
						},
					},
				}

				events = append(events, c.createDeltaEvent(response))
				lastBlockType = "text"
			case "thinking":
				logger.Debug(fmt.Sprintf("thinking block with %d chars", len(block.Thinking)))

				response := map[string]any{
					"choices": []map[string]any{
						{
							"delta": map[string]any{
								"reasoning_content": block.Thinking,
							},
							"index": 0,
						},
					},
				}

				events = append(events, c.createDeltaEvent(response))
			case "tool_use":
				logger.Debug(fmt.Sprintf("tool_use block: %s (id: %s)", block.Name, block.ID))

				response := map[string]any{
					"choices": []map[string]any{
						{
							"delta": map[string]any{
								"tool_calls": []map[string]any{
									{
										"index": 0,
										"id":    block.ID,
										"type":  "function",
										"function": map[string]any{
											"name":      block.Name,
											"arguments": string(block.Input),
										},
									},
								},
							},
							"index": 0,
						},
					},
				}

				events = append(events, c.createDeltaEvent(response))
			}
		}

		return events

	case "result":
		logger.Debug(fmt.Sprintf("result message: cost=$%.4f, duration=%dms, turns=%d",
			msg.TotalCostUSD, msg.DurationMS, msg.NumTurns))

		doneBytes := []byte("done")
		eventType := sdk.SSEventEvent("message_stop")
		return []sdk.SSEvent{{
			Event: &eventType,
			Data:  &doneBytes,
		}}

	case "user":
		logger.Debug("user message (tool results)")

		var toolResultMsg ToolResultMessage
		if err := json.Unmarshal(msg.Message, &toolResultMsg); err != nil {
			logger.Debug(fmt.Sprintf("failed to unmarshal tool result message: %v", err))
			return events
		}

		for _, content := range toolResultMsg.Content {
			if content.Type == "tool_result" {
				logger.Debug(fmt.Sprintf("tool result for %s: %s", content.ToolUseID, truncateString(content.Content, 100)))

				response := map[string]any{
					"choices": []map[string]any{
						{
							"delta": map[string]any{
								"tool_calls": []map[string]any{
									{
										"index":  0,
										"id":     content.ToolUseID,
										"result": content.Content,
									},
								},
							},
							"finish_reason": "stop",
							"index":         0,
						},
					},
				}

				events = append(events, c.createDeltaEvent(response))
			}
		}

		return events

	case "system":
		logger.Debug("System message (ignored)")
		return events

	default:
		logger.Debug(fmt.Sprintf("unknown message type: %s", msg.Type))
		return events
	}
}

// AssistantMessage represents the structure of an assistant message from Claude CLI
type AssistantMessage struct {
	Content []ContentBlock `json:"content"`
	Role    string         `json:"role"`
}

// ToolResultMessage represents a user message containing tool results
type ToolResultMessage struct {
	Role    string              `json:"role"`
	Content []ToolResultContent `json:"content"`
}

// ToolResultContent represents a tool result in the message
type ToolResultContent struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// ContentBlock represents a content block in the assistant message
type ContentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	Thinking string          `json:"thinking,omitempty"`
	ID       string          `json:"id,omitempty"`    // For tool_use
	Name     string          `json:"name,omitempty"`  // For tool_use
	Input    json.RawMessage `json:"input,omitempty"` // For tool_use
}

// filterMessages removes unsupported content (like images) from messages
// For now, we pass messages through as-is
// TODO: Implement proper image filtering if Claude CLI doesn't support them
func (c *ClaudeCodeClient) filterMessages(messages []sdk.Message) []sdk.Message {
	logger.Debug(fmt.Sprintf("passing %d messages to claude cli", len(messages)))
	return messages
}

// wrapError provides user-friendly error messages
func (c *ClaudeCodeClient) wrapError(err error) error {
	if os.IsNotExist(err) || strings.Contains(err.Error(), "executable file not found") {
		return fmt.Errorf(
			"claude Code CLI not found at '%s'.\n\n"+
				"Install Claude Code CLI:\n"+
				"  npm install -g @anthropic-ai/claude-code\n\n"+
				"Or set custom path in config:\n"+
				"  claude_code:\n"+
				"    cli_path: /path/to/claude",
			c.config.CLIPath,
		)
	}
	return err
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

// ClaudeCodeMessage represents a message from the Claude CLI JSON output
type ClaudeCodeMessage struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	SessionID string          `json:"session_id,omitempty"`

	// Result fields
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	IsError      bool    `json:"is_error,omitempty"`
	DurationMS   int     `json:"duration_ms,omitempty"`
	NumTurns     int     `json:"num_turns,omitempty"`
}

// filterEnv removes a specific environment variable from the env slice
func filterEnv(env []string, key string) []string {
	filtered := make([]string, 0, len(env))
	prefix := key + "="

	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			filtered = append(filtered, e)
		}
	}

	return filtered
}

// processEvents processes SSE events and accumulates content and tool calls
func (c *ClaudeCodeClient) processEvents(eventChan <-chan sdk.SSEvent) (string, map[string]*sdk.ChatCompletionMessageToolCall, error) {
	var content strings.Builder
	toolCallsMap := make(map[string]*sdk.ChatCompletionMessageToolCall)

	for event := range eventChan {
		if err := c.handleEvent(event, &content, toolCallsMap); err != nil {
			return "", nil, err
		}
	}

	return content.String(), toolCallsMap, nil
}

// handleEvent processes a single SSE event
func (c *ClaudeCodeClient) handleEvent(event sdk.SSEvent, content *strings.Builder, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall) error {
	if event.Event != nil && string(*event.Event) == "error" {
		errMsg := "unknown error"
		if event.Data != nil {
			errMsg = string(*event.Data)
		}
		return fmt.Errorf("stream error: %s", errMsg)
	}

	if event.Data == nil {
		return nil
	}

	return c.processEventData(*event.Data, content, toolCallsMap)
}

// processEventData processes the data from an SSE event
func (c *ClaudeCodeClient) processEventData(data []byte, content *strings.Builder, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall) error {
	var delta map[string]any
	if err := json.Unmarshal(data, &delta); err != nil {
		logger.Debug(fmt.Sprintf("failed to parse SSE event: %v", err))
		return nil // Continue processing other events
	}

	choices, ok := delta["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil
	}

	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil
	}

	deltaContent, ok := choice["delta"].(map[string]any)
	if !ok {
		return nil
	}

	c.processDeltaContent(deltaContent, content, toolCallsMap)
	return nil
}

// processDeltaContent processes the delta content from a choice
func (c *ClaudeCodeClient) processDeltaContent(deltaContent map[string]any, content *strings.Builder, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall) {
	if textContent, ok := deltaContent["content"].(string); ok {
		content.WriteString(textContent)
	}

	if toolCallsRaw, ok := deltaContent["tool_calls"].([]any); ok {
		c.processToolCalls(toolCallsRaw, toolCallsMap)
	}
}

// processToolCalls processes tool calls from delta content
func (c *ClaudeCodeClient) processToolCalls(toolCallsRaw []any, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall) {
	for _, tcRaw := range toolCallsRaw {
		tc, ok := tcRaw.(map[string]any)
		if !ok {
			continue
		}

		id, _ := tc["id"].(string)
		if id == "" {
			continue
		}

		toolCall, exists := toolCallsMap[id]
		if !exists {
			toolCall = &sdk.ChatCompletionMessageToolCall{
				Id:   id,
				Type: "function",
			}
			toolCallsMap[id] = toolCall
		}

		c.processToolCallFunction(tc, toolCall)
	}
}

// processToolCallFunction processes function details from a tool call
func (c *ClaudeCodeClient) processToolCallFunction(tc map[string]any, toolCall *sdk.ChatCompletionMessageToolCall) {
	if funcRaw, ok := tc["function"].(map[string]any); ok {
		if name, ok := funcRaw["name"].(string); ok {
			toolCall.Function.Name = name
		}
		if args, ok := funcRaw["arguments"].(string); ok {
			toolCall.Function.Arguments += args
		}
	}
}

// buildResponse constructs the final response from accumulated content and tool calls
func (c *ClaudeCodeClient) buildResponse(content string, toolCallsMap map[string]*sdk.ChatCompletionMessageToolCall) *sdk.CreateChatCompletionResponse {
	var toolCalls []sdk.ChatCompletionMessageToolCall
	for _, tc := range toolCallsMap {
		toolCalls = append(toolCalls, *tc)
	}

	msgContent := sdk.MessageContent{}
	_ = msgContent.FromMessageContent0(content)

	msg := sdk.Message{
		Role:    "assistant",
		Content: msgContent,
	}

	if len(toolCalls) > 0 {
		msg.ToolCalls = &toolCalls
	}

	return &sdk.CreateChatCompletionResponse{
		Choices: []sdk.ChatCompletionChoice{
			{
				Message: msg,
			},
		},
	}
}

// Wait blocks until all background goroutines have completed
// This should be called during shutdown to ensure clean resource cleanup
func (c *ClaudeCodeClient) Wait() {
	if c.wg != nil {
		c.wg.Wait()
	}
}
