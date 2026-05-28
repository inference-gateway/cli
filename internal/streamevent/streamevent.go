// Package streamevent emits diagnostic JSON-line events on stdout so
// observers (the inference-gateway/infer-action runner, log scrapers,
// human eyeballs in CI) can see internal agent lifecycle moments that
// are otherwise silent - system reminder injections, conversation
// compaction triggers, and similar checkpoints.
//
// Events are HIDDEN BY DEFAULT and only emitted when the global logger
// is configured at debug level (via `--verbose`, `logging.debug=true`,
// or env var `INFER_LOGGING_DEBUG=true`). This keeps normal `infer
// agent` runs quiet on stdout while letting operators turn on the
// firehose when they need to diagnose.
//
// Two shapes are emitted:
//
//   - EmitDebugMessage mirrors a real conversation message
//     (`role`, `content`) with `hidden: true` and a `kind` discriminator.
//     Use this when surfacing an internally-injected message the LLM
//     received - e.g. a hidden user-role system-reminder turn.
//
//   - EmitDebugEvent emits an operational lifecycle event keyed by
//     `type`, not `role`. Use this for things that aren't conversation
//     turns at all - e.g. "auto-compaction crossed the token threshold".
package streamevent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	zapcore "go.uber.org/zap/zapcore"

	logger "github.com/inference-gateway/cli/internal/logger"
)

var (
	writerMu sync.Mutex
	writer   io.Writer = os.Stdout

	gateMu       sync.RWMutex
	gateOverride *bool
)

// SetWriter overrides the destination io.Writer for emitted events and
// returns a restore function. Used by tests; production callers should
// not touch it.
func SetWriter(w io.Writer) func() {
	writerMu.Lock()
	defer writerMu.Unlock()
	prev := writer
	writer = w
	return func() {
		writerMu.Lock()
		defer writerMu.Unlock()
		writer = prev
	}
}

// SetDebugEnabledForTest forces the debug gate to a specific value,
// bypassing the global-logger probe. Returns a restore function. Test-only.
func SetDebugEnabledForTest(enabled bool) func() {
	gateMu.Lock()
	defer gateMu.Unlock()
	prev := gateOverride
	v := enabled
	gateOverride = &v
	return func() {
		gateMu.Lock()
		defer gateMu.Unlock()
		gateOverride = prev
	}
}

// debugEnabled reports whether emission is currently turned on.
//
// In production this defers to the global zap logger's level: if the
// core accepts DebugLevel (i.e. the user passed `--verbose` or set
// `logging.debug=true`), events fire. Otherwise they're silently
// dropped. Tests can pin the gate via SetDebugEnabledForTest.
func debugEnabled() bool {
	gateMu.RLock()
	override := gateOverride
	gateMu.RUnlock()
	if override != nil {
		return *override
	}

	l := logger.GetGlobalLogger()
	if l == nil {
		return false
	}
	return l.Core().Enabled(zapcore.DebugLevel)
}

// EmitDebugMessage writes a JSON-line that mirrors the shape of a normal
// conversation message but flagged `hidden: true` and tagged with a
// `kind` discriminator so observers can distinguish it from real
// conversation turns. Used to surface internally-injected messages (e.g.
// the hidden user-role system-reminder appended every N turns) that the
// user would not otherwise see.
//
// No-op unless debug is enabled (see package docs).
func EmitDebugMessage(role, content, kind string, extra map[string]any) {
	if !debugEnabled() {
		return
	}

	event := make(map[string]any, len(extra)+5)
	event["role"] = role
	event["content"] = content
	event["hidden"] = true
	event["kind"] = kind
	event["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	for k, v := range extra {
		event[k] = v
	}

	writeEvent(event, kind)
}

// EmitDebugEvent writes a JSON-line describing an operational lifecycle
// event keyed by `type`. Used for things that are NOT conversation
// messages (e.g. compaction triggers, token-threshold crossings).
//
// No-op unless debug is enabled (see package docs).
func EmitDebugEvent(eventType string, fields map[string]any) {
	if !debugEnabled() {
		return
	}

	event := make(map[string]any, len(fields)+2)
	event["type"] = eventType
	event["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	for k, v := range fields {
		event[k] = v
	}

	writeEvent(event, eventType)
}

// writeEvent serializes event to JSON and writes one newline-terminated
// line to the configured writer. Failures are logged and swallowed.
func writeEvent(event map[string]any, label string) {
	out, err := json.Marshal(event)
	if err != nil {
		logger.Error("Failed to marshal stream event", "label", label, "error", err)
		return
	}

	writerMu.Lock()
	w := writer
	writerMu.Unlock()

	if _, werr := fmt.Fprintln(w, string(out)); werr != nil {
		logger.Error("Failed to write stream event", "label", label, "error", werr)
	}
}
