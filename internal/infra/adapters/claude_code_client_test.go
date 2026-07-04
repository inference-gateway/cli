package adapters

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

func transform(t *testing.T, c *ClaudeCodeClient, msg ClaudeCodeMessage) []sdk.SSEvent {
	t.Helper()
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	return c.transformMessage(msg, raw, "")
}

func eventName(ev sdk.SSEvent) string {
	if ev.Event == nil {
		return ""
	}
	return string(*ev.Event)
}

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
	Model   string `json:"model"`
	Choices []struct {
		Delta map[string]any `json:"delta"`
		Index int            `json:"index"`
	} `json:"choices"`
	Usage *sdk.CompletionUsage `json:"usage"`
}

// resultMetadataShape mirrors the result_metadata event payload.
type resultMetadataShape struct {
	SessionID         string          `json:"session_id"`
	Model             string          `json:"model"`
	Subtype           string          `json:"subtype"`
	IsError           bool            `json:"is_error"`
	DurationMS        int             `json:"duration_ms"`
	DurationAPIMS     int             `json:"duration_api_ms"`
	TTFTMS            int             `json:"ttft_ms"`
	TTFTStreamMS      int             `json:"ttft_stream_ms"`
	NumTurns          int             `json:"num_turns"`
	StopReason        string          `json:"stop_reason"`
	TerminalReason    string          `json:"terminal_reason"`
	TotalCostUSD      float64         `json:"total_cost_usd"`
	PermissionDenials json.RawMessage `json:"permission_denials"`
	Usage             struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

func assertResultMetadata(t *testing.T, ev sdk.SSEvent) {
	t.Helper()

	if eventName(ev) != "result_metadata" {
		t.Fatalf("first event = %q, want result_metadata", eventName(ev))
	}
	if ev.Data == nil {
		t.Fatal("result_metadata has nil data")
	}
	var meta resultMetadataShape
	if err := json.Unmarshal(*ev.Data, &meta); err != nil {
		t.Fatalf("unmarshal result_metadata: %v", err)
	}
	if meta.SessionID != "sess_1" || meta.Subtype != "success" || meta.StopReason != "end_turn" {
		t.Errorf("metadata = %+v, want sess_1/success/end_turn", meta)
	}
	if meta.DurationMS != 1806 || meta.DurationAPIMS != 1764 || meta.TTFTMS != 1771 || meta.TTFTStreamMS != 1296 {
		t.Errorf("durations = %d/%d/%d/%d, want 1806/1764/1771/1296",
			meta.DurationMS, meta.DurationAPIMS, meta.TTFTMS, meta.TTFTStreamMS)
	}
	if meta.NumTurns != 3 || meta.TotalCostUSD != 0.0165 {
		t.Errorf("num_turns/cost = %d/%f, want 3/0.0165", meta.NumTurns, meta.TotalCostUSD)
	}
	if meta.Usage.InputTokens != 100 || meta.Usage.OutputTokens != 20 ||
		meta.Usage.CacheCreationInputTokens != 30 || meta.Usage.CacheReadInputTokens != 50 {
		t.Errorf("usage = %+v, want 100/20/30/50", meta.Usage)
	}
}

func TestTransformMessageResultEmitsMetadataUsageAndStop(t *testing.T) {
	c := &ClaudeCodeClient{}

	events := transform(t, c, ClaudeCodeMessage{
		Type:           "result",
		Subtype:        "success",
		SessionID:      "sess_1",
		DurationMS:     1806,
		DurationAPIMS:  1764,
		TTFTMS:         1771,
		TTFTStreamMS:   1296,
		NumTurns:       3,
		StopReason:     "end_turn",
		TerminalReason: "completed",
		TotalCostUSD:   0.0165,
		Usage:          &ClaudeUsage{InputTokens: 100, OutputTokens: 20, CacheCreationInputTokens: 30, CacheReadInputTokens: 50},
	})

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3 (result_metadata + usage chunk + message_stop)", len(events))
	}

	assertResultMetadata(t, events[0])

	if events[1].Data == nil {
		t.Fatal("usage chunk has nil data")
	}
	var chunk chunkShape
	if err := json.Unmarshal(*events[1].Data, &chunk); err != nil {
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

	if eventName(events[2]) != "message_stop" {
		t.Errorf("last event = %q, want message_stop", eventName(events[2]))
	}
	if events[2].Data == nil || string(*events[2].Data) != "done" {
		t.Errorf("message_stop data = %v, want done", events[2].Data)
	}
}

func TestTransformMessageResultNilUsageEmitsZeroUsage(t *testing.T) {
	c := &ClaudeCodeClient{}

	events := transform(t, c, ClaudeCodeMessage{Type: "result"})
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	var chunk chunkShape
	if err := json.Unmarshal(*events[1].Data, &chunk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if chunk.Usage == nil {
		t.Fatal("want a zero usage object, got nil")
	}
	if chunk.Usage.PromptTokens != 0 || chunk.Usage.CompletionTokens != 0 || chunk.Usage.TotalTokens != 0 {
		t.Errorf("want zero usage, got %+v", chunk.Usage)
	}
}

// permission_denials arrives as an array of objects; the previous []string typing
// made the whole result line fail to unmarshal, dropping usage and message_stop.
func TestResultWithPermissionDenialObjectsParsesAndForwards(t *testing.T) {
	rawJSON := `{
		"type": "result",
		"subtype": "success",
		"session_id": "sess_pd",
		"duration_ms": 100,
		"total_cost_usd": 0.01,
		"usage": {"input_tokens": 1, "output_tokens": 2},
		"permission_denials": [{"tool_name":"Read","tool_use_id":"toolu_1","tool_input":{"file_path":"/x"}}]
	}`

	var msg ClaudeCodeMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		t.Fatalf("unmarshal result with permission denial objects: %v", err)
	}

	c := &ClaudeCodeClient{}
	events := c.transformMessage(msg, []byte(rawJSON), "claude-haiku-4-5")
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	var meta resultMetadataShape
	if err := json.Unmarshal(*events[0].Data, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	var denials []struct {
		ToolName  string `json:"tool_name"`
		ToolUseID string `json:"tool_use_id"`
	}
	if err := json.Unmarshal(meta.PermissionDenials, &denials); err != nil {
		t.Fatalf("unmarshal denials: %v", err)
	}
	if len(denials) != 1 || denials[0].ToolName != "Read" || denials[0].ToolUseID != "toolu_1" {
		t.Errorf("denials = %+v, want one Read/toolu_1", denials)
	}
}

func TestResultEmptyPermissionDenialsOmitted(t *testing.T) {
	rawJSON := `{"type":"result","subtype":"success","permission_denials":[]}`
	var msg ClaudeCodeMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	c := &ClaudeCodeClient{}
	events := c.transformMessage(msg, []byte(rawJSON), "")
	var meta map[string]any
	if err := json.Unmarshal(*events[0].Data, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if _, ok := meta["permission_denials"]; ok {
		t.Error("empty permission_denials should be omitted from metadata")
	}
}

func TestProcessEventsCapturesUsage(t *testing.T) {
	c := &ClaudeCodeClient{}

	ch := make(chan sdk.SSEvent, 8)
	for _, ev := range transform(t, c, ClaudeCodeMessage{
		Type:    "assistant",
		Message: json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"hello"}]}`),
	}) {
		ch <- ev
	}
	for _, ev := range transform(t, c, ClaudeCodeMessage{
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
		t.Errorf("content = %q, want hello (metadata/usage events must not corrupt content)", content)
	}
	if usage == nil {
		t.Fatal("usage is nil")
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 5 || usage.TotalTokens != 15 {
		t.Errorf("usage = %+v, want prompt=10 completion=5 total=15", usage)
	}
}

func TestBuildArgsStripsProviderPrefix(t *testing.T) {
	c := &ClaudeCodeClient{config: &config.ClaudeCodeConfig{}}

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
	c := &ClaudeCodeClient{config: &config.ClaudeCodeConfig{}}

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

	msg := ClaudeCodeMessage{
		Type:    "user",
		Message: json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_xxx","is_error":true,"content":"File does not exist."}]}`),
	}

	events := transform(t, c, msg)

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (delta + tool_failure)", len(events))
	}

	lastEvent := events[1]
	if eventName(lastEvent) != "tool_failure" {
		t.Fatalf("last event type = %q, want tool_failure", eventName(lastEvent))
	}
	if lastEvent.Data == nil {
		t.Fatal("tool_failure has nil data")
	}

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

