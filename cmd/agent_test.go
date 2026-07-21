package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	models "github.com/inference-gateway/cli/internal/models"
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

func TestFormatToolResult_FailedWithEmptyErrorSurfacesData(t *testing.T) {
	session := &AgentSession{}

	result := &domain.ToolExecutionResult{
		ToolName: "Bash",
		Success:  false,
		Data: &domain.BashToolResult{
			Command:  "gh search code foo --path bar",
			Output:   "unknown flag: --path",
			Error:    "exit status 1",
			ExitCode: 1,
		},
	}

	out := session.formatToolResult(result)

	if !strings.HasPrefix(out, "Tool execution failed:") {
		t.Fatalf("expected the failure prefix, got %q", out)
	}
	if strings.TrimSpace(strings.TrimPrefix(out, "Tool execution failed:")) == "" {
		t.Fatal("expected a non-empty failure body, got a bare envelope")
	}
	if !strings.Contains(out, "unknown flag: --path") {
		t.Errorf("expected the body to surface the command output, got %q", out)
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
			cfg.Tools.Safety.RequireApproval = true
			cfg.Tools.Safety.ApprovalBehaviour = behaviour

			session := &AgentSession{
				toolService:     mockToolService,
				config:          cfg,
				requireApproval: false,
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

func TestInheritedSubagentMode(t *testing.T) {
	t.Run("unset defaults to Standard", func(t *testing.T) {
		t.Setenv("INFER_SUBAGENT_AGENT_MODE", "")
		if got := inheritedSubagentMode(); got != domain.AgentModeStandard {
			t.Fatalf("inheritedSubagentMode() = %v, want Standard", got)
		}
	})
	t.Run("auto parses to AutoAccept", func(t *testing.T) {
		t.Setenv("INFER_SUBAGENT_AGENT_MODE", "auto")
		if got := inheritedSubagentMode(); got != domain.AgentModeAutoAccept {
			t.Fatalf("inheritedSubagentMode() = %v, want AutoAccept", got)
		}
	})
	t.Run("unrecognized falls back to Standard", func(t *testing.T) {
		t.Setenv("INFER_SUBAGENT_AGENT_MODE", "bogus")
		if got := inheritedSubagentMode(); got != domain.AgentModeStandard {
			t.Fatalf("inheritedSubagentMode() = %v, want Standard", got)
		}
	})
}

// TestExecuteToolCalls_AutoAcceptBypassesApproval verifies that a headless
// subagent inheriting Auto-Accept (via INFER_SUBAGENT_AGENT_MODE) runs an
// approval-requiring tool without an approver attached - matching chat YOLO.
// Contrast with TestExecuteToolCalls_BlocksWhenNoApprover, which blocks the
// same call in the default Standard mode.
func TestExecuteToolCalls_AutoAcceptBypassesApproval(t *testing.T) {
	mockToolService := &domainmocks.FakeToolService{}
	mockToolService.ExecuteToolReturns(&domain.ToolExecutionResult{ToolName: "Write", Success: true, Data: "ok"}, nil)

	cfg := &config.Config{Agent: config.AgentConfig{MaxConcurrentTools: 5}}
	cfg.Tools.Safety.RequireApproval = true
	cfg.Tools.Safety.ApprovalBehaviour = config.ApprovalBehaviourBlock

	session := &AgentSession{
		toolService:     mockToolService,
		config:          cfg,
		agentMode:       domain.AgentModeAutoAccept,
		requireApproval: false,
	}

	results := session.executeToolCalls([]sdk.ChatCompletionMessageToolCall{
		{ID: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{
			Name: "Write", Arguments: `{"file_path":"x","content":"y"}`,
		}},
	})

	if mockToolService.ExecuteToolCallCount() != 1 {
		t.Errorf("auto-accept must execute the tool, got %d calls", mockToolService.ExecuteToolCallCount())
	}
	if len(results) != 1 || results[0].ToolExecution == nil || !results[0].ToolExecution.Success {
		t.Errorf("expected a successful execution result, got %+v", results)
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
		requireApproval: true,
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

// seededRepo returns an in-memory conversation repository - the shared sink the
// agent service accumulates into - seeded with n AddTokenUsage calls of the
// given per-request token counts.
func seededRepo(t *testing.T, pricing domain.PricingService, model string, n, prompt, completion, total int) *services.InMemoryConversationRepository {
	t.Helper()
	repo := services.NewInMemoryConversationRepository(nil, pricing)
	for range n {
		if err := repo.AddTokenUsage(model, prompt, completion, total); err != nil {
			t.Fatalf("AddTokenUsage: %v", err)
		}
	}
	return repo
}

func TestEmitSessionStatsFullLine(t *testing.T) {
	session := &AgentSession{
		model:  "deepseek/deepseek-v4-flash",
		config: &config.Config{Pricing: config.PricingConfig{Currency: "USD"}},
		conversationRepo: seededRepo(t, fakePricing(0.25, 0.125, 0.375),
			"deepseek/deepseek-v4-flash", 7, 3000, 180, 3180),
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
	if got.Cost.Input != 1.75 || got.Cost.Output != 0.875 || got.Cost.Total != 2.625 {
		t.Errorf("cost = %v/%v/%v, want 1.75/0.875/2.625",
			got.Cost.Input, got.Cost.Output, got.Cost.Total)
	}
	if got.Cost.Currency != "USD" {
		t.Errorf("currency = %q, want USD", got.Cost.Currency)
	}
}

func TestEmitSessionStatsSuppressedWhenNoRequests(t *testing.T) {
	t.Run("empty repo", func(t *testing.T) {
		session := &AgentSession{
			config:           &config.Config{},
			conversationRepo: services.NewInMemoryConversationRepository(nil, nil),
		}
		out := captureStdout(t, session.emitSessionStats)
		if strings.TrimSpace(out) != "" {
			t.Errorf("expected no output for zero requests, got %q", out)
		}
	})

	t.Run("nil repo", func(t *testing.T) {
		session := &AgentSession{config: &config.Config{}}
		out := captureStdout(t, session.emitSessionStats)
		if strings.TrimSpace(out) != "" {
			t.Errorf("expected no output with nil repo, got %q", out)
		}
	})
}

func TestEmitSessionStatsZeroCostWhenPricingDisabled(t *testing.T) {
	session := &AgentSession{
		model:            "some/model",
		config:           &config.Config{Pricing: config.PricingConfig{Currency: "USD"}},
		conversationRepo: seededRepo(t, fakePricing(0, 0, 0), "some/model", 1, 10, 5, 15),
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
		model:            "some/model",
		config:           &config.Config{},
		conversationRepo: seededRepo(t, fakePricing(0, 0, 0.01), "some/model", 1, 10, 5, 15),
	}

	got := decodeStatsLine(t, session)

	if got.Cost.Currency != "USD" {
		t.Errorf("currency = %q, want USD fallback", got.Cost.Currency)
	}
}

func TestEmitSessionStatsNilPricingServiceSafe(t *testing.T) {
	session := &AgentSession{
		model:            "some/model",
		config:           &config.Config{},
		conversationRepo: seededRepo(t, nil, "some/model", 1, 10, 0, 10),
	}

	got := decodeStatsLine(t, session)

	if got.Cost.Total != 0 {
		t.Errorf("expected zero cost with nil pricing service, got %v", got.Cost.Total)
	}
	if got.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", got.PromptTokens)
	}
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

func TestDispatchHooks_Reminders(t *testing.T) {
	newSession := func(enabled bool, reminders ...config.ReminderConfig) *AgentSession {
		return &AgentSession{
			config: &config.Config{
				Reminders: config.RemindersConfig{Enabled: enabled, Reminders: reminders},
			},
			conversation:   []ConversationMessage{},
			firedReminders: map[string]bool{},
			maxTurns:       10,
		}
	}

	t.Run("appends internal user message and emits hook/name-tagged event", func(t *testing.T) {
		s := newSession(true, config.ReminderConfig{Name: "todo", Text: "remember to push", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerInterval, Interval: 2})
		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.dispatchHooks(domain.HookPreStream, 2)

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
		if event["hook"] != "pre_stream" {
			t.Errorf("hook = %v, want pre_stream", event["hook"])
		}
		if event["name"] != "todo" {
			t.Errorf("name = %v, want todo", event["name"])
		}
		if v, _ := event["turn"].(float64); v != 2 {
			t.Errorf("turn = %v, want 2", event["turn"])
		}
	})

	t.Run("no-op when disabled", func(t *testing.T) {
		s := newSession(false, config.ReminderConfig{Name: "todo", Text: "ignored", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerAlways})
		var buf bytes.Buffer
		t.Cleanup(streamevent.SetWriter(&buf))
		t.Cleanup(streamevent.SetDebugEnabledForTest(true))

		s.dispatchHooks(domain.HookPreStream, 2)

		if len(s.conversation) != 0 || buf.Len() != 0 {
			t.Errorf("disabled reminders must not inject (%d msgs) or emit (%q)", len(s.conversation), buf.String())
		}
	})

	t.Run("interval miss does not fire", func(t *testing.T) {
		s := newSession(true, config.ReminderConfig{Name: "todo", Text: "x", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerInterval, Interval: 5})
		s.dispatchHooks(domain.HookPreStream, 3)
		if len(s.conversation) != 0 {
			t.Errorf("interval miss must not fire, got %d", len(s.conversation))
		}
	})

	t.Run("turns_before_max reminder fires near max at post_session", func(t *testing.T) {
		s := newSession(true, config.ReminderConfig{Name: "wrap", Text: "wrap up now", Hook: domain.HookPostSession, Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 2})
		s.dispatchHooks(domain.HookPostSession, 9)
		if len(s.conversation) != 1 || s.conversation[0].Content != "wrap up now" {
			t.Errorf("turns_before_max reminder should fire near max, got %+v", s.conversation)
		}
	})

	t.Run("skips injection while awaiting tool results", func(t *testing.T) {
		s := newSession(true, config.ReminderConfig{Name: "todo", Text: "x", Hook: domain.HookPostTool, Trigger: config.ReminderTriggerAlways})
		toolCalls := []sdk.ChatCompletionMessageToolCall{{ID: "c1"}}
		s.conversation = []ConversationMessage{{Role: "assistant", ToolCalls: &toolCalls}}
		s.dispatchHooks(domain.HookPostTool, 1)
		if len(s.conversation) != 1 {
			t.Errorf("must not inject between tool_calls and results, got %d", len(s.conversation))
		}
	})
}

// Wrap-up behaviour is now the turns_before_max trigger, covered by
// TestDispatchHooks_Reminders and the config truth table in
// config/reminders_test.go.

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
	models.SetGatewayContextWindows(map[string]int{"moonshot/moonshot-v1-8k": 8192})
	t.Cleanup(func() { models.SetGatewayContextWindows(nil) })

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

// hookSession builds a headless session with an enabled command hook plus a bash
// allow-list. allow is the every-mode allow-list (empty means the command is
// off-list). The command writes marker so the test can assert whether it ran -
// independent of stream-event gating.
func hookSession(command, _ string, allow []string) *AgentSession {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode:    config.BashModesConfig{All: config.BashModeAllowConfig{Allow: allow}},
			},
		},
		Hooks: config.HooksConfig{
			Enabled: true,
			Hooks: []config.HookCommandConfig{
				{Name: "marker", Hook: domain.HookPostSession, Command: command, Timeout: 5},
			},
		},
	}
	return &AgentSession{
		config:       cfg,
		hookProvider: cfg.Hooks,
		agentMode:    domain.AgentModeStandard,
		sessionID:    "sess",
		conversation: []ConversationMessage{},
	}
}

func TestAgentSession_DispatchHooks_RunsAllowListedCommandHook(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "hook-ran")
	s := hookSession("touch "+marker, marker, []string{"touch .*"})

	s.dispatchHooks(domain.HookPostSession, 1)

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("post_session command hook did not run: %v", err)
	}
}

// Secure-by-default: an off-list command hook must NOT run in the headless
// (unattended) path - no approver is reachable, so it is skipped, not executed.
func TestAgentSession_DispatchHooks_SkipsOffListCommandHook(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "should-not-exist")
	s := hookSession("touch "+marker, marker, nil) // empty allow-list -> off-list

	s.dispatchHooks(domain.HookPostSession, 1)

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("off-list command hook must not run headless (secure-by-default)")
	}
}

// TestCompletionNotice checks the drained-result header is distilled into a
// clean one-line channel notification (icon + kind/verb, UUID label dropped).
func TestCompletionNotice(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a2a completed", "[A2A Task Completed: 2e6320b3-120d-431e]\n\nA2A_SubmitTask()\n╰── Result: ok", "✅ A2A Task Completed"},
		{"a2a failed", "[A2A Task Failed: abc-123]\n\nsomething broke", "❌ A2A Task Failed"},
		{"subagent completed", "[Subagent Completed: worker-1]", "✅ Subagent Completed"},
		{"empty content", "", "✅ Background task completed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := completionNotice(tt.in); got != tt.want {
				t.Errorf("completionNotice(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
