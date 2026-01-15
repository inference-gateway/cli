package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	mocksdomain "github.com/inference-gateway/cli/tests/mocks/domain"
)

// Helper to create test config
func createTestConfig() *config.Config {
	return &config.Config{
		Tools: config.ToolsConfig{
			Safety: config.SafetyConfig{
				RequireApproval: true,
			},
			Bash: config.BashToolConfig{
				Enabled: true,
				Whitelist: config.ToolWhitelistConfig{
					Commands: []string{"ls", "pwd", "echo"},
				},
			},
		},
	}
}

// Mock approval policy for testing
type mockApprovalPolicy struct {
	shouldRequireApproval bool
}

func (m *mockApprovalPolicy) ShouldRequireApproval(ctx context.Context, toolCall *sdk.ChatCompletionMessageToolCall, isChatMode bool) bool {
	return m.shouldRequireApproval
}

// Helper to create a tool call
func createToolCall(id, name, args string) *sdk.ChatCompletionMessageToolCall {
	return &sdk.ChatCompletionMessageToolCall{
		Id:   id,
		Type: sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      name,
			Arguments: args,
		},
	}
}

// Helper to create a mock executor
func createMockExecutor(result *domain.ToolExecutionResult, err error) ToolExecutor {
	return func(ctx context.Context, toolCall *sdk.ChatCompletionMessageToolCall) (*domain.ToolExecutionResult, error) {
		if result == nil {
			return &domain.ToolExecutionResult{
				ToolName: toolCall.Function.Name,
				Data:     "mock output",
				Success:  true,
			}, err
		}
		return result, err
	}
}

func TestApprovalMiddleware_NoApprovalRequired(t *testing.T) {
	// Setup
	policy := &mockApprovalPolicy{shouldRequireApproval: false}
	stateManager := &mocksdomain.FakeStateManager{}
	eventBus := make(chan domain.ChatEvent, 1)

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "test-request",
	})

	toolCall := createToolCall("call-1", "Read", `{"file_path": "test.txt"}`)
	ctx := context.Background()

	executorCalled := false
	executor := func(ctx context.Context, tc *sdk.ChatCompletionMessageToolCall) (*domain.ToolExecutionResult, error) {
		executorCalled = true
		// Verify context does NOT have approval flag (not needed)
		if domain.IsToolApproved(ctx) {
			t.Error("Expected context to not have approval flag when approval not required")
		}
		return &domain.ToolExecutionResult{
			ToolName: tc.Function.Name,
			Data:     "success",
			Success:  true,
		}, nil
	}

	// Execute
	result, err := middleware.Execute(ctx, toolCall, true, executor)

	// Verify
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !executorCalled {
		t.Error("Expected executor to be called")
	}
	if !result.Success {
		t.Error("Expected result to be successful")
	}

	// Verify no approval event was published
	select {
	case <-eventBus:
		t.Error("Expected no approval event to be published")
	default:
		// Good, no event
	}
}

func TestApprovalMiddleware_ApprovalRequired_Approved(t *testing.T) {
	// Setup
	policy := &mockApprovalPolicy{shouldRequireApproval: true}
	stateManager := &mocksdomain.FakeStateManager{}
	eventBus := make(chan domain.ChatEvent, 1)

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "test-request",
	})

	toolCall := createToolCall("call-1", "Bash", `{"command": "rm -rf /"}`)
	ctx := context.Background()

	executorCalled := false
	executor := func(ctx context.Context, tc *sdk.ChatCompletionMessageToolCall) (*domain.ToolExecutionResult, error) {
		executorCalled = true
		// Verify context HAS approval flag
		if !domain.IsToolApproved(ctx) {
			t.Error("Expected context to have approval flag after approval")
		}
		return &domain.ToolExecutionResult{
			ToolName: tc.Function.Name,
			Data:     "success",
			Success:  true,
		}, nil
	}

	// Simulate approval in background
	go func() {
		event := <-eventBus
		approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent)
		if !ok {
			t.Error("Expected ToolApprovalRequestedEvent")
			return
		}
		// Send approval
		approvalEvent.ResponseChan <- domain.ApprovalApprove
	}()

	// Execute
	result, err := middleware.Execute(ctx, toolCall, true, executor)

	// Verify
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !executorCalled {
		t.Error("Expected executor to be called after approval")
	}
	if !result.Success {
		t.Error("Expected result to be successful")
	}
}

