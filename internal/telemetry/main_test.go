package telemetry

import (
	"os"
	"testing"
)

// TestMain scrubs trace-context and OTLP env vars inherited from a parent
// infer run (ChildEnv injects them into every subprocess, including go test),
// so fixture spans stay local instead of exporting into the live trace.
func TestMain(m *testing.M) {
	for _, k := range []string{
		"TRACEPARENT",
		"TRACESTATE",
		"BAGGAGE",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_SERVICE_NAME",
		"OTEL_RESOURCE_ATTRIBUTES",
		"A2A_TELEMETRY_ATTR_SESSION_ID_KEY",
		"A2A_TELEMETRY_ATTR_TOOL_CALL_ID_KEY",
	} {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