func TestTransformMessageUserWithSuccessfulTool(t *testing.T) {
	c := &ClaudeCodeClient{}

	msg := ClaudeCodeMessage{
		Type:    "user",
		Message: json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_success","is_error":false,"content":"Operation completed."}]}`),
	}

	events := transform(t, c, msg)

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (delta only, no tool_failure)", len(events))
	}
	if eventName(events[0]) == "tool_failure" {
		t.Error("unexpected tool_failure event for successful tool result")
	}
}

func TestTransformMessageSystemInit(t *testing.T) {
	c := &ClaudeCodeClient{}

	msg := ClaudeCodeMessage{
		Type:              "system",
		Subtype:           "init",
		SessionID:         "session_123",
		CWD:               "/home/user/repo",
		ClaudeModel:       "claude-haiku-4-5",
		PermissionMode:    "default",
		ClaudeCodeVersion: "2.1.197",
		Tools:             []string{"Read", "Bash", "Write"},
	}

	events := transform(t, c, msg)

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (system_init)", len(events))
	}
	if eventName(events[0]) != "system_init" {
		t.Fatalf("event type = %q, want system_init", eventName(events[0]))
	}
	if events[0].Data == nil {
		t.Fatal("system_init has nil data")
	}

	var init struct {
		Type           string   `json:"type"`
		CWD            string   `json:"cwd"`
		Model          string   `json:"model"`
		PermissionMode string   `json:"permission_mode"`
		Version        string   `json:"claude_code_version"`
		Tools          []string `json:"tools"`
	}
	if err := json.Unmarshal(*events[0].Data, &init); err != nil {
		t.Fatalf("unmarshal system_init: %v", err)
	}
	if init.Type != "system_init" {
		t.Errorf("type = %q, want system_init", init.Type)
	}
	if init.CWD != "/home/user/repo" || init.PermissionMode != "default" {
		t.Errorf("cwd/permission_mode = %q/%q, want /home/user/repo/default", init.CWD, init.PermissionMode)
	}
	if init.Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want claude-haiku-4-5", init.Model)
	}
	if init.Version != "2.1.197" {
		t.Errorf("version = %q, want 2.1.197", init.Version)
	}
	if len(init.Tools) != 3 || init.Tools[0] != "Read" {
		t.Errorf("tools = %v, want [Read Bash Write]", init.Tools)
	}
}

