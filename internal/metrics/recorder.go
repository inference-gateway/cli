// Package metrics records local, low-overhead usage metrics - tool outcomes,
// token usage, and session lifecycle - as append-only JSONL under
// ~/.infer/metrics/, rotated one file per month.
//
// The on-disk model is a faithful local mirror of the ecosystem's OpenTelemetry
// GenAI / infer.* metric model (gateway internal/otel, infer-action src/otel.ts),
// so the OTLP exporter this unblocks is a 1:1 projection with no schema
// translation. The mapping:
//
//	event kind "usage"   -> gen_ai.client.token.usage (histogram, {token})
//	                        + infer.client.cost (sum, USD)   [cost derived at read time]
//	event kind "tool"    -> infer.agent.tool.calls (counter, {call})
//	                        + gen_ai.execute_tool.duration (histogram, s)
//	event kind "session" -> infer.agent.runs (counter) + infer.agent.run.duration (s)
//
// The outcome / token-type strings in the constants below are the exact values
// those instruments use; recorder_test.go pins them so a rename can't silently
// drift the CLI from the gateway/infer-action model.
package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
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

// event is one JSONL line. Compact keys keep the on-disk footprint small; every
// field a canonical OTel instrument needs is present (see the package doc).
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

// Recorder appends metric events to monthly-rotated JSONL files. A nil *Recorder
// is a valid no-op (every method is nil-safe), so callers guard with
// `if rec != nil` on hot paths and the container simply skips wrapping when
// disabled. Recording is best-effort: write errors are swallowed so metrics
// never break a run.
type Recorder struct {
	dir string
}

// New returns a Recorder writing to dir, or nil when disabled. The directory is
// created lazily on the first event, so building a Recorder has no filesystem
// side effect - read-only commands and container-only tests that never record
// leave no dir behind.
func New(dir string, enabled bool) *Recorder {
	if !enabled {
		return nil
	}
	return &Recorder{dir: dir}
}

// RecordTool records one tool execution. errType is the error.type class on
// non-success (empty on success/rejected).
func (r *Recorder) RecordTool(tool, outcome, errType string, dur time.Duration) {
	if r == nil {
		return
	}
	r.write(event{Kind: KindTool, Tool: tool, Outcome: outcome, Err: errType, DurMs: dur.Milliseconds()})
}

// RecordUsage records one request's token usage. Cost is intentionally not
// stored - `infer stats` derives it from the model + counts at read time.
func (r *Recorder) RecordUsage(model string, prompt, completion int) {
	if r == nil {
		return
	}
	r.write(event{Kind: KindUsage, Model: model, Prompt: prompt, Completion: completion})
}

// RecordSessionStart marks the beginning of an agent session.
func (r *Recorder) RecordSessionStart(session, mode string) {
	if r == nil {
		return
	}
	r.write(event{Kind: KindSession, Phase: phaseStart, Session: session, Mode: mode})
}

// RecordSessionEnd marks the end of an agent session with its duration and
// outcome (one of RunSuccess / RunFailed / RunStoppedEarly).
func (r *Recorder) RecordSessionEnd(session, mode string, dur time.Duration, outcome string) {
	if r == nil {
		return
	}
	r.write(event{Kind: KindSession, Phase: phaseEnd, Session: session, Mode: mode, DurMs: dur.Milliseconds(), Outcome: outcome})
}

// write marshals e to one line and appends it to the current month's file.
//
// ponytail: O_APPEND is the whole concurrency story - POSIX makes each append
// write of a sub-PIPE_BUF line atomic across goroutines AND across processes
// (chat + the headless `infer agent` subprocess share the file), so no mutex and
// no long-lived handle. Switch to a buffered handle + mutex only if event volume
// ever spikes past that.
func (r *Recorder) write(e event) {
	e.Time = time.Now()
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	line = append(line, '\n')

	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return // best-effort: metrics never break a run
	}
	f, err := os.OpenFile(r.filePath(e.Time), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(line)
}

func (r *Recorder) filePath(t time.Time) string {
	return filepath.Join(r.dir, t.Format("2006-01")+".jsonl")
}
