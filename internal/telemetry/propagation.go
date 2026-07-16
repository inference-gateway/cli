package telemetry

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strings"

	baggage "go.opentelemetry.io/otel/baggage"
	propagation "go.opentelemetry.io/otel/propagation"
	trace "go.opentelemetry.io/otel/trace"

	domain "github.com/inference-gateway/cli/internal/domain"
)

var propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

// envCarrier is a TextMapCarrier over process environment naming (traceparent → TRACEPARENT)
type envCarrier map[string]string

func (c envCarrier) Get(key string) string {
	if v, ok := c[strings.ToUpper(key)]; ok {
		return v
	}
	return os.Getenv(strings.ToUpper(key))
}

func (c envCarrier) Set(key, value string) { c[strings.ToUpper(key)] = value }

func (c envCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// Default baggage member names, per the OTel semantic conventions. These are
// a cross-repo contract with the consumer (the ADK reads the same defaults)
// and must match byte-for-byte; override via telemetry.attr_session_id_key /
// telemetry.attr_tool_call_id_key.
const (
	defaultAttrSessionIDKey  = "session.id"
	defaultAttrToolCallIDKey = "gen_ai.tool.call.id"
)

func (r *Recorder) contextWithBaggage(ctx context.Context) context.Context {
	if r == nil {
		return ctx
	}
	var members []baggage.Member
	for k, v := range map[string]string{
		r.attrSessionIDKey:  r.sessionID,
		r.attrToolCallIDKey: domain.GetToolCallID(ctx),
	} {
		if v == "" {
			continue
		}
		if m, err := baggage.NewMember(k, v); err == nil {
			members = append(members, m)
		}
	}
	if len(members) == 0 {
		return ctx
	}
	b, err := baggage.New(members...)
	if err != nil {
		return ctx
	}
	return baggage.ContextWithBaggage(ctx, b)
}

// ChildEnv returns the W3C trace-context environment for a subprocess launched
// under the current tool span, plus its OTLP span sink: a configured endpoint
// passes through, an env-configured one is inherited, otherwise the ephemeral
// loopback receiver keeps child spans in the local session store. Nil when
// the recorder is nil or no span is active.
func (r *Recorder) ChildEnv(ctx context.Context) []string {
	if r == nil || !trace.SpanContextFromContext(ctx).IsValid() {
		return nil
	}

	carrier := envCarrier{}
	propagator.Inject(ctx, carrier)
	env := make([]string, 0, len(carrier)+3)
	for k, v := range carrier {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)

	switch {
	case r.otlpEndpoint != "":
		env = append(env, "OTEL_EXPORTER_OTLP_ENDPOINT="+r.otlpEndpoint)
		if h := joinHeaders(r.otlpHeaders); h != "" {
			env = append(env, "OTEL_EXPORTER_OTLP_HEADERS="+h)
		}
	case os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "":
	default:
		if u := r.localReceiverURL(); u != "" {
			env = append(env,
				"OTEL_EXPORTER_OTLP_ENDPOINT="+u,
				"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
			)
		}
	}
	return env
}

// propagatingTransport is an http.RoundTripper that injects W3C trace-context
// and baggage headers into every outgoing request.
type propagatingTransport struct{ base http.RoundTripper }

func (t propagatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	propagator.Inject(req.Context(), propagation.HeaderCarrier(req.Header))
	return t.base.RoundTrip(req)
}

// PropagationTransport returns an http.RoundTripper that injects W3C
// trace-context and baggage headers into every outgoing request.
// When base is nil, http.DefaultTransport is used.
func PropagationTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return propagatingTransport{base: base}
}

func joinHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(headers))
	for k, v := range headers {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}
