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
		if domain.IsToolApproved(ctx) {
			t.Error("Expected context to not have approval flag when approval not required")
		}
		return &domain.ToolExecutionResult{
			ToolName: tc.Function.Name,
			Data:     "success",
			Success:  true,
		}, nil
	}

	result, err := middleware.Execute(ctx, toolCall, true, executor)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !executorCalled {
		t.Error("Expected executor to be called")
	}
	if !result.Success {
		t.Error("Expected result to be successful")
	}

	select {
	case <-eventBus:
		t.Error("Expected no approval event to be published")
	default:
	}
}

func TestApprovalMiddleware_ApprovalRequired_Approved(t *testing.T) {
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
		if !domain.IsToolApproved(ctx) {
			t.Error("Expected context to have approval flag after approval")
		}
		return &domain.ToolExecutionResult{
			ToolName: tc.Function.Name,
			Data:     "success",
			Success:  true,
		}, nil
	}

	go func() {
		event := <-eventBus
		approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent)
		if !ok {
			t.Error("Expected ToolApprovalRequestedEvent")
			return
		}
		approvalEvent.ResponseChan <- domain.ApprovalApprove
	}()

	result, err := middleware.Execute(ctx, toolCall, true, executor)

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

	go func() {
		event := <-eventBus
		approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent)
		if !ok {
			t.Error("Expected ToolApprovalRequestedEvent")
			return
		}
		approvalEvent.ResponseChan <- domain.ApprovalReject
	}()

	result, err := middleware.Execute(ctx, toolCall, true, executor)

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

	go func() {
		event := <-eventBus
		approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent)
		if !ok {
			t.Error("Expected ToolApprovalRequestedEvent")
			return
		}
		approvalEvent.ResponseChan <- domain.ApprovalAutoAccept
	}()

	result, err := middleware.Execute(ctx, toolCall, true, executor)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !result.Success {
		t.Error("Expected result to be successful")
	}

	if stateManager.SetAgentModeCallCount() != 1 {
		t.Error("Expected SetAgentMode to be called once")
	}
	if stateManager.SetAgentModeArgsForCall(0) != domain.AgentModeAutoAccept {
		t.Error("Expected mode to be switched to AutoAccept")
	}
}

func TestApprovalMiddleware_ContextCancellation(t *testing.T) {
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

	cancel()

	result, err := middleware.Execute(ctx, toolCall, true, executor)

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	executor := createMockExecutor(nil, nil)

	go func() {
		<-eventBus
	}()

	result, err := middleware.Execute(ctx, toolCall, true, executor)

	if err == nil {
		t.Fatal("Expected timeout error")
	}
	if result != nil {
		t.Error("Expected nil result on timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded error, got: %v", err)
	}
}

func TestApprovalMiddleware_ExecuteBatch(t *testing.T) {
	approvalRequiredTools := map[string]bool{
		"Bash": true,
	}
	policy := &mockApprovalPolicy{shouldRequireApproval: false}

	customPolicy := &struct {
		*mockApprovalPolicy
	}{mockApprovalPolicy: &mockApprovalPolicy{}}

	realPolicy := services.NewStandardApprovalPolicy(
		createTestConfig(),
		&mocksdomain.FakeStateManager{},
	)
	_ = realPolicy
	_ = customPolicy
	_ = approvalRequiredTools

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
		*createToolCall("call-1", "Read", `{"file_path": "test.txt"}`),
		*createToolCall("call-2", "Bash", `{"command": "rm -rf /"}`),
		*createToolCall("call-3", "Write", `{"file_path": "out.txt"}`),
	}

	ctx := context.Background()
	executor := createMockExecutor(nil, nil)

	go func() {
		for event := range eventBus {
			if approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent); ok {
				approvalEvent.ResponseChan <- domain.ApprovalApprove
			}
		}
	}()

	results, err := middleware.ExecuteBatch(ctx, toolCalls, true, executor)

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
		*createToolCall("call-2", "Write", `{}`),
		*createToolCall("call-3", "Edit", `{}`),
	}

	ctx := context.Background()
	executor := createMockExecutor(nil, nil)

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

	results, err := middleware.ExecuteBatch(ctx, toolCalls, true, executor)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results (execution stopped after rejection), got: %d", len(results))
	}

	if len(results) >= 2 && !results[1].Rejected {
		t.Error("Expected second result to be rejected")
	}
}

func TestApprovalMiddleware_WithRequestID(t *testing.T) {
	policy := &mockApprovalPolicy{shouldRequireApproval: false}
	stateManager := &mocksdomain.FakeStateManager{}
	eventBus := make(chan domain.ChatEvent, 1)

	originalMiddleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     eventBus,
		RequestID:    "original-request",
	})

	newMiddleware := originalMiddleware.WithRequestID("new-request")

	if newMiddleware.requestID != "new-request" {
		t.Errorf("Expected request ID 'new-request', got: %s", newMiddleware.requestID)
	}

	if newMiddleware.policy != originalMiddleware.policy {
		t.Error("Expected policy to be preserved")
	}
	if newMiddleware.stateManager != originalMiddleware.stateManager {
		t.Error("Expected state manager to be preserved")
	}
}

func TestApprovalMiddleware_NilEventBus(t *testing.T) {
	policy := &mockApprovalPolicy{shouldRequireApproval: true}
	stateManager := &mocksdomain.FakeStateManager{}

	middleware := NewApprovalMiddleware(ApprovalMiddlewareConfig{
		Policy:       policy,
		StateManager: stateManager,
		EventBus:     nil,
		RequestID:    "test-request",
	})

	toolCall := createToolCall("call-1", "Bash", `{"command": "ls"}`)
	ctx := context.Background()
	executor := createMockExecutor(nil, nil)

	result, err := middleware.Execute(ctx, toolCall, true, executor)

	if err == nil {
		t.Fatal("Expected error when event bus is nil")
	}
	if result != nil {
		t.Error("Expected nil result when event bus is nil")
	}
}