func TestTransformMessageSystemNonInitSubtypeIgnored(t *testing.T) {
	c := &ClaudeCodeClient{}

	for _, subtype := range []string{"", "thinking_tokens", "other"} {
		events := transform(t, c, ClaudeCodeMessage{Type: "system", Subtype: subtype})
		if len(events) != 0 {
			t.Errorf("subtype %q: got %d events, want 0", subtype, len(events))
		}
	}
}

// Hook lifecycle events arrive as type=system with subtype hook_started /
// hook_response; the raw line is forwarded verbatim as a hook_event.
func TestTransformMessageHookEventForwardsRawLine(t *testing.T) {
	c := &ClaudeCodeClient{}

	rawJSON := `{"type":"system","subtype":"hook_started","hook_id":"h1","hook_name":"PostToolUse:Bash","hook_event":"PostToolUse","session_id":"sess_1"}`
	var msg ClaudeCodeMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	events := c.transformMessage(msg, []byte(rawJSON), "")

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (hook_event)", len(events))
	}
	if eventName(events[0]) != "hook_event" {
		t.Fatalf("event type = %q, want hook_event", eventName(events[0]))
	}
	if events[0].Data == nil || string(*events[0].Data) != rawJSON {
		t.Errorf("hook_event data must be the raw line, got %s", *events[0].Data)
	}
}

func TestTransformMessageUnknownType(t *testing.T) {
	c := &ClaudeCodeClient{}

	events := transform(t, c, ClaudeCodeMessage{Type: "rate_limit_event"})
	if len(events) != 0 {
		t.Errorf("got %d events, want 0 for unknown type", len(events))
	}
}