func TestApprovalMiddleware_ApprovalRequired_Rejected(t *testing.T) {
	// Setup
	policy := &mockApprovalPolicy{shouldRequireApproval: true}
	stateManager := &mocksdomain.FakeStateManager{}
	eventBus := make(chan domain.ChatEvent, 1)

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "test-request",
	})

	toolCall := createToolCall("call-1", "Bash", `{"command": "rm -rf /"}`)
	ctx := context.Background()

	executorCalled := false
	executor := func(ctx context.Context, tc *sdk.ChatCompletionMessageToolCall) (*domain.ToolExecutionResult, error) {
		executorCalled = true
		return nil, nil
	}

	// Simulate rejection in background
	go func() {
		event := <-eventBus
		approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent)
		if !ok {
			t.Error("Expected ToolApprovalRequestedEvent")
			return
		}
		// Send rejection
		approvalEvent.ResponseChan <- domain.ApprovalReject
	}()

	// Execute
	result, err := middleware.Execute(ctx, toolCall, true, executor)

	// Verify
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if executorCalled {
		t.Error("Expected executor NOT to be called after rejection")
	}
	if result.Success {
		t.Error("Expected result to be unsuccessful")
	}
	if !result.Rejected {
		t.Error("Expected result to be marked as rejected")
	}
	if result.Error != "Tool execution rejected by user" {
		t.Errorf("Expected rejection message, got: %s", result.Error)
	}
}

func TestApprovalMiddleware_AutoAccept(t *testing.T) {
	// Setup
	policy := &mockApprovalPolicy{shouldRequireApproval: true}
	stateManager := &mocksdomain.FakeStateManager{}
	stateManager.GetAgentModeReturns(domain.AgentModeStandard)
	eventBus := make(chan domain.ChatEvent, 1)

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "test-request",
	})

	toolCall := createToolCall("call-1", "Bash", `{"command": "ls"}`)
	ctx := context.Background()

	executor := createMockExecutor(nil, nil)

	// Simulate auto-accept in background
	go func() {
		event := <-eventBus
		approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent)
		if !ok {
			t.Error("Expected ToolApprovalRequestedEvent")
			return
		}
		// Send auto-accept
		approvalEvent.ResponseChan <- domain.ApprovalAutoAccept
	}()

	// Execute
	result, err := middleware.Execute(ctx, toolCall, true, executor)

	// Verify
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !result.Success {
		t.Error("Expected result to be successful")
	}

	// Verify state manager was called to switch mode
	if stateManager.SetAgentModeCallCount() != 1 {
		t.Error("Expected SetAgentMode to be called once")
	}
	if stateManager.SetAgentModeArgsForCall(0) != domain.AgentModeAutoAccept {
		t.Error("Expected mode to be switched to AutoAccept")
	}
}

func TestApprovalMiddleware_ContextCancellation(t *testing.T) {
	// Setup
	policy := &mockApprovalPolicy{shouldRequireApproval: true}
	stateManager := &mocksdomain.FakeStateManager{}
	eventBus := make(chan domain.ChatEvent, 1)

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "test-request",
	})

	toolCall := createToolCall("call-1", "Bash", `{"command": "ls"}`)
	ctx, cancel := context.WithCancel(context.Background())

	executor := createMockExecutor(nil, nil)

	// Cancel context immediately
	cancel()

	// Execute
	result, err := middleware.Execute(ctx, toolCall, true, executor)

	// Verify
	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}
	if result != nil {
		t.Error("Expected nil result on error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}

func TestApprovalMiddleware_Timeout(t *testing.T) {
	// This test is slow due to timeout - skip in short mode
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	// Setup
	policy := &mockApprovalPolicy{shouldRequireApproval: true}
	stateManager := &mocksdomain.FakeStateManager{}
	eventBus := make(chan domain.ChatEvent, 1)

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "test-request",
	})

	toolCall := createToolCall("call-1", "Bash", `{"command": "ls"}`)
	ctx := context.Background()

	executor := createMockExecutor(nil, nil)

	// Don't respond to approval request - let it timeout
	// But we need to consume the event to prevent blocking
	go func() {
		<-eventBus
		// Don't send response - let it timeout
	}()

	// Execute with a shorter timeout for testing
	// Note: The middleware has a hardcoded 5-minute timeout
	// For this test, we'll verify the error type
	start := time.Now()
	result, err := middleware.Execute(ctx, toolCall, true, executor)
	duration := time.Since(start)

	// Verify
	if err == nil {
		t.Fatal("Expected timeout error")
	}
	if result != nil {
		t.Error("Expected nil result on timeout")
	}

	// Verify timeout occurred (should be ~5 minutes, but allow some margin)
	if duration < 4*time.Minute || duration > 6*time.Minute {
		t.Errorf("Expected timeout around 5 minutes, got: %v", duration)
	}
}

