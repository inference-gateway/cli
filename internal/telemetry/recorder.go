// Package telemetry records the CLI's OpenTelemetry signals - metrics, traces,
// and logs - and exports them. Named "telemetry" (not "metrics") to cover all
// three OTel signals.
//
// Metrics: tool outcomes, token usage, and sessions. Recorded into OTel SDK
// instruments named per the GenAI semantic conventions and infer-action's
// exporter, so they line up with the gateway's OTLP ingest and existing
// dashboards.
//
// Traces: one root span per session, child spans for each LLM turn and each
// tool call, so latency and failures attribute to a specific step.
//
// Logs: structured logs emitted through the OTel logs signal, correlated to
// the active trace/span.
//
// All three signals share the same resource and OTLP endpoint/headers config.
// Local file export is always attempted; OTLP/HTTP export is opt-in via
// endpoint configuration or OTEL_EXPORTER_OTLP_ENDPOINT.
//
// Metrics use delta temporality (what the gateway ingest requires, and what
// makes the local files trivially summable by `infer stats`).
package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	otel "go.opentelemetry.io/otel"
	attribute "go.opentelemetry.io/otel/attribute"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	stdoutmetric "go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	metric "go.opentelemetry.io/otel/metric"
	propagation "go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	metricdata "go.opentelemetry.io/otel/sdk/metric/metricdata"
	resource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	trace "go.opentelemetry.io/otel/trace"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// Process-wide facts stamped onto every metric via the resource. Version is the
// build version; ExecutionMode distinguishes interactive chat from headless
// `infer agent`. cmd sets these before building the service container.
var (
	Version       = "dev"
	ExecutionMode = ExecHeadless
)

// Execution modes (resource attribute infer.execution.mode).
const (
	ExecInteractive = "interactive"
	ExecHeadless    = "headless"
)

// Tool outcomes (attribute infer.tool.outcome; error.type on non-success).
const (
	ToolSuccess  = "success"
	ToolError    = "error"
	ToolRejected = "rejected"

	ErrTypeTool = "tool_error"
)

// Session/run outcomes (attribute infer.run.outcome) - infer-action's enum.
const (
	RunSuccess      = "success"
	RunFailed       = "failed"
	RunStoppedEarly = "stopped_early"
)

// CostFunc returns the input, output, and total cost for a model's token counts
// (wraps domain.PricingService.CalculateCost). Pass nil to skip cost.
type CostFunc func(model string, prompt, completion int) (input, output, total float64)

// Options configures a Recorder. Dir + SessionID locate the per-process local
// file; OTLP* enable the optional remote export.
type Options struct {
	Enabled      bool
	Dir          string
	SessionID    string
	OTLPEndpoint string
	OTLPHeaders  map[string]string
	OTLPInterval time.Duration
	Cost         CostFunc
}

// Recorder maps recorded events onto OTel instruments and manages the three
// OTel signals (metrics, traces, logs). A nil *Recorder is a valid no-op, so
// callers guard hot paths with `if rec != nil` and the container skips wrapping
// when disabled.
type Recorder struct {
	// Meter provider and instruments (metrics)
	provider *sdkmetric.MeterProvider
	file     *os.File // local metric stdout-exporter target; closed on Shutdown
	cost     CostFunc

	tokenUsage   metric.Int64Histogram   // gen_ai.client.token.usage
	toolDuration metric.Float64Histogram // gen_ai.execute_tool.duration
	toolCalls    metric.Int64Counter     // infer.agent.tool.calls
	runs         metric.Int64Counter     // infer.agent.runs
	runDuration  metric.Float64Histogram // infer.agent.run.duration
	costCounter  metric.Float64Counter   // infer.client.cost

	// Tracer provider (traces)
	tracerProvider *sdktrace.TracerProvider
	traceFile      *os.File // local trace stdout-exporter target

	// Logger provider (logs)
	loggerProvider *sdklog.LoggerProvider
	logFile        *os.File // local log stdout-exporter target
}

func deltaTemporality(sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.DeltaTemporality
}

