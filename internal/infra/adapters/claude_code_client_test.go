package adapters

import (
	"encoding/json"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

func TestClaudeUsageToCompletionUsage(t *testing.T) {
	u := &ClaudeUsage{
		InputTokens:              100,
		OutputTokens:             20,
		CacheCreationInputTokens: 30,
		CacheReadInputTokens:     50,
	}

	got := u.toCompletionUsage()

	if got.PromptTokens != 180 {
		t.Errorf("PromptTokens = %d, want 180", got.PromptTokens)
	}
	if got.CompletionTokens != 20 {
		t.Errorf("CompletionTokens = %d, want 20", got.CompletionTokens)
	}
	if got.TotalTokens != 200 {
		t.Errorf("TotalTokens = %d, want 200", got.TotalTokens)
	}
}

// chunkShape mirrors the JSON the adapter emits for a usage chunk.
type chunkShape struct {
	Choices []struct {
		Delta map[string]any `json:"delta"`
		Index int            `json:"index"`
	} `json:"choices"`
	Usage *sdk.CompletionUsage `json:"usage"`
}

func TestTransformMessageResultEmitsUsageChunk(t *testing.T) {
	c := &ClaudeCodeClient{}

	events := c.transformMessage(ClaudeCodeMessage{
		Type:  "result",
		Usage: &ClaudeUsage{InputTokens: 100, OutputTokens: 20, CacheCreationInputTokens: 30, CacheReadInputTokens: 50},
	})

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (usage chunk + message_stop)", len(events))
	}

	if events[0].Data == nil {
		t.Fatal("usage chunk has nil data")
	}
	var chunk chunkShape
	if err := json.Unmarshal(*events[0].Data, &chunk); err != nil {
		t.Fatalf("unmarshal usage chunk: %v", err)
	}
	if chunk.Usage == nil {
		t.Fatal("usage chunk is missing the usage object")
	}
	if chunk.Usage.PromptTokens != 180 || chunk.Usage.CompletionTokens != 20 || chunk.Usage.TotalTokens != 200 {
		t.Errorf("usage = %+v, want prompt=180 completion=20 total=200", chunk.Usage)
	}
	if len(chunk.Choices) != 1 || len(chunk.Choices[0].Delta) != 0 {
		t.Errorf("want exactly one empty-delta choice, got %+v", chunk.Choices)
	}

	if events[1].Event == nil || string(*events[1].Event) != "message_stop" {
		t.Errorf("second event = %v, want message_stop", events[1].Event)
	}
	if events[1].Data == nil || string(*events[1].Data) != "done" {
		t.Errorf("second event data = %v, want done", events[1].Data)
	}
}

