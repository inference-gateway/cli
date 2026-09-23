package logger

import (
	"context"
	"testing"

	trace "go.opentelemetry.io/otel/trace"
	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
	observer "go.uber.org/zap/zaptest/observer"
)

func TestCtxLoggingCarriesTraceContext(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	prev := GetGlobalLogger()
	SetGlobalLogger(zap.New(core))
	defer SetGlobalLogger(prev)

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01},
		SpanID:     trace.SpanID{0x02},
		TraceFlags: trace.FlagsSampled,
	})
	spanCtx := trace.ContextWithSpanContext(context.Background(), sc)

	WarnCtx(spanCtx, "with span", "k", "v")
	ErrorCtx(context.Background(), "without span", "k", "v")

	entries := logs.AllUntimed()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	withSpan := entries[0].ContextMap()
	if withSpan["trace_id"] != sc.TraceID().String() || withSpan["span_id"] != sc.SpanID().String() {
		t.Errorf("trace fields = %v/%v, want %s/%s", withSpan["trace_id"], withSpan["span_id"], sc.TraceID(), sc.SpanID())
	}
	if withSpan["k"] != "v" {
		t.Errorf("caller fields lost: %v", withSpan)
	}

	withoutSpan := entries[1].ContextMap()
	if _, ok := withoutSpan["trace_id"]; ok {
		t.Errorf("trace_id set without a span: %v", withoutSpan)
	}
}
