// Package metrics records local, low-overhead usage metrics - tool outcomes,
// token usage, and session lifecycle - and optionally exports them to an
// OpenTelemetry collector.
//
// Events are fanned out to one or more sinks:
//   - jsonlSink (always, when enabled): append-only JSONL under ~/.infer/metrics/,
//     rotated one file per month; powers `infer stats`. Private - nothing leaves
//     the machine, no prompt/response content is ever recorded.
//   - otlpSink (opt-in): when an OTLP endpoint is configured, the same event
//     stream is mapped onto OpenTelemetry GenAI/infer.* instruments and pushed to
//     the collector. This is a second sink, not a second instrumentation pass.
//
// The event model and the OTLP mapping mirror the ecosystem's existing metrics
// (gateway internal/otel using GenAI semconv, infer-action src/otel.ts):
//
//	event kind "usage"   -> gen_ai.client.token.usage (histogram, {token})
//	                        + infer.client.cost (sum, USD)
//	event kind "tool"    -> infer.agent.tool.calls (counter, {call})
//	                        + gen_ai.execute_tool.duration (histogram, s)
//	event kind "session" -> infer.agent.runs (counter) + infer.agent.run.duration (s)
//
// The outcome / token-type strings in the constants below are the exact values
// those instruments use; recorder_test.go pins them so a rename can't silently
// drift the CLI from the gateway/infer-action model.
package metrics

import (
	"context"
	"os"
	"strings"
	"time"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// Event kinds.
const (
	KindTool    = "tool"
	KindUsage   = "usage"
	KindSession = "session"
)

// Tool outcomes. Non-success also carries an error.type ("tool_error"); Rejected
// is the CLI-specific approval rejection with no gateway equivalent.
const (
	ToolSuccess  = "success"
	ToolError    = "error"
	ToolRejected = "rejected"

	errTypeTool = "tool_error"
)

// Session/run outcomes - the same enum as infer-action's infer.run.outcome.
const (
	RunSuccess      = "success"
	RunFailed       = "failed"
	RunStoppedEarly = "stopped_early"
)

// Session phases.
const (
	phaseStart = "start"
	phaseEnd   = "end"
)

// event is one recorded measurement. jsonlSink serializes it verbatim (compact
// keys, small footprint); otlpSink maps it onto OTel instruments.
type event struct {
	Time time.Time `json:"t"`
	Kind string    `json:"kind"`

	// tool
	Tool    string `json:"tool,omitempty"`
	Outcome string `json:"outcome,omitempty"` // tool outcome or session outcome
	Err     string `json:"err,omitempty"`     // -> error.type (tool only)
	DurMs   int64  `json:"dur_ms,omitempty"`

	// usage
	Model      string `json:"model,omitempty"`
	Prompt     int    `json:"prompt,omitempty"`
	Completion int    `json:"completion,omitempty"`

	// session
	Session string `json:"session,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Phase   string `json:"phase,omitempty"`
}

// CostFunc returns the input, output, and total cost for a model's token counts
// (wraps domain.PricingService.CalculateCost). Pass nil to skip cost.
type CostFunc func(model string, prompt, completion int) (input, output, total float64)

// sink consumes recorded events. record must be safe for concurrent use;
// shutdown flushes and releases any resources.
type sink interface {
	record(e event)
	shutdown(ctx context.Context) error
}

// Options configures a Recorder. Dir is the JSONL directory; OTLP* enable the
// optional exporter (inactive when the resolved endpoint is empty).
type Options struct {
	Enabled        bool
	Dir            string
	OTLPEndpoint   string
	OTLPHeaders    map[string]string
	OTLPInterval   time.Duration
	Cost           CostFunc
	ServiceVersion string
}

// Recorder fans each event out to its sinks. A nil *Recorder is a valid no-op
// (every method is nil-safe), so callers guard hot paths with `if rec != nil`
// and the container simply skips wrapping when disabled. Recording is
// best-effort: sink errors never break a run.
type Recorder struct {
	sinks []sink
}

// New builds a Recorder, or nil when disabled. The JSONL sink is always present;
// an OTLP sink is added when an endpoint is configured (or OTEL_EXPORTER_OTLP_ENDPOINT
// is set). OTLP init failures are logged and dropped - local JSONL still works.
func New(opts Options) *Recorder {
	if !opts.Enabled {
		return nil
	}
	sinks := []sink{&jsonlSink{dir: opts.Dir}}

	if endpoint := resolveOTLPEndpoint(opts.OTLPEndpoint); endpoint != "" {
		if o, err := newOTLPSink(endpoint, opts); err != nil {
			logger.Warn("metrics: OTLP export disabled", "error", err)
		} else {
			sinks = append(sinks, o)
		}
	}

	return &Recorder{sinks: sinks}
}

// resolveOTLPEndpoint prefers the explicit config value, then the standard
// OpenTelemetry env var, so `OTEL_EXPORTER_OTLP_ENDPOINT=... infer chat` works.
func resolveOTLPEndpoint(configured string) string {
	if configured != "" {
		return configured
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
}

// RecordTool records one tool execution. errType is the error.type class on
// non-success (empty on success/rejected).
func (r *Recorder) RecordTool(tool, outcome, errType string, dur time.Duration) {
	r.emit(event{Kind: KindTool, Tool: tool, Outcome: outcome, Err: errType, DurMs: dur.Milliseconds()})
}

// RecordUsage records one request's token usage. Cost is derived (not stored)
// from the model + counts - by `infer stats` locally and by the OTLP sink.
func (r *Recorder) RecordUsage(model string, prompt, completion int) {
	r.emit(event{Kind: KindUsage, Model: model, Prompt: prompt, Completion: completion})
}

// RecordSessionStart marks the beginning of an agent session.
func (r *Recorder) RecordSessionStart(session, mode string) {
	r.emit(event{Kind: KindSession, Phase: phaseStart, Session: session, Mode: mode})
}

// RecordSessionEnd marks the end of an agent session with its duration and
// outcome (one of RunSuccess / RunFailed / RunStoppedEarly).
func (r *Recorder) RecordSessionEnd(session, mode string, dur time.Duration, outcome string) {
	r.emit(event{Kind: KindSession, Phase: phaseEnd, Session: session, Mode: mode, DurMs: dur.Milliseconds(), Outcome: outcome})
}

// Shutdown flushes and releases every sink (the OTLP sink's final export). Safe
// on a nil recorder.
func (r *Recorder) Shutdown(ctx context.Context) {
	if r == nil {
		return
	}
	for _, s := range r.sinks {
		if err := s.shutdown(ctx); err != nil {
			logger.Warn("metrics: sink shutdown failed", "error", err)
		}
	}
}

func (r *Recorder) emit(e event) {
	if r == nil {
		return
	}
	e.Time = time.Now()
	for _, s := range r.sinks {
		s.record(e)
	}
}

// providerFromModel derives gen_ai.provider.name from a "provider/model" string
// (mirrors infer-action's extractProvider), defaulting to "unknown".
func providerFromModel(model string) string {
	if provider, _, ok := strings.Cut(model, "/"); ok && provider != "" {
		return provider
	}
	return "unknown"
}
