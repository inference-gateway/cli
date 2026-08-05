// Package adapters bridges external SDK surfaces to the shapes the agent
// consumes.
package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// defaultMaxTokens guards the mandatory Messages API max_tokens field when no
// value was configured via WithOptions.
const defaultMaxTokens = 4096

// AnthropicMessages is a removable sdk.Client decorator that reroutes agent
// traffic for the Anthropic provider to the gateway's native /v1/messages
// endpoint, enabling prompt caching via cache_control breakpoints (system
// prompt, last tool, and a rolling conversation breakpoint). Anthropic SSE
// events are translated back into OpenAI-shaped chat-completion chunks, so
// the agent's streaming pipeline is unaware of the endpoint switch. Every
// other provider - and every non-generate method - delegates to the wrapped
// client untouched. To remove the adapter, delete this package, unwrap the
// client in the container, and drop the TakeCacheCreationTokens read in
// storeIterationMetrics.
type AnthropicMessages struct {
	inner         sdk.Client
	opts          *sdk.CreateChatCompletionRequest
	tools         *[]sdk.ChatCompletionTool
	cacheCreation atomic.Int64
	// noEffort remembers models that rejected output_config.effort (e.g.
	// Haiku 4.5) so later turns skip the parameter instead of re-tripping
	// the same 400.
	noEffort sync.Map
}

var _ sdk.Client = (*AnthropicMessages)(nil)

// NewAnthropicMessages wraps the raw SDK client with Anthropic /v1/messages
// routing.
func NewAnthropicMessages(inner sdk.Client) *AnthropicMessages {
	return &AnthropicMessages{inner: inner}
}

// TakeCacheCreationTokens returns the cache-write token count of the last
// completed Anthropic request and resets it.
func (a *AnthropicMessages) TakeCacheCreationTokens() int {
	return int(a.cacheCreation.Swap(0))
}

// The builder methods capture what the Messages translation needs and return
// the adapter itself - returning the inner client would let subsequent calls
// in a builder chain bypass the adapter.

func (a *AnthropicMessages) WithAuthToken(token string) sdk.Client {
	a.inner.WithAuthToken(token)
	return a
}

func (a *AnthropicMessages) WithTools(tools *[]sdk.ChatCompletionTool) sdk.Client {
	a.tools = tools
	a.inner.WithTools(tools)
	return a
}

func (a *AnthropicMessages) WithOptions(options *sdk.CreateChatCompletionRequest) sdk.Client {
	a.opts = options
	a.inner.WithOptions(options)
	return a
}

func (a *AnthropicMessages) WithHeaders(headers map[string]string) sdk.Client {
	a.inner.WithHeaders(headers)
	return a
}

func (a *AnthropicMessages) WithHeader(name, value string) sdk.Client {
	a.inner.WithHeader(name, value)
	return a
}

func (a *AnthropicMessages) WithMiddlewareOptions(options *sdk.MiddlewareOptions) sdk.Client {
	a.inner.WithMiddlewareOptions(options)
	return a
}

func (a *AnthropicMessages) ListModels(ctx context.Context, include ...sdk.ListModelsParamsInclude) (*sdk.ListModelsResponse, error) {
	return a.inner.ListModels(ctx, include...)
}

func (a *AnthropicMessages) ListProviderModels(ctx context.Context, provider sdk.Provider, include ...sdk.ListModelsParamsInclude) (*sdk.ListModelsResponse, error) {
	return a.inner.ListProviderModels(ctx, provider, include...)
}

func (a *AnthropicMessages) ListTools(ctx context.Context) (*sdk.ListToolsResponse, error) {
	return a.inner.ListTools(ctx)
}

func (a *AnthropicMessages) HealthCheck(ctx context.Context) error {
	return a.inner.HealthCheck(ctx)
}

func (a *AnthropicMessages) CreateMessage(ctx context.Context, provider sdk.Provider, request sdk.CreateMessagesRequest) (*sdk.MessagesResponse, error) {
	return a.inner.CreateMessage(ctx, provider, request)
}

func (a *AnthropicMessages) CreateMessageStream(ctx context.Context, provider sdk.Provider, request sdk.CreateMessagesRequest) (<-chan sdk.SSEvent, error) {
	return a.inner.CreateMessageStream(ctx, provider, request)
}

