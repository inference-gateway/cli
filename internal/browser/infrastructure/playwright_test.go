package infrastructure

import (
	"fmt"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestBrowserSessionEventBuffer(t *testing.T) {
	session := NewSession(config.DefaultBrowserUseConfig())

	for i := range maxBrowserEvents + 10 {
		session.recordEvent(fmt.Sprintf("event %d", i))
	}
	events := session.DrainEvents()
	if len(events) != maxBrowserEvents {
		t.Fatalf("Expected %d buffered events, got %d", maxBrowserEvents, len(events))
	}
	if events[0] != "event 10" {
		t.Errorf("Expected oldest events dropped, first is %q", events[0])
	}
	if len(session.DrainEvents()) != 0 {
		t.Error("Expected buffer cleared after drain")
	}
}

// TestBrowserReadRedaction locks the security invariant: a sensitive input's
// value is never returned to the LLM. It exercises the real Go redaction path
// (extractReadContent + isSensitiveField) against the exact JSON payload the
// in-page reader (browserReadJS) emits, so no browser launch is needed.
func TestBrowserReadRedaction(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"password field", `{"field":true,"type":"password","name":"pw","value":"hunter2"}`, "[redacted]"},
		{"current-password autocomplete", `{"field":true,"type":"text","autocomplete":"current-password","value":"hunter2"}`, "[redacted]"},
		{"one-time code", `{"field":true,"type":"text","autocomplete":"one-time-code","value":"123456"}`, "[redacted]"},
		{"secret-ish name", `{"field":true,"type":"text","name":"api_token","value":"sk-abc"}`, "[redacted]"},
		{"cvc field", `{"field":true,"type":"text","id":"cardCvc","value":"999"}`, "[redacted]"},
		{"plain search box", `{"field":true,"type":"text","name":"q","value":"cats"}`, "cats"},
		{"container text", `{"field":false,"text":"hello world"}`, "hello world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractReadContent(tt.payload)
			if got != tt.want {
				t.Fatalf("extractReadContent(%s) = %q, want %q", tt.payload, got, tt.want)
			}
			if strings.Contains(got, "hunter2") {
				t.Fatalf("leaked secret value: %q", got)
			}
		})
	}
}
