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
	eventChan, err := c.GenerateContentStream(ctx, provider, model, messages)
	if err != nil {
		return nil, err
	}

	content, toolCallsMap, usage, err := c.processEvents(eventChan)
	if err != nil {
		return nil, err
	}

	response := c.buildResponse(content, toolCallsMap)
	response.Usage = usage
	return response, nil
}

// GenerateContentStream makes a streaming request to Claude Code CLI
func (c *ClaudeCodeClient) GenerateContentStream(
	ctx context.Context,
	provider sdk.Provider,
	model string,
	messages []sdk.Message,
) (<-chan sdk.SSEvent, error) {
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

	if err := cmd.Start(); err != nil {
		return nil, c.wrapError(err)
	}

	filteredMessages := c.filterMessages(messages)
	messagesJSON, err := json.Marshal(filteredMessages)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal messages: %w", err)
	}

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

	if idx := strings.LastIndex(model, "/"); idx != -1 {
		model = model[idx+1:]
	}

	args := []string{
		"--output-format", "stream-json",
		"--verbose",
		"--include-hook-events",
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
	case domain.AgentModeReadOnly:
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

// streamOutput reads stdout and stderr from the Claude CLI process and sends events.
// It parses each JSONL line incrementally and converts them into SSE events for the
// downstream consumer. Stderr is buffered and only reported if the process exits with
// an error. Malformed JSON lines are logged and skipped; the stream continues.
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
			logger.Error(fmt.Sprintf("error reading stderr: %v", stderrErr))
		}
	}()

	scanner := bufio.NewScanner(stdout)
	// Increase scanner buffer to handle large JSON lines (e.g. long tool inputs)
	scanner.Buffer(make([]byte, 0, 256*1024), 512*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if line == "done" || line == `"done"` {
			break
		}

		var msg ClaudeCodeMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			logger.Error(fmt.Sprintf("Failed to parse JSON line: %v", err))
			continue
		}

		for _, event := range c.transformMessage(msg) {
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Error(fmt.Sprintf("scanner error: %v", err))
		errMsg := []byte(err.Error())
		eventType := sdk.SSEventEvent("error")
		select {
		case events <- sdk.SSEvent{Event: &eventType, Data: &errMsg}:
		case <-ctx.Done():
			return
		}
	}

	if err := cmd.Wait(); err != nil {
		stderrDone.Wait()

		stderrOutput := stderrBuf.String()
		logger.Error(fmt.Sprintf("Claude CLI process error: %v, stderr: %s", err, stderrOutput))

		if stderrOutput != "" {
			errMsg := []byte(fmt.Sprintf("Claude CLI error: %s", stderrOutput))
			eventType := sdk.SSEventEvent("error")
			select {
			case events <- sdk.SSEvent{Event: &eventType, Data: &errMsg}:
			case <-ctx.Done():
				return
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
	eventType := sdk.SSEventEvent("content_block_delta")
	return sdk.SSEvent{Event: &eventType, Data: &responseBytes}
}

// createUsageEvent builds a usage-bearing chat.completion.chunk. It carries a
// single empty-delta choice (a no-op for the sync content/tool accumulator, and
// required by the chat TUI consumer which only reads Usage inside its choices
// loop) plus the top-level usage object that both consumers read.
func (c *ClaudeCodeClient) createUsageEvent(usage *sdk.CompletionUsage) sdk.SSEvent {
	streamResponse := map[string]any{
		"id":      "chatcmpl-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   "",
		"choices": []map[string]any{
			{"delta": map[string]any{}, "index": 0},
		},
		"usage": map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		},
	}

	responseBytes, _ := json.Marshal(streamResponse)
	eventType := sdk.SSEventEvent("content_block_delta")
	return sdk.SSEvent{Event: &eventType, Data: &responseBytes}
}

func (c *ClaudeCodeClient) transformMessage(msg ClaudeCodeMessage) []sdk.SSEvent {
	var events []sdk.SSEvent

	switch msg.Type {
	case "assistant":
		var assistantMsg AssistantMessage
		if err := json.Unmarshal(msg.Message, &assistantMsg); err != nil {
			return events
		}

		var lastBlockType string
		for _, block := range assistantMsg.Content {
			switch block.Type {
			case "text":
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
		usage := &ClaudeUsage{}
		if msg.Usage != nil {
			usage = msg.Usage
		}
		doneBytes := []byte("done")
		stopType := sdk.SSEventEvent("message_stop")
		return []sdk.SSEvent{
			c.createUsageEvent(usage.toCompletionUsage()),
			{Event: &stopType, Data: &doneBytes},
		}

	case "user":
		var toolResultMsg ToolResultMessage
		if err := json.Unmarshal(msg.Message, &toolResultMsg); err != nil {
			// Log but don't fail the stream for a single unparsable message
			logger.Error(fmt.Sprintf("Failed to parse user message: %v", err))
			return events
		}

		for _, content := range toolResultMsg.Content {
			if content.Type == "tool_result" {
				// Build a delta event for the tool result, including the is_error status
				toolResultData := []map[string]any{
					{
						"index":    0,
						"id":       content.ToolUseID,
						"result":   content.Content,
						"is_error": content.IsError,
					},
				}

				response := map[string]any{
					"choices": []map[string]any{
						{
							"delta": map[string]any{
								"tool_calls": toolResultData,
							},
							"finish_reason": "stop",
							"index":         0,
						},
					},
				}

				events = append(events, c.createDeltaEvent(response))

				// Emit a separate tool_failure event when the tool result indicates an error
				if content.IsError {
					failureEvent := c.createToolFailureEvent(content.ToolUseID, content.Content)
					events = append(events, failureEvent)
				}
			}
		}

		return events

	case "system":
		// Log system events (init, etc.) for debugging but don't forward as content
		if msg.Subtype != "" {
			logger.Debug(fmt.Sprintf("Claude Code system event: subtype=%s session=%s", msg.Subtype, msg.SessionID))
		}
		// Emit a lightweight notification that the session has started
		systemEvent := c.createSystemEvent(msg)
		if systemEvent != nil {
			events = append(events, *systemEvent)
		}
		return events

	case "hook":
		// Hook events from --include-hook-events are forwarded as a typed
		// progress event so callers can observe hook lifecycle
		hookBytes, _ := json.Marshal(map[string]any{
			"type":    "hook",
			"subtype": msg.Subtype,
		})
		eventType := sdk.SSEventEvent("hook_event")
		events = append(events, sdk.SSEvent{
			Event: &eventType,
			Data:  &hookBytes,
		})
		return events

	default:
		logger.Debug(fmt.Sprintf("Claude Code unknown event type: %s", msg.Type))
		return events
	}
}

// createToolFailureEvent emits a typed SSE event for tool call failures so that
// downstream consumers can track which tool calls failed even when the overall
// Claude Code run completes with is_error=false.
func (c *ClaudeCodeClient) createToolFailureEvent(toolUseID, errorMsg string) sdk.SSEvent {
	failureBytes, _ := json.Marshal(map[string]any{
		"tool_use_id": toolUseID,
		"error":       errorMsg,
	})
	eventType := sdk.SSEventEvent("tool_failure")
	return sdk.SSEvent{
		Event: &eventType,
		Data:  &failureBytes,
	}
}

// createSystemEvent emits a lightweight notification for system-level events
// (e.g. session init).
func (c *ClaudeCodeClient) createSystemEvent(msg ClaudeCodeMessage) *sdk.SSEvent {
	if msg.Subtype != "init" {
		return nil
	}
	initBytes, _ := json.Marshal(map[string]any{
		"type":                "system_init",
		"session_id":          msg.SessionID,
		"model":               msg.ClaudeModel,
		"claude_code_version": msg.ClaudeCodeVersion,
		"tools":               msg.Tools,
	})
	eventType := sdk.SSEventEvent("system_init")
	return &sdk.SSEvent{
		Event: &eventType,
		Data:  &initBytes,
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
	IsError   bool   `json:"is_error"`
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

// ClaudeCodeMessage represents a JSONL line from the Claude CLI streaming output.
// It covers all event types: system init, assistant messages, user/tool results,
// final result, and hook events.
type ClaudeCodeMessage struct {
	// Common fields
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	SessionID string          `json:"session_id,omitempty"`

	// System init fields (type: system, subtype: init)
	CWD               string   `json:"cwd,omitempty"`
	ClaudeModel       string   `json:"model,omitempty"`
	PermissionMode    string   `json:"permissionMode,omitempty"`
	ClaudeCodeVersion string   `json:"claude_code_version,omitempty"`
	Tools             []string `json:"tools,omitempty"`

	// User/tool_result fields
	ToolUseResult json.RawMessage `json:"tool_use_result,omitempty"`

	// Final result fields (type: result)
	IsError           bool         `json:"is_error,omitempty"`
	DurationMS        int          `json:"duration_ms,omitempty"`
	NumTurns          int          `json:"num_turns,omitempty"`
	Result            string       `json:"result,omitempty"`
	StopReason        string       `json:"stop_reason,omitempty"`
	TotalCostUSD      float64      `json:"total_cost_usd,omitempty"`
	PermissionDenials []string     `json:"permission_denials,omitempty"`
	TerminalReason    string       `json:"terminal_reason,omitempty"`
	Usage             *ClaudeUsage `json:"usage,omitempty"`
}

// ClaudeUsage is Anthropic's token breakdown as reported on the result message.
type ClaudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// toCompletionUsage maps Anthropic's token breakdown onto the SDK's
// prompt/completion shape. The SDK usage type has no cache fields, so cache
// tokens fold into prompt_tokens - matching how the gateway reports a single
// prompt_tokens count for cached input.
func (u *ClaudeUsage) toCompletionUsage() *sdk.CompletionUsage {
	prompt := u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	return &sdk.CompletionUsage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
	}
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

// processEvents processes SSE events and accumulates content, tool calls, and
// the usage carried by the final usage chunk.
func (c *ClaudeCodeClient) processEvents(eventChan <-chan sdk.SSEvent) (string, map[string]*sdk.ChatCompletionMessageToolCall, *sdk.CompletionUsage, error) {
	var content strings.Builder
	toolCallsMap := make(map[string]*sdk.ChatCompletionMessageToolCall)
	var usage *sdk.CompletionUsage

	for event := range eventChan {
		if event.Data != nil {
			if u := parseChunkUsage(*event.Data); u != nil {
				usage = u
			}
		}
		if err := c.handleEvent(event, &content, toolCallsMap); err != nil {
			return "", nil, nil, err
		}
	}

	return content.String(), toolCallsMap, usage, nil
}

// parseChunkUsage extracts a top-level usage object from a chat.completion.chunk
// payload, returning nil when the chunk carries no usage (content/tool deltas).
func parseChunkUsage(data []byte) *sdk.CompletionUsage {
	var chunk struct {
		Usage *sdk.CompletionUsage `json:"usage"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil
	}
	return chunk.Usage
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
		return nil
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
				ID:   id,
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