func (a *AnthropicMessages) CreateImage(ctx context.Context, provider sdk.Provider, request sdk.CreateImageRequest) (*sdk.ImagesResponse, error) {
	return a.inner.CreateImage(ctx, provider, request)
}

func (a *AnthropicMessages) CreateImageEdit(ctx context.Context, provider sdk.Provider, request sdk.CreateImageEditMultipartBody) (*sdk.ImagesResponse, error) {
	return a.inner.CreateImageEdit(ctx, provider, request)
}

func (a *AnthropicMessages) CreateImageVariation(ctx context.Context, provider sdk.Provider, request sdk.CreateImageVariationMultipartBody) (*sdk.ImagesResponse, error) {
	return a.inner.CreateImageVariation(ctx, provider, request)
}

// GenerateContent routes Anthropic requests through /v1/messages and
// translates the response back into the chat-completions shape.
func (a *AnthropicMessages) GenerateContent(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (*sdk.CreateChatCompletionResponse, error) {
	if provider != sdk.Anthropic {
		return a.inner.GenerateContent(ctx, provider, model, messages)
	}

	req := a.buildMessagesRequest(model, messages)
	resp, err := a.inner.CreateMessage(ctx, provider, req)
	if a.effortRejected(model, err) {
		req.OutputConfig = nil
		resp, err = a.inner.CreateMessage(ctx, provider, req)
	}
	if err != nil {
		return nil, fmt.Errorf("anthropic /v1/messages request failed (the gateway must support the Messages API): %w", err)
	}

	return a.translateResponse(resp), nil
}

// GenerateContentStream routes Anthropic requests through /v1/messages and
// translates the native Anthropic SSE events into chat-completion chunks.
func (a *AnthropicMessages) GenerateContentStream(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (<-chan sdk.SSEvent, error) {
	if provider != sdk.Anthropic {
		return a.inner.GenerateContentStream(ctx, provider, model, messages)
	}

	req := a.buildMessagesRequest(model, messages)
	in, err := a.inner.CreateMessageStream(ctx, provider, req)
	if a.effortRejected(model, err) {
		req.OutputConfig = nil
		in, err = a.inner.CreateMessageStream(ctx, provider, req)
	}
	if err != nil {
		return nil, fmt.Errorf("anthropic /v1/messages request failed (the gateway must support the Messages API): %w", err)
	}

	out := make(chan sdk.SSEvent, 8)
	go a.translateStream(ctx, in, out, model)
	return out, nil
}

// translateStream converts the Anthropic event stream to chat-completion
// chunks. The cache-creation side channel is only written on a cleanly
// finished stream so a cancelled turn can't leak its value into the next
// request's metrics.
func (a *AnthropicMessages) translateStream(ctx context.Context, in <-chan sdk.SSEvent, out chan<- sdk.SSEvent, model string) {
	defer close(out)

	t := &streamTranslator{model: model, toolIndex: map[int]int{}}
	for event := range in {
		for _, translated := range t.translate(event) {
			select {
			case out <- translated:
			case <-ctx.Done():
				for range in {
				}
				return
			}
		}
	}

	a.cacheCreation.Store(t.usage.cacheCreation)
}

// buildMessagesRequest maps the agent's chat-completions payload onto the
// Anthropic Messages API, placing the three cache_control breakpoints:
// system prompt, last tool, and the rolling conversation breakpoint.
func (a *AnthropicMessages) buildMessagesRequest(model string, messages []sdk.Message) sdk.CreateMessagesRequest {
	req := sdk.CreateMessagesRequest{
		Model:     model,
		MaxTokens: a.maxTokens(),
		Messages:  buildMessages(messages),
	}

	if system := systemPrompt(messages); system != "" {
		var union sdk.CreateMessagesRequest_System
		_ = union.FromCreateMessagesRequestSystem1([]sdk.MessagesTextBlock{{
			Type:         sdk.MessagesTextBlockTypeText,
			Text:         system,
			CacheControl: ephemeral(),
		}})
		req.System = &union
	}

	if tools := a.buildTools(); len(tools) > 0 {
		req.Tools = &tools
	}

	if _, rejected := a.noEffort.Load(model); !rejected {
		req.OutputConfig = &sdk.MessagesOutputConfig{Effort: a.effortOption()}
	}

	return req
}

// effortRejected reports whether err is Anthropic's 400 for models that do
// not support output_config.effort (e.g. Haiku 4.5), recording the model so
// subsequent requests omit the parameter up front. Anthropic publishes no
// per-model capability flag for effort, so the rejection itself is the
// detection.
func (a *AnthropicMessages) effortRejected(model string, err error) bool {
	if err == nil || !strings.Contains(err.Error(), "does not support the effort parameter") {
		return false
	}
	a.noEffort.Store(model, true)
	logger.Debug("model rejected output_config.effort; retrying without it", "model", model)
	return true
}

func (a *AnthropicMessages) maxTokens() int {
	if a.opts != nil && a.opts.MaxTokens != nil && *a.opts.MaxTokens > 0 {
		return *a.opts.MaxTokens
	}
	return defaultMaxTokens
}

// effortOption maps the reasoning_effort captured via WithOptions onto the
// Messages API output_config.effort. minimal is an OpenAI-only level -
// Anthropic's scale starts at low. Unset and unknown values fall back to the
// CLI's hardcoded Anthropic default.
func (a *AnthropicMessages) effortOption() *sdk.MessagesOutputConfigEffort {
	effort := sdk.MessagesOutputConfigEffort(config.DefaultAnthropicEffort)
	if a.opts != nil && a.opts.ReasoningEffort != nil && *a.opts.ReasoningEffort != sdk.CreateChatCompletionRequestReasoningEffortMinimal {
		if e := sdk.MessagesOutputConfigEffort(*a.opts.ReasoningEffort); e.Valid() {
			effort = e
		}
	}
	return &effort
}

// buildTools converts the OpenAI tool definitions to Anthropic tools. The
// last tool carries the cache breakpoint - tool order is deterministic
// (sorted by name), so the prefix stays byte-stable across turns.
func (a *AnthropicMessages) buildTools() []sdk.MessagesTool {
	if a.tools == nil || len(*a.tools) == 0 {
		return nil
	}

	tools := make([]sdk.MessagesTool, 0, len(*a.tools))
	for _, t := range *a.tools {
		tool := sdk.MessagesTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: sdk.FunctionParameters{"type": "object"},
		}
		if t.Function.Parameters != nil {
			tool.InputSchema = *t.Function.Parameters
		}
		tools = append(tools, tool)
	}

	tools[len(tools)-1].CacheControl = ephemeral()
	return tools
}

// systemPrompt extracts the leading system message, which the Messages API
// takes as a dedicated top-level field.
func systemPrompt(messages []sdk.Message) string {
	if len(messages) > 0 && messages[0].Role == sdk.System {
		return messageText(messages[0])
	}
	return ""
}

// stagedMessage is the intermediate representation used while grouping and
// merging messages, before sealing the content blocks into their unions.
type stagedMessage struct {
	role     sdk.MessagesMessageRole
	blocks   []sdk.MessagesRequestContentBlock
	volatile []bool
}

// buildMessages converts the conversation (minus the hoisted system prompt)
// into Anthropic messages: tool calls become tool_use blocks, tool results
// become tool_result blocks on user turns, and adjacent same-role messages
// are merged into one legal turn.
func buildMessages(messages []sdk.Message) []sdk.MessagesMessage {
	staged := stageMessages(messages)
	applyRollingCacheControl(staged)

	out := make([]sdk.MessagesMessage, 0, len(staged))
	for _, m := range staged {
		var content sdk.MessagesMessage_Content
		if err := content.FromMessagesMessageContent1(m.blocks); err != nil {
			logger.Error("failed to seal anthropic message content", "error", err)
			continue
		}
		out = append(out, sdk.MessagesMessage{Role: m.role, Content: content})
	}
	return out
}

func stageMessages(messages []sdk.Message) []*stagedMessage {
	var staged []*stagedMessage

	appendBlocks := func(role sdk.MessagesMessageRole, volatile bool, blocks ...sdk.MessagesRequestContentBlock) {
		if len(blocks) == 0 {
			return
		}
		flags := make([]bool, len(blocks))
		for i := range flags {
			flags[i] = volatile
		}
		if n := len(staged); n > 0 && staged[n-1].role == role {
			staged[n-1].blocks = append(staged[n-1].blocks, blocks...)
			staged[n-1].volatile = append(staged[n-1].volatile, flags...)
			return
		}
		staged = append(staged, &stagedMessage{role: role, blocks: blocks, volatile: flags})
	}

	for i, msg := range messages {
		switch msg.Role {
		case sdk.System:
			if i == 0 {
				continue // hoisted into the top-level system field
			}
			if text := messageText(msg); text != "" {
				appendBlocks(sdk.MessagesMessageRoleUser, isVolatile(text), textBlock(text))
			}
		case sdk.User:
			blocks, text := userBlocks(msg)
			appendBlocks(sdk.MessagesMessageRoleUser, isVolatile(text), blocks...)
		case sdk.Assistant:
			blocks, text := assistantBlocks(msg)
			appendBlocks(sdk.MessagesMessageRoleAssistant, isVolatile(text), blocks...)
		case sdk.Tool:
			if msg.ToolCallID == nil {
				continue
			}
			appendBlocks(sdk.MessagesMessageRoleUser, false, toolResultBlock(*msg.ToolCallID, messageText(msg)))
		}
	}

	return staged
}

// assistantBlocks maps an assistant message to text + tool_use blocks.
// Reasoning is intentionally dropped: replaying unsigned thinking is
// rejected by Anthropic, and extended thinking is out of scope here.
func assistantBlocks(msg sdk.Message) ([]sdk.MessagesRequestContentBlock, string) {
	var blocks []sdk.MessagesRequestContentBlock

	text := messageText(msg)
	if text != "" {
		blocks = append(blocks, textBlock(text))
	}

	if msg.ToolCalls != nil {
		for _, tc := range *msg.ToolCalls {
			blocks = append(blocks, toolUseBlock(tc))
		}
	}

	return blocks, text
}

// isVolatile reports whether text is injected per-turn CLI content the
// rolling cache breakpoint must skip.
func isVolatile(text string) bool {
	return strings.Contains(text, "<system-reminder>")
}

// applyRollingCacheControl sets the rolling cache breakpoint on the newest
// non-volatile block, so the growing conversation prefix caches
// incrementally across turns while the <system-reminder> tail (merged into
// the last user message) stays outside the cached prefix.
func applyRollingCacheControl(staged []*stagedMessage) {
	for i := len(staged) - 1; i >= 0; i-- {
		m := staged[i]
		for j := len(m.blocks) - 1; j >= 0; j-- {
			if m.volatile[j] {
				continue
			}
			m.blocks[j] = blockWithCacheControl(m.blocks[j])
			return
		}
	}
}

// blockWithCacheControl returns the block with cache_control set, going
// through JSON because the union's raw payload is not directly editable.
func blockWithCacheControl(block sdk.MessagesRequestContentBlock) sdk.MessagesRequestContentBlock {
	raw, err := block.MarshalJSON()
	if err != nil {
		return block
	}

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return block
	}
	fields["cache_control"] = map[string]any{"type": string(sdk.Ephemeral)}

	patched, err := json.Marshal(fields)
	if err != nil {
		return block
	}

	var out sdk.MessagesRequestContentBlock
	if err := out.UnmarshalJSON(patched); err != nil {
		return block
	}
	return out
}

