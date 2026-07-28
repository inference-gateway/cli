package adapters

import (
	"context"
	"encoding/json"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	mocksdk "github.com/inference-gateway/cli/tests/mocks/sdk"
)

// requestShape mirrors the JSON the gateway receives, for mapping assertions.
type requestShape struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	System    []struct {
		Text         string         `json:"text"`
		CacheControl map[string]any `json:"cache_control"`
	} `json:"system"`
	Tools []struct {
		Name         string         `json:"name"`
		InputSchema  map[string]any `json:"input_schema"`
		CacheControl map[string]any `json:"cache_control"`
	} `json:"tools"`
	Messages []struct {
		Role    string           `json:"role"`
		Content []map[string]any `json:"content"`
	} `json:"messages"`
	OutputConfig *struct {
		Effort string `json:"effort"`
	} `json:"output_config"`
}

func decodeRequest(t *testing.T, req sdk.CreateMessagesRequest) requestShape {
	t.Helper()
	data, err := json.Marshal(req)
	require.NoError(t, err)
	var shape requestShape
	require.NoError(t, json.Unmarshal(data, &shape))
	return shape
}

func textMessage(role sdk.MessageRole, text string) sdk.Message {
	return sdk.Message{Role: role, Content: sdk.NewMessageContent(text)}
}

func conversationFixture() []sdk.Message {
	args := `{"path":"a"}`
	toolCalls := []sdk.ChatCompletionMessageToolCall{
		{ID: "call_1", Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read", Arguments: args}},
		{ID: "call_2", Type: sdk.Function, Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: "not-json"}},
	}
	reasoning := "internal thoughts"
	assistant := sdk.Message{
		Role:             sdk.Assistant,
		Content:          sdk.NewMessageContent("I'll check"),
		Reasoning:        &reasoning,
		ReasoningContent: &reasoning,
		ToolCalls:        &toolCalls,
	}
	call1, call2 := "call_1", "call_2"
	return []sdk.Message{
		textMessage(sdk.System, "You are helpful"),
		textMessage(sdk.User, "Turn 1"),
		assistant,
		{Role: sdk.Tool, ToolCallID: &call1, Content: sdk.NewMessageContent("data 1")},
		{Role: sdk.Tool, ToolCallID: &call2, Content: sdk.NewMessageContent("data 2")},
		textMessage(sdk.User, "Turn 2"),
		textMessage(sdk.User, "<system-reminder>volatile tail</system-reminder>"),
	}
}

func TestBuildMessagesRequest(t *testing.T) {
	toolDesc := "read a file"
	params := sdk.FunctionParameters{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}
	tools := []sdk.ChatCompletionTool{
		{Type: sdk.Function, Function: sdk.FunctionObject{Name: "Bash"}},
		{Type: sdk.Function, Function: sdk.FunctionObject{Name: "Read", Description: &toolDesc, Parameters: &params}},
	}
	maxTokens := 2048

	adapter := NewAnthropicMessages(&mocksdk.FakeClient{})
	adapter.WithOptions(&sdk.CreateChatCompletionRequest{MaxTokens: &maxTokens}).WithTools(&tools)

	shape := decodeRequest(t, adapter.buildMessagesRequest("claude-sonnet-4-5", conversationFixture()))

	assert.Equal(t, "claude-sonnet-4-5", shape.Model)
	assert.Equal(t, 2048, shape.MaxTokens)

	require.Len(t, shape.System, 1)
	assert.Equal(t, "You are helpful", shape.System[0].Text)
	assert.Equal(t, map[string]any{"type": "ephemeral"}, shape.System[0].CacheControl)

	require.Len(t, shape.Tools, 2)
	assert.Equal(t, "Bash", shape.Tools[0].Name)
	assert.Nil(t, shape.Tools[0].CacheControl)
	assert.Equal(t, map[string]any{"type": "object"}, shape.Tools[0].InputSchema)
	assert.Equal(t, "Read", shape.Tools[1].Name)
	assert.NotNil(t, shape.Tools[1].CacheControl, "last tool carries the cache breakpoint")
	assert.Contains(t, shape.Tools[1].InputSchema, "properties")

	require.Len(t, shape.Messages, 3)

	assert.Equal(t, "user", shape.Messages[0].Role)
	require.Len(t, shape.Messages[0].Content, 1)
	assert.Equal(t, "Turn 1", shape.Messages[0].Content[0]["text"])

	assistant := shape.Messages[1]
	assert.Equal(t, "assistant", assistant.Role)
	require.Len(t, assistant.Content, 3)
	assert.Equal(t, "text", assistant.Content[0]["type"])
	assert.Equal(t, "I'll check", assistant.Content[0]["text"])
	assert.Equal(t, "tool_use", assistant.Content[1]["type"])
	assert.Equal(t, "call_1", assistant.Content[1]["id"])
	assert.Equal(t, map[string]any{"path": "a"}, assistant.Content[1]["input"])
	assert.Equal(t, "tool_use", assistant.Content[2]["type"])
	assert.Equal(t, map[string]any{}, assistant.Content[2]["input"], "unparseable arguments fall back to an empty object")
	assert.Nil(t, assistant.Content[2]["cache_control"])

	user := shape.Messages[2]
	assert.Equal(t, "user", user.Role)
	require.Len(t, user.Content, 4, "tool results + user text + volatile tail merge into one user turn")
	assert.Equal(t, "tool_result", user.Content[0]["type"])
	assert.Equal(t, "call_1", user.Content[0]["tool_use_id"])
	assert.Equal(t, "data 1", user.Content[0]["content"])
	assert.Equal(t, "tool_result", user.Content[1]["type"])
	assert.Equal(t, "Turn 2", user.Content[2]["text"])
	assert.NotNil(t, user.Content[2]["cache_control"], "rolling breakpoint lands on the newest non-volatile block")
	assert.Nil(t, user.Content[3]["cache_control"], "the volatile tail must not eat a cache slot")
}

