package telemetry

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	proto "google.golang.org/protobuf/proto"

	logger "github.com/inference-gateway/cli/internal/logger"
)

const (
	recvMaxBody  = 4 << 20
	recvMaxSpans = 5000
)

// localReceiverURL lazily starts a loopback OTLP/HTTP receiver that appends
// received spans to the per-session trace file. Returns "" on any failure —
// receiver problems never fail the tool.
func (r *Recorder) localReceiverURL() string {
	if r == nil || r.traceWriter == nil {
		return ""
	}
	r.recvOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			logger.Warn("telemetry: local OTLP receiver disabled", "error", err)
			return
		}
		mux := http.NewServeMux()
		mux.HandleFunc("POST /v1/traces", r.handleTraces)
		mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.recvSrv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		r.recvURL = "http://" + ln.Addr().String()
		go func() { _ = r.recvSrv.Serve(ln) }()
	})
	return r.recvURL
}

func (r *Recorder) handleTraces(w http.ResponseWriter, req *http.Request) {
	defer w.WriteHeader(http.StatusOK)

	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, recvMaxBody))
	if err != nil {
		logger.Warn("telemetry: OTLP receiver read failed", "error", err)
		return
	}
	var export coltracepb.ExportTraceServiceRequest
	if err := proto.Unmarshal(body, &export); err != nil {
		logger.Warn("telemetry: OTLP receiver bad payload", "error", err)
		return
	}
	for _, rs := range export.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, span := range ss.Spans {
				if r.recvSpans.Add(1) > recvMaxSpans {
					return
				}
				r.appendSpanStub(span)
			}
		}
	}
}

// recvStub mirrors the stdouttrace SpanStub subset that LoadTraceTree reads
type recvStub struct {
	Name        string
	SpanContext recvSpanCtx
	Parent      recvSpanCtx
	StartTime   time.Time
	EndTime     time.Time
	Attributes  []recvAttr
	Status      struct {
		Code        string
		Description string
	}
}

type recvSpanCtx struct {
	TraceID string
	SpanID  string
}

type recvAttr struct {
	Key   string
	Value struct {
		Type  string
		Value any
	}
}

func (r *Recorder) appendSpanStub(span *tracepb.Span) {
	stub := recvStub{
		Name:        span.Name,
		SpanContext: recvSpanCtx{TraceID: hex.EncodeToString(span.TraceId), SpanID: hex.EncodeToString(span.SpanId)},
		Parent:      recvSpanCtx{TraceID: hex.EncodeToString(span.TraceId), SpanID: hex.EncodeToString(span.ParentSpanId)},
		StartTime:   time.Unix(0, int64(span.StartTimeUnixNano)).UTC(),
		EndTime:     time.Unix(0, int64(span.EndTimeUnixNano)).UTC(),
	}
	for _, attr := range span.Attributes {
		a := recvAttr{Key: attr.Key}
		a.Value.Type, a.Value.Value = anyValue(attr.Value)
		stub.Attributes = append(stub.Attributes, a)
	}
	if span.Status != nil {
		stub.Status.Description = span.Status.Message
		if span.Status.Code == tracepb.Status_STATUS_CODE_ERROR {
			stub.Status.Code = "Error"
		} else {
			stub.Status.Code = "Unset"
		}
	}
	line, err := json.Marshal(stub)
	if err != nil {
		return
	}
	if _, err := r.traceWriter.Write(append(line, '\n')); err != nil {
		logger.Warn("telemetry: OTLP receiver append failed", "error", err)
	}
}

func anyValue(v *commonpb.AnyValue) (string, any) {
	switch val := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return "STRING", val.StringValue
	case *commonpb.AnyValue_BoolValue:
		return "BOOL", val.BoolValue
	case *commonpb.AnyValue_IntValue:
		return "INT64", val.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return "FLOAT64", val.DoubleValue
	default:
		return "STRING", v.String()
	}
}