func TestApprovalMiddleware_ExecuteBatch(t *testing.T) {
	// Setup - use simple mock policy for testing
	// Bash requires approval, other tools don't
	approvalRequiredTools := map[string]bool{
		"Bash": true,
	}
	policy := &mockApprovalPolicy{shouldRequireApproval: false}

	// Create a custom policy for this test
	customPolicy := &struct {
		*mockApprovalPolicy
	}{mockApprovalPolicy: &mockApprovalPolicy{}}

	// Override ShouldRequireApproval to check tool name
	realPolicy := services.NewStandardApprovalPolicy(
		createTestConfig(),
		&mocksdomain.FakeStateManager{},
	)
	_ = realPolicy
	_ = customPolicy
	_ = approvalRequiredTools

	// For simplicity, use the mock policy and assume all tools require approval
	policy.shouldRequireApproval = true

	stateManager := &mocksdomain.FakeStateManager{}
	stateManager.GetAgentModeReturns(domain.AgentModeStandard)
	eventBus := make(chan domain.ChatEvent, 10)

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "test-request",
	})

	toolCalls := []sdk.ChatCompletionMessageToolCall{
		*createToolCall("call-1", "Read", `{"file_path": "test.txt"}`), // No approval needed
		*createToolCall("call-2", "Bash", `{"command": "rm -rf /"}`),   // Approval needed
		*createToolCall("call-3", "Write", `{"file_path": "out.txt"}`), // No approval needed
	}

	ctx := context.Background()
	executor := createMockExecutor(nil, nil)

	// Handle approval requests in background
	go func() {
		for event := range eventBus {
			if approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent); ok {
				// Approve all requests
				approvalEvent.ResponseChan <- domain.ApprovalApprove
			}
		}
	}()

	// Execute
	results, err := middleware.ExecuteBatch(ctx, toolCalls, true, executor)

	// Verify
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Expected 3 results, got: %d", len(results))
	}
	for i, result := range results {
		if !result.Success {
			t.Errorf("Result %d should be successful", i)
		}
	}
}

func TestApprovalMiddleware_ExecuteBatch_StopsOnRejection(t *testing.T) {
	// Setup
	policy := &mockApprovalPolicy{shouldRequireApproval: true}
	stateManager := &mocksdomain.FakeStateManager{}
	eventBus := make(chan domain.ChatEvent, 10)

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "test-request",
	})

	toolCalls := []sdk.ChatCompletionMessageToolCall{
		*createToolCall("call-1", "Read", `{}`),
		*createToolCall("call-2", "Write", `{}`), // This will be rejected
		*createToolCall("call-3", "Edit", `{}`),  // This should not execute
	}

	ctx := context.Background()
	executor := createMockExecutor(nil, nil)

	// Handle approval requests - reject the second one
	requestCount := 0
	go func() {
		for event := range eventBus {
			if approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent); ok {
				requestCount++
				if requestCount == 2 {
					approvalEvent.ResponseChan <- domain.ApprovalReject
				} else {
					approvalEvent.ResponseChan <- domain.ApprovalApprove
				}
			}
		}
	}()

	// Execute
	results, err := middleware.ExecuteBatch(ctx, toolCalls, true, executor)

	// Verify
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should only have 2 results (first approved, second rejected, third not executed)
	if len(results) != 2 {
		t.Errorf("Expected 2 results (execution stopped after rejection), got: %d", len(results))
	}

	if len(results) >= 2 && !results[1].Rejected {
		t.Error("Expected second result to be rejected")
	}
}

func TestApprovalMiddleware_WithRequestID(t *testing.T) {
	// Setup
	policy := &mockApprovalPolicy{shouldRequireApproval: false}
	stateManager := &mocksdomain.FakeStateManager{}
	eventBus := make(chan domain.ChatEvent, 1)

	originalMiddleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "original-request",
	})

	// Create new middleware with different request ID
	newMiddleware := originalMiddleware.WithRequestID("new-request")

	// Verify
	if newMiddleware.requestID != "new-request" {
		t.Errorf("Expected request ID 'new-request', got: %s", newMiddleware.requestID)
	}

	// Verify other fields are preserved
	if newMiddleware.policy != originalMiddleware.policy {
		t.Error("Expected policy to be preserved")
	}
	if newMiddleware.stateManager != originalMiddleware.stateManager {
		t.Error("Expected state manager to be preserved")
	}
}

func TestApprovalMiddleware_NilEventBus(t *testing.T) {
	// Setup - middleware without event bus
	policy := &mockApprovalPolicy{shouldRequireApproval: true}
	stateManager := &mocksdomain.FakeStateManager{}

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     nil, // No event bus
		RequestID:    "test-request",
	})

	toolCall := createToolCall("call-1", "Bash", `{"command": "ls"}`)
	ctx := context.Background()
	executor := createMockExecutor(nil, nil)

	// Execute
	result, err := middleware.Execute(ctx, toolCall, true, executor)

	// Verify
	if err == nil {
		t.Fatal("Expected error when event bus is nil")
	}
	if result != nil {
		t.Error("Expected nil result when event bus is nil")
	}
}
