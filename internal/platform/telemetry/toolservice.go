package telemetry

import (
	"context"
	"time"

	attribute "go.opentelemetry.io/otel/attribute"
	codes "go.opentelemetry.io/otel/codes"
	trace "go.opentelemetry.io/otel/trace"
	noop "go.opentelemetry.io/otel/trace/noop"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// toolService decorates a agentdomain.ToolService, recording one metric and one
// span per execution. It embeds the interface so every other method passes
// through unchanged - only the two execute entry points are overridden.
type toolService struct {
	agentdomain.ToolService
	rec *Recorder
}

// NewToolService wraps inner so tool executions are recorded. The container only
// applies this when rec is non-nil, so the disabled tool path carries no
// decorator at all.
func NewToolService(inner agentdomain.ToolService, rec *Recorder) agentdomain.ToolService {
	return &toolService{ToolService: inner, rec: rec}
}

func (t *toolService) ExecuteTool(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
	return t.record(ctx, tool, t.ToolService.ExecuteTool)
}

// ExecuteToolDirect is instrumented exactly like ExecuteTool: a user-typed `!!`
// invocation is a tool execution like any other, and leaving it uninstrumented
// made direct runs invisible in `infer traces` - a session whose only activity
// was `!!` produced a root span with no children.
func (t *toolService) ExecuteToolDirect(ctx context.Context, tool sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
	return t.record(ctx, tool, t.ToolService.ExecuteToolDirect)
}

// record wraps one execution in the span and metric shared by both entry
// points, so instrumentation cannot drift between them.
func (t *toolService) record(
	ctx context.Context,
	tool sdk.ChatCompletionMessageToolCallFunction,
	execute func(context.Context, sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error),
) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()

	ctx, span := t.rec.startToolSpan(ctx, tool.Name)
	defer span.End()

	ctx = t.rec.contextWithBaggage(ctx)
	if env := t.rec.ChildEnv(ctx); env != nil {
		ctx = agentdomain.WithTraceEnv(ctx, env)
	}

	res, err := execute(ctx, tool)
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
	if toolCallID := agentdomain.GetToolCallID(ctx); toolCallID != "" {
		attrs = append(attrs, attribute.String("gen_ai.tool.call.id", toolCallID))
	}
	// Mark user-typed `!!` runs so a trace tree distinguishes them from the
	// model's own calls - otherwise the two are indistinguishable after the fact.
	if agentdomain.IsDirectExecution(ctx) {
		attrs = append(attrs, attribute.Bool("infer.tool.direct", true))
	}
	return r.Tracer().Start(ctx, "execute_tool "+toolName,
		trace.WithAttributes(attrs...),
	)
}

// classify maps an execution result to (infer.tool.outcome, error.type). A nil
// result or transport error is an error; an explicit rejection is "rejected".
func classify(res *agentdomain.ToolExecutionResult, err error) (outcome, errType string) {
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
