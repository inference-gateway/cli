package metrics

import (
	"context"
	"testing"

	attribute "go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	metricdata "go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestOTLPSinkMapping asserts the event -> OTel instrument projection: the exact
// GenAI/infer.* metric names, attributes, and values the gateway ingest and
// infer-action expect. A ManualReader lets us read the SDK's aggregation without
// a network exporter.
func TestOTLPSinkMapping(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	cost := func(_ string, _, _ int) (float64, float64, float64) { return 0.01, 0.02, 0.03 }
	sink, err := newOTLPSinkWithReader(reader, cost, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sink.shutdown(context.Background()) })

	sink.record(event{Kind: KindUsage, Model: "deepseek/deepseek-chat", Prompt: 100, Completion: 42})
	sink.record(event{Kind: KindTool, Tool: "Read", Outcome: ToolError, Err: errTypeTool, DurMs: 12})
	sink.record(event{Kind: KindSession, Phase: phaseEnd, Session: "s1", Mode: "standard", Outcome: RunSuccess, DurMs: 44700})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}

	// gen_ai.client.token.usage: two datapoints, input=100 / output=42.
	tokens := histogramInt(t, rm, "gen_ai.client.token.usage")
	for _, dp := range tokens.DataPoints {
		if got := attrOf(dp.Attributes, "gen_ai.request.model"); got != "deepseek/deepseek-chat" {
			t.Errorf("token model=%q", got)
		}
		if got := attrOf(dp.Attributes, "gen_ai.provider.name"); got != "deepseek" {
			t.Errorf("provider=%q, want deepseek (derived from prefix)", got)
		}
		switch dir := attrOf(dp.Attributes, "gen_ai.token.type"); dir {
		case "input":
			if dp.Sum != 100 {
				t.Errorf("input tokens=%d, want 100", dp.Sum)
			}
		case "output":
			if dp.Sum != 42 {
				t.Errorf("output tokens=%d, want 42", dp.Sum)
			}
		default:
			t.Errorf("unexpected gen_ai.token.type=%q", dir)
		}
	}

	// infer.agent.tool.calls: one call, Read, error, error.type=tool_error.
	calls := sumInt(t, rm, "infer.agent.tool.calls")
	if len(calls.DataPoints) != 1 || calls.DataPoints[0].Value != 1 {
		t.Fatalf("tool.calls datapoints=%+v", calls.DataPoints)
	}
	tc := calls.DataPoints[0].Attributes
	if attrOf(tc, "gen_ai.tool.name") != "Read" || attrOf(tc, "infer.tool.outcome") != "error" || attrOf(tc, "error.type") != "tool_error" {
		t.Errorf("tool.calls attrs wrong: %v", tc.ToSlice())
	}

	// gen_ai.execute_tool.duration exists with gen_ai.tool.type=function.
	dur := histogramFloat(t, rm, "gen_ai.execute_tool.duration")
	if len(dur.DataPoints) != 1 || attrOf(dur.DataPoints[0].Attributes, "gen_ai.tool.type") != "function" {
		t.Errorf("execute_tool.duration attrs wrong: %+v", dur.DataPoints)
	}

	// infer.agent.runs: one run, success, standard.
	runs := sumInt(t, rm, "infer.agent.runs")
	if len(runs.DataPoints) != 1 || runs.DataPoints[0].Value != 1 {
		t.Fatalf("runs datapoints=%+v", runs.DataPoints)
	}
	ra := runs.DataPoints[0].Attributes
	if attrOf(ra, "infer.run.outcome") != "success" || attrOf(ra, "infer.agent.mode") != "standard" {
		t.Errorf("runs attrs wrong: %v", ra.ToSlice())
	}

	// infer.client.cost: input + output datapoints present.
	if _, ok := findMetric(rm, "infer.client.cost"); !ok {
		t.Error("infer.client.cost not emitted")
	}
	if _, ok := findMetric(rm, "infer.agent.run.duration"); !ok {
		t.Error("infer.agent.run.duration not emitted")
	}
}

// TestNewAddsOTLPSinkWhenEndpointSet verifies the additive model: the JSONL sink
// is always present, and an OTLP sink is added only when an endpoint is set.
func TestNewAddsOTLPSinkWhenEndpointSet(t *testing.T) {
	local := New(Options{Enabled: true, Dir: t.TempDir()})
	if local == nil || len(local.sinks) != 1 {
		t.Fatalf("local-only: want 1 sink, got %d", len(local.sinks))
	}

	dual := New(Options{Enabled: true, Dir: t.TempDir(), OTLPEndpoint: "http://127.0.0.1:4318"})
	t.Cleanup(func() { dual.Shutdown(context.Background()) })
	if dual == nil || len(dual.sinks) != 2 {
		t.Fatalf("with endpoint: want 2 sinks (jsonl + otlp), got %d", len(dual.sinks))
	}
}

func findMetric(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

func histogramInt(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Histogram[int64] {
	t.Helper()
	m, ok := findMetric(rm, name)
	if !ok {
		t.Fatalf("metric %q not found", name)
	}
	return m.Data.(metricdata.Histogram[int64])
}

func histogramFloat(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Histogram[float64] {
	t.Helper()
	m, ok := findMetric(rm, name)
	if !ok {
		t.Fatalf("metric %q not found", name)
	}
	return m.Data.(metricdata.Histogram[float64])
}

func sumInt(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Sum[int64] {
	t.Helper()
	m, ok := findMetric(rm, name)
	if !ok {
		t.Fatalf("metric %q not found", name)
	}
	return m.Data.(metricdata.Sum[int64])
}

func attrOf(set attribute.Set, key string) string {
	v, ok := set.Value(attribute.Key(key))
	if !ok {
		return ""
	}
	return v.AsString()
}