// New builds a Recorder, or nil when disabled or no sink could be created. The
// local file sink is always attempted; the OTLP sink is added when an endpoint
// is configured (or OTEL_EXPORTER_OTLP_ENDPOINT is set). Sink failures are
// logged and dropped so telemetry never breaks a run.
func New(opts Options) *Recorder {
	if !opts.Enabled {
		return nil
	}

	interval := opts.OTLPInterval
	if interval <= 0 {
		interval = 60 * time.Second
	}

	var readers []sdkmetric.Reader

	file, fileReader, err := newFileReader(opts.Dir, opts.SessionID, interval)
	if err != nil {
		logger.Warn("telemetry: local file sink disabled", "error", err)
	} else {
		readers = append(readers, fileReader)
	}

	if endpoint := resolveOTLPEndpoint(opts.OTLPEndpoint); endpoint != "" {
		if r, err := newOTLPReader(endpoint, opts.OTLPHeaders, interval); err != nil {
			logger.Warn("telemetry: OTLP export disabled", "error", err)
		} else {
			readers = append(readers, r)
		}
	}

	if len(readers) == 0 {
		if file != nil {
			_ = file.Close()
		}
		return nil
	}

	mpOpts := []sdkmetric.Option{sdkmetric.WithResource(newResource())}
	for _, r := range readers {
		mpOpts = append(mpOpts, sdkmetric.WithReader(r))
	}
	provider := sdkmetric.NewMeterProvider(mpOpts...)

	res := newResource()
	traceProvider, traceFile, _ := newTracerProvider(res, opts.Dir, opts.SessionID, opts.OTLPEndpoint, opts.OTLPHeaders, interval)
	logProvider, logFile, _ := newLoggerProvider(res, opts.Dir, opts.SessionID, opts.OTLPEndpoint, opts.OTLPHeaders, interval)

	rec := &Recorder{
		provider:       provider,
		file:           file,
		cost:           opts.Cost,
		tracerProvider: traceProvider,
		traceFile:      traceFile,
		loggerProvider: logProvider,
		logFile:        logFile,
	}

	// Set the global W3C trace-context propagator so SDK client requests
	// carry the traceparent header when a span is active.
	if traceProvider != nil {
		otel.SetTextMapPropagator(propagation.TraceContext{})
	}

	if err := rec.initInstruments(provider.Meter("infer-cli")); err != nil {
		logger.Warn("telemetry: instrument init failed", "error", err)
		rec.Shutdown(context.Background())
		return nil
	}
	return rec
}

// newFileReader opens the per-session local file and a periodic reader feeding
// the stdout exporter (OTLP/semconv JSON). Per-session files avoid concurrent
// writes when chat and headless subprocesses run at once.
func newFileReader(dir, session string, interval time.Duration) (*os.File, sdkmetric.Reader, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, session+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	exp, err := stdoutmetric.New(
		stdoutmetric.WithWriter(f),
		stdoutmetric.WithTemporalitySelector(deltaTemporality),
	)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval)), nil
}

func newOTLPReader(endpoint string, headers map[string]string, interval time.Duration) (sdkmetric.Reader, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpointURL(endpoint),
		otlpmetrichttp.WithTemporalitySelector(deltaTemporality),
	}
	if len(headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(headers))
	}
	exp, err := otlpmetrichttp.New(context.Background(), opts...)
	if err != nil {
		return nil, err
	}
	return sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval)), nil
}

// resolveOTLPEndpoint prefers the explicit config value, then the standard OTel
// env var, so `OTEL_EXPORTER_OTLP_ENDPOINT=... infer chat` works.
func resolveOTLPEndpoint(configured string) string {
	if configured != "" {
		return configured
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
}

// newResource stamps the CLI's identity onto every metric, then merges the
// standard OTel env vars (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES) on top so
// callers - CI, infer-action, an operator - can add or override attributes
// (e.g. actor/repo/run id) without a code change. Env wins on conflicts.
func newResource() *resource.Resource {
	base := resource.NewSchemaless(
		attribute.String("service.name", "infer"),
		attribute.String("service.version", Version),
		attribute.String("infer.execution.mode", ExecutionMode),
	)
	env, err := resource.New(context.Background(), resource.WithFromEnv())
	if err != nil || env == nil {
		return base
	}
	merged, err := resource.Merge(base, env)
	if err != nil {
		return base
	}
	return merged
}

func (r *Recorder) initInstruments(meter metric.Meter) error {
	var err error
	if r.tokenUsage, err = meter.Int64Histogram("gen_ai.client.token.usage",
		metric.WithDescription("Number of input and output tokens used per operation"),
		metric.WithUnit("{token}")); err != nil {
		return err
	}
	if r.toolDuration, err = meter.Float64Histogram("gen_ai.execute_tool.duration",
		metric.WithDescription("Tool execution duration"), metric.WithUnit("s")); err != nil {
		return err
	}
	if r.toolCalls, err = meter.Int64Counter("infer.agent.tool.calls",
		metric.WithDescription("Number of tool calls by outcome"), metric.WithUnit("{call}")); err != nil {
		return err
	}
	if r.runs, err = meter.Int64Counter("infer.agent.runs",
		metric.WithDescription("Number of agent sessions by outcome"), metric.WithUnit("{run}")); err != nil {
		return err
	}
	if r.runDuration, err = meter.Float64Histogram("infer.agent.run.duration",
		metric.WithDescription("Agent session duration"), metric.WithUnit("s")); err != nil {
		return err
	}
	r.costCounter, err = meter.Float64Counter("infer.client.cost",
		metric.WithDescription("Estimated request cost in USD"), metric.WithUnit("USD"))
	return err
}

// RecordUsage records one request's token usage (gen_ai.client.token.usage, one
// datapoint per token type) and the derived infer.client.cost split.
func (r *Recorder) RecordUsage(model string, prompt, completion int) {
	if r == nil {
		return
	}
	ctx := context.Background()
	base := []attribute.KeyValue{
		attribute.String("gen_ai.request.model", model),
		attribute.String("gen_ai.provider.name", providerFromModel(model)),
		attribute.String("gen_ai.operation.name", "chat"),
	}
	r.tokenUsage.Record(ctx, int64(prompt), metric.WithAttributes(append(base, attribute.String("gen_ai.token.type", "input"))...))
	r.tokenUsage.Record(ctx, int64(completion), metric.WithAttributes(append(base, attribute.String("gen_ai.token.type", "output"))...))

	if r.cost != nil {
		in, out, _ := r.cost(model, prompt, completion)
		m := attribute.String("gen_ai.request.model", model)
		r.costCounter.Add(ctx, in, metric.WithAttributes(m, attribute.String("infer.cost.type", "input")))
		r.costCounter.Add(ctx, out, metric.WithAttributes(m, attribute.String("infer.cost.type", "output")))
	}
}

// RecordTool records one tool execution (infer.agent.tool.calls + gen_ai.execute_tool.duration).
func (r *Recorder) RecordTool(tool, outcome, errType string, dur time.Duration) {
	if r == nil {
		return
	}
	ctx := context.Background()
	callAttrs := []attribute.KeyValue{
		attribute.String("gen_ai.tool.name", tool),
		attribute.String("infer.tool.outcome", outcome),
	}
	durAttrs := []attribute.KeyValue{
		attribute.String("gen_ai.tool.name", tool),
		attribute.String("gen_ai.tool.type", "function"),
	}
	if errType != "" {
		e := attribute.String("error.type", errType)
		callAttrs = append(callAttrs, e)
		durAttrs = append(durAttrs, e)
	}
	r.toolCalls.Add(ctx, 1, metric.WithAttributes(callAttrs...))
	r.toolDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(durAttrs...))
}

