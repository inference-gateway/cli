package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	services "github.com/inference-gateway/cli/internal/services"
	streamevent "github.com/inference-gateway/cli/internal/streamevent"
)

// captureStdout redirects os.Stdout for the duration of fn and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestOutputAgentError(t *testing.T) {
	t.Run("emits valid agent_error JSON line", func(t *testing.T) {
		out := captureStdout(t, func() {
			outputAgentError("context length exceeded")
		})

		line := strings.TrimRight(out, "\n")
		if line == "" || strings.Contains(line, "\n") {
			t.Fatalf("expected single JSON line, got %q", out)
		}
		var msg domain.AgentErrorMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("unmarshal: %v (raw: %q)", err, line)
		}
		if msg.Type != "agent_error" {
			t.Errorf("Type = %q, want %q", msg.Type, "agent_error")
		}
		if msg.Message != "context length exceeded" {
			t.Errorf("Message = %q", msg.Message)
		}
	})

	t.Run("truncates very long messages", func(t *testing.T) {
		long := strings.Repeat("x", 5000)
		out := captureStdout(t, func() {
			outputAgentError(long)
		})

		var msg domain.AgentErrorMessage
		if err := json.Unmarshal([]byte(strings.TrimRight(out, "\n")), &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if !strings.HasSuffix(msg.Message, "…") {
			t.Errorf("expected truncation suffix, got message ending with %q",
				msg.Message[max(0, len(msg.Message)-10):])
		}
		runes := []rune(msg.Message)
		if len(runes) > 3501 {
			t.Errorf("expected at most 3501 runes, got %d", len(runes))
		}
	})
}

func TestFormatToolCallSummary(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		args     string
		want     string
		contains []string
	}{
		{
			name: "empty args returns bare name",
			tool: "Schedule",
			args: "",
			want: "Schedule",
		},
		{
			name: "empty json object returns bare name",
			tool: "Schedule",
			args: "{}",
			want: "Schedule",
		},
		{
			name: "invalid json returns bare name",
			tool: "Schedule",
			args: "not json",
			want: "Schedule",
		},
		{
			name: "single arg",
			tool: "Bash",
			args: `{"command":"echo hello"}`,
			want: "Bash(command=echo hello)",
		},
		{
			name: "multiple args sorted alphabetically",
			tool: "Schedule",
			args: `{"operation":"delete","job_id":"abc"}`,
			want: "Schedule(job_id=abc, operation=delete)",
		},
		{
			name:     "long values are truncated",
			tool:     "Write",
			args:     `{"content":"` + strings.Repeat("x", 200) + `"}`,
			contains: []string{"Write(content=", "…)"},
		},
		{
			name: "newlines in values are stripped",
			tool: "Write",
			args: `{"content":"line1\nline2"}`,
			want: "Write(content=line1 line2)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatToolCallSummary(tc.tool, tc.args)
			if tc.want != "" && got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			for _, sub := range tc.contains {
				if !strings.Contains(got, sub) {
					t.Fatalf("got %q does not contain %q", got, sub)
				}
			}
		})
	}
}

