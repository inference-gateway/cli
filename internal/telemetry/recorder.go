// Package telemetry records and exports the CLI's OpenTelemetry metrics and
// traces.
//
// Metrics: tool outcomes, token usage, and sessions, recorded into OTel SDK
// instruments named per the GenAI semantic conventions and infer-action's
// exporter, so they line up with the gateway's OTLP ingest and existing
// dashboards.
//
// Traces: one root span per session, child spans for each LLM turn and each
// tool call. No prompt/response content is recorded.
//
// Both signals share the same resource and OTLP endpoint/headers config.
// Local file export is always attempted; OTLP/HTTP export is opt-in via an
// endpoint (config or OTEL_EXPORTER_OTLP_ENDPOINT). Metrics use delta
// temporality (required by the gateway ingest, and what makes the local
// files trivially summable by `infer stats`).
package telemetry

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	attribute "go.opentelemetry.io/otel/attribute"
	codes "go.opentelemetry.io/otel/codes"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	stdoutmetric "go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	metric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	metricdata "go.opentelemetry.io/otel/sdk/metric/metricdata"
	resource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	trace "go.opentelemetry.io/otel/trace"
	noop "go.opentelemetry.io/otel/trace/noop"

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
	ExecDaemon      = "daemon"
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
	Enabled         bool
	Dir             string
	SessionID       string
	OTLPEndpoint    string
	OTLPHeaders     map[string]string
	OTLPInterval    time.Duration
	ReceiverAddress string
	Cost            CostFunc
	// AttrSessionIDKey / AttrToolCallIDKey override the baggage member names;
	// empty falls back to the OTel semconv defaults.
	AttrSessionIDKey  string
	AttrToolCallIDKey string
}

// Recorder maps recorded events onto OTel instruments and spans. A nil
// *Recorder is a valid no-op, so callers guard hot paths with `if rec != nil`
// and the container skips wrapping when disabled.
type Recorder struct {
	// Meter provider and instruments (metrics)
	provider  *sdkmetric.MeterProvider
	file      *os.File // local metric stdout-exporter target; closed on Shutdown
	cost      CostFunc
	sessionID string // gen_ai.conversation.id on session and turn spans

	tokenUsage   metric.Int64Histogram   // gen_ai.client.token.usage
	toolDuration metric.Float64Histogram // gen_ai.execute_tool.duration
	toolCalls    metric.Int64Counter     // infer.agent.tool.calls
	runs         metric.Int64Counter     // infer.agent.runs
	runDuration  metric.Float64Histogram // infer.agent.run.duration
	costCounter  metric.Float64Counter   // infer.client.cost

	// Tracer provider (traces)
	tracerProvider *sdktrace.TracerProvider
	traceFile      *os.File      // local trace stdout-exporter target
	traceWriter    *lockedWriter // serialized writer shared with the OTLP receiver

	otlpEndpoint string
	otlpHeaders  map[string]string

	attrSessionIDKey  string
	attrToolCallIDKey string

	recvOnce  sync.Once
	recvSrv   *http.Server
	recvURL   string
	recvSpans atomic.Int64

	// sessionCtx carries the root span from StartSession so SpanContext can
	// graft it onto request contexts that don't descend from session start.
	sessionCtx atomic.Pointer[context.Context]

	// conversationID, when set, is stamped as gen_ai.conversation.id on every
	// metric datapoint so channel /stats can aggregate a single conversation.
	conversationID atomic.Pointer[string]
}

// SetConversationID tags subsequent metric datapoints with the given
// conversation id (gen_ai.conversation.id) so aggregation can scope to it.
// No-op on a nil recorder or empty id.
func (r *Recorder) SetConversationID(id string) {
	if r == nil || id == "" {
		return
	}
	r.conversationID.Store(&id)
}

