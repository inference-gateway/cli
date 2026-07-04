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

	defer func() {
		if err := cmd.Wait(); err != nil {
			stderrDone.Wait()
			stderrOutput := stderrBuf.String()
			logger.Error(fmt.Sprintf("Claude CLI process error: %v, stderr: %s", err, stderrOutput))

			if stderrOutput != "" && ctx.Err() == nil {
				errMsg := []byte(fmt.Sprintf("Claude CLI error: %s", stderrOutput))
				eventType := sdk.SSEventEvent("error")
				select {
				case events <- sdk.SSEvent{Event: &eventType, Data: &errMsg}:
				case <-ctx.Done():
				}
			}
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)

	var model string

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

		if msg.Type == "system" && msg.Subtype == "init" && msg.ClaudeModel != "" {
			model = msg.ClaudeModel
		}

		for _, event := range c.transformMessage(msg, []byte(line), model) {
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

}

// createDeltaEvent wraps choices into a chat.completion.chunk SSE event
func (c *ClaudeCodeClient) createDeltaEvent(delta map[string]any, model string) sdk.SSEvent {
	streamResponse := map[string]any{
		"id":      "chatcmpl-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
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
func (c *ClaudeCodeClient) createUsageEvent(usage *sdk.CompletionUsage, model string) sdk.SSEvent {
	streamResponse := map[string]any{
		"id":      "chatcmpl-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
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

// transformMessage converts one Claude Code JSONL line into SDK SSEvents.
// raw is the original line, forwarded verbatim for hook events; model is the
// model learned from the system/init event, stamped on every chunk.
func (c *ClaudeCodeClient) transformMessage(msg ClaudeCodeMessage, raw []byte, model string) []sdk.SSEvent {
	switch msg.Type {
	case "assistant":
		return c.transformAssistantMessage(msg, model)
	case "result":
		return c.transformResultMessage(msg, model)
	case "user":
		return c.transformUserMessage(msg, model)
	case "system":
		return c.transformSystemMessage(msg, raw)
	default:
		logger.Debug(fmt.Sprintf("Claude Code unknown event type: %s", msg.Type))
		return nil
	}
}

// transformAssistantMessage converts assistant content blocks (text, thinking,
// tool_use) into content_block_delta chunks.
func (c *ClaudeCodeClient) transformAssistantMessage(msg ClaudeCodeMessage, model string) []sdk.SSEvent {
	var events []sdk.SSEvent

	var assistantMsg AssistantMessage
	if err := json.Unmarshal(msg.Message, &assistantMsg); err != nil {
		logger.Error(fmt.Sprintf("Failed to parse assistant message: %v", err))
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
			events = append(events, c.createDeltaEvent(map[string]any{
				"choices": []map[string]any{
					{"delta": map[string]any{"content": content}, "index": 0},
				},
			}, model))
			lastBlockType = "text"
		case "thinking":
			events = append(events, c.createDeltaEvent(map[string]any{
				"choices": []map[string]any{
					{"delta": map[string]any{"reasoning_content": block.Thinking}, "index": 0},
				},
			}, model))
		case "tool_use":
			events = append(events, c.createDeltaEvent(map[string]any{
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
			}, model))
		}
	}

	return events
}

// transformUserMessage converts tool_result blocks into tool-call delta chunks,
// plus a typed tool_failure event when the result carries is_error=true.
func (c *ClaudeCodeClient) transformUserMessage(msg ClaudeCodeMessage, model string) []sdk.SSEvent {
	var events []sdk.SSEvent

	var toolResultMsg ToolResultMessage
	if err := json.Unmarshal(msg.Message, &toolResultMsg); err != nil {
		logger.Error(fmt.Sprintf("Failed to parse user message: %v", err))
		return events
	}

	for _, content := range toolResultMsg.Content {
		if content.Type != "tool_result" {
			continue
		}

		events = append(events, c.createDeltaEvent(map[string]any{
			"choices": []map[string]any{
				{
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index":    0,
								"id":       content.ToolUseID,
								"result":   content.Content,
								"is_error": content.IsError,
							},
						},
					},
					"finish_reason": "stop",
					"index":         0,
				},
			},
		}, model))

		if content.IsError {
			events = append(events, c.createToolFailureEvent(content.ToolUseID, content.Content))
		}
	}

	return events
}

// transformResultMessage emits the final-result triple: a result_metadata event
// carrying run metadata (durations, TTFT, cost, token breakdown incl. cache
// splits, permission denials), the usage-bearing chunk both accumulators depend
// on, and the message_stop terminator.
func (c *ClaudeCodeClient) transformResultMessage(msg ClaudeCodeMessage, model string) []sdk.SSEvent {
	usage := &ClaudeUsage{}
	if msg.Usage != nil {
		usage = msg.Usage
	}

	metadata := map[string]any{
		"session_id":      msg.SessionID,
		"model":           model,
		"subtype":         msg.Subtype,
		"is_error":        msg.IsError,
		"duration_ms":     msg.DurationMS,
		"duration_api_ms": msg.DurationAPIMS,
		"ttft_ms":         msg.TTFTMS,
		"ttft_stream_ms":  msg.TTFTStreamMS,
		"num_turns":       msg.NumTurns,
		"stop_reason":     msg.StopReason,
		"terminal_reason": msg.TerminalReason,
		"total_cost_usd":  msg.TotalCostUSD,
		"usage": map[string]any{
			"input_tokens":                usage.InputTokens,
			"output_tokens":               usage.OutputTokens,
			"cache_creation_input_tokens": usage.CacheCreationInputTokens,
			"cache_read_input_tokens":     usage.CacheReadInputTokens,
		},
	}
	if denials := string(msg.PermissionDenials); denials != "" && denials != "[]" && denials != "null" {
		metadata["permission_denials"] = msg.PermissionDenials
	}

	metadataBytes, _ := json.Marshal(metadata)
	metadataType := sdk.SSEventEvent("result_metadata")
	doneBytes := []byte("done")
	stopType := sdk.SSEventEvent("message_stop")

	return []sdk.SSEvent{
		{Event: &metadataType, Data: &metadataBytes},
		c.createUsageEvent(usage.toCompletionUsage(), model),
		{Event: &stopType, Data: &doneBytes},
	}
}

// transformSystemMessage handles system-typed lines: init becomes a system_init
// event with session metadata; hook lifecycle events (from --include-hook-events,
// emitted as subtype hook_started/hook_response) are forwarded verbatim as
// hook_event so downstream consumers can derive tool/hook timings.
func (c *ClaudeCodeClient) transformSystemMessage(msg ClaudeCodeMessage, raw []byte) []sdk.SSEvent {
	switch msg.Subtype {
	case "init":
		initBytes, _ := json.Marshal(map[string]any{
			"type":                "system_init",
			"session_id":          msg.SessionID,
			"cwd":                 msg.CWD,
			"model":               msg.ClaudeModel,
			"permission_mode":     msg.PermissionMode,
			"claude_code_version": msg.ClaudeCodeVersion,
			"tools":               msg.Tools,
		})
		eventType := sdk.SSEventEvent("system_init")
		return []sdk.SSEvent{{Event: &eventType, Data: &initBytes}}
	case "hook_started", "hook_response":
		hookBytes := make([]byte, len(raw))
		copy(hookBytes, raw)
		eventType := sdk.SSEventEvent("hook_event")
		return []sdk.SSEvent{{Event: &eventType, Data: &hookBytes}}
	default:
		logger.Debug(fmt.Sprintf("Claude Code system event: subtype=%s session=%s", msg.Subtype, msg.SessionID))
		return nil
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
	IsError           bool            `json:"is_error,omitempty"`
	DurationMS        int             `json:"duration_ms,omitempty"`
	DurationAPIMS     int             `json:"duration_api_ms,omitempty"`
	TTFTMS            int             `json:"ttft_ms,omitempty"`
	TTFTStreamMS      int             `json:"ttft_stream_ms,omitempty"`
	NumTurns          int             `json:"num_turns,omitempty"`
	Result            string          `json:"result,omitempty"`
	StopReason        string          `json:"stop_reason,omitempty"`
	TotalCostUSD      float64         `json:"total_cost_usd,omitempty"`
	TerminalReason    string          `json:"terminal_reason,omitempty"`
	PermissionDenials json.RawMessage `json:"permission_denials,omitempty"`
	Usage             *ClaudeUsage    `json:"usage,omitempty"`
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