func TestIsModelAvailable(t *testing.T) {
	models := []string{"openai/gpt-4", "anthropic/claude-4", "openai/gpt-4.5-turbo"}

	tests := []struct {
		name        string
		targetModel string
		expected    bool
	}{
		{
			name:        "Model exists",
			targetModel: "openai/gpt-4",
			expected:    true,
		},
		{
			name:        "Model does not exist",
			targetModel: "google/gemini",
			expected:    false,
		},
		{
			name:        "Empty target model",
			targetModel: "",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isModelAvailable(models, tt.targetModel)
			if result != tt.expected {
				t.Errorf("isModelAvailable() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildSDKMessages(t *testing.T) {
	session := &AgentSession{
		conversation: []ConversationMessage{
			{
				Role:      "user",
				Content:   "Hello",
				Timestamp: mockTime(),
			},
			{
				Role:      "assistant",
				Content:   "Hi there!",
				Timestamp: mockTime(),
			},
		},
	}

	messages := session.buildSDKMessages()

	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}

	if messages[0].Role != sdk.User {
		t.Errorf("Expected first message role to be 'user', got %v", messages[0].Role)
	}

	if messages[1].Role != sdk.Assistant {
		t.Errorf("Expected second message role to be 'assistant', got %v", messages[1].Role)
	}

	content0, _ := messages[0].Content.AsMessageContent0()
	if content0 != "Hello" {
		t.Errorf("Expected first message content to be 'Hello', got %s", content0)
	}
}

func TestExecuteToolCallsParallel(t *testing.T) {
	tests := []struct {
		name              string
		toolCalls         []sdk.ChatCompletionMessageToolCall
		maxConcurrentTool int
		mockResults       []*domain.ToolExecutionResult
		mockErrors        []error
		expectedCount     int
		expectedRoles     []string
	}{
		{
			name:              "empty tool calls",
			toolCalls:         []sdk.ChatCompletionMessageToolCall{},
			maxConcurrentTool: 5,
			mockResults:       []*domain.ToolExecutionResult{},
			mockErrors:        []error{},
			expectedCount:     0,
			expectedRoles:     []string{},
		},
		{
			name: "single tool call",
			toolCalls: []sdk.ChatCompletionMessageToolCall{
				{
					ID: "call_1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Read",
						Arguments: `{"file_path": "test.txt"}`,
					},
				},
			},
			maxConcurrentTool: 5,
			mockResults: []*domain.ToolExecutionResult{
				{
					ToolName: "Read",
					Success:  true,
					Data:     "file content",
				},
			},
			mockErrors:    []error{nil},
			expectedCount: 1,
			expectedRoles: []string{"tool"},
		},
		{
			name: "multiple tool calls",
			toolCalls: []sdk.ChatCompletionMessageToolCall{
				{
					ID: "call_1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Read",
						Arguments: `{"file_path": "test1.txt"}`,
					},
				},
				{
					ID: "call_2",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Grep",
						Arguments: `{"pattern": "func"}`,
					},
				},
				{
					ID: "call_3",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Write",
						Arguments: `{"file_path": "output.txt", "content": "hello"}`,
					},
				},
			},
			maxConcurrentTool: 2,
			mockResults: []*domain.ToolExecutionResult{
				{ToolName: "Read", Success: true, Data: "content1"},
				{ToolName: "Grep", Success: true, Data: "matches"},
				{ToolName: "Write", Success: true, Data: "written"},
			},
			mockErrors:    []error{nil, nil, nil},
			expectedCount: 3,
			expectedRoles: []string{"tool", "tool", "tool"},
		},
		{
			name: "tool call with error",
			toolCalls: []sdk.ChatCompletionMessageToolCall{
				{
					ID: "call_1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Read",
						Arguments: `{"file_path": "nonexistent.txt"}`,
					},
				},
			},
			maxConcurrentTool: 5,
			mockResults:       []*domain.ToolExecutionResult{nil},
			mockErrors:        []error{errors.New("file not found")},
			expectedCount:     1,
			expectedRoles:     []string{"tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockToolService := &domainmocks.FakeToolService{}

			for i := range tt.toolCalls {
				if i < len(tt.mockErrors) && tt.mockErrors[i] != nil {
					mockToolService.ExecuteToolReturns(tt.mockResults[i], tt.mockErrors[i])
				} else if i < len(tt.mockResults) {
					mockToolService.ExecuteToolReturns(tt.mockResults[i], nil)
				}
			}

			cfg := &config.Config{
				Agent: config.AgentConfig{
					MaxConcurrentTools: tt.maxConcurrentTool,
				},
			}

			session := &AgentSession{
				toolService: mockToolService,
				config:      cfg,
			}

			results := session.executeToolCallsParallel(tt.toolCalls)

			if len(results) != tt.expectedCount {
				t.Errorf("Expected %d results, got %d", tt.expectedCount, len(results))
			}

			for i, result := range results {
				if i < len(tt.expectedRoles) && result.Role != tt.expectedRoles[i] {
					t.Errorf("Expected result[%d].Role to be %s, got %s", i, tt.expectedRoles[i], result.Role)
				}
			}

			if len(tt.toolCalls) > 0 {
				expectedCallCount := len(tt.toolCalls)
				if mockToolService.ExecuteToolCallCount() != expectedCallCount {
					t.Errorf("Expected ExecuteTool to be called %d times, got %d", expectedCallCount, mockToolService.ExecuteToolCallCount())
				}
			}
		})
	}
}

// TestExecuteToolCalls_BlocksWhenNoApprover verifies the secure-by-default headless
// behaviour: a tool that needs approval is blocked (not executed) when no approver
// is reachable - approval_behaviour=block, or the default prompt with no broker.
func TestExecuteToolCalls_BlocksWhenNoApprover(t *testing.T) {
	for _, behaviour := range []string{config.ApprovalBehaviourBlock, config.ApprovalBehaviourPrompt} {
		t.Run(behaviour, func(t *testing.T) {
			mockToolService := &domainmocks.FakeToolService{}

			cfg := &config.Config{Agent: config.AgentConfig{MaxConcurrentTools: 5}}
			cfg.Tools.Safety.RequireApproval = true // Write needs approval
			cfg.Tools.Safety.ApprovalBehaviour = behaviour

			session := &AgentSession{
				toolService:     mockToolService,
				config:          cfg,
				requireApproval: false, // no IPC broker (CI/heartbeat)
			}

			results := session.executeToolCalls([]sdk.ChatCompletionMessageToolCall{
				{ID: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{
					Name: "Write", Arguments: `{"file_path":"x","content":"y"}`,
				}},
			})

			if mockToolService.ExecuteToolCallCount() != 0 {
				t.Errorf("blocked tool must not execute, but ExecuteTool was called %d times", mockToolService.ExecuteToolCallCount())
			}
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}
			if exec := results[0].ToolExecution; exec == nil || !exec.Rejected {
				t.Errorf("expected a rejected/blocked tool result, got %+v", exec)
			}
			if !strings.Contains(results[0].Content, "Blocked") {
				t.Errorf("expected a 'Blocked' reason, got %q", results[0].Content)
			}
		})
	}
}

// TestExecuteToolCalls_IPCApprovalExecutesWhenApproved verifies that with an IPC
// broker attached (--require-approval) and the default prompt behaviour, an
// approval-requiring tool is delivered over IPC and runs once the user approves.
func TestExecuteToolCalls_IPCApprovalExecutesWhenApproved(t *testing.T) {
	mockToolService := &domainmocks.FakeToolService{}
	mockToolService.ExecuteToolReturns(&domain.ToolExecutionResult{ToolName: "Write", Success: true, Data: "ok"}, nil)

	cfg := &config.Config{Agent: config.AgentConfig{MaxConcurrentTools: 5}}
	cfg.Tools.Safety.RequireApproval = true
	cfg.Tools.Safety.ApprovalBehaviour = config.ApprovalBehaviourPrompt

	approvalCh := make(chan domain.ApprovalResponse, 1)
	approvalCh <- domain.ApprovalResponse{Type: "approval_response", ToolCallID: "call_1", Approved: true}

	session := &AgentSession{
		toolService:     mockToolService,
		config:          cfg,
		requireApproval: true, // broker attached -> prompt resolves to IPC
		approvalCh:      approvalCh,
	}

	results := session.executeToolCalls([]sdk.ChatCompletionMessageToolCall{
		{ID: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{
			Name: "Write", Arguments: `{"file_path":"x","content":"y"}`,
		}},
	})

	if mockToolService.ExecuteToolCallCount() != 1 {
		t.Errorf("approved tool should execute once, got %d calls", mockToolService.ExecuteToolCallCount())
	}
	if len(results) != 1 || results[0].ToolExecution == nil || !results[0].ToolExecution.Success {
		t.Errorf("expected a successful execution result, got %+v", results)
	}
}

