package middleware

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// ToolExecutor is a function that executes a tool and returns the result
type ToolExecutor func(ctx context.Context, toolCall *sdk.ChatCompletionMessageToolCall) (*domain.ToolExecutionResult, error)

// ApprovalMiddleware intercepts tool execution and injects approval logic
type ApprovalMiddleware struct {
	policy       domain.ApprovalPolicy
	stateManager domain.StateManager
	eventBus     chan<- domain.ChatEvent
	requestID    string
}

// ApprovalMiddlewareConfig contains configuration for the approval middleware
type ApprovalMiddlewareConfig struct {
	Policy       domain.ApprovalPolicy
	StateManager domain.StateManager
	EventBus     chan<- domain.ChatEvent
	RequestID    string
}

// NewApprovalMiddleware creates a new approval middleware
func NewApprovalMiddleware(cfg ApprovalMiddlewareConfig) *ApprovalMiddleware {
	return &ApprovalMiddleware{
		policy:       cfg.Policy,
		stateManager: cfg.StateManager,
		eventBus:     cfg.EventBus,
		requestID:    cfg.RequestID,
	}
}

// Execute intercepts tool execution and applies approval logic
// If approval is required, it requests approval from the user via events
// If approval is granted (or not required), it enriches context and calls the next handler
func (m *ApprovalMiddleware) Execute(
	ctx context.Context,
	toolCall *sdk.ChatCompletionMessageToolCall,
	isChatMode bool,
	next ToolExecutor,
) (*domain.ToolExecutionResult, error) {
	// Check if approval is required using the policy
	requiresApproval := m.policy.ShouldRequireApproval(ctx, toolCall, isChatMode)

	// If no approval required, execute directly with enriched context
	if !requiresApproval {
		return next(ctx, toolCall)
	}

	// Request approval from user
	approved, err := m.requestApproval(ctx, *toolCall)
	if err != nil {
		return nil, fmt.Errorf("approval request failed: %w", err)
	}

	if !approved {
		return &domain.ToolExecutionResult{
			ToolName: toolCall.Function.Name,
			Error:    "Tool execution rejected by user",
			Success:  false,
			Rejected: true,
		}, nil
	}

	// Enrich context with approval flag and execute
	ctx = domain.WithToolApproved(ctx)
	return next(ctx, toolCall)
}

// requestApproval requests user approval for a tool execution
// This follows the same pattern as the current agent service implementation
func (m *ApprovalMiddleware) requestApproval(
	ctx context.Context,
	toolCall sdk.ChatCompletionMessageToolCall,
) (bool, error) {
	// Create response channel
	responseChan := make(chan domain.ApprovalAction, 1)

	// Publish approval request event
	event := domain.ToolApprovalRequestedEvent{
		RequestID:    m.requestID,
		Timestamp:    time.Now(),
		ToolCall:     toolCall,
		ResponseChan: responseChan,
	}

	if m.eventBus != nil {
		m.eventBus <- event
	} else {
		return false, fmt.Errorf("event bus not configured")
	}

	// Wait for approval response with timeout
	var approved bool
	var err error

	select {
	case response := <-responseChan:
		// Handle auto-accept mode switch
		if response == domain.ApprovalAutoAccept {
			logger.Info("Switching to auto-accept mode from approval middleware")
			if m.stateManager != nil {
				m.stateManager.SetAgentMode(domain.AgentModeAutoAccept)
			}
		}
		approved = response == domain.ApprovalApprove || response == domain.ApprovalAutoAccept

	case <-ctx.Done():
		err = fmt.Errorf("approval request cancelled: %w", ctx.Err())

	case <-time.After(5 * time.Minute):
		err = fmt.Errorf("approval request timed out after 5 minutes")
	}

	return approved, err
}

// ExecuteBatch executes multiple tool calls, handling approval for each
// Tools requiring approval are executed sequentially, while approved tools can be executed in parallel
func (m *ApprovalMiddleware) ExecuteBatch(
	ctx context.Context,
	toolCalls []sdk.ChatCompletionMessageToolCall,
	isChatMode bool,
	executor ToolExecutor,
) ([]*domain.ToolExecutionResult, error) {
	// Separate tools into approval-required and approval-not-required
	var approvalRequired []sdk.ChatCompletionMessageToolCall
	var approvalNotRequired []sdk.ChatCompletionMessageToolCall

	for _, tc := range toolCalls {
		if m.policy.ShouldRequireApproval(ctx, &tc, isChatMode) {
			approvalRequired = append(approvalRequired, tc)
		} else {
			approvalNotRequired = append(approvalNotRequired, tc)
		}
	}

	results := make([]*domain.ToolExecutionResult, 0, len(toolCalls))

	// Execute tools not requiring approval (can be parallel)
	for _, tc := range approvalNotRequired {
		tcCopy := tc
		result, err := m.Execute(ctx, &tcCopy, isChatMode, executor)
		if err != nil {
			return results, fmt.Errorf("tool execution failed: %w", err)
		}
		results = append(results, result)
	}

	// Execute tools requiring approval (must be sequential)
	for _, tc := range approvalRequired {
		tcCopy := tc
		result, err := m.Execute(ctx, &tcCopy, isChatMode, executor)
		if err != nil {
			return results, fmt.Errorf("tool execution failed: %w", err)
		}
		results = append(results, result)

		// If rejected, stop processing remaining tools
		if result.Rejected {
			logger.Info("Tool execution rejected, stopping batch execution")
			break
		}
	}

	return results, nil
}

// WithRequestID creates a new middleware with a different request ID
// Useful for different execution contexts
func (m *ApprovalMiddleware) WithRequestID(requestID string) *ApprovalMiddleware {
	return &ApprovalMiddleware{
		policy:       m.policy,
		stateManager: m.stateManager,
		eventBus:     m.eventBus,
		requestID:    requestID,
	}
}