// withConv appends gen_ai.conversation.id to attrs when a conversation id is set.
func (r *Recorder) withConv(attrs []attribute.KeyValue) []attribute.KeyValue {
	if id := r.conversationID.Load(); id != nil {
		return append(attrs, attribute.String("gen_ai.conversation.id", *id))
	}
	return attrs
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

	if otlpEnabled(opts.OTLPEndpoint, "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") {
		if r, err := newOTLPReader(opts.OTLPEndpoint, opts.OTLPHeaders, interval); err != nil {
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

	res := newResource()

	mpOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	for _, r := range readers {
		mpOpts = append(mpOpts, sdkmetric.WithReader(r))
	}
	provider := sdkmetric.NewMeterProvider(mpOpts...)

	traceProvider, traceFile, traceWriter := newTracerProvider(res, opts.Dir, opts.SessionID, opts.OTLPEndpoint, opts.OTLPHeaders, interval)

	rec := &Recorder{
		provider:       provider,
		file:           file,
		cost:           opts.Cost,
		sessionID:      opts.SessionID,
		tracerProvider: traceProvider,
		traceFile:      traceFile,
		traceWriter:    traceWriter,
		otlpEndpoint:   opts.OTLPEndpoint,
		otlpHeaders:    opts.OTLPHeaders,

		attrSessionIDKey:  cmp.Or(opts.AttrSessionIDKey, defaultAttrSessionIDKey),
		attrToolCallIDKey: cmp.Or(opts.AttrToolCallIDKey, defaultAttrToolCallIDKey),
	}

	if err := rec.initInstruments(provider.Meter("infer-cli")); err != nil {
		logger.Warn("telemetry: instrument init failed", "error", err)
		rec.Shutdown(context.Background())
		return nil
	}
	if opts.ReceiverAddress != "" {
		rec.startReceiver(opts.ReceiverAddress)
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

// newOTLPReader builds the remote metric exporter. A configured endpoint takes
// precedence; when empty, the exporter's native spec-compliant env handling
// applies (OTEL_EXPORTER_OTLP_METRICS_ENDPOINT over OTEL_EXPORTER_OTLP_ENDPOINT,
// per-signal path appending, headers, timeouts).
func newOTLPReader(endpoint string, headers map[string]string, interval time.Duration) (sdkmetric.Reader, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithTemporalitySelector(deltaTemporality),
	}
	if host, insecure, ok := baseEndpoint(endpoint); ok {
		opts = append(opts, otlpmetrichttp.WithEndpoint(host))
		if insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
	} else if endpoint != "" {
		opts = append(opts, otlpmetrichttp.WithEndpointURL(endpoint))
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

// otlpEnabled reports whether remote OTLP export is on for a signal: an
// explicit config endpoint, the generic OTEL_EXPORTER_OTLP_ENDPOINT, or the
// signal-specific env var (which the exporter resolves itself, per spec).
func otlpEnabled(configured, signalEnvVar string) bool {
	return configured != "" || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv(signalEnvVar) != ""
}

// baseEndpoint splits a path-less endpoint URL ("https://collector:4318") into
// host and scheme so the exporter appends its own per-signal path (/v1/traces,
// /v1/metrics) per the OTLP spec. A URL with an explicit path is left to
// WithEndpointURL verbatim.
func baseEndpoint(endpoint string) (host string, insecure, ok bool) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return "", false, false
	}
	return u.Host, u.Scheme == "http", true
}

// newResource stamps the CLI's identity onto every signal: the SDK defaults
// (telemetry.sdk.*, required by the resource semconv), then our identity, then
// the standard OTel env vars (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES) on
// top so callers - CI, infer-action, an operator - can add or override
// attributes without a code change. Later wins on conflicts.
func newResource() *resource.Resource {
	base := resource.NewSchemaless(
		attribute.String("service.name", "infer"),
		attribute.String("service.version", Version),
		attribute.String("infer.execution.mode", ExecutionMode),
	)
	if merged, err := resource.Merge(resource.Default(), base); err == nil && merged != nil {
		base = merged
	}
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
	r.tokenUsage.Record(ctx, int64(prompt), metric.WithAttributes(r.withConv(append(base, attribute.String("gen_ai.token.type", "input")))...))
	r.tokenUsage.Record(ctx, int64(completion), metric.WithAttributes(r.withConv(append(base, attribute.String("gen_ai.token.type", "output")))...))

	if r.cost != nil {
		in, out, _ := r.cost(model, prompt, completion)
		m := attribute.String("gen_ai.request.model", model)
		r.costCounter.Add(ctx, in, metric.WithAttributes(r.withConv([]attribute.KeyValue{m, attribute.String("infer.cost.type", "input")})...))
		r.costCounter.Add(ctx, out, metric.WithAttributes(r.withConv([]attribute.KeyValue{m, attribute.String("infer.cost.type", "output")})...))
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
	r.toolCalls.Add(ctx, 1, metric.WithAttributes(r.withConv(callAttrs)...))
	r.toolDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(r.withConv(durAttrs)...))
}

// RecordSession records one completed agent session (infer.agent.runs +
// infer.agent.run.duration). outcome is one of RunSuccess/RunFailed/RunStoppedEarly.
func (r *Recorder) RecordSession(mode, outcome string, dur time.Duration) {
	if r == nil {
		return
	}
	ctx := context.Background()
	r.runs.Add(ctx, 1, metric.WithAttributes(r.withConv([]attribute.KeyValue{
		attribute.String("infer.run.outcome", outcome),
		attribute.String("infer.agent.mode", mode),
	})...))
	r.runDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(r.withConv([]attribute.KeyValue{attribute.String("infer.run.outcome", outcome)})...))
}

// Meter returns the meter from the provider, or nil when the recorder is nil
// or the provider is nil. Used by subsystems (e.g. channels-manager) to
// register their own instruments on the shared meter provider.
func (r *Recorder) Meter() metric.Meter {
	if r == nil || r.provider == nil {
		return nil
	}
	return r.provider.Meter("infer-cli")
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
}

// Shutdown flushes the final export and releases resources for both OTel
// signals. Safe on nil.
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
	if r.recvSrv != nil {
		_ = r.recvSrv.Close()
	}
	if r.file != nil {
		_ = r.file.Close()
	}
	if r.traceFile != nil {
		_ = r.traceFile.Close()
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

// StartSession begins the session root span. The returned end function
// stamps infer.run.outcome and ends the span. Safe on nil.
func (r *Recorder) StartSession(mode string) func(outcome string) {
	if r == nil || r.tracerProvider == nil {
		return func(string) {}
	}
	ctx, span := r.Tracer().Start(propagator.Extract(context.Background(), envCarrier{}), "session",
		trace.WithAttributes(
			attribute.String("infer.execution.mode", ExecutionMode),
			attribute.String("infer.agent.mode", mode),
			attribute.String("gen_ai.conversation.id", r.sessionID),
		),
	)
	r.sessionCtx.Store(&ctx)
	return func(outcome string) {
		span.SetAttributes(attribute.String("infer.run.outcome", outcome))
		if outcome == RunFailed {
			span.SetStatus(codes.Error, outcome)
		}
		span.End()
	}
}

// SpanContext grafts the session root span onto ctx so spans created from
// the returned context parent to it. Safe on nil.
func (r *Recorder) SpanContext(ctx context.Context) context.Context {
	if r == nil {
		return ctx
	}
	if p := r.sessionCtx.Load(); p != nil {
		return trace.ContextWithSpan(ctx, trace.SpanFromContext(*p))
	}
	return ctx
}

// StartLLMTurnSpan creates a span for one LLM request with GenAI semconv
// attributes and CLIENT kind (a remote call to the gateway). Safe on nil
// (returns ctx unchanged and a no-op span).
func (r *Recorder) StartLLMTurnSpan(ctx context.Context, model string) (context.Context, trace.Span) {
	if r == nil {
		return ctx, noop.Span{}
	}
	return r.Tracer().Start(ctx, "chat "+model,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("gen_ai.request.model", model),
			attribute.String("gen_ai.provider.name", providerFromModel(model)),
			attribute.String("gen_ai.operation.name", "chat"),
			attribute.String("gen_ai.conversation.id", r.sessionID),
		),
	)
}

// SetSpanUsage stamps token usage (gen_ai.usage.*) onto the span in ctx.
func SetSpanUsage(ctx context.Context, inputTokens, outputTokens int) {
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int("gen_ai.usage.input_tokens", inputTokens),
		attribute.Int("gen_ai.usage.output_tokens", outputTokens),
	)
}

// SetSpanError marks the span in ctx failed: error.type attribute, recorded
// error event, and Error status, per the semconv recording-errors rules.
func SetSpanError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("error.type", "_OTHER"))
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
