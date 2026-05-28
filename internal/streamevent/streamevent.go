// Package streamevent emits structured JSON-line events on stdout
// describing internal agent lifecycle moments that are otherwise invisible
// to outside observers - system reminder injection, conversation
// compaction triggers, and similar checkpoints. Downstream tooling (the
// inference-gateway/infer-action runner, log scrapers, etc.) can react to
// or surface these events without scraping free-form log text.
//
// Events always carry a `role` field so they flow through consumers that
// discriminate the agent's JSON-line stream by role. Existing roles
// ("assistant", "user", "tool", "system") are reserved for conversation
// messages; the roles emitted here ("system_reminder",
// "compaction_started", "compaction_completed", ...) describe metadata
// about the run rather than turns in the conversation.
package streamevent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	logger "github.com/inference-gateway/cli/internal/logger"
)

var (
	writerMu sync.Mutex
	writer   io.Writer = os.Stdout
)

// SetWriter overrides the destination io.Writer for stream events and
// returns a restore function. Intended for tests that need to capture
// emitted events; production callers should not call this.
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

// Emit writes a single JSON line to the configured writer. The output
// object carries `role`, an RFC3339Nano `timestamp`, and any caller-
// supplied `fields` merged in. Caller-supplied keys that collide with
// `role` or `timestamp` win, so tests can pin a deterministic timestamp.
//
// Marshal or write failures are logged and swallowed: stream events are
// best-effort observability and must never break the agent.
func Emit(role string, fields map[string]any) {
	event := make(map[string]any, len(fields)+2)
	event["role"] = role
	event["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	for k, v := range fields {
		event[k] = v
	}

	out, err := json.Marshal(event)
	if err != nil {
		logger.Error("Failed to marshal stream event", "role", role, "error", err)
		return
	}

	writerMu.Lock()
	w := writer
	writerMu.Unlock()

	if _, werr := fmt.Fprintln(w, string(out)); werr != nil {
		logger.Error("Failed to write stream event", "role", role, "error", werr)
	}
}