func TestTransformMessageResultNilUsageEmitsZeroUsage(t *testing.T) {
	c := &ClaudeCodeClient{}

	events := c.transformMessage(ClaudeCodeMessage{Type: "result"})
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	var chunk chunkShape
	if err := json.Unmarshal(*events[0].Data, &chunk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if chunk.Usage == nil {
		t.Fatal("want a zero usage object, got nil")
	}
	if chunk.Usage.PromptTokens != 0 || chunk.Usage.CompletionTokens != 0 || chunk.Usage.TotalTokens != 0 {
		t.Errorf("want zero usage, got %+v", chunk.Usage)
	}
}

func TestProcessEventsCapturesUsage(t *testing.T) {
	c := &ClaudeCodeClient{}

	ch := make(chan sdk.SSEvent, 8)
	for _, ev := range c.transformMessage(ClaudeCodeMessage{
		Type:    "assistant",
		Message: json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"hello"}]}`),
	}) {
		ch <- ev
	}
	for _, ev := range c.transformMessage(ClaudeCodeMessage{
		Type:  "result",
		Usage: &ClaudeUsage{InputTokens: 10, OutputTokens: 5},
	}) {
		ch <- ev
	}
	close(ch)

	content, _, usage, err := c.processEvents(ch)
	if err != nil {
		t.Fatalf("processEvents: %v", err)
	}
	if content != "hello" {
		t.Errorf("content = %q, want hello (empty-delta usage chunk must not corrupt content)", content)
	}
	if usage == nil {
		t.Fatal("usage is nil")
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 5 || usage.TotalTokens != 15 {
		t.Errorf("usage = %+v, want prompt=10 completion=5 total=15", usage)
	}
}

func TestBuildArgsStripsProviderPrefix(t *testing.T) {
	c := &ClaudeCodeClient{config: &config.ClaudeCodeConfig{MaxTurns: 10}}

	if got := modelArg(c.buildArgs("anthropic/claude-sonnet-4-5-20250929")); got != "claude-sonnet-4-5-20250929" {
		t.Errorf("--model = %q, want bare claude-sonnet-4-5-20250929", got)
	}
	if got := modelArg(c.buildArgs("claude-opus-4-5")); got != "claude-opus-4-5" {
		t.Errorf("--model = %q, want claude-opus-4-5 (bare passes through)", got)
	}
}

func modelArg(args []string) string {
	for i, a := range args {
		if a == "--model" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// hasFlag checks if a given flag appears in the argument list.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func TestBuildArgsIncludesHookEvents(t *testing.T) {
	c := &ClaudeCodeClient{config: &config.ClaudeCodeConfig{MaxTurns: 10}}

	args := c.buildArgs("anthropic/claude-sonnet-4-5-20250929")

	if !hasFlag(args, "--include-hook-events") {
		t.Error("buildArgs missing --include-hook-events flag")
	}
	if !hasFlag(args, "--verbose") {
		t.Error("buildArgs missing --verbose flag")
	}
	if !hasFlag(args, "--output-format") {
		t.Error("buildArgs missing --output-format flag")
	}
}

func TestTransformMessageUserWithToolFailure(t *testing.T) {
	c := &ClaudeCodeClient{}

	// Simulate a user message with a failed tool result
	msg := ClaudeCodeMessage{
		Type:    "user",
		Message: json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_xxx","is_error":true,"content":"File does not exist."}]}`),
	}

	events := c.transformMessage(msg)

	// Should get the delta event + tool_failure event
	if len(events) < 2 {
		t.Fatalf("got %d events, want at least 2 (delta + tool_failure)", len(events))
	}

	// Last event should be a tool_failure
	lastEvent := events[len(events)-1]
	if lastEvent.Event == nil || string(*lastEvent.Event) != "tool_failure" {
		t.Errorf("last event type = %v, want tool_failure", lastEvent.Event)
	}

	if lastEvent.Data != nil {
		var failure struct {
			ToolUseID string `json:"tool_use_id"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(*lastEvent.Data, &failure); err != nil {
			t.Fatalf("unmarshal tool_failure: %v", err)
		}
		if failure.ToolUseID != "toolu_xxx" {
			t.Errorf("tool_use_id = %q, want toolu_xxx", failure.ToolUseID)
		}
		if failure.Error != "File does not exist." {
			t.Errorf("error = %q, want File does not exist.", failure.Error)
		}
	}
}

func TestTransformMessageUserWithSuccessfulTool(t *testing.T) {
	c := &ClaudeCodeClient{}

	msg := ClaudeCodeMessage{
		Type:    "user",
		Message: json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_success","is_error":false,"content":"Operation completed."}]}`),
	}

	events := c.transformMessage(msg)

	// Should only get the delta event, no tool_failure
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (delta only, no tool_failure)", len(events))
	}
	if events[0].Event != nil && string(*events[0].Event) == "tool_failure" {
		t.Error("unexpected tool_failure event for successful tool result")
	}
}

func TestTransformMessageSystemInit(t *testing.T) {
	c := &ClaudeCodeClient{}

	msg := ClaudeCodeMessage{
		Type:              "system",
		Subtype:           "init",
		SessionID:         "session_123",
		ClaudeModel:       "claude-fable-5",
		ClaudeCodeVersion: "2.1.197",
		Tools:             []string{"Read", "Bash", "Write"},
	}

	events := c.transformMessage(msg)

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (system_init)", len(events))
	}

	if events[0].Event == nil || string(*events[0].Event) != "system_init" {
		t.Errorf("event type = %v, want system_init", events[0].Event)
	}

	if events[0].Data != nil {
		var init struct {
			Type    string   `json:"type"`
			Model   string   `json:"model"`
			Version string   `json:"claude_code_version"`
			Tools   []string `json:"tools"`
		}
		if err := json.Unmarshal(*events[0].Data, &init); err != nil {
			t.Fatalf("unmarshal system_init: %v", err)
		}
		if init.Type != "system_init" {
			t.Errorf("type = %q, want system_init", init.Type)
		}
		if init.Model != "claude-fable-5" {
			t.Errorf("model = %q, want claude-fable-5", init.Model)
		}
		if init.Version != "2.1.197" {
			t.Errorf("version = %q, want 2.1.197", init.Version)
		}
		if len(init.Tools) != 3 || init.Tools[0] != "Read" {
			t.Errorf("tools = %v, want [Read Bash Write]", init.Tools)
		}
	}
}

