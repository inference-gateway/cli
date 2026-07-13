package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

var traceparentRe = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`)

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func TestChildEnv(t *testing.T) {
	tests := []struct {
		name         string
		recorder     bool
		span         bool
		otlpEndpoint string
		envEndpoint  string
		wantNil      bool
		wantEndpoint string
		wantLocal    bool
	}{
		{name: "nil recorder", recorder: false, span: true, wantNil: true},
		{name: "no active span", recorder: true, span: false, wantNil: true},
		{name: "local receiver by default", recorder: true, span: true, wantLocal: true},
		{name: "config endpoint passthrough", recorder: true, span: true, otlpEndpoint: "https://collector:4318", wantEndpoint: "https://collector:4318"},
		{name: "env endpoint inherited", recorder: true, span: true, envEndpoint: "https://collector:4318"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envEndpoint != "" {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tt.envEndpoint)
			} else {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
			}

			var rec *Recorder
			if tt.recorder {
				rec = New(Options{Enabled: true, Dir: t.TempDir(), SessionID: "sess-env", OTLPEndpoint: tt.otlpEndpoint})
				if rec == nil {
					t.Fatal("expected a recorder")
				}
				defer rec.Shutdown(context.Background())
			}

			ctx := context.Background()
			if tt.span && rec != nil {
				ctx, _ = rec.startToolSpan(ctx, "Bash")
			}

			env := rec.ChildEnv(ctx)
			if tt.wantNil {
				if env != nil {
					t.Fatalf("env=%v, want nil", env)
				}
				return
			}

			m := envMap(env)
			if !traceparentRe.MatchString(m["TRACEPARENT"]) {
				t.Fatalf("TRACEPARENT=%q, want w3c format", m["TRACEPARENT"])
			}
			switch {
			case tt.wantEndpoint != "":
				if m["OTEL_EXPORTER_OTLP_ENDPOINT"] != tt.wantEndpoint {
					t.Fatalf("endpoint=%q, want %q", m["OTEL_EXPORTER_OTLP_ENDPOINT"], tt.wantEndpoint)
				}
			case tt.wantLocal:
				if !strings.HasPrefix(m["OTEL_EXPORTER_OTLP_ENDPOINT"], "http://127.0.0.1:") {
					t.Fatalf("endpoint=%q, want loopback receiver", m["OTEL_EXPORTER_OTLP_ENDPOINT"])
				}
				if m["OTEL_EXPORTER_OTLP_PROTOCOL"] != "http/protobuf" {
					t.Fatalf("protocol=%q, want http/protobuf", m["OTEL_EXPORTER_OTLP_PROTOCOL"])
				}
			default:
				if _, ok := m["OTEL_EXPORTER_OTLP_ENDPOINT"]; ok {
					t.Fatalf("endpoint should be inherited, got %q", m["OTEL_EXPORTER_OTLP_ENDPOINT"])
				}
			}
		})
	}
}

func TestChildEnvBaggage(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	rec := New(Options{Enabled: true, Dir: t.TempDir(), SessionID: "sess-bag"})
	defer rec.Shutdown(context.Background())

	ctx := domain.WithToolCallID(context.Background(), "call_1")
	ctx, _ = rec.startToolSpan(ctx, "Bash")
	ctx = rec.contextWithBaggage(ctx)

	m := envMap(rec.ChildEnv(ctx))
	if !strings.Contains(m["BAGGAGE"], "infer.session.id=sess-bag") {
		t.Fatalf("BAGGAGE=%q, want infer.session.id", m["BAGGAGE"])
	}
	if !strings.Contains(m["BAGGAGE"], "infer.tool.call.id=call_1") {
		t.Fatalf("BAGGAGE=%q, want infer.tool.call.id", m["BAGGAGE"])
	}
}

func TestPropagatingTransport(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer srv.Close()

	rec := New(Options{Enabled: true, Dir: t.TempDir(), SessionID: "sess-h"})
	defer rec.Shutdown(context.Background())
	ctx, _ := rec.startToolSpan(context.Background(), "A2A_SubmitTask")
	ctx = rec.contextWithBaggage(ctx)

	c := &http.Client{Transport: propagatingTransport{http.DefaultTransport}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	if !traceparentRe.MatchString(got.Get("traceparent")) {
		t.Fatalf("traceparent=%q, want w3c format", got.Get("traceparent"))
	}
	if !strings.Contains(got.Get("baggage"), "infer.session.id=sess-h") {
		t.Fatalf("baggage=%q, want infer.session.id", got.Get("baggage"))
	}

	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	if got.Get("traceparent") != "" || got.Get("baggage") != "" {
		t.Fatalf("untraced request must carry no headers, got traceparent=%q baggage=%q", got.Get("traceparent"), got.Get("baggage"))
	}
}

func TestStartSessionInheritsTraceparent(t *testing.T) {
	const parentTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	const parentSpan = "00f067aa0ba902b7"
	t.Setenv("TRACEPARENT", "00-"+parentTrace+"-"+parentSpan+"-01")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	dir := t.TempDir()
	rec := New(Options{Enabled: true, Dir: dir, SessionID: "sess-child"})
	endSession := rec.StartSession("standard")
	endSession(RunSuccess)
	rec.Shutdown(context.Background())

	spans := readSpans(t, filepath.Join(dir, "sess-child-traces.jsonl"))
	session := spans["session"]
	if session.Parent.SpanID != parentSpan {
		t.Fatalf("session parent=%s, want inherited %s", session.Parent.SpanID, parentSpan)
	}
	if session.SpanContext.TraceID != parentTrace {
		t.Fatalf("session trace=%s, want inherited %s", session.SpanContext.TraceID, parentTrace)
	}
}
