package telemetry

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	trace "go.opentelemetry.io/otel/trace"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	proto "google.golang.org/protobuf/proto"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func postTraces(t *testing.T, url string, body []byte) {
	t.Helper()
	resp, err := http.Post(url+"/v1/traces", "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
}

func exportRequest(traceID, parentSpanID, spanID []byte, name string, errStatus bool) []byte {
	span := &tracepb.Span{
		TraceId:           traceID,
		SpanId:            spanID,
		ParentSpanId:      parentSpanID,
		Name:              name,
		StartTimeUnixNano: uint64(time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC).UnixNano()),
		EndTimeUnixNano:   uint64(time.Date(2026, 1, 1, 0, 0, 3, 0, time.UTC).UnixNano()),
		Attributes: []*commonpb.KeyValue{{
			Key:   "process.command",
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "go test"}},
		}},
	}
	if errStatus {
		span.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "exit 1"}
	}
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{span}}}}},
	}
	out, _ := proto.Marshal(req)
	return out
}

func TestReceiverIngestsChildSpans(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	dir := t.TempDir()
	rec := New(Options{Enabled: true, Dir: dir, SessionID: "sess-recv"})

	endSession := rec.StartSession("standard")
	toolCtx, toolSpan := rec.startToolSpan(rec.SpanContext(context.Background()), "Bash")
	sc := trace.SpanContextFromContext(toolCtx)

	url := rec.localReceiverURL()
	if url == "" {
		t.Fatal("expected a receiver URL")
	}
	traceID := mustHex(t, sc.TraceID().String())
	parentSpan := mustHex(t, sc.SpanID().String())
	postTraces(t, url, exportRequest(traceID, parentSpan, mustHex(t, "aabbccdd11223344"), "go test", true))
	postTraces(t, url, exportRequest(mustHex(t, "ffffffffffffffffffffffffffffffff"), parentSpan, mustHex(t, "aabbccdd11223355"), "foreign trace", false))

	postTraces(t, url, []byte("not protobuf"))
	resp, err := http.Post(url+"/v1/metrics", "application/x-protobuf", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	toolSpan.End()
	endSession(RunSuccess)
	rec.Shutdown(context.Background())

	roots, err := LoadTraceTree(dir, "sess-recv")
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Name != "session" {
		t.Fatalf("roots=%v, want single session root", roots)
	}
	tool := roots[0].Children[0]
	if tool.Name != "execute_tool Bash" || len(tool.Children) != 1 {
		t.Fatalf("tool=%+v, want one ingested child", tool)
	}
	child := tool.Children[0]
	if child.Name != "go test" || child.DurationMs != 2000 {
		t.Fatalf("child=%+v, want go test / 2000ms", child)
	}
	if child.Error == "" {
		t.Fatalf("child.Error empty, want error status")
	}
	if child.Attributes["process.command"] != "go test" {
		t.Fatalf("attrs=%v, want process.command", child.Attributes)
	}
}

func TestReceiverEagerStart(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	dir := t.TempDir()
	rec := New(Options{Enabled: true, Dir: dir, SessionID: "sess-eager", ReceiverAddress: "0.0.0.0:0"})
	defer rec.Shutdown(context.Background())

	if rec.recvURL == "" {
		t.Fatal("expected receiver to start eagerly")
	}
	if !strings.HasPrefix(rec.recvURL, "http://127.0.0.1:") {
		t.Fatalf("recvURL=%q, want unspecified host rewritten to 127.0.0.1", rec.recvURL)
	}
	if url := rec.localReceiverURL(); url != rec.recvURL {
		t.Fatalf("localReceiverURL=%q, want %q", url, rec.recvURL)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(exportRequestWithService(t, "mock-agent")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, rec.recvURL+"/v1/traces", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	roots, err := LoadTraceTree(dir, "sess-eager")
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Attributes["service.name"] != "mock-agent" {
		t.Fatalf("roots=%+v, want one span with service.name=mock-agent", roots)
	}
}

func exportRequestWithService(t *testing.T, service string) []byte {
	t.Helper()
	span := &tracepb.Span{
		TraceId:           mustHex(t, "4bf92f3577b34da6a3ce929d0e0e4736"),
		SpanId:            mustHex(t, "aabbccdd11223344"),
		Name:              "a2a.request",
		StartTimeUnixNano: uint64(time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC).UnixNano()),
		EndTimeUnixNano:   uint64(time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC).UnixNano()),
	}
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{
				Key:   "service.name",
				Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: service}},
			}}},
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{span}}},
		}},
	}
	out, _ := proto.Marshal(req)
	return out
}

func TestReceiverSpanCap(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	dir := t.TempDir()
	rec := New(Options{Enabled: true, Dir: dir, SessionID: "sess-cap"})
	defer rec.Shutdown(context.Background())

	url := rec.localReceiverURL()
	rec.recvSpans.Store(recvMaxSpans)
	postTraces(t, url, exportRequest(mustHex(t, "4bf92f3577b34da6a3ce929d0e0e4736"), nil, mustHex(t, "aabbccdd11223344"), "dropped", false))

	roots, err := LoadTraceTree(dir, "sess-cap")
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 0 {
		t.Fatalf("roots=%v, want none past cap", roots)
	}
}
