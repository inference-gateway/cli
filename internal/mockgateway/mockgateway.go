// Package mockgateway implements a hermetic, deterministic stand-in for the
// inference-gateway HTTP API surface the CLI consumes: GET /v1/models,
// POST /v1/chat/completions (sync JSON and SSE streaming) and GET /v1/health.
//
// Scenario resolution is stateless: every request carries the full message
// history, so the scenario is chosen by matching each scenario's regex
// against the first user message, and the turn within the scenario is the
// count of assistant messages already present in the request. The only
// mutable state is the error-injection counter and the request recording,
// both guarded by one mutex, which makes the server safe for concurrent use.
package mockgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// DefaultModel is the single model the mock advertises on /v1/models. Model
// ids carry the provider prefix; request bodies arrive with it stripped.
const DefaultModel = "openai/gpt-4o"

const defaultChunkSize = 16

// Recorded captures one /v1/chat/completions request for test assertions.
type Recorded struct {
	// Provider is the ?provider= query parameter sent by the SDK.
	Provider string
	// Model is the request-body model (provider prefix already stripped).
	Model string
	// Scenario is the matched scenario name; empty when only the fallback applied.
	Scenario string
	// Step is the assistant-message count at the time of the request.
	Step int
	// Stream reports whether the request asked for SSE.
	Stream bool
	// Body is the full decoded request.
	Body sdk.CreateChatCompletionRequest
}

// Server is an http.Handler implementing the inference-gateway API surface
// the CLI consumes.
type Server struct {
	defs *ScenarioFile

	mu    sync.Mutex
	fails map[string]int
	reqs  []Recorded
}

// New returns a Server serving the given scenario definitions.
func New(defs *ScenarioFile) *Server {
	return &Server{defs: defs, fails: make(map[string]int)}
}

// Requests returns a copy of all recorded chat-completion requests.
func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reqs)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
		s.handleCompletions(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
		writeJSON(w, sdk.ListModelsResponse{
			Object: "list",
			Data:   []sdk.Model{{ID: DefaultModel, Object: "model", OwnedBy: "openai", ServedBy: "openai"}},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/health":
		writeJSON(w, map[string]string{"status": "ok"})
	default:
		http.Error(w, fmt.Sprintf("mockgateway: unexpected %s %s", r.Method, r.URL.Path), http.StatusNotFound)
	}
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var req sdk.CreateChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	name, step, turn := s.defs.resolve(&req)
	stream := req.Stream != nil && *req.Stream
	w.Header().Set("X-Mockgateway-Scenario", name)
	w.Header().Set("X-Mockgateway-Step", fmt.Sprintf("%d", step))
	s.record(Recorded{
		Provider: r.URL.Query().Get("provider"),
		Model:    req.Model,
		Scenario: name,
		Step:     step,
		Stream:   stream,
		Body:     req,
	})

	if turn.Error != nil && s.consumeFailure(name, step, turn.Error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(turn.Error.Status)
		_, _ = fmt.Fprint(w, `{"error":"injected"}`)
		return
	}

	if stream {
		if turn.Stall != nil && s.consumeFailure("stall:"+name, step, &ErrorInject{Times: turn.Stall.Times}) {
			renderStalledStream(w, r, turn.Stall.Connect)
			return
		}
		s.renderStream(w, r, req.Model, step, turn)
		return
	}
	s.renderSync(w, r, req.Model, step, turn)
}

func (s *Server) record(rec Recorded) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, rec)
}

// consumeFailure reports whether this request still receives the injected
// error and advances the per-scenario/step failure counter.
func (s *Server) consumeFailure(name string, step int, e *ErrorInject) bool {
	if e.Times < 0 {
		return true
	}

	key := fmt.Sprintf("%s/%d", name, step)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fails[key] >= e.Times {
		return false
	}
	s.fails[key]++
	return true
}

func (s *Server) renderSync(w http.ResponseWriter, r *http.Request, model string, step int, turn Turn) {
	if !wait(r.Context(), turn.DelayMs) {
		return
	}

	msg, err := sdk.NewTextMessage(sdk.Assistant, turn.Content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if turn.Reasoning != "" {
		reasoning := turn.Reasoning
		msg.ReasoningContent = &reasoning
	}
	if calls := turn.sdkToolCalls(step); len(calls) > 0 {
		msg.ToolCalls = &calls
	}

	writeJSON(w, sdk.CreateChatCompletionResponse{
		ID:      "chatcmpl-mock",
		Object:  "chat.completion",
		Model:   model,
		Choices: []sdk.ChatCompletionChoice{{Index: 0, FinishReason: turn.finishReason(), Message: msg}},
		Usage:   turn.usage(),
	})
}

// renderStalledStream holds the connection open without frames until the
// client disconnects - after the initial role delta by default, or before
// the response headers when connect is true.
func renderStalledStream(w http.ResponseWriter, r *http.Request, connect bool) {
	if connect {
		<-r.Context().Done()
		return
	}

	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "mockgateway: streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	sw := &streamWriter{w: w, fl: fl, ctx: r.Context()}
	if !sw.delta(sdk.ChatCompletionStreamResponseDelta{Role: sdk.Assistant}, "") {
		return
	}
	<-r.Context().Done()
}

func (s *Server) renderStream(w http.ResponseWriter, r *http.Request, model string, step int, turn Turn) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "mockgateway: streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	sw := &streamWriter{w: w, fl: fl, ctx: r.Context(), model: model, delay: turn.DelayMs}
	if !sw.delta(sdk.ChatCompletionStreamResponseDelta{Role: sdk.Assistant}, "") {
		return
	}
	if turn.Malformed && !sw.raw("{this is not json") {
		return
	}
	if !streamText(sw, turn) || !streamToolCalls(sw, step, turn) {
		return
	}
	if !sw.delta(sdk.ChatCompletionStreamResponseDelta{}, turn.finishReason()) {
		return
	}
	if !sw.frame([]sdk.ChatCompletionStreamChoice{}, turn.usage()) {
		return
	}
	sw.raw("[DONE]")
}

