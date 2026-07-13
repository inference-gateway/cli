package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"time"

	otlploghttp "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	stdoutlog "go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// newLoggerProvider creates a LoggerProvider sharing the resource and OTLP
// config with the meter and tracer providers. Local file export is always
// attempted; OTLP export is added when an endpoint is configured. Returns
// (nil, nil, nil) when no sink could be created.
func newLoggerProvider(res *resource.Resource, dir, session, endpoint string, headers map[string]string, interval time.Duration) (*sdklog.LoggerProvider, *os.File, error) {
	var processors []sdklog.Processor

	file, fileProc, err := newLogFileProcessor(dir, session)
	if err != nil {
		logger.Warn("telemetry: local log file disabled", "error", err)
	} else {
		processors = append(processors, fileProc)
	}

	if ep := resolveOTLPEndpoint(endpoint); ep != "" {
		if p, err := newOTLPLogProcessor(ep, headers, interval); err != nil {
			logger.Warn("telemetry: OTLP log export disabled", "error", err)
		} else {
			processors = append(processors, p)
		}
	}

	if len(processors) == 0 {
		if file != nil {
			_ = file.Close()
		}
		return nil, nil, nil
	}

	opts := []sdklog.LoggerProviderOption{sdklog.WithResource(res)}
	for _, p := range processors {
		opts = append(opts, sdklog.WithProcessor(p))
	}
	provider := sdklog.NewLoggerProvider(opts...)

	return provider, file, nil
}

// newLogFileProcessor creates a local file exporter and a simple processor
// that writes log records as OTLP/semconv JSON to a per-session file.
func newLogFileProcessor(dir, session string) (*os.File, sdklog.Processor, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, session+"-logs.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	exp, err := stdoutlog.New(stdoutlog.WithWriter(f))
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, sdklog.NewSimpleProcessor(exp), nil
}

// newOTLPLogProcessor creates a batch processor that exports log records
// over OTLP/HTTP to the configured endpoint.
func newOTLPLogProcessor(endpoint string, headers map[string]string, interval time.Duration) (sdklog.Processor, error) {
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpointURL(endpoint),
	}
	if len(headers) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(headers))
	}
	exp, err := otlploghttp.New(context.Background(), opts...)
	if err != nil {
		return nil, err
	}
	return sdklog.NewBatchProcessor(exp, sdklog.WithExportInterval(interval)), nil
}

// Logger returns the logger for the CLI's instrumentation scope. Returns a
// no-op logger when the Recorder is nil or has no logger provider.
func (r *Recorder) Logger() log.Logger {
	if r == nil || r.loggerProvider == nil {
		return noop.NewLoggerProvider().Logger("infer-cli")
	}
	return r.loggerProvider.Logger("infer-cli")
}
