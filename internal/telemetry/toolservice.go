package telemetry

import (
	"context"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// toolService decorates a domain.ToolService, recording one tool metric per
// ExecuteTool call. It embeds the interface so every other method passes through
// unchanged - only ExecuteTool is overridden.
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
	res, err := t.ToolService.ExecuteTool(ctx, tool)
	outcome, errType := classify(res, err)
	t.rec.RecordTool(tool.Name, outcome, errType, time.Since(start))
	return res, err
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