func TestBuildMessagesRequestDefaults(t *testing.T) {
	adapter := NewAnthropicMessages(&mocksdk.FakeClient{})

	shape := decodeRequest(t, adapter.buildMessagesRequest("claude-sonnet-4-5", []sdk.Message{
		textMessage(sdk.User, "hi"),
	}))

	assert.Equal(t, defaultMaxTokens, shape.MaxTokens)
	assert.Empty(t, shape.System)
	assert.Empty(t, shape.Tools)
	assert.Nil(t, shape.OutputConfig, "no effort configured means no output_config on the wire")
	require.Len(t, shape.Messages, 1)
	assert.NotNil(t, shape.Messages[0].Content[0]["cache_control"], "sole user message takes the rolling breakpoint")
}

func TestBuildMessagesRequestEffort(t *testing.T) {
	tests := []struct {
		name   string
		effort string
		want   string
	}{
		{"minimal maps to low", "minimal", "low"},
		{"low passes through", "low", "low"},
		{"high passes through", "high", "high"},
		{"xhigh passes through", "xhigh", "xhigh"},
		{"max passes through", "max", "max"},
		{"unknown value is dropped", "bogus", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effort := sdk.CreateChatCompletionRequestReasoningEffort(tt.effort)
			adapter := NewAnthropicMessages(&mocksdk.FakeClient{})
			adapter.WithOptions(&sdk.CreateChatCompletionRequest{ReasoningEffort: &effort})

			shape := decodeRequest(t, adapter.buildMessagesRequest("claude-sonnet-4-5", []sdk.Message{
				textMessage(sdk.User, "hi"),
			}))

			if tt.want == "" {
				assert.Nil(t, shape.OutputConfig)
				return
			}
			require.NotNil(t, shape.OutputConfig)
			assert.Equal(t, tt.want, shape.OutputConfig.Effort)
		})
	}
}

func anthropicEvent(payload string) sdk.SSEvent {
	event := sdk.ContentDelta
	data := []byte(payload)
	return sdk.SSEvent{Event: &event, Data: &data}
}

func collectChunks(t *testing.T, out <-chan sdk.SSEvent) []sdk.CreateChatCompletionStreamResponse {
	t.Helper()
	var chunks []sdk.CreateChatCompletionStreamResponse
	for event := range out {
		require.NotNil(t, event.Event)
		require.NotNil(t, event.Data)
		var chunk sdk.CreateChatCompletionStreamResponse
		require.NoError(t, json.Unmarshal(*event.Data, &chunk))
		chunks = append(chunks, chunk)
	}
	return chunks
}