func TestTransformMessageSystemNoSubtype(t *testing.T) {
	c := &ClaudeCodeClient{}

	// system event without init subtype should not produce events
	msg := ClaudeCodeMessage{Type: "system"}
	events := c.transformMessage(msg)
	if len(events) != 0 {
		t.Errorf("got %d events, want 0 for non-init system event", len(events))
	}
}

func TestTransformMessageHookEvent(t *testing.T) {
	c := &ClaudeCodeClient{}

	msg := ClaudeCodeMessage{
		Type:    "hook",
		Subtype: "pre_tool",
	}

	events := c.transformMessage(msg)

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (hook_event)", len(events))
	}

	if events[0].Event == nil || string(*events[0].Event) != "hook_event" {
		t.Errorf("event type = %v, want hook_event", events[0].Event)
	}

	if events[0].Data != nil {
		var hook struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
		}
		if err := json.Unmarshal(*events[0].Data, &hook); err != nil {
			t.Fatalf("unmarshal hook_event: %v", err)
		}
		if hook.Type != "hook" {
			t.Errorf("type = %q, want hook", hook.Type)
		}
		if hook.Subtype != "pre_tool" {
			t.Errorf("subtype = %q, want pre_tool", hook.Subtype)
		}
	}
}

func TestTransformMessageUnknownType(t *testing.T) {
	c := &ClaudeCodeClient{}

	msg := ClaudeCodeMessage{Type: "unknown_event_type"}
	events := c.transformMessage(msg)

	// Unknown events should be silently ignored
	if len(events) != 0 {
		t.Errorf("got %d events, want 0 for unknown type", len(events))
	}
}

func TestTransformMessageResultWithExtendedFields(t *testing.T) {
	c := &ClaudeCodeClient{}

	msg := ClaudeCodeMessage{
		Type:           "result",
		Subtype:        "success",
		IsError:        false,
		DurationMS:     14073,
		NumTurns:       3,
		Result:         "Task completed successfully.",
		StopReason:     "end_turn",
		TotalCostUSD:   0.378879,
		TerminalReason: "completed",
	}

	events := c.transformMessage(msg)

	// Should still produce 2 events (usage + message_stop)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	if events[1].Event == nil || string(*events[1].Event) != "message_stop" {
		t.Errorf("second event = %v, want message_stop", events[1].Event)
	}
}