// streamText emits reasoning fragments followed by content fragments.
func streamText(sw *streamWriter, turn Turn) bool {
	for _, c := range chunks(turn.Reasoning, turn.ChunkSize) {
		frag := c
		if !sw.delta(sdk.ChatCompletionStreamResponseDelta{ReasoningContent: &frag}, "") {
			return false
		}
	}
	for _, c := range chunks(turn.Content, turn.ChunkSize) {
		if !sw.delta(sdk.ChatCompletionStreamResponseDelta{Content: c}, "") {
			return false
		}
	}
	return true
}

// streamToolCalls emits, per tool call, a name fragment followed by at least
// two argument fragments so the CLI's index-keyed accumulator is always
// exercised on real multi-fragment input.
func streamToolCalls(sw *streamWriter, step int, turn Turn) bool {
	for i, tc := range turn.ToolCalls {
		id := fmt.Sprintf("call_%d_%d", step, i)
		typ := string(sdk.Function)
		head := sdk.ChatCompletionMessageToolCallChunk{
			Index:    i,
			ID:       &id,
			Type:     &typ,
			Function: &sdk.ChatCompletionMessageToolCallFunction{Name: tc.Name},
		}
		if !sw.toolFrame(head) {
			return false
		}

		for _, frag := range argFragments(tc.argsJSON, turn.ChunkSize) {
			part := sdk.ChatCompletionMessageToolCallChunk{
				Index:    i,
				Function: &sdk.ChatCompletionMessageToolCallFunction{Arguments: frag},
			}
			if !sw.toolFrame(part) {
				return false
			}
		}
	}
	return true
}

func (t Turn) finishReason() sdk.FinishReason {
	if len(t.ToolCalls) > 0 {
		return sdk.ToolCalls
	}
	return sdk.Stop
}

func (t Turn) usage() *sdk.CompletionUsage {
	u := Usage{PromptTokens: 10, CompletionTokens: 5}
	if t.Usage != nil {
		u = *t.Usage
	}
	return &sdk.CompletionUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.PromptTokens + u.CompletionTokens,
	}
}

func (t Turn) sdkToolCalls(step int) []sdk.ChatCompletionMessageToolCall {
	calls := make([]sdk.ChatCompletionMessageToolCall, len(t.ToolCalls))
	for i, tc := range t.ToolCalls {
		calls[i] = sdk.ChatCompletionMessageToolCall{
			ID:   fmt.Sprintf("call_%d_%d", step, i),
			Type: sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      tc.Name,
				Arguments: tc.argsJSON,
			},
		}
	}
	return calls
}

// streamWriter writes SSE frames in the exact format the SDK parses:
// "data: <json>\n\n" (the space after the colon is required) with a flush
// per frame, honoring the per-frame delay and client disconnect.
type streamWriter struct {
	w     io.Writer
	fl    http.Flusher
	ctx   context.Context
	model string
	delay int
}

func (sw *streamWriter) delta(d sdk.ChatCompletionStreamResponseDelta, finish sdk.FinishReason) bool {
	return sw.frame([]sdk.ChatCompletionStreamChoice{{Index: 0, Delta: d, FinishReason: finish}}, nil)
}

func (sw *streamWriter) toolFrame(call sdk.ChatCompletionMessageToolCallChunk) bool {
	calls := []sdk.ChatCompletionMessageToolCallChunk{call}
	return sw.delta(sdk.ChatCompletionStreamResponseDelta{ToolCalls: &calls}, "")
}

func (sw *streamWriter) frame(choices []sdk.ChatCompletionStreamChoice, usage *sdk.CompletionUsage) bool {
	b, err := json.Marshal(sdk.CreateChatCompletionStreamResponse{
		ID:      "chatcmpl-mock",
		Object:  "chat.completion.chunk",
		Model:   sw.model,
		Choices: choices,
		Usage:   usage,
	})
	if err != nil {
		return false
	}
	return sw.raw(string(b))
}

func (sw *streamWriter) raw(payload string) bool {
	if !wait(sw.ctx, sw.delay) {
		return false
	}
	if _, err := fmt.Fprintf(sw.w, "data: %s\n\n", payload); err != nil {
		return false
	}
	sw.fl.Flush()
	return true
}

// wait sleeps for ms milliseconds, returning false if ctx finishes first.
func wait(ctx context.Context, ms int) bool {
	if ms <= 0 {
		return true
	}
	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return true
	case <-ctx.Done():
		return false
	}
}

// chunks splits s into size-rune pieces (default 16), never splitting a
// UTF-8 rune across fragments.
func chunks(s string, size int) []string {
	if s == "" {
		return nil
	}
	if size <= 0 {
		size = defaultChunkSize
	}

	runes := []rune(s)
	out := make([]string, 0, len(runes)/size+1)
	for start := 0; start < len(runes); start += size {
		out = append(out, string(runes[start:min(start+size, len(runes))]))
	}
	return out
}

// argFragments splits a tool call's arguments JSON into at least two pieces
// whenever it is long enough, so accumulation is always exercised.
func argFragments(args string, size int) []string {
	if size <= 0 {
		size = defaultChunkSize
	}
	half := (len([]rune(args)) + 1) / 2
	return chunks(args, min(size, max(1, half)))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