func ephemeral() *sdk.CacheControl {
	return &sdk.CacheControl{Type: sdk.Ephemeral}
}

// messageText extracts the text of a message: plain string content, or the
// concatenated text parts of multimodal content (non-text parts have no
// Messages-path mapping and are dropped).
func messageText(msg sdk.Message) string {
	if text, err := msg.Content.AsMessageContent0(); err == nil {
		return text
	}

	parts, err := msg.Content.AsMessageContent1()
	if err != nil {
		return ""
	}

	var b strings.Builder
	for _, part := range parts {
		if tp, err := part.AsTextContentPart(); err == nil && tp.Text != "" {
			b.WriteString(tp.Text)
		}
	}
	return b.String()
}

// userBlocks maps a user message to text + image blocks. All Anthropic
// models accept image input, so image parts translate unconditionally; the
// chat/agent paths attach them as data URLs, remote images as http(s) URLs.
func userBlocks(msg sdk.Message) ([]sdk.MessagesRequestContentBlock, string) {
	var blocks []sdk.MessagesRequestContentBlock
	text := messageText(msg)
	if text != "" {
		blocks = append(blocks, textBlock(text))
	}

	parts, err := msg.Content.AsMessageContent1()
	if err != nil {
		return blocks, text
	}
	for _, part := range parts {
		ip, err := part.AsImageContentPart()
		if err != nil || ip.ImageURL.URL == "" {
			continue
		}
		if block, ok := imageBlock(ip.ImageURL.URL); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks, text
}

// imageBlock converts a chat-completions image URL — a "data:<media>;base64,"
// payload or a plain http(s) URL — into an Anthropic image content block.
func imageBlock(url string) (sdk.MessagesRequestContentBlock, bool) {
	var block sdk.MessagesRequestContentBlock
	if rest, found := strings.CutPrefix(url, "data:"); found {
		mediaType, data, ok := strings.Cut(rest, ";base64,")
		if !ok || data == "" {
			return block, false
		}
		_ = block.FromMessagesImageBlock(sdk.MessagesImageBlock{
			Type: sdk.MessagesImageBlockTypeImage,
			Source: sdk.MessagesImageSource{
				Type:      sdk.MessagesImageSourceTypeBase64,
				MediaType: &mediaType,
				Data:      &data,
			},
		})
		return block, true
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		_ = block.FromMessagesImageBlock(sdk.MessagesImageBlock{
			Type: sdk.MessagesImageBlockTypeImage,
			Source: sdk.MessagesImageSource{
				Type: sdk.MessagesImageSourceTypeURL,
				URL:  &url,
			},
		})
		return block, true
	}
	return block, false
}

func textBlock(text string) sdk.MessagesRequestContentBlock {
	var block sdk.MessagesRequestContentBlock
	_ = block.FromMessagesTextBlock(sdk.MessagesTextBlock{
		Type: sdk.MessagesTextBlockTypeText,
		Text: text,
	})
	return block
}

func toolUseBlock(tc sdk.ChatCompletionMessageToolCall) sdk.MessagesRequestContentBlock {
	input := map[string]any{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil || input == nil {
		input = map[string]any{}
	}

	var block sdk.MessagesRequestContentBlock
	_ = block.FromMessagesToolUseBlock(sdk.MessagesToolUseBlock{
		ID:    tc.ID,
		Name:  tc.Function.Name,
		Input: input,
		Type:  sdk.MessagesToolUseBlockTypeToolUse,
	})
	return block
}

func toolResultBlock(toolUseID, text string) sdk.MessagesRequestContentBlock {
	var content sdk.MessagesToolResultBlock_Content
	_ = content.FromMessagesToolResultBlockContent0(text)

	var block sdk.MessagesRequestContentBlock
	_ = block.FromMessagesToolResultBlock(sdk.MessagesToolResultBlock{
		ToolUseID: toolUseID,
		Content:   &content,
		Type:      sdk.ToolResult,
	})
	return block
}

// anthropicUsage accumulates Anthropic usage across message_start and
// message_delta events.
type anthropicUsage struct {
	input         int64
	output        int64
	cacheRead     int64
	cacheCreation int64
}

func (u *anthropicUsage) absorb(usage sdk.MessagesUsage) {
	if usage.InputTokens > 0 {
		u.input = usage.InputTokens
	}
	if usage.OutputTokens > 0 {
		u.output = usage.OutputTokens
	}
	if usage.CacheReadInputTokens != nil && *usage.CacheReadInputTokens > 0 {
		u.cacheRead = *usage.CacheReadInputTokens
	}
	if usage.CacheCreationInputTokens != nil && *usage.CacheCreationInputTokens > 0 {
		u.cacheCreation = *usage.CacheCreationInputTokens
	}
}

// toCompletionUsage synthesizes OpenAI-shaped usage: prompt_tokens covers
// the full input (Anthropic's input_tokens excludes cached tokens), and
// cache reads surface as prompt_tokens_details.cached_tokens so the
// existing display and cost paths work unchanged.
func (u anthropicUsage) toCompletionUsage() sdk.CompletionUsage {
	prompt := u.input + u.cacheRead + u.cacheCreation
	usage := sdk.CompletionUsage{
		PromptTokens:     prompt,
		CompletionTokens: u.output,
		TotalTokens:      prompt + u.output,
	}

	if u.cacheRead > 0 {
		cached := u.cacheRead
		usage.PromptTokensDetails = &struct {
			AudioTokens  *int64 `json:"audio_tokens,omitempty"`
			CachedTokens *int64 `json:"cached_tokens,omitempty"`
		}{CachedTokens: &cached}
	}

	return usage
}

// streamTranslator converts one Anthropic SSE stream into chat-completion
// chunks, tracking the block-index-to-tool-index mapping (Anthropic indexes
// over all content blocks, the tool-call accumulator over tool calls only).
type streamTranslator struct {
	model      string
	toolIndex  map[int]int
	usage      anthropicUsage
	stopReason string
}

func (t *streamTranslator) translate(event sdk.SSEvent) []sdk.SSEvent {
	if event.Event == nil || event.Data == nil {
		return []sdk.SSEvent{event} // transport errors pass through untouched
	}

	var ev sdk.MessagesStreamEvent
	if err := json.Unmarshal(*event.Data, &ev); err != nil {
		logger.Error("failed to unmarshal anthropic stream event",
			"error", err,
			"raw_data", string(*event.Data))
		return nil
	}

	switch ev.Type {
	case sdk.MessagesStreamEventTypeMessageStart:
		if ev.Message != nil {
			t.usage.absorb(ev.Message.Usage)
		}
		return nil
	case sdk.MessagesStreamEventTypeContentBlockStart:
		return t.translateBlockStart(ev)
	case sdk.MessagesStreamEventTypeContentBlockDelta:
		return t.translateDelta(ev)
	case sdk.MessagesStreamEventTypeMessageDelta:
		if ev.Usage != nil {
			t.usage.absorb(*ev.Usage)
		}
		if ev.Delta != nil && ev.Delta.StopReason != nil {
			t.stopReason = *ev.Delta.StopReason
		}
		return nil
	case sdk.MessagesStreamEventTypeMessageStop:
		return t.finishEvents()
	case sdk.MessagesStreamEventTypeError:
		return []sdk.SSEvent{{Data: event.Data}}
	default:
		return nil
	}
}

func (t *streamTranslator) translateBlockStart(ev sdk.MessagesStreamEvent) []sdk.SSEvent {
	if ev.ContentBlock == nil || ev.Index == nil {
		return nil
	}

	tu, ok := asToolUse(*ev.ContentBlock)
	if !ok {
		return nil
	}

	idx := len(t.toolIndex)
	t.toolIndex[*ev.Index] = idx

	id := tu.ID
	toolType := string(sdk.Function)
	return t.chunk(sdk.ChatCompletionStreamResponseDelta{
		ToolCalls: &[]sdk.ChatCompletionMessageToolCallChunk{{
			Index:    idx,
			ID:       &id,
			Type:     &toolType,
			Function: &sdk.ChatCompletionMessageToolCallFunction{Name: tu.Name},
		}},
	})
}

func (t *streamTranslator) translateDelta(ev sdk.MessagesStreamEvent) []sdk.SSEvent {
	d := ev.Delta
	if d == nil || d.Type == nil {
		return nil
	}

	switch *d.Type {
	case "text_delta":
		if d.Text == nil || *d.Text == "" {
			return nil
		}
		return t.chunk(sdk.ChatCompletionStreamResponseDelta{Content: *d.Text})
	case "thinking_delta":
		if d.Thinking == nil || *d.Thinking == "" {
			return nil
		}
		return t.chunk(sdk.ChatCompletionStreamResponseDelta{ReasoningContent: d.Thinking})
	case "input_json_delta":
		if d.PartialJSON == nil || *d.PartialJSON == "" || ev.Index == nil {
			return nil
		}
		idx, ok := t.toolIndex[*ev.Index]
		if !ok {
			return nil
		}
		return t.chunk(sdk.ChatCompletionStreamResponseDelta{
			ToolCalls: &[]sdk.ChatCompletionMessageToolCallChunk{{
				Index:    idx,
				Function: &sdk.ChatCompletionMessageToolCallFunction{Arguments: *d.PartialJSON},
			}},
		})
	default:
		return nil
	}
}

// finishEvents emits the finish-reason chunk followed by a usage-only chunk,
// mirroring the stream_options.include_usage shape of the OpenAI path.
func (t *streamTranslator) finishEvents() []sdk.SSEvent {
	finish := chatChunk(sdk.CreateChatCompletionStreamResponse{
		ID:     "anthropic-messages",
		Object: "chat.completion.chunk",
		Model:  t.model,
		Choices: []sdk.ChatCompletionStreamChoice{{
			FinishReason: mapStopReason(t.stopReason),
		}},
	})

	usage := t.usage.toCompletionUsage()
	usageChunk := chatChunk(sdk.CreateChatCompletionStreamResponse{
		ID:      "anthropic-messages",
		Object:  "chat.completion.chunk",
		Model:   t.model,
		Choices: []sdk.ChatCompletionStreamChoice{},
		Usage:   &usage,
	})

	return []sdk.SSEvent{finish, usageChunk}
}

func (t *streamTranslator) chunk(delta sdk.ChatCompletionStreamResponseDelta) []sdk.SSEvent {
	return []sdk.SSEvent{chatChunk(sdk.CreateChatCompletionStreamResponse{
		ID:      "anthropic-messages",
		Object:  "chat.completion.chunk",
		Model:   t.model,
		Choices: []sdk.ChatCompletionStreamChoice{{Delta: delta}},
	})}
}

func chatChunk(resp sdk.CreateChatCompletionStreamResponse) sdk.SSEvent {
	data, err := json.Marshal(resp)
	if err != nil {
		logger.Error("failed to marshal translated stream chunk", "error", err)
		data = []byte("{}")
	}
	event := sdk.ContentDelta
	return sdk.SSEvent{Event: &event, Data: &data}
}

func mapStopReason(reason string) sdk.FinishReason {
	switch reason {
	case string(sdk.MessagesResponseStopReasonToolUse):
		return sdk.ToolCalls
	case string(sdk.MessagesResponseStopReasonMaxTokens):
		return sdk.Length
	default:
		return sdk.Stop
	}
}

// translateResponse converts a non-streaming Messages response into the
// chat-completions shape used by the sync Run path.
func (a *AnthropicMessages) translateResponse(resp *sdk.MessagesResponse) *sdk.CreateChatCompletionResponse {
	var text, thinking strings.Builder
	var toolCalls []sdk.ChatCompletionMessageToolCall

	for _, block := range resp.Content {
		switch blockType(block) {
		case "text":
			if tb, err := block.AsMessagesTextBlock(); err == nil {
				text.WriteString(tb.Text)
			}
		case "thinking":
			if th, err := block.AsMessagesThinkingBlock(); err == nil {
				thinking.WriteString(th.Thinking)
			}
		case "tool_use":
			if tu, err := block.AsMessagesToolUseBlock(); err == nil {
				toolCalls = append(toolCalls, toToolCall(tu))
			}
		}
	}

	message := sdk.Message{
		Role:    sdk.Assistant,
		Content: sdk.NewMessageContent(text.String()),
	}
	if reasoning := thinking.String(); reasoning != "" {
		r := reasoning
		message.Reasoning = &r
		message.ReasoningContent = &r
	}
	if len(toolCalls) > 0 {
		message.ToolCalls = &toolCalls
	}

	var u anthropicUsage
	u.absorb(resp.Usage)
	a.cacheCreation.Store(u.cacheCreation)
	usage := u.toCompletionUsage()

	return &sdk.CreateChatCompletionResponse{
		ID:     resp.ID,
		Object: "chat.completion",
		Model:  resp.Model,
		Choices: []sdk.ChatCompletionChoice{{
			Index:        0,
			Message:      message,
			FinishReason: mapStopReason(string(resp.StopReason)),
		}},
		Usage: &usage,
	}
}

func toToolCall(tu sdk.MessagesToolUseBlock) sdk.ChatCompletionMessageToolCall {
	args, err := json.Marshal(tu.Input)
	if err != nil {
		args = []byte("{}")
	}
	return sdk.ChatCompletionMessageToolCall{
		ID:   tu.ID,
		Type: sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      tu.Name,
			Arguments: string(args),
		},
	}
}

// blockType reads the discriminating type field of a response content block;
// the generated As* accessors don't validate it themselves.
func blockType(block sdk.MessagesResponseContentBlock) string {
	raw, err := block.MarshalJSON()
	if err != nil {
		return ""
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.Type
}

// asToolUse returns the tool_use payload of a content block, verifying the
// discriminator first.
func asToolUse(block sdk.MessagesResponseContentBlock) (sdk.MessagesToolUseBlock, bool) {
	if blockType(block) != string(sdk.MessagesToolUseBlockTypeToolUse) {
		return sdk.MessagesToolUseBlock{}, false
	}
	tu, err := block.AsMessagesToolUseBlock()
	if err != nil {
		return sdk.MessagesToolUseBlock{}, false
	}
	return tu, true
}