// RecordSession records one completed agent session (infer.agent.runs +
// infer.agent.run.duration). outcome is one of RunSuccess/RunFailed/RunStoppedEarly.
func (r *Recorder) RecordSession(mode, outcome string, dur time.Duration) {
	if r == nil {
		return
	}
	ctx := context.Background()
	r.runs.Add(ctx, 1, metric.WithAttributes(
		attribute.String("infer.run.outcome", outcome),
		attribute.String("infer.agent.mode", mode),
	))
	r.runDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(attribute.String("infer.run.outcome", outcome)))
}

// Flush forces an immediate export of everything recorded so far, without
// tearing down (used by tests and callers that want data on disk now). Safe on nil.
func (r *Recorder) Flush(ctx context.Context) {
	if r == nil {
		return
	}
	if r.provider != nil {
		if err := r.provider.ForceFlush(ctx); err != nil {
			logger.Warn("telemetry: metric flush failed", "error", err)
		}
	}
	if r.tracerProvider != nil {
		if err := r.tracerProvider.ForceFlush(ctx); err != nil {
			logger.Warn("telemetry: trace flush failed", "error", err)
		}
	}
	if r.loggerProvider != nil {
		if err := r.loggerProvider.ForceFlush(ctx); err != nil {
			logger.Warn("telemetry: log flush failed", "error", err)
		}
	}
}

// Shutdown flushes the final export and releases resources for all three
// OTel signals. Safe on nil.
func (r *Recorder) Shutdown(ctx context.Context) {
	if r == nil {
		return
	}
	if r.provider != nil {
		if err := r.provider.Shutdown(ctx); err != nil {
			logger.Warn("telemetry: metric shutdown failed", "error", err)
		}
	}
	if r.tracerProvider != nil {
		if err := r.tracerProvider.Shutdown(ctx); err != nil {
			logger.Warn("telemetry: trace shutdown failed", "error", err)
		}
	}
	if r.loggerProvider != nil {
		if err := r.loggerProvider.Shutdown(ctx); err != nil {
			logger.Warn("telemetry: log shutdown failed", "error", err)
		}
	}
	if r.file != nil {
		_ = r.file.Close()
	}
	if r.traceFile != nil {
		_ = r.traceFile.Close()
	}
	if r.logFile != nil {
		_ = r.logFile.Close()
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

// StartLLMTurnSpan creates a child span for an LLM turn with GenAI semconv
// attributes. Returns a no-op span when the Recorder is nil.
func (r *Recorder) StartLLMTurnSpan(ctx context.Context, model string) (context.Context, trace.Span) {
	if r == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	provider := providerFromModel(model)
	return r.Tracer().Start(ctx, "chat "+model,
		trace.WithAttributes(
			attribute.String("gen_ai.request.model", model),
			attribute.String("gen_ai.provider.name", provider),
			attribute.String("gen_ai.operation.name", "chat"),
		),
	)
}

// StartSessionSpan creates a root span for an agent session. Returns a no-op
// span when the Recorder is nil.
func (r *Recorder) StartSessionSpan(ctx context.Context, mode string) (context.Context, trace.Span) {
	if r == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return r.Tracer().Start(ctx, "session",
		trace.WithAttributes(
			attribute.String("infer.execution.mode", ExecutionMode),
			attribute.String("infer.agent.mode", mode),
		),
	)
}