func TestModelStampedOnChunks(t *testing.T) {
	c := &ClaudeCodeClient{}

	msg := ClaudeCodeMessage{
		Type:    "assistant",
		Message: json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"hi"}]}`),
	}
	events := c.transformMessage(msg, nil, "claude-haiku-4-5")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	var chunk chunkShape
	if err := json.Unmarshal(*events[0].Data, &chunk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if chunk.Model != "claude-haiku-4-5" {
		t.Errorf("model = %q, want claude-haiku-4-5", chunk.Model)
	}
}

// TestGoldenStreamFixture runs every captured real-CLI line (claude 2.1.197)
// through the parse+transform path and asserts the event kinds produced.
func TestGoldenStreamFixture(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "stream_events.jsonl"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	c := &ClaudeCodeClient{}
	var model string
	var eventNames []string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var msg ClaudeCodeMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			t.Fatalf("fixture line failed to unmarshal: %v\n%s", err, line)
		}
		if msg.Type == "system" && msg.Subtype == "init" {
			model = msg.ClaudeModel
		}
		for _, ev := range c.transformMessage(msg, line, model) {
			eventNames = append(eventNames, eventName(ev))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}

	want := []string{
		"system_init",
		"content_block_delta",
		"content_block_delta",
		"hook_event",
		"hook_event",
		"content_block_delta",
		"content_block_delta",
		"tool_failure",
		"result_metadata",
		"content_block_delta",
		"message_stop",
	}
	if len(eventNames) != len(want) {
		t.Fatalf("event sequence = %v, want %v", eventNames, want)
	}
	for i := range want {
		if eventNames[i] != want[i] {
			t.Errorf("event[%d] = %q, want %q (full: %v)", i, eventNames[i], want[i], eventNames)
		}
	}
}

func TestCreateToolFailureEvent(t *testing.T) {
	c := &ClaudeCodeClient{}

	event := c.createToolFailureEvent("toolu_fail_123", "Permission denied")

	if eventName(event) != "tool_failure" {
		t.Fatalf("event type = %q, want tool_failure", eventName(event))
	}
	if event.Data == nil {
		t.Fatal("tool_failure has nil data")
	}

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

func TestClaudeCodeMessageResultFieldsParsing(t *testing.T) {
	rawJSON := `{
		"type": "result",
		"subtype": "success",
		"is_error": false,
		"session_id": "sess_001",
		"duration_ms": 5000,
		"duration_api_ms": 4800,
		"ttft_ms": 1771,
		"ttft_stream_ms": 1296,
		"num_turns": 5,
		"result": "Done",
		"stop_reason": "end_turn",
		"terminal_reason": "completed",
		"total_cost_usd": 0.15,
		"permission_denials": [],
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 10,
			"cache_read_input_tokens": 20
		}
	}`

	var msg ClaudeCodeMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if msg.StopReason != "end_turn" || msg.TerminalReason != "completed" {
		t.Errorf("stop/terminal = %q/%q, want end_turn/completed", msg.StopReason, msg.TerminalReason)
	}
	if msg.DurationMS != 5000 || msg.DurationAPIMS != 4800 {
		t.Errorf("durations = %d/%d, want 5000/4800", msg.DurationMS, msg.DurationAPIMS)
	}
	if msg.TTFTMS != 1771 || msg.TTFTStreamMS != 1296 {
		t.Errorf("ttft = %d/%d, want 1771/1296", msg.TTFTMS, msg.TTFTStreamMS)
	}
	if msg.TotalCostUSD != 0.15 || msg.NumTurns != 5 || msg.Result != "Done" {
		t.Errorf("cost/turns/result = %f/%d/%q", msg.TotalCostUSD, msg.NumTurns, msg.Result)
	}
	if msg.Usage == nil || msg.Usage.InputTokens != 100 || msg.Usage.CacheReadInputTokens != 20 {
		t.Errorf("usage = %+v, want input=100 cache_read=20", msg.Usage)
	}
}

func TestClaudeCodeMessageSystemInitParsing(t *testing.T) {
	rawJSON := `{
		"type": "system",
		"subtype": "init",
		"session_id": "sess_002",
		"cwd": "/path/to/repo",
		"model": "claude-haiku-4-5",
		"permissionMode": "default",
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
	if msg.ClaudeModel != "claude-haiku-4-5" {
		t.Errorf("ClaudeModel = %q, want claude-haiku-4-5", msg.ClaudeModel)
	}
	if msg.ClaudeCodeVersion != "2.1.197" {
		t.Errorf("ClaudeCodeVersion = %q, want 2.1.197", msg.ClaudeCodeVersion)
	}
	if len(msg.Tools) != 4 {
		t.Errorf("len(Tools) = %d, want 4", len(msg.Tools))
	}
}