func TestGenerateContentStreamTranslation(t *testing.T) {
	in := make(chan sdk.SSEvent, 20)
	fake := &mocksdk.FakeClient{}
	fake.CreateMessageStreamReturns(in, nil)
	adapter := NewAnthropicMessages(fake)

	out, err := adapter.GenerateContentStream(context.Background(), sdk.Anthropic, "claude-sonnet-4-5", []sdk.Message{textMessage(sdk.User, "hi")})
	require.NoError(t, err)
	assert.Equal(t, 1, fake.CreateMessageStreamCallCount())
	assert.Equal(t, 0, fake.GenerateContentStreamCallCount())

	for _, payload := range []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5","role":"assistant","type":"message","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":120,"cache_creation_input_tokens":100}}}`,
		`{"type":"ping"}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"a\"}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_2","name":"Bash","input":{}}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":25}}`,
		`{"type":"message_stop"}`,
	} {
		in <- anthropicEvent(payload)
	}
	close(in)

	chunks := collectChunks(t, out)
	require.Len(t, chunks, 9)

	require.NotNil(t, chunks[0].Choices[0].Delta.ReasoningContent)
	assert.Equal(t, "hmm", *chunks[0].Choices[0].Delta.ReasoningContent)

	assert.Equal(t, "Hello", chunks[1].Choices[0].Delta.Content)

	firstTool := (*chunks[2].Choices[0].Delta.ToolCalls)[0]
	assert.Equal(t, 0, firstTool.Index)
	assert.Equal(t, "toolu_1", *firstTool.ID)
	assert.Equal(t, "Read", firstTool.Function.Name)

	assert.Equal(t, `{"path":`, (*chunks[3].Choices[0].Delta.ToolCalls)[0].Function.Arguments)
	assert.Equal(t, `"a"}`, (*chunks[4].Choices[0].Delta.ToolCalls)[0].Function.Arguments)

	secondTool := (*chunks[5].Choices[0].Delta.ToolCalls)[0]
	assert.Equal(t, 1, secondTool.Index, "anthropic block index 3 remaps to tool index 1")
	assert.Equal(t, "toolu_2", *secondTool.ID)

	assert.Equal(t, 1, (*chunks[6].Choices[0].Delta.ToolCalls)[0].Index)

	finish := chunks[7]
	require.Len(t, finish.Choices, 1)
	assert.Equal(t, sdk.ToolCalls, finish.Choices[0].FinishReason)

	usageChunk := chunks[8]
	assert.Empty(t, usageChunk.Choices)
	require.NotNil(t, usageChunk.Usage)
	assert.Equal(t, int64(230), usageChunk.Usage.PromptTokens, "input + cache read + cache creation")
	assert.Equal(t, int64(25), usageChunk.Usage.CompletionTokens)
	assert.Equal(t, int64(255), usageChunk.Usage.TotalTokens)
	require.NotNil(t, usageChunk.Usage.PromptTokensDetails)
	assert.Equal(t, int64(120), *usageChunk.Usage.PromptTokensDetails.CachedTokens)

	assert.Equal(t, 100, adapter.TakeCacheCreationTokens())
	assert.Equal(t, 0, adapter.TakeCacheCreationTokens(), "side channel resets after read")
}

func TestGenerateContentStreamPassthrough(t *testing.T) {
	fake := &mocksdk.FakeClient{}
	inner := make(chan sdk.SSEvent)
	close(inner)
	fake.GenerateContentStreamReturns(inner, nil)
	adapter := NewAnthropicMessages(fake)

	messages := []sdk.Message{textMessage(sdk.User, "hi")}
	_, err := adapter.GenerateContentStream(context.Background(), sdk.Openai, "gpt-4o", messages)
	require.NoError(t, err)

	assert.Equal(t, 1, fake.GenerateContentStreamCallCount())
	assert.Equal(t, 0, fake.CreateMessageStreamCallCount())
	_, provider, model, gotMessages := fake.GenerateContentStreamArgsForCall(0)
	assert.Equal(t, sdk.Openai, provider)
	assert.Equal(t, "gpt-4o", model)
	assert.Equal(t, messages, gotMessages)
}

