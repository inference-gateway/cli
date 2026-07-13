package telemetry

import (
	"context"
	"os"
	"path/filepath"
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
// export is added when an endpoint is configured. Returns (nil, nil, nil) when
// no sink could be created.
func newTracerProvider(res *resource.Resource, dir, session, endpoint string, headers map[string]string, interval time.Duration) (*sdktrace.TracerProvider, *os.File, error) {
	var spanProcessors []sdktrace.SpanProcessor

	file, fileProc, err := newTraceFileProcessor(dir, session)
	if err != nil {
		logger.Warn("telemetry: local trace file disabled", "error", err)
	} else {
		spanProcessors = append(spanProcessors, fileProc)
	}

	if ep := resolveOTLPEndpoint(endpoint); ep != "" {
		if p, err := newOTLPTraceProcessor(ep, headers, interval); err != nil {
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
	provider := sdktrace.NewTracerProvider(opts...)

	return provider, file, nil
}

// newTraceFileProcessor creates a local file exporter and a simple span
// processor that writes spans as OTLP/semconv JSON to a per-session file.
func newTraceFileProcessor(dir, session string) (*os.File, sdktrace.SpanProcessor, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, session+"-traces.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	exp, err := stdouttrace.New(stdouttrace.WithWriter(f))
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, sdktrace.NewSimpleSpanProcessor(exp), nil
}

// newOTLPTraceProcessor creates a batch span processor that exports spans
// over OTLP/HTTP to the configured endpoint.
func newOTLPTraceProcessor(endpoint string, headers map[string]string, interval time.Duration) (sdktrace.SpanProcessor, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(endpoint),
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

// Tracer returns the tracer for the CLI's instrumentation scope. Returns a
// no-op tracer when the Recorder is nil or has no tracer provider.
func (r *Recorder) Tracer() trace.Tracer {
	if r == nil || r.tracerProvider == nil {
		return noop.NewTracerProvider().Tracer("infer-cli")
	}
	return r.tracerProvider.Tracer("infer-cli")
}