func TestProcessSyncResponseParallel(t *testing.T) {
	tests := []struct {
		name                  string
		response              *domain.ChatSyncResponse
		maxConcurrentTools    int
		expectedMessageCount  int
		expectedToolCallCount int
	}{
		{
			name: "response with content only",
			response: &domain.ChatSyncResponse{
				RequestID: "req_1",
				Content:   "Hello, world!",
				ToolCalls: []sdk.ChatCompletionMessageToolCall{},
			},
			maxConcurrentTools:    5,
			expectedMessageCount:  1,
			expectedToolCallCount: 0,
		},
		{
			name: "response with tool calls",
			response: &domain.ChatSyncResponse{
				RequestID: "req_1",
				Content:   "I'll help you with that.",
				ToolCalls: []sdk.ChatCompletionMessageToolCall{
					{
						ID: "call_1",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Read",
							Arguments: `{"file_path": "test.txt"}`,
						},
					},
					{
						ID: "call_2",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Grep",
							Arguments: `{"pattern": "test"}`,
						},
					},
				},
			},
			maxConcurrentTools:    2,
			expectedMessageCount:  3,
			expectedToolCallCount: 2,
		},
		{
			name: "empty response",
			response: &domain.ChatSyncResponse{
				RequestID: "req_1",
				Content:   "",
				ToolCalls: []sdk.ChatCompletionMessageToolCall{},
			},
			maxConcurrentTools:    5,
			expectedMessageCount:  0,
			expectedToolCallCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockToolService := &domainmocks.FakeToolService{}
			mockToolService.ExecuteToolReturns(&domain.ToolExecutionResult{
				ToolName: "TestTool",
				Success:  true,
				Data:     "test result",
			}, nil)

			cfg := &config.Config{
				Agent: config.AgentConfig{
					MaxConcurrentTools: tt.maxConcurrentTools,
				},
			}

			session := &AgentSession{
				toolService:  mockToolService,
				config:       cfg,
				conversation: []ConversationMessage{},
			}

			err := session.processSyncResponse(tt.response, "request_123")

			if err != nil {
				t.Errorf("processSyncResponse() returned error: %v", err)
			}

			if len(session.conversation) != tt.expectedMessageCount {
				t.Errorf("Expected %d messages in conversation, got %d", tt.expectedMessageCount, len(session.conversation))
			}

			if mockToolService.ExecuteToolCallCount() != tt.expectedToolCallCount {
				t.Errorf("Expected ExecuteTool to be called %d times, got %d", tt.expectedToolCallCount, mockToolService.ExecuteToolCallCount())
			}
		})
	}
}

func mockTime() time.Time {
	return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
}

// fakePricing returns a FakePricingService whose CalculateCost yields the given costs.
func fakePricing(in, out, total float64) *domainmocks.FakePricingService {
	fake := &domainmocks.FakePricingService{}
	fake.IsEnabledReturns(true)
	fake.CalculateCostReturns(in, out, total)
	return fake
}