func TestBuilderChainKeepsAdapterOutermost(t *testing.T) {
	fake := &mocksdk.FakeClient{}
	adapter := NewAnthropicMessages(fake)
	maxTokens := 512
	tools := []sdk.ChatCompletionTool{{Type: sdk.Function, Function: sdk.FunctionObject{Name: "Read"}}}

	chained := adapter.
		WithOptions(&sdk.CreateChatCompletionRequest{MaxTokens: &maxTokens}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{SkipMCP: true}).
		WithTools(&tools).
		WithAuthToken("token").
		WithHeader("X-Test", "1").
		WithHeaders(map[string]string{"X-Other": "2"})

	require.Same(t, adapter, chained, "builder chain must never leak the inner client")
	assert.Equal(t, 1, fake.WithOptionsCallCount())
	assert.Equal(t, 1, fake.WithMiddlewareOptionsCallCount())
	assert.Equal(t, 1, fake.WithToolsCallCount())

	in := make(chan sdk.SSEvent)
	close(in)
	fake.CreateMessageStreamReturns(in, nil)
	_, err := chained.GenerateContentStream(context.Background(), sdk.Anthropic, "claude-sonnet-4-5", []sdk.Message{textMessage(sdk.User, "hi")})
	require.NoError(t, err)

	_, _, request := fake.CreateMessageStreamArgsForCall(0)
	assert.Equal(t, 512, request.MaxTokens, "options captured through the chain")
	require.NotNil(t, request.Tools)
	assert.Len(t, *request.Tools, 1)
}

func responseBlock(t *testing.T, payload string) sdk.MessagesResponseContentBlock {
	t.Helper()
	var block sdk.MessagesResponseContentBlock
	require.NoError(t, block.UnmarshalJSON([]byte(payload)))
	return block
}

func TestGenerateContentSyncTranslation(t *testing.T) {
	fake := &mocksdk.FakeClient{}
	cacheRead, cacheCreation := int64(40), int64(60)
	fake.CreateMessageReturns(&sdk.MessagesResponse{
		ID:    "msg_1",
		Model: "claude-sonnet-4-5",
		Role:  sdk.MessagesResponseRoleAssistant,
		Content: []sdk.MessagesResponseContentBlock{
			responseBlock(t, `{"type":"thinking","thinking":"pondering","signature":"sig"}`),
			responseBlock(t, `{"type":"text","text":"All done"}`),
			responseBlock(t, `{"type":"tool_use","id":"toolu_1","name":"Read","input":{"path":"a"}}`),
		},
		StopReason: sdk.MessagesResponseStopReasonToolUse,
		Usage: sdk.MessagesUsage{
			InputTokens:              10,
			OutputTokens:             5,
			CacheReadInputTokens:     &cacheRead,
			CacheCreationInputTokens: &cacheCreation,
		},
	}, nil)
	adapter := NewAnthropicMessages(fake)

	resp, err := adapter.GenerateContent(context.Background(), sdk.Anthropic, "claude-sonnet-4-5", []sdk.Message{textMessage(sdk.User, "hi")})
	require.NoError(t, err)
	assert.Equal(t, 0, fake.GenerateContentCallCount())

	require.Len(t, resp.Choices, 1)
	choice := resp.Choices[0]
	content, err := choice.Message.Content.AsMessageContent0()
	require.NoError(t, err)
	assert.Equal(t, "All done", content)
	require.NotNil(t, choice.Message.ReasoningContent)
	assert.Equal(t, "pondering", *choice.Message.ReasoningContent)
	require.NotNil(t, choice.Message.ToolCalls)
	toolCall := (*choice.Message.ToolCalls)[0]
	assert.Equal(t, "toolu_1", toolCall.ID)
	assert.Equal(t, "Read", toolCall.Function.Name)
	assert.JSONEq(t, `{"path":"a"}`, toolCall.Function.Arguments)
	assert.Equal(t, sdk.ToolCalls, choice.FinishReason)

	require.NotNil(t, resp.Usage)
	assert.Equal(t, int64(110), resp.Usage.PromptTokens)
	require.NotNil(t, resp.Usage.PromptTokensDetails)
	assert.Equal(t, int64(40), *resp.Usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 60, adapter.TakeCacheCreationTokens())
}

func TestGenerateContentSyncPassthrough(t *testing.T) {
	fake := &mocksdk.FakeClient{}
	fake.GenerateContentReturns(&sdk.CreateChatCompletionResponse{}, nil)
	adapter := NewAnthropicMessages(fake)

	_, err := adapter.GenerateContent(context.Background(), sdk.Openai, "gpt-4o", []sdk.Message{textMessage(sdk.User, "hi")})
	require.NoError(t, err)
	assert.Equal(t, 1, fake.GenerateContentCallCount())
	assert.Equal(t, 0, fake.CreateMessageCallCount())
}