func TestCreateToolFailureEvent(t *testing.T) {
	c := &ClaudeCodeClient{}

	event := c.createToolFailureEvent("toolu_fail_123", "Permission denied")

	if event.Event == nil || string(*event.Event) != "tool_failure" {
		t.Errorf("event type = %v, want tool_failure", event.Event)
	}

	if event.Data != nil {
		var data map[string]any
		if err := json.Unmarshal(*event.Data, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if data["tool_use_id"] != "toolu_fail_123" {
			t.Errorf("tool_use_id = %v, want toolu_fail_123", data["tool_use_id"])
		}
		if data["error"] != "Permission denied" {
			t.Errorf("error = %v, want Permission denied", data["error"])
		}
	}
}

func TestCreateSystemEventNonInitReturnsNil(t *testing.T) {
	c := &ClaudeCodeClient{}

	// System event without init subtype should return nil
	msg := ClaudeCodeMessage{Type: "system", Subtype: "other"}
	event := c.createSystemEvent(msg)
	if event != nil {
		t.Error("expected nil for non-init system event")
	}
}

func TestClaudeCodeMessageExtendedFieldsParsing(t *testing.T) {
	// Verify that the JSONL stream JSON can be unmarshalled into ClaudeCodeMessage
	// with the extended fields
	rawJSON := `{
		"type": "result",
		"subtype": "success",
		"is_error": false,
		"session_id": "sess_001",
		"duration_ms": 5000,
		"num_turns": 5,
		"result": "Done",
		"stop_reason": "end_turn",
		"total_cost_usd": 0.15,
		"permission_denials": [],
		"terminal_reason": "completed",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens": 0
		}
	}`

	var msg ClaudeCodeMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if msg.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", msg.StopReason)
	}
	if msg.TerminalReason != "completed" {
		t.Errorf("TerminalReason = %q, want completed", msg.TerminalReason)
	}
	if msg.Result != "Done" {
		t.Errorf("Result = %q, want Done", msg.Result)
	}
	if msg.TotalCostUSD != 0.15 {
		t.Errorf("TotalCostUSD = %f, want 0.15", msg.TotalCostUSD)
	}
	if msg.DurationMS != 5000 {
		t.Errorf("DurationMS = %d, want 5000", msg.DurationMS)
	}
	if msg.NumTurns != 5 {
		t.Errorf("NumTurns = %d, want 5", msg.NumTurns)
	}
	if msg.Usage == nil || msg.Usage.InputTokens != 100 {
		t.Errorf("Usage.InputTokens = %d, want 100", msg.Usage.InputTokens)
	}
}

func TestClaudeCodeMessageSystemInitParsing(t *testing.T) {
	rawJSON := `{
		"type": "system",
		"subtype": "init",
		"session_id": "sess_002",
		"cwd": "/path/to/repo",
		"model": "claude-fable-5",
		"permissionMode": "auto",
		"claude_code_version": "2.1.197",
		"tools": ["Read", "Bash", "Write", "Edit"]
	}`

	var msg ClaudeCodeMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if msg.Type != "system" || msg.Subtype != "init" {
		t.Errorf("type/subtype = %s/%s, want system/init", msg.Type, msg.Subtype)
	}
	if msg.CWD != "/path/to/repo" {
		t.Errorf("CWD = %q, want /path/to/repo", msg.CWD)
	}
	if msg.ClaudeModel != "claude-fable-5" {
		t.Errorf("ClaudeModel = %q, want claude-fable-5", msg.ClaudeModel)
	}
	if msg.ClaudeCodeVersion != "2.1.197" {
		t.Errorf("ClaudeCodeVersion = %q, want 2.1.197", msg.ClaudeCodeVersion)
	}
	if len(msg.Tools) != 4 {
		t.Errorf("len(Tools) = %d, want 4", len(msg.Tools))
	}
}

func TestToolResultContentWithIsError(t *testing.T) {
	// Verify that tool_result content blocks with is_error are parsed correctly
	rawJSON := `{
		"type": "user",
		"session_id": "sess_003",
		"message": {
			"role": "user",
			"content": [
				{
					"type": "tool_result",
					"tool_use_id": "toolu_err_1",
					"is_error": true,
					"content": "File not found"
				},
				{
					"type": "tool_result",
					"tool_use_id": "toolu_ok_1",
					"is_error": false,
					"content": "Success"
				}
			]
		}
	}`

	var msg ClaudeCodeMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var toolResultMsg ToolResultMessage
	if err := json.Unmarshal(msg.Message, &toolResultMsg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}

	if len(toolResultMsg.Content) != 2 {
		t.Fatalf("got %d content blocks, want 2", len(toolResultMsg.Content))
	}

	if !toolResultMsg.Content[0].IsError {
		t.Error("expected first tool result to have is_error=true")
	}
	if toolResultMsg.Content[0].ToolUseID != "toolu_err_1" {
		t.Errorf("tool_use_id = %q, want toolu_err_1", toolResultMsg.Content[0].ToolUseID)
	}
	if toolResultMsg.Content[0].Content != "File not found" {
		t.Errorf("content = %q, want File not found", toolResultMsg.Content[0].Content)
	}

	if toolResultMsg.Content[1].IsError {
		t.Error("expected second tool result to have is_error=false")
	}
	if toolResultMsg.Content[1].ToolUseID != "toolu_ok_1" {
		t.Errorf("tool_use_id = %q, want toolu_ok_1", toolResultMsg.Content[1].ToolUseID)
	}
}