// sessionStatsLine mirrors the emitted session_stats JSON for assertions.
type sessionStatsLine struct {
	Type             string `json:"type"`
	Message          string `json:"message"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Requests         int    `json:"requests"`
	Cost             struct {
		Input    float64 `json:"input"`
		Output   float64 `json:"output"`
		Total    float64 `json:"total"`
		Currency string  `json:"currency"`
	} `json:"cost"`
}

// decodeStatsLine runs emitSessionStats, asserts a single JSON line was emitted,
// and unmarshals it into a sessionStatsLine.
func decodeStatsLine(t *testing.T, session *AgentSession) sessionStatsLine {
	t.Helper()
	out := captureStdout(t, session.emitSessionStats)
	line := strings.TrimRight(out, "\n")
	if line == "" || strings.Contains(line, "\n") {
		t.Fatalf("expected single JSON line, got %q", out)
	}
	var got sessionStatsLine
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal: %v (raw: %q)", err, line)
	}
	return got
}

func TestEmitSessionStatsFullLine(t *testing.T) {
	session := &AgentSession{
		model:                 "deepseek/deepseek-v4-flash",
		pricingService:        fakePricing(0.0021, 0.0008, 0.0029),
		config:                &config.Config{Pricing: config.PricingConfig{Currency: "USD"}},
		totalPromptTokens:     21000,
		totalCompletionTokens: 1260,
		totalTokens:           22260,
		requestCount:          7,
	}

	got := decodeStatsLine(t, session)

	if got.Type != "session_stats" {
		t.Errorf("Type = %q, want session_stats", got.Type)
	}
	if got.Message != "Session complete" {
		t.Errorf("Message = %q", got.Message)
	}
	if got.Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("Model = %q", got.Model)
	}
	if got.PromptTokens != 21000 || got.CompletionTokens != 1260 || got.TotalTokens != 22260 {
		t.Errorf("tokens = %d/%d/%d, want 21000/1260/22260",
			got.PromptTokens, got.CompletionTokens, got.TotalTokens)
	}
	if got.Requests != 7 {
		t.Errorf("Requests = %d, want 7", got.Requests)
	}
	if got.Cost.Input != 0.0021 || got.Cost.Output != 0.0008 || got.Cost.Total != 0.0029 {
		t.Errorf("cost = %v/%v/%v, want 0.0021/0.0008/0.0029",
			got.Cost.Input, got.Cost.Output, got.Cost.Total)
	}
	if got.Cost.Currency != "USD" {
		t.Errorf("currency = %q, want USD", got.Cost.Currency)
	}
}

func TestEmitSessionStatsSuppressedWhenNoRequests(t *testing.T) {
	session := &AgentSession{config: &config.Config{}}
	out := captureStdout(t, session.emitSessionStats)
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected no output for zero requests, got %q", out)
	}
}

func TestEmitSessionStatsZeroCostWhenPricingDisabled(t *testing.T) {
	session := &AgentSession{
		model:                 "some/model",
		pricingService:        fakePricing(0, 0, 0), // pricing disabled / zero cost
		config:                &config.Config{Pricing: config.PricingConfig{Currency: "USD"}},
		totalPromptTokens:     10,
		totalCompletionTokens: 5,
		totalTokens:           15,
		requestCount:          1,
	}

	got := decodeStatsLine(t, session)

	if got.Cost.Total != 0 || got.Cost.Input != 0 || got.Cost.Output != 0 {
		t.Errorf("expected zero cost, got %v/%v/%v", got.Cost.Input, got.Cost.Output, got.Cost.Total)
	}
	if got.Cost.Currency != "USD" {
		t.Errorf("currency = %q, want USD", got.Cost.Currency)
	}
}

func TestEmitSessionStatsCurrencyFallback(t *testing.T) {
	session := &AgentSession{
		model:          "some/model",
		pricingService: fakePricing(0, 0, 0.01),
		config:         &config.Config{},
		requestCount:   1,
	}

	got := decodeStatsLine(t, session)

	if got.Cost.Currency != "USD" {
		t.Errorf("currency = %q, want USD fallback", got.Cost.Currency)
	}
}

func TestEmitSessionStatsNilPricingServiceSafe(t *testing.T) {
	session := &AgentSession{
		model:             "some/model",
		pricingService:    nil,
		config:            &config.Config{},
		totalPromptTokens: 10,
		requestCount:      1,
	}

	got := decodeStatsLine(t, session)

	if got.Cost.Total != 0 {
		t.Errorf("expected zero cost with nil pricing service, got %v", got.Cost.Total)
	}
	if got.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", got.PromptTokens)
	}
}

func TestProcessSyncResponseAccumulatesTokens(t *testing.T) {
	cfg := &config.Config{Agent: config.AgentConfig{MaxConcurrentTools: 1}}

	usage := func(p, c, total int64) *sdk.CompletionUsage {
		return &sdk.CompletionUsage{PromptTokens: p, CompletionTokens: c, TotalTokens: total}
	}

	t.Run("accumulates usage from a content response", func(t *testing.T) {
		session := &AgentSession{config: cfg, conversation: []ConversationMessage{}}
		_ = captureStdout(t, func() {
			if err := session.processSyncResponse(&domain.ChatSyncResponse{
				Content: "done", Usage: usage(100, 20, 120),
			}, "req_1"); err != nil {
				t.Fatalf("processSyncResponse: %v", err)
			}
		})
		if session.totalPromptTokens != 100 || session.totalCompletionTokens != 20 ||
			session.totalTokens != 120 || session.requestCount != 1 {
			t.Errorf("accumulators = %d/%d/%d req=%d, want 100/20/120 req=1",
				session.totalPromptTokens, session.totalCompletionTokens,
				session.totalTokens, session.requestCount)
		}
	})

	t.Run("nil usage does not increment request count", func(t *testing.T) {
		session := &AgentSession{config: cfg, conversation: []ConversationMessage{}}
		_ = captureStdout(t, func() {
			if err := session.processSyncResponse(&domain.ChatSyncResponse{Content: "done"}, "req_1"); err != nil {
				t.Fatalf("processSyncResponse: %v", err)
			}
		})
		if session.requestCount != 0 {
			t.Errorf("requestCount = %d, want 0 for nil usage", session.requestCount)
		}
	})

	t.Run("usage counted even when response content is empty", func(t *testing.T) {
		session := &AgentSession{config: cfg, conversation: []ConversationMessage{}}
		if err := session.processSyncResponse(&domain.ChatSyncResponse{
			Content: "", Usage: usage(5, 3, 8),
		}, "req_1"); err != nil {
			t.Fatalf("processSyncResponse: %v", err)
		}
		if session.requestCount != 1 || session.totalTokens != 8 {
			t.Errorf("requestCount=%d totalTokens=%d, want 1 / 8 (increment precedes early return)",
				session.requestCount, session.totalTokens)
		}
		if len(session.conversation) != 0 {
			t.Errorf("expected no message recorded for empty response, got %d", len(session.conversation))
		}
	})
}

func TestConvertFromConversationEntry(t *testing.T) {
	session := &AgentSession{
		model: "openai/gpt-4",
	}

	tests := getConvertFromConversationEntryTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := session.convertFromConversationEntry(tt.entry)

			if result.Role != tt.expected.Role {
				t.Errorf("Role = %v, want %v", result.Role, tt.expected.Role)
			}

			if result.Content != tt.expected.Content {
				t.Errorf("Content = %v, want %v", result.Content, tt.expected.Content)
			}

			if !result.Timestamp.Equal(tt.expected.Timestamp) {
				t.Errorf("Timestamp = %v, want %v", result.Timestamp, tt.expected.Timestamp)
			}

			if result.Internal != tt.expected.Internal {
				t.Errorf("Internal = %v, want %v", result.Internal, tt.expected.Internal)
			}

			if result.ToolCallID != tt.expected.ToolCallID {
				t.Errorf("ToolCallID = %v, want %v", result.ToolCallID, tt.expected.ToolCallID)
			}

			if result.ReasoningContent != tt.expected.ReasoningContent {
				t.Errorf("ReasoningContent = %q, want %q", result.ReasoningContent, tt.expected.ReasoningContent)
			}

			if len(result.Images) != len(tt.expected.Images) {
				t.Errorf("Images length = %v, want %v", len(result.Images), len(tt.expected.Images))
			}

			if tt.expected.ToolCalls != nil {
				if result.ToolCalls == nil {
					t.Error("ToolCalls is nil, expected non-nil")
				} else if len(*result.ToolCalls) != len(*tt.expected.ToolCalls) {
					t.Errorf("ToolCalls length = %v, want %v", len(*result.ToolCalls), len(*tt.expected.ToolCalls))
				}
			}

			validateToolExecution(t, result.ToolExecution, tt.expected.ToolExecution)
		})
	}
}

func getConvertFromConversationEntryTestCases() []struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	var tests []struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}

	tests = append(tests, getUserMessageWithPlainTextTestCase())
	tests = append(tests, getAssistantMessageWithToolCallsTestCase())
	tests = append(tests, getToolResponseWithToolCallIDTestCase())
	tests = append(tests, getMessageWithImagesTestCase())
	tests = append(tests, getInternalMessageTestCase())
	tests = append(tests, getMessageWithToolExecutionMetadataTestCase())

	return tests
}

func getUserMessageWithPlainTextTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "user message with plain text",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Hello, how are you?"),
			},
			Model:  "openai/gpt-4",
			Time:   mockTime(),
			Hidden: false,
		},
		expected: ConversationMessage{
			Role:      "user",
			Content:   "Hello, how are you?",
			Timestamp: mockTime(),
			Internal:  false,
		},
	}
}

func getAssistantMessageWithToolCallsTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "assistant message with tool calls",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(""),
				ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
					{
						ID: "call_1",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Read",
							Arguments: `{"file_path":"test.txt"}`,
						},
					},
				},
			},
			Model: "openai/gpt-4",
			Time:  mockTime(),
		},
		expected: ConversationMessage{
			Role:      "assistant",
			Content:   "",
			Timestamp: mockTime(),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{
					ID: "call_1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "Read",
						Arguments: `{"file_path":"test.txt"}`,
					},
				},
			},
		},
	}
}

func getToolResponseWithToolCallIDTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "tool response with tool_call_id",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("File content here"),
				ToolCallID: stringPtr("call_1"),
			},
			Model: "openai/gpt-4",
			Time:  mockTime(),
		},
		expected: ConversationMessage{
			Role:       "tool",
			Content:    "File content here",
			Timestamp:  mockTime(),
			ToolCallID: "call_1",
		},
	}
}

func getMessageWithImagesTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "message with images",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Check this image"),
			},
			Model: "openai/gpt-4",
			Time:  mockTime(),
			Images: []domain.ImageAttachment{
				{
					Filename: "screenshot.png",
					MimeType: "image/png",
					Data:     "base64data",
				},
			},
		},
		expected: ConversationMessage{
			Role:      "user",
			Content:   "Check this image",
			Timestamp: mockTime(),
			Images: []domain.ImageAttachment{
				{
					Filename: "screenshot.png",
					MimeType: "image/png",
					Data:     "base64data",
				},
			},
		},
	}
}

func getInternalMessageTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "internal message",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Continue working"),
			},
			Model:  "openai/gpt-4",
			Time:   mockTime(),
			Hidden: true,
		},
		expected: ConversationMessage{
			Role:      "user",
			Content:   "Continue working",
			Timestamp: mockTime(),
			Internal:  true,
		},
	}
}

func getMessageWithToolExecutionMetadataTestCase() struct {
	name     string
	entry    domain.ConversationEntry
	expected ConversationMessage
} {
	return struct {
		name     string
		entry    domain.ConversationEntry
		expected ConversationMessage
	}{
		name: "message with tool execution metadata",
		entry: domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("Result data"),
				ToolCallID: stringPtr("call_2"),
			},
			Model: "openai/gpt-4",
			Time:  mockTime(),
			ToolExecution: &domain.ToolExecutionResult{
				ToolName: "Read",
				Success:  true,
				Data:     "file content",
			},
		},
		expected: ConversationMessage{
			Role:       "tool",
			Content:    "Result data",
			Timestamp:  mockTime(),
			ToolCallID: "call_2",
			ToolExecution: &domain.ToolExecutionResult{
				ToolName: "Read",
				Success:  true,
				Data:     "file content",
			},
		},
	}
}

func validateToolExecution(t *testing.T, actual, expected *domain.ToolExecutionResult) {
	t.Helper()
	if expected != nil {
		if actual == nil {
			t.Error("ToolExecution is nil, expected non-nil")
			return
		}
		if actual.ToolName != expected.ToolName {
			t.Errorf("ToolExecution.ToolName = %v, want %v", actual.ToolName, expected.ToolName)
		}
		if actual.Success != expected.Success {
			t.Errorf("ToolExecution.Success = %v, want %v", actual.Success, expected.Success)
		}
	}
}

func stringPtr(s string) *string {
	return &s
}

// TestReasoningContentRoundTripSyncPath is a regression test for issue #428:
// the non-streaming agent path used to drop reasoning_content end-to-end,
// which broke multi-turn DeepSeek v4-pro requests after a tool call.
//
// This test exercises three layers of the fix:
//  1. processSyncResponse persists reasoning_content from ChatSyncResponse
//     onto the assistant ConversationMessage.
//  2. buildSDKMessages emits both `reasoning` and `reasoning_content` on the
//     outbound sdk.Message (provider-agnostic).
//  3. convertToConversationEntry / convertFromConversationEntry round-trip
//     the field through storage so resumed sessions still carry it.
func TestReasoningContentRoundTripSyncPath(t *testing.T) {
	const reasoning = "The user asked me to echo, so I'll call Bash."

	t.Run("processSyncResponse persists reasoning_content on assistant message", func(t *testing.T) {
		session := &AgentSession{
			toolService:  &domainmocks.FakeToolService{},
			config:       &config.Config{Agent: config.AgentConfig{MaxConcurrentTools: 1}},
			conversation: []ConversationMessage{},
		}

		resp := &domain.ChatSyncResponse{
			RequestID:        "req_1",
			Content:          "",
			ReasoningContent: reasoning,
			ToolCalls: []sdk.ChatCompletionMessageToolCall{{
				ID: "call_1",
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Bash",
					Arguments: `{"command":"echo hi"}`,
				},
			}},
		}

		if err := session.processSyncResponse(resp, "req_1"); err != nil {
			t.Fatalf("processSyncResponse: %v", err)
		}

		if len(session.conversation) == 0 {
			t.Fatal("expected conversation to contain assistant message")
		}
		assistant := session.conversation[0]
		if assistant.Role != "assistant" {
			t.Fatalf("expected first message to be assistant, got %q", assistant.Role)
		}
		if assistant.ReasoningContent != reasoning {
			t.Errorf("assistant.ReasoningContent = %q, want %q", assistant.ReasoningContent, reasoning)
		}
	})

	t.Run("buildSDKMessages emits reasoning and reasoning_content on outbound", func(t *testing.T) {
		session := &AgentSession{
			conversation: []ConversationMessage{
				{Role: "user", Content: "echo hi"},
				{
					Role:             "assistant",
					Content:          "",
					ReasoningContent: reasoning,
					ToolCalls: &[]sdk.ChatCompletionMessageToolCall{{
						ID: "call_1",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name: "Bash", Arguments: `{"command":"echo hi"}`,
						},
					}},
				},
				{Role: "tool", Content: "hi", ToolCallID: "call_1"},
			},
		}

		msgs := session.buildSDKMessages()
		if len(msgs) != 3 {
			t.Fatalf("expected 3 sdk messages, got %d", len(msgs))
		}

		out := msgs[1]
		if out.Reasoning == nil || *out.Reasoning != reasoning {
			t.Errorf("outbound Reasoning = %v, want pointer to %q", out.Reasoning, reasoning)
		}
		if out.ReasoningContent == nil || *out.ReasoningContent != reasoning {
			t.Errorf("outbound ReasoningContent = %v, want pointer to %q", out.ReasoningContent, reasoning)
		}
	})

	t.Run("convertTo/From round-trips reasoning_content through storage", func(t *testing.T) {
		session := &AgentSession{model: "deepseek/deepseek-v4-pro"}

		original := ConversationMessage{
			Role:             "assistant",
			Content:          "",
			ReasoningContent: reasoning,
			Timestamp:        mockTime(),
		}

		entry := session.convertToConversationEntry(original)
		if entry.ReasoningContent != reasoning {
			t.Errorf("entry.ReasoningContent = %q, want %q", entry.ReasoningContent, reasoning)
		}
		if entry.Message.ReasoningContent == nil || *entry.Message.ReasoningContent != reasoning {
			t.Errorf("entry.Message.ReasoningContent = %v, want pointer to %q", entry.Message.ReasoningContent, reasoning)
		}

		restored := session.convertFromConversationEntry(entry)
		if restored.ReasoningContent != reasoning {
			t.Errorf("restored.ReasoningContent = %q, want %q", restored.ReasoningContent, reasoning)
		}
	})
}

func TestInjectSystemReminderIfDue(t *testing.T) {
	newSession := func(enabled bool, interval int, text string) *AgentSession {
		return &AgentSession{
			config: &config.Config{
				Prompts: config.PromptsConfig{
					Agent: config.PromptsAgentConfig{
						SystemReminders: config.PromptsAgentRemindersConfig{
							Enabled:      enabled,
							Interval:     interval,
							ReminderText: text,
						},
					},
				},
			},
			conversation: []ConversationMessage{},
		}
	}

	t.Run("appends hidden user message and emits stream event when due", func(t *testing.T) {
		s := newSession(true, 2, "remember to push")
		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.injectSystemReminderIfDue(2)

		if len(s.conversation) != 1 {
			t.Fatalf("expected 1 message appended, got %d", len(s.conversation))
		}
		msg := s.conversation[0]
		if msg.Role != "user" || msg.Content != "remember to push" || !msg.Internal {
			t.Errorf("unexpected message: %+v", msg)
		}

		var event map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event); err != nil {
			t.Fatalf("unmarshal stream event: %v (raw: %q)", err, buf.String())
		}
		if event["kind"] != "system_reminder" {
			t.Errorf("kind = %v, want system_reminder", event["kind"])
		}
		if event["hidden"] != true {
			t.Errorf("hidden = %v, want true", event["hidden"])
		}
		if v, _ := event["turn"].(float64); v != 2 {
			t.Errorf("turn = %v, want 2", event["turn"])
		}
		if v, _ := event["interval"].(float64); v != 2 {
			t.Errorf("interval = %v, want 2", event["interval"])
		}
	})

	t.Run("no-op when disabled", func(t *testing.T) {
		s := newSession(false, 2, "ignored")
		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.injectSystemReminderIfDue(2)

		if len(s.conversation) != 0 {
			t.Errorf("expected no messages, got %d", len(s.conversation))
		}
		if buf.Len() != 0 {
			t.Errorf("expected no stream event, got %q", buf.String())
		}
	})

	t.Run("no-op between intervals", func(t *testing.T) {
		s := newSession(true, 5, "wait")
		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.injectSystemReminderIfDue(3)

		if len(s.conversation) != 0 {
			t.Errorf("expected no messages, got %d", len(s.conversation))
		}
		if buf.Len() != 0 {
			t.Errorf("expected no stream event, got %q", buf.String())
		}
	})

	newWrapUpSession := func(enabled bool, interval int, reminderText, wrapUpText string, wrapUpThreshold int, maxTurns int) *AgentSession {
		return &AgentSession{
			config: &config.Config{
				Agent: config.AgentConfig{
					MaxTurns: maxTurns,
				},
				Prompts: config.PromptsConfig{
					Agent: config.PromptsAgentConfig{
						SystemReminders: config.PromptsAgentRemindersConfig{
							Enabled:         enabled,
							Interval:        interval,
							ReminderText:    reminderText,
							WrapUpText:      wrapUpText,
							WrapUpThreshold: wrapUpThreshold,
						},
					},
				},
			},
			maxTurns:     maxTurns,
			conversation: []ConversationMessage{},
		}
	}

	t.Run("wrap-up fires within threshold even when turn is not an interval boundary", func(t *testing.T) {
		s := newWrapUpSession(true, 4, "regular reminder", "wrap up now!", 1, 10)

		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.injectSystemReminderIfDue(9)
		if len(s.conversation) != 1 {
			t.Fatalf("expected 1 message at turn 9, got %d", len(s.conversation))
		}
		if s.conversation[0].Content != "wrap up now!" {
			t.Errorf("expected wrap-up text at turn 9, got %q", s.conversation[0].Content)
		}
		var event map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if event["phase"] != "wrap_up" {
			t.Errorf("phase = %v, want wrap_up", event["phase"])
		}
	})

	t.Run("wrap-up fires when wrap_up_threshold < interval", func(t *testing.T) {
		s := newWrapUpSession(true, 5, "regular reminder", "wrap up now!", 1, 10)

		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.injectSystemReminderIfDue(9)
		if len(s.conversation) != 1 {
			t.Fatalf("expected 1 message when wrap_up_threshold < interval, got %d", len(s.conversation))
		}
		if s.conversation[0].Content != "wrap up now!" {
			t.Errorf("expected wrap-up text, got %q", s.conversation[0].Content)
		}
	})

	t.Run("wrap-up uses wrap_up_text within threshold", func(t *testing.T) {
		s := newWrapUpSession(true, 2, "regular reminder", "wrap up now!", 3, 10)
		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.injectSystemReminderIfDue(8)
		if len(s.conversation) != 1 {
			t.Fatalf("expected 1 message, got %d", len(s.conversation))
		}
		if s.conversation[0].Content != "wrap up now!" {
			t.Errorf("expected wrap-up text, got %q", s.conversation[0].Content)
		}
	})

	t.Run("regular text used before wrap-up threshold", func(t *testing.T) {
		s := newWrapUpSession(true, 2, "regular reminder", "wrap up now!", 3, 10)
		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.injectSystemReminderIfDue(4)
		if len(s.conversation) != 1 {
			t.Fatalf("expected 1 message, got %d", len(s.conversation))
		}
		if s.conversation[0].Content != "regular reminder" {
			t.Errorf("expected regular text before threshold, got %q", s.conversation[0].Content)
		}
	})

	t.Run("wrap-up empty falls back to regular text", func(t *testing.T) {
		s := newWrapUpSession(true, 2, "regular reminder", "", 3, 10)
		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.injectSystemReminderIfDue(8)
		if len(s.conversation) != 1 {
			t.Fatalf("expected 1 message, got %d", len(s.conversation))
		}
		if s.conversation[0].Content != "regular reminder" {
			t.Errorf("expected regular text fallback, got %q", s.conversation[0].Content)
		}
	})

	t.Run("no-op on turn 0", func(t *testing.T) {
		s := newSession(true, 2, "x")
		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.injectSystemReminderIfDue(0)

		if len(s.conversation) != 0 {
			t.Errorf("expected no messages, got %d", len(s.conversation))
		}
		if buf.Len() != 0 {
			t.Errorf("expected no stream event, got %q", buf.String())
		}
	})
}

// rolloverFakeOptimizer is a minimal ConversationOptimizer used to exercise
// the in-loop rollover path. It returns a single summary message regardless
// of input - the rollover machinery only cares that something is returned
// that PerformRollover can re-add to the new conversation.
type rolloverFakeOptimizer struct{ calls int }

func (f *rolloverFakeOptimizer) OptimizeMessages(_ []sdk.Message, _ string, _ bool) []sdk.Message {
	f.calls++
	return []sdk.Message{
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("--- Context Summary ---\nfake summary\n--- End Summary ---")},
	}
}

// newAgentRolloverFixture stands up a real SessionRolloverManager backed by
// an in-memory SQLite PersistentConversationRepository and an in-memory
// SessionGroupStorage. The same pattern is used in
// internal/services/session_rollover_manager_test.go - inlined here so the
// cmd-package test stays self-contained.
func newAgentRolloverFixture(t *testing.T) (*services.SessionRolloverManager, *services.PersistentConversationRepository, storage.SessionGroupStorage, func()) {
	t.Helper()

	storageBackend, err := storage.NewSQLiteStorage(storage.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("create sqlite storage: %v", err)
	}
	repo := services.NewPersistentConversationRepository(&services.ToolFormatterService{}, nil, storageBackend)

	cfg := &config.Config{}
	cfg.Compact.Enabled = true
	cfg.Compact.AutoAt = 80
	cfg.Compact.RolloverOnIdleMinutes = 0
	cfg.Compact.KeepFirstMessages = 2

	groupStore := storage.NewMemorySessionGroupStorage()
	mgr := services.NewSessionRolloverManager(
		cfg,
		&rolloverFakeOptimizer{},
		repo,
		services.NewTokenizerService(services.DefaultTokenizerConfig()),
		groupStore,
	)

	cleanup := func() {
		_ = repo.Close()
		_ = storageBackend.Close()
	}
	return mgr, repo, groupStore, cleanup
}

func TestMaybeRolloverInLoop(t *testing.T) {
	t.Run("no-op when rolloverManager is nil", func(t *testing.T) {
		s := &AgentSession{
			sessionID: "orig-id",
			model:     "openai/gpt-4",
		}
		out := captureStdout(t, func() { s.maybeRollover() })

		if s.sessionID != "orig-id" {
			t.Errorf("sessionID must not change when rolloverManager is nil; got %q", s.sessionID)
		}
		if out != "" {
			t.Errorf("expected no stdout output, got %q", out)
		}
	})

	t.Run("no-op when ShouldRollover returns false", func(t *testing.T) {
		mgr, repo, _, cleanup := newAgentRolloverFixture(t)
		defer cleanup()

		if err := repo.StartNewConversation("Initial"); err != nil {
			t.Fatalf("start: %v", err)
		}
		originalID := repo.GetCurrentConversationID()

		s := &AgentSession{
			config:           &config.Config{},
			conversationRepo: repo,
			sessionID:        originalID,
			model:            "openai/gpt-4",
			rolloverManager:  mgr,
		}

		out := captureStdout(t, func() { s.maybeRollover() })

		if s.sessionID != originalID {
			t.Errorf("sessionID must not change when ShouldRollover is false; got %q want %q", s.sessionID, originalID)
		}
		if out != "" {
			t.Errorf("expected no stdout output, got %q", out)
		}
	})

	t.Run("token trigger fires mid-loop: swaps sessionID, preserves groupKey, preserves completedTurns", func(t *testing.T) {
		mgr, repo, groupStore, cleanup := newAgentRolloverFixture(t)
		defer cleanup()

		if _, _, err := mgr.ResolveSessionID("channel-test-group"); err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if err := repo.StartNewConversation("Initial"); err != nil {
			t.Fatalf("start: %v", err)
		}
		originalID := repo.GetCurrentConversationID()

		if err := repo.AddMessage(domain.ConversationEntry{
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
			Time:    time.Now(),
		}); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
		if err := repo.AddTokenUsage("moonshot/moonshot-v1-8k", 7000, 100, 7100); err != nil {
			t.Fatalf("AddTokenUsage: %v", err)
		}

		s := &AgentSession{
			config:           &config.Config{},
			conversationRepo: repo,
			sessionID:        originalID,
			model:            "moonshot/moonshot-v1-8k",
			rolloverManager:  mgr,
			groupKey:         "channel-test-group",
			completedTurns:   7,
		}

		captureStdout(t, func() { s.maybeRollover() })

		if s.sessionID == "" || s.sessionID == originalID {
			t.Errorf("sessionID must be swapped to a new id; got %q (original %q)", s.sessionID, originalID)
		}
		if got := repo.GetCurrentConversationID(); got != s.sessionID {
			t.Errorf("repo must be aligned with new session: got %q want %q", got, s.sessionID)
		}
		if s.groupKey != "channel-test-group" {
			t.Errorf("groupKey must be preserved across rollover; got %q", s.groupKey)
		}
		if s.completedTurns != 7 {
			t.Errorf("completedTurns must NOT be reset on rollover; got %d want 7", s.completedTurns)
		}

		entry, ok, err := groupStore.GetSessionGroup(context.Background(), "channel-test-group")
		if err != nil || !ok {
			t.Fatalf("GetSessionGroup: ok=%v err=%v", ok, err)
		}
		if entry.CurrentSessionID != s.sessionID {
			t.Errorf("group index must point at new id %q, got %q", s.sessionID, entry.CurrentSessionID)
		}
		if len(entry.History) == 0 || entry.History[len(entry.History)-1] != originalID {
			t.Errorf("group history must record previous id %q, got %v", originalID, entry.History)
		}
	})

	t.Run("PerformRollover error: no swap, sessionID unchanged", func(t *testing.T) {
		mgr, repo, _, cleanup := newAgentRolloverFixture(t)
		defer cleanup()

		if err := repo.StartNewConversation("Initial"); err != nil {
			t.Fatalf("start: %v", err)
		}
		originalID := repo.GetCurrentConversationID()

		if err := repo.AddMessage(domain.ConversationEntry{
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hidden")},
			Time:    time.Now(),
			Hidden:  true,
		}); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
		if err := repo.AddTokenUsage("moonshot/moonshot-v1-8k", 7000, 100, 7100); err != nil {
			t.Fatalf("AddTokenUsage: %v", err)
		}

		s := &AgentSession{
			config:           &config.Config{},
			conversationRepo: repo,
			sessionID:        originalID,
			model:            "moonshot/moonshot-v1-8k",
			rolloverManager:  mgr,
		}

		captureStdout(t, func() { s.maybeRollover() })

		if s.sessionID != originalID {
			t.Errorf("sessionID must not change on PerformRollover error; got %q want %q", s.sessionID, originalID)
		}
	})
}