func TestToolResultContentWithIsError(t *testing.T) {
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

// todoWriteCounts tracks the counts collected during processing.
type todoWriteCounts struct {
	ToolUse int
	Result  int
}

// extractToolCallsFromEvent extracts tool call maps from an SSE event's delta.
// Returns nil if the event has no tool calls.
func extractToolCallsFromEvent(ev sdk.SSEvent) []map[string]any {
	if ev.Data == nil {
		return nil
	}
	var delta map[string]any
	if err := json.Unmarshal(*ev.Data, &delta); err != nil {
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
	toolCallsRaw, ok := deltaContent["tool_calls"].([]any)
	if !ok {
		return nil
	}
	toolCalls := make([]map[string]any, 0, len(toolCallsRaw))
	for _, tcRaw := range toolCallsRaw {
		if tc, ok := tcRaw.(map[string]any); ok {
			toolCalls = append(toolCalls, tc)
		}
	}
	return toolCalls
}

// todoWriteArgs holds the expected shape of TodoWrite arguments.
type todoWriteArgs struct {
	Todos []struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"todos"`
}

// todoWriteResult holds the expected shape of a TodoWrite result.
type todoWriteResult struct {
	Todos          []any `json:"todos"`
	TotalTasks     int   `json:"total_tasks"`
	CompletedTasks int   `json:"completed_tasks"`
	ValidationOK   bool  `json:"validation_ok"`
}

// validateTodoWriteArgs checks the shape of a TodoWrite arguments JSON string.
func validateTodoWriteArgs(t *testing.T, args string) {
	t.Helper()

	var ta todoWriteArgs
	if err := json.Unmarshal([]byte(args), &ta); err != nil {
		t.Errorf("TodoWrite arguments not valid JSON: %v", err)
		return
	}
	if len(ta.Todos) != 1 {
		t.Errorf("TodoWrite arguments: got %d todos, want 1", len(ta.Todos))
		return
	}
	if ta.Todos[0].Status != "in_progress" {
		t.Errorf("TodoWrite todo status = %q, want in_progress", ta.Todos[0].Status)
		return
	}
	if ta.Todos[0].Content == "" {
		t.Error("TodoWrite todo content is empty")
	}
}

// processToolCall inspects a single tool call map and updates counts.
// It returns true if the tool call was a TodoWrite (for result matching).
func processToolCall(t *testing.T, tc map[string]any, c *ClaudeCodeClient, counts *todoWriteCounts) bool {
	t.Helper()

	funcRaw, ok := tc["function"].(map[string]any)
	if !ok {
		return false
	}
	name, _ := funcRaw["name"].(string)
	if name != "TodoWrite" {
		return false
	}
	counts.ToolUse++
	args, _ := funcRaw["arguments"].(string)
	validateTodoWriteArgs(t, args)
	return true
}

// processToolResult inspects a tool result and updates counts.
func processToolResult(t *testing.T, tc map[string]any, c *ClaudeCodeClient, counts *todoWriteCounts) {
	t.Helper()

	resultStr, ok := tc["result"].(string)
	if !ok {
		return
	}
	if _, isTaskCreate := c.taskCreateIDs[tc["id"].(string)]; isTaskCreate {
		// This was a mapped TaskCreate result; it should have been remapped.
		// If we see it here, the mapping didn't work.
		return
	}
	var tr todoWriteResult
	if err := json.Unmarshal([]byte(resultStr), &tr); err != nil || !tr.ValidationOK {
		return
	}
	counts.Result++
	if tr.TotalTasks != 1 {
		t.Errorf("TodoWrite result: total_tasks = %d, want 1", tr.TotalTasks)
	}
	if len(tr.Todos) != 1 {
		t.Errorf("TodoWrite result: got %d todos, want 1", len(tr.Todos))
	}
}

// TestTaskCreateToTodoWriteMapping uses the real claude-run.jsonl fixture to
// verify that TaskCreate tool_use and tool_result events are mapped to TodoWrite
// equivalents through the full transform pipeline.
func TestTaskCreateToTodoWriteMapping(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "todos_write.jsonl"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	c := &ClaudeCodeClient{
		taskCreateIDs: make(map[string]string),
	}
	var model string
	var counts todoWriteCounts

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var msg ClaudeCodeMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			t.Fatalf("fixture line failed to unmarshal: %v\n%s", err, line)
		}
		if msg.Type == "system" && msg.Subtype == "init" {
			model = msg.ClaudeModel
		}
		for _, ev := range c.transformMessage(msg, line, model) {
			for _, tc := range extractToolCallsFromEvent(ev) {
				processToolCall(t, tc, c, &counts)
				processToolResult(t, tc, c, &counts)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}

	if counts.ToolUse != 3 {
		t.Errorf("got %d TodoWrite tool_use events, want 3", counts.ToolUse)
	}
	if counts.Result != 3 {
		t.Errorf("got %d TodoWrite result events, want 3", counts.Result)
	}
	if len(c.taskCreateIDs) != 0 {
		t.Errorf("taskCreateIDs map not empty after processing, got %d entries", len(c.taskCreateIDs))
	}
}
