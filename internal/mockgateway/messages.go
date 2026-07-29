package mockgateway

import (
	"encoding/json"
	"fmt"
	"net/http"

	sdk "github.com/inference-gateway/sdk"
)

// handleMessages serves POST /v1/messages, the Anthropic-native endpoint the
// CLI's AnthropicMessages adapter targets. Scenarios are shared with the
// chat-completions endpoint via resolveMessages; the turn is rendered as
// native Anthropic SSE (message_start .. message_stop, no [DONE] sentinel;
// the stream ends when the response body closes) or as a sync
// MessagesResponse. Stall and Malformed injection are not supported here:
// they exercise OpenAI-stream reconnect paths the adapter never takes.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	var req sdk.CreateMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"invalid request body"}}`, http.StatusBadRequest)
		return
	}

	name, step, turn := s.defs.resolveMessages(&req)
	stream := req.Stream != nil && *req.Stream
	w.Header().Set("X-Mockgateway-Scenario", name)
	w.Header().Set("X-Mockgateway-Step", fmt.Sprintf("%d", step))
	s.record(Recorded{
		Endpoint:     r.URL.Path,
		Provider:     r.URL.Query().Get("provider"),
		Model:        req.Model,
		Scenario:     name,
		Step:         step,
		Stream:       stream,
		MessagesBody: &req,
	})

	if turn.Error != nil && s.consumeFailure(name, step, turn.Error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(turn.Error.Status)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"api_error","message":"injected"}}`)
		return
	}

	if stream {
		s.renderMessagesStream(w, r, req.Model, step, turn)
		return
	}
	s.renderMessagesSync(w, r, req.Model, step, turn)
}

// messagesUsage splits a turn's OpenAI-shaped usage into Anthropic semantics:
// input_tokens excludes cache reads and writes, so the adapter's
// input+read+write synthesis lands back on the scenario's PromptTokens and
// the same scenario numbers hold on both endpoints.
func (t Turn) messagesUsage() (input, output, cacheRead, cacheWrite int64) {
	u := Usage{PromptTokens: 10, CompletionTokens: 5}
	if t.Usage != nil {
		u = *t.Usage
	}
	return max(u.PromptTokens-u.CachedTokens-u.CacheWriteTokens, 0), u.CompletionTokens, u.CachedTokens, u.CacheWriteTokens
}

func (t Turn) messagesStopReason() string {
	if len(t.ToolCalls) > 0 {
		return "tool_use"
	}
	return "end_turn"
}

func (s *Server) renderMessagesStream(w http.ResponseWriter, r *http.Request, model string, step int, turn Turn) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "mockgateway: streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	sw := &streamWriter{w: w, fl: fl, ctx: r.Context(), model: model, delay: turn.DelayMs}
	input, output, cacheRead, cacheWrite := turn.messagesUsage()

	if !sw.event(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg-mock", "type": "message", "role": "assistant", "model": model,
			"content": []any{}, "stop_reason": nil,
			"usage": map[string]any{
				"input_tokens": input, "output_tokens": 0,
				"cache_read_input_tokens": cacheRead, "cache_creation_input_tokens": cacheWrite,
			},
		},
	}) {
		return
	}

	index := 0
	if turn.Reasoning != "" {
		if !sw.textBlock(index, "thinking", "thinking_delta", "thinking", turn.Reasoning, turn.ChunkSize) {
			return
		}
		index++
	}
	if turn.Content != "" {
		if !sw.textBlock(index, "text", "text_delta", "text", turn.Content, turn.ChunkSize) {
			return
		}
		index++
	}
	for i, tc := range turn.ToolCalls {
		if !sw.toolUseBlock(index, fmt.Sprintf("call_%d_%d", step, i), tc.Name, tc.argsJSON, turn.ChunkSize) {
			return
		}
		index++
	}

	if !sw.event(map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": turn.messagesStopReason()},
		"usage": map[string]any{"output_tokens": output},
	}) {
		return
	}
	sw.event(map[string]any{"type": "message_stop"})
}

// textBlock streams one text or thinking content block: start, per-chunk
// deltas, stop.
func (sw *streamWriter) textBlock(index int, blockType, deltaType, deltaField, text string, chunkSize int) bool {
	start := map[string]any{
		"type":          "content_block_start",
		"index":         index,
		"content_block": map[string]any{"type": blockType, blockType: ""},
	}
	if !sw.event(start) {
		return false
	}
	for _, c := range chunks(text, chunkSize) {
		if !sw.event(map[string]any{
			"type":  "content_block_delta",
			"index": index,
			"delta": map[string]any{"type": deltaType, deltaField: c},
		}) {
			return false
		}
	}
	return sw.event(map[string]any{"type": "content_block_stop", "index": index})
}

// toolUseBlock streams one tool_use content block with at least two
// input_json_delta fragments (via argFragments) so the adapter's partial-JSON
// accumulation is always exercised.
func (sw *streamWriter) toolUseBlock(index int, id, name, argsJSON string, chunkSize int) bool {
	if !sw.event(map[string]any{
		"type":          "content_block_start",
		"index":         index,
		"content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}},
	}) {
		return false
	}
	for _, frag := range argFragments(argsJSON, chunkSize) {
		if !sw.event(map[string]any{
			"type":  "content_block_delta",
			"index": index,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": frag},
		}) {
			return false
		}
	}
	return sw.event(map[string]any{"type": "content_block_stop", "index": index})
}

// event marshals one Anthropic stream event and writes it as an SSE data frame.
func (sw *streamWriter) event(v map[string]any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return sw.raw(string(b))
}

func (s *Server) renderMessagesSync(w http.ResponseWriter, r *http.Request, model string, step int, turn Turn) {
	if !wait(r.Context(), turn.DelayMs) {
		return
	}

	blocks := make([]map[string]any, 0, len(turn.ToolCalls)+2)
	if turn.Reasoning != "" {
		blocks = append(blocks, map[string]any{"type": "thinking", "thinking": turn.Reasoning, "signature": ""})
	}
	if turn.Content != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": turn.Content})
	}
	for i, tc := range turn.ToolCalls {
		var input map[string]any
		if err := json.Unmarshal([]byte(tc.argsJSON), &input); err != nil {
			input = map[string]any{}
		}
		blocks = append(blocks, map[string]any{
			"type": "tool_use", "id": fmt.Sprintf("call_%d_%d", step, i), "name": tc.Name, "input": input,
		})
	}

	input, output, cacheRead, cacheWrite := turn.messagesUsage()
	writeJSON(w, map[string]any{
		"id": "msg-mock", "type": "message", "role": "assistant", "model": model,
		"content":     blocks,
		"stop_reason": turn.messagesStopReason(),
		"usage": map[string]any{
			"input_tokens": input, "output_tokens": output,
			"cache_read_input_tokens": cacheRead, "cache_creation_input_tokens": cacheWrite,
		},
	})
}
