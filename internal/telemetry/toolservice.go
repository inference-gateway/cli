package telemetry

import (
	"context"
	"time"

	attribute "go.opentelemetry.io/otel/attribute"
	codes "go.opentelemetry.io/otel/codes"
	trace "go.opentelemetry.io/otel/trace"
	noop "go.opentelemetry.io/otel/trace/noop"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// toolService decorates a domain.ToolService, recording one metric and one
// span per ExecuteTool call. It embeds the interface so every other method
// passes through unchanged - only ExecuteTool is overridden.
type toolService struct {
	domain.ToolService
	rec *Recorder
}

// NewToolService wraps inner so tool executions are recorded. The container only
// applies this when rec is non-nil, so the disabled tool path carries no
// decorator at all.
func NewToolService(inner domain.ToolService, rec *Recorder) domain.ToolService {
	return &toolService{ToolService: inner, rec: rec}
}

func (t *toolService) ExecuteTool(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	ctx, span := t.rec.startToolSpan(ctx, tool.Name)
	defer span.End()

	ctx = t.rec.contextWithBaggage(ctx)
	if env := t.rec.ChildEnv(ctx); env != nil {
		ctx = domain.WithTraceEnv(ctx, env)
	}

	res, err := t.ToolService.ExecuteTool(ctx, tool)
	outcome, errType := classify(res, err)
	t.rec.RecordTool(tool.Name, outcome, errType, time.Since(start))

	span.SetAttributes(attribute.String("infer.tool.outcome", outcome))
	if errType != "" {
		span.SetAttributes(attribute.String("error.type", errType))
		span.SetStatus(codes.Error, errType)
	}
	if err != nil {
		span.RecordError(err)
	}

	return res, err
}

// startToolSpan creates a span for a tool execution with GenAI semconv
// attributes. Safe on nil (returns ctx unchanged and a no-op span).
func (r *Recorder) startToolSpan(ctx context.Context, toolName string) (context.Context, trace.Span) {
	if r == nil {
		return ctx, noop.Span{}
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "execute_tool"),
		attribute.String("gen_ai.tool.name", toolName),
		attribute.String("gen_ai.tool.type", "function"),
	}
	if toolCallID := domain.GetToolCallID(ctx); toolCallID != "" {
		attrs = append(attrs, attribute.String("gen_ai.tool.call.id", toolCallID))
	}
	return r.Tracer().Start(ctx, "execute_tool "+toolName,
		trace.WithAttributes(attrs...),
	)
}

// classify maps an execution result to (infer.tool.outcome, error.type). A nil
// result or transport error is an error; an explicit rejection is "rejected".
func classify(res *domain.ToolExecutionResult, err error) (outcome, errType string) {
	switch {
	case err != nil || res == nil:
		return ToolError, ErrTypeTool
	case res.Rejected:
		return ToolRejected, ""
	case res.Success:
		return ToolSuccess, ""
	default:
		return ToolError, ErrTypeTool
	}
}
