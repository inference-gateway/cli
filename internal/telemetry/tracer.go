package telemetry

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	otlptracehttp "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	stdouttrace "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	resource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	trace "go.opentelemetry.io/otel/trace"
	noop "go.opentelemetry.io/otel/trace/noop"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// newTracerProvider creates a TracerProvider sharing the resource and OTLP
// config with the meter provider. Local file export is always attempted; OTLP
// export is added when an endpoint is configured. Sink failures are logged
// and dropped; returns (nil, nil) when no sink could be created.
func newTracerProvider(res *resource.Resource, dir, session, endpoint string, headers map[string]string, interval time.Duration) (*sdktrace.TracerProvider, *os.File, *lockedWriter) {
	var spanProcessors []sdktrace.SpanProcessor

	file, writer, fileProc, err := newTraceFileProcessor(dir, session)
	if err != nil {
		logger.Warn("telemetry: local trace file disabled", "error", err)
	} else {
		spanProcessors = append(spanProcessors, fileProc)
	}

	if otlpEnabled(endpoint, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") {
		if p, err := newOTLPTraceProcessor(endpoint, headers, interval); err != nil {
			logger.Warn("telemetry: OTLP trace export disabled", "error", err)
		} else {
			spanProcessors = append(spanProcessors, p)
		}
	}

	if len(spanProcessors) == 0 {
		if file != nil {
			_ = file.Close()
		}
		return nil, nil, nil
	}

	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	for _, p := range spanProcessors {
		opts = append(opts, sdktrace.WithSpanProcessor(p))
	}
	return sdktrace.NewTracerProvider(opts...), file, writer
}

// lockedWriter serializes trace-file writes between the stdouttrace exporter
// and the OTLP receiver's appends
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// newTraceFileProcessor writes spans as OTLP/semconv JSON to a per-session
// file, synchronously (tiny volume; spans survive an abrupt exit).
func newTraceFileProcessor(dir, session string) (*os.File, *lockedWriter, sdktrace.SpanProcessor, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, session+"-traces.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, nil, err
	}
	w := &lockedWriter{w: f}
	exp, err := stdouttrace.New(stdouttrace.WithWriter(w))
	if err != nil {
		_ = f.Close()
		return nil, nil, nil, err
	}
	return f, w, sdktrace.NewSimpleSpanProcessor(exp), nil
}

// newOTLPTraceProcessor creates a batch span processor exporting over
// OTLP/HTTP. A configured endpoint takes precedence; when empty, the
// exporter's native spec-compliant env handling applies
// (OTEL_EXPORTER_OTLP_TRACES_ENDPOINT over OTEL_EXPORTER_OTLP_ENDPOINT,
// per-signal path appending, headers, timeouts).
func newOTLPTraceProcessor(endpoint string, headers map[string]string, interval time.Duration) (sdktrace.SpanProcessor, error) {
	var opts []otlptracehttp.Option
	if host, insecure, ok := baseEndpoint(endpoint); ok {
		opts = append(opts, otlptracehttp.WithEndpoint(host))
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
	} else if endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpointURL(endpoint))
	}
	if len(headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(headers))
	}
	exp, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, err
	}
	return sdktrace.NewBatchSpanProcessor(exp, sdktrace.WithBatchTimeout(interval)), nil
}

// Tracer returns the tracer for the CLI's instrumentation scope. Safe on nil
// (returns a no-op tracer).
func (r *Recorder) Tracer() trace.Tracer {
	if r == nil || r.tracerProvider == nil {
		return noop.NewTracerProvider().Tracer("infer-cli")
	}
	return r.tracerProvider.Tracer("infer-cli")
}
