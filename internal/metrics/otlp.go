package metrics

import (
	"context"
	"time"

	attribute "go.opentelemetry.io/otel/attribute"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	metric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	resource "go.opentelemetry.io/otel/sdk/resource"
)

// otlpSink maps the recorder's event stream onto OpenTelemetry GenAI/infer.*
// instruments and pushes them to a collector via a periodic reader. The
// instrument names, attributes, and units match the gateway's OTLP ingest and
// infer-action's exporter so the CLI's metrics line up with existing dashboards.
type otlpSink struct {
	provider *sdkmetric.MeterProvider
	cost     CostFunc

	tokenUsage   metric.Int64Histogram   // gen_ai.client.token.usage
	toolDuration metric.Float64Histogram // gen_ai.execute_tool.duration
	toolCalls    metric.Int64Counter     // infer.agent.tool.calls
	runs         metric.Int64Counter     // infer.agent.runs
	runDuration  metric.Float64Histogram // infer.agent.run.duration
	costCounter  metric.Float64Counter   // infer.client.cost
}

// newOTLPSink builds the HTTP exporter + periodic reader and the sink. It does
// not connect (the HTTP exporter dials lazily on first export), so an
// unreachable collector fails later at export/shutdown - logged and dropped -
// never here.
func newOTLPSink(endpoint string, opts Options) (*otlpSink, error) {
	exporter, err := otlpmetrichttp.New(context.Background(),
		otlpmetrichttp.WithEndpointURL(endpoint),
		otlpmetrichttp.WithHeaders(opts.OTLPHeaders),
	)
	if err != nil {
		return nil, err
	}

	interval := opts.OTLPInterval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(interval))
	return newOTLPSinkWithReader(reader, opts.Cost, opts.ServiceVersion)
}

// newOTLPSinkWithReader wires a sink to any reader - the periodic OTLP reader in
// production, a ManualReader in tests that assert the metric mapping.
func newOTLPSinkWithReader(reader sdkmetric.Reader, cost CostFunc, version string) (*otlpSink, error) {
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(newResource(version)),
	)
	o := &otlpSink{provider: provider, cost: cost}
	if err := o.initInstruments(provider.Meter("infer-cli")); err != nil {
		_ = provider.Shutdown(context.Background())
		return nil, err
	}
	return o, nil
}

func newResource(version string) *resource.Resource {
	attrs := []attribute.KeyValue{attribute.String("service.name", "infer")}
	if version != "" {
		attrs = append(attrs, attribute.String("service.version", version))
	}
	return resource.NewSchemaless(attrs...)
}

func (o *otlpSink) initInstruments(meter metric.Meter) error {
	var err error
	if o.tokenUsage, err = meter.Int64Histogram("gen_ai.client.token.usage",
		metric.WithDescription("Number of input and output tokens used per operation"),
		metric.WithUnit("{token}")); err != nil {
		return err
	}
	if o.toolDuration, err = meter.Float64Histogram("gen_ai.execute_tool.duration",
		metric.WithDescription("Tool execution duration"),
		metric.WithUnit("s")); err != nil {
		return err
	}
	if o.toolCalls, err = meter.Int64Counter("infer.agent.tool.calls",
		metric.WithDescription("Number of tool calls by outcome"),
		metric.WithUnit("{call}")); err != nil {
		return err
	}
	if o.runs, err = meter.Int64Counter("infer.agent.runs",
		metric.WithDescription("Number of agent sessions by outcome"),
		metric.WithUnit("{run}")); err != nil {
		return err
	}
	if o.runDuration, err = meter.Float64Histogram("infer.agent.run.duration",
		metric.WithDescription("Agent session duration"),
		metric.WithUnit("s")); err != nil {
		return err
	}
	o.costCounter, err = meter.Float64Counter("infer.client.cost",
		metric.WithDescription("Estimated request cost in USD"),
		metric.WithUnit("USD"))
	return err
}

func (o *otlpSink) record(e event) {
	ctx := context.Background()
	switch e.Kind {
	case KindUsage:
		o.recordUsage(ctx, e)
	case KindTool:
		o.recordTool(ctx, e)
	case KindSession:
		if e.Phase == phaseEnd {
			o.recordSession(ctx, e)
		}
	}
}

// recordUsage emits gen_ai.client.token.usage (one datapoint per token type,
// matching semconv) plus the derived infer.client.cost split.
func (o *otlpSink) recordUsage(ctx context.Context, e event) {
	base := []attribute.KeyValue{
		attribute.String("gen_ai.request.model", e.Model),
		attribute.String("gen_ai.provider.name", providerFromModel(e.Model)),
		attribute.String("gen_ai.operation.name", "chat"),
	}
	o.tokenUsage.Record(ctx, int64(e.Prompt), metric.WithAttributes(append(base, attribute.String("gen_ai.token.type", "input"))...))
	o.tokenUsage.Record(ctx, int64(e.Completion), metric.WithAttributes(append(base, attribute.String("gen_ai.token.type", "output"))...))

	if o.cost != nil {
		in, out, _ := o.cost(e.Model, e.Prompt, e.Completion)
		model := attribute.String("gen_ai.request.model", e.Model)
		o.costCounter.Add(ctx, in, metric.WithAttributes(model, attribute.String("infer.cost.type", "input")))
		o.costCounter.Add(ctx, out, metric.WithAttributes(model, attribute.String("infer.cost.type", "output")))
	}
}

// recordTool emits infer.agent.tool.calls (counter) and gen_ai.execute_tool.duration.
func (o *otlpSink) recordTool(ctx context.Context, e event) {
	callAttrs := []attribute.KeyValue{
		attribute.String("gen_ai.tool.name", e.Tool),
		attribute.String("infer.tool.outcome", e.Outcome),
	}
	durAttrs := []attribute.KeyValue{
		attribute.String("gen_ai.tool.name", e.Tool),
		attribute.String("gen_ai.tool.type", "function"),
	}
	if e.Err != "" {
		errType := attribute.String("error.type", e.Err)
		callAttrs = append(callAttrs, errType)
		durAttrs = append(durAttrs, errType)
	}
	o.toolCalls.Add(ctx, 1, metric.WithAttributes(callAttrs...))
	o.toolDuration.Record(ctx, secondsFromMs(e.DurMs), metric.WithAttributes(durAttrs...))
}

// recordSession emits infer.agent.runs (counter) and infer.agent.run.duration.
func (o *otlpSink) recordSession(ctx context.Context, e event) {
	o.runs.Add(ctx, 1, metric.WithAttributes(
		attribute.String("infer.run.outcome", e.Outcome),
		attribute.String("infer.agent.mode", e.Mode),
	))
	o.runDuration.Record(ctx, secondsFromMs(e.DurMs),
		metric.WithAttributes(attribute.String("infer.run.outcome", e.Outcome)))
}

func (o *otlpSink) shutdown(ctx context.Context) error {
	return o.provider.Shutdown(ctx)
}

func secondsFromMs(ms int64) float64 {
	return float64(ms) / 1000.0
}
