package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	lipgloss "charm.land/lipgloss/v2"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// stubToolFormatter is a minimal ToolFormatter for tests that need the
// view to render entries containing ToolExecution payloads.
type stubToolFormatter struct{}

func (s *stubToolFormatter) FormatToolCall(toolName string, _ map[string]any) string {
	return toolName + "()"
}
func (s *stubToolFormatter) FormatToolResultForUI(result *domain.ToolExecutionResult, _ int) string {
	if result == nil {
		return ""
	}
	return "Tool: " + result.ToolName
}
func (s *stubToolFormatter) FormatToolResultExpanded(result *domain.ToolExecutionResult, _ int) string {
	if result == nil {
		return ""
	}
	return "Tool: " + result.ToolName
}
func (s *stubToolFormatter) FormatToolResultForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return ""
	}
	return "Tool: " + result.ToolName
}
func (s *stubToolFormatter) ShouldAlwaysExpandTool(_ string) bool { return false }

// createMockStyleProvider creates a mock styles provider for testing
func createMockStyleProvider() *styles.Provider {
	fakeTheme := &uimocks.FakeTheme{}
	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)
	return styles.NewProvider(fakeThemeService)
}

func TestNewConversationView(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	if cv.width != 80 {
		t.Errorf("Expected default width 80, got %d", cv.width)
	}

	if cv.height != 20 {
		t.Errorf("Expected default height 20, got %d", cv.height)
	}

	if cv.expandedToolResults == nil {
		t.Error("Expected expandedToolResults to be initialized")
	}

	if cv.allToolsExpanded {
		t.Error("Expected allToolsExpanded to be false")
	}

	if len(cv.conversation) != 0 {
		t.Errorf("Expected empty conversation, got length %d", len(cv.conversation))
	}
}

func TestConversationView_SetConversation(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Hello"),
			},
			Time: time.Now(),
		},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("Hi there!"),
			},
			Time: time.Now(),
		},
	}

	cv.SetConversation(conversation)

	if len(cv.conversation) != 2 {
		t.Errorf("Expected conversation length 2, got %d", len(cv.conversation))
	}

	if cv.conversation[0].Message.Role != sdk.User {
		t.Errorf("Expected first entry role 'user', got '%s'", cv.conversation[0].Message.Role)
	}

	contentStr, _ := cv.conversation[1].Message.Content.AsMessageContent0()
	if contentStr != "Hi there!" {
		t.Errorf("Expected second entry content 'Hi there!', got '%s'", contentStr)
	}
}

func TestConversationView_GetScrollOffset(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	offset := cv.GetScrollOffset()

	if offset != 0 {
		t.Errorf("Expected scroll offset 0, got %d", offset)
	}
}

func TestConversationView_CanScrollUp(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	if cv.CanScrollUp() {
		t.Error("Expected CanScrollUp to be false when at top")
	}
}

func TestConversationView_CanScrollDown(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	if cv.CanScrollDown() {
		t.Error("Expected CanScrollDown to be false with no content")
	}
}

func TestConversationView_ToggleToolResultExpansion(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Test message"),
			},
			Time: time.Now(),
		},
	}
	cv.SetConversation(conversation)

	cv.ToggleToolResultExpansion(0)

	if !cv.IsToolResultExpanded(0) {
		t.Error("Expected tool result 0 to be expanded after toggle")
	}

	cv.ToggleToolResultExpansion(0)

	if cv.IsToolResultExpanded(0) {
		t.Error("Expected tool result 0 to be collapsed after second toggle")
	}
}

func TestConversationView_ToggleAllToolResultsExpansion(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.Tool,
				Content: sdk.NewMessageContent("Tool result 1"),
			},
			Time: time.Now(),
		},
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("User message"),
			},
			Time: time.Now(),
		},
		{
			Message: sdk.Message{
				Role:    sdk.Tool,
				Content: sdk.NewMessageContent("Tool result 2"),
			},
			Time: time.Now(),
		},
	}
	cv.SetConversation(conversation)

	if cv.IsToolResultExpanded(0) || cv.IsToolResultExpanded(2) {
		t.Error("Expected all tool results to be collapsed initially")
	}

	cv.ToggleAllToolResultsExpansion()

	if !cv.IsToolResultExpanded(0) || !cv.IsToolResultExpanded(2) {
		t.Error("Expected all tool results to be expanded after first toggle")
	}

	if cv.IsToolResultExpanded(1) {
		t.Error("Expected non-tool message to remain unaffected")
	}

	cv.ToggleAllToolResultsExpansion()

	if cv.IsToolResultExpanded(0) || cv.IsToolResultExpanded(2) {
		t.Error("Expected all tool results to be collapsed after second toggle")
	}
}

func TestConversationView_IsToolResultExpanded(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	if cv.IsToolResultExpanded(0) {
		t.Error("Expected tool result 0 to not be expanded initially")
	}

	if cv.IsToolResultExpanded(999) {
		t.Error("Expected non-existent tool result to not be expanded")
	}
}

func TestConversationView_DefaultExpandedDiffTools(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetToolFormatter(&stubToolFormatter{})

	cv.SetConversation([]domain.ConversationEntry{
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("edited")},
			ToolExecution: &domain.ToolExecutionResult{ToolName: "Edit"},
			Time:          time.Now(),
		},
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("ran")},
			ToolExecution: &domain.ToolExecutionResult{ToolName: "Bash"},
			Time:          time.Now(),
		},
	})

	// Edit/MultiEdit diffs are expanded by default; other tools stay collapsed.
	if !cv.IsToolResultExpanded(0) {
		t.Error("expected Edit tool result to be expanded by default")
	}
	if cv.IsToolResultExpanded(1) {
		t.Error("expected Bash tool result to be collapsed by default")
	}

	// ctrl+o / per-entry toggle must still collapse a default-expanded diff.
	cv.ToggleToolResultExpansion(0)
	if cv.IsToolResultExpanded(0) {
		t.Error("expected Edit tool result to collapse after toggle")
	}
	cv.ToggleToolResultExpansion(0)
	if !cv.IsToolResultExpanded(0) {
		t.Error("expected Edit tool result to expand again after second toggle")
	}
}

func TestConversationView_ToggleAllCollapsesDefaultExpanded(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetToolFormatter(&stubToolFormatter{})

	cv.SetConversation([]domain.ConversationEntry{
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("edited")},
			ToolExecution: &domain.ToolExecutionResult{ToolName: "MultiEdit"},
			Time:          time.Now(),
		},
	})

	// The diff is expanded by default, so the first ctrl+o should collapse it.
	if !cv.IsToolResultExpanded(0) {
		t.Fatal("precondition: MultiEdit should be expanded by default")
	}
	cv.ToggleAllToolResultsExpansion()
	if cv.IsToolResultExpanded(0) {
		t.Error("expected first ToggleAll to collapse the default-expanded diff")
	}
	cv.ToggleAllToolResultsExpansion()
	if !cv.IsToolResultExpanded(0) {
		t.Error("expected second ToggleAll to expand again")
	}
}

func TestConversationView_SetWidth(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.SetWidth(120)

	if cv.width != 120 {
		t.Errorf("Expected width 120, got %d", cv.width)
	}

	if cv.Viewport.Width() != 120 {
		t.Errorf("Expected viewport width 120, got %d", cv.Viewport.Width())
	}
}

func TestConversationView_SetHeight(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.SetHeight(30)

	if cv.height != 30 {
		t.Errorf("Expected height 30, got %d", cv.height)
	}

	if cv.Viewport.Height() != 30 {
		t.Errorf("Expected viewport height 30, got %d", cv.Viewport.Height())
	}
}

func TestConversationView_Render(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	output := cv.Render()

	if output == "" {
		t.Error("Expected non-empty render output")
	}

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Test message"),
			},
			Time: time.Now(),
		},
	}

	cv.SetConversation(conversation)
	output = cv.Render()

	if output == "" {
		t.Error("Expected non-empty render output with conversation")
	}
}

func TestBackgroundTaskDisplay_SubmittedCreatesEntry(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{
		TaskID:    "task-1",
		AgentName: "weather-agent",
	}, nil)

	display, ok := cv.backgroundTasks["task-1"]
	if !ok {
		t.Fatal("expected backgroundTasks to contain entry for task-1")
	}
	if display.AgentName != "weather-agent" {
		t.Errorf("expected AgentName 'weather-agent', got %q", display.AgentName)
	}
	if display.State != "submitted" {
		t.Errorf("expected State 'submitted', got %q", display.State)
	}
	if display.IsTerminal {
		t.Error("expected IsTerminal to be false on submission")
	}
}

func TestBackgroundTaskDisplay_StatusUpdateChangesState(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{
		TaskID:    "task-1",
		AgentName: "weather-agent",
	}, nil)
	cv.handleA2ATaskStatusUpdate(domain.A2ATaskStatusUpdateEvent{
		TaskID:  "task-1",
		Status:  "working",
		Message: "fetching forecast",
	}, nil)

	display := cv.backgroundTasks["task-1"]
	if display.State != "working" {
		t.Errorf("expected State 'working', got %q", display.State)
	}
	if display.Message != "fetching forecast" {
		t.Errorf("expected Message 'fetching forecast', got %q", display.Message)
	}
	if display.AgentName != "weather-agent" {
		t.Errorf("expected AgentName preserved as 'weather-agent', got %q", display.AgentName)
	}
}

func TestBackgroundTaskDisplay_CompletedCapturesUsage(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{
		TaskID:    "task-1",
		AgentName: "weather-agent",
	}, nil)

	resultData := map[string]any{
		"task_id":   "task-1",
		"agent_url": "http://example.com",
		"state":     "completed",
		"task": map[string]any{
			"metadata": map[string]any{
				"usage": map[string]any{
					"input_tokens":  42,
					"output_tokens": 100,
					"total_tokens":  142,
				},
			},
		},
	}
	cv.handleA2ATaskCompleted(domain.A2ATaskCompletedEvent{
		TaskID: "task-1",
		Result: domain.ToolExecutionResult{
			ToolName: "A2A_SubmitTask",
			Success:  true,
			Data:     resultData,
		},
	}, nil)

	display := cv.backgroundTasks["task-1"]
	if display.State != "completed" {
		t.Errorf("expected State 'completed', got %q", display.State)
	}
	if !display.IsTerminal {
		t.Error("expected IsTerminal to be true after completion")
	}
	if !strings.Contains(display.UsageJSON, `"input_tokens":42`) {
		t.Errorf("expected UsageJSON to contain input_tokens, got %q", display.UsageJSON)
	}
	if !strings.Contains(display.UsageJSON, `"total_tokens":142`) {
		t.Errorf("expected UsageJSON to contain total_tokens, got %q", display.UsageJSON)
	}
}

func TestBackgroundTaskDisplay_CompletedCapturesExecutionStats(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{
		TaskID:    "task-1",
		AgentName: "browser-agent",
	}, nil)

	resultData := map[string]any{
		"task_id":   "task-1",
		"agent_url": "http://example.com",
		"state":     "completed",
		"task": map[string]any{
			"metadata": map[string]any{
				"usage": map[string]any{
					"prompt_tokens":     12255,
					"completion_tokens": 502,
					"total_tokens":      12757,
				},
				"execution_stats": map[string]any{
					"iterations":   2,
					"messages":     4,
					"tool_calls":   3,
					"failed_tools": 1,
				},
			},
		},
	}
	cv.handleA2ATaskCompleted(domain.A2ATaskCompletedEvent{
		TaskID: "task-1",
		Result: domain.ToolExecutionResult{
			ToolName: "A2A_SubmitTask",
			Success:  true,
			Data:     resultData,
		},
	}, nil)

	display := cv.backgroundTasks["task-1"]
	if !strings.Contains(display.ExecutionStatsJSON, `"tool_calls":3`) {
		t.Errorf("expected ExecutionStatsJSON to contain tool_calls, got %q", display.ExecutionStatsJSON)
	}
	if !strings.Contains(display.ExecutionStatsJSON, `"failed_tools":1`) {
		t.Errorf("expected ExecutionStatsJSON to contain failed_tools, got %q", display.ExecutionStatsJSON)
	}

	out := cv.renderBackgroundTaskLine(display, 0)
	if !strings.Contains(out, "Agent(browser-agent=completed)") {
		t.Errorf("expected header line, got %q", out)
	}
	if !strings.Contains(out, "├── usage=") {
		t.Errorf("expected branched usage line, got %q", out)
	}
	if !strings.Contains(out, "└── execution_stats=") {
		t.Errorf("expected branched execution_stats line, got %q", out)
	}
	if !strings.Contains(out, `"failed_tools":1`) {
		t.Errorf("expected failed_tools=1 in output, got %q", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Errorf("expected exactly 3 lines (2 newlines) when both usage and stats present, got %q", out)
	}
}

func TestBackgroundTaskDisplay_CompletedWithoutMetadataRendersBareName(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	out := cv.renderBackgroundTaskLine(&BackgroundTaskDisplay{
		TaskID:     "t1",
		AgentName:  "old-agent",
		State:      "completed",
		IsTerminal: true,
	}, 0)
	if !strings.Contains(out, "Agent(old-agent=completed)") {
		t.Errorf("expected 'Agent(old-agent=completed)' header, got %q", out)
	}
	if strings.Contains(out, "\n") {
		t.Errorf("expected single-line output when no metadata to show, got %q", out)
	}
}

func TestBackgroundTaskDisplay_CompletedUsesLastBranchWhenOnlyOneDetail(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	out := cv.renderBackgroundTaskLine(&BackgroundTaskDisplay{
		TaskID:     "t1",
		AgentName:  "x",
		State:      "completed",
		UsageJSON:  `{"total_tokens":10}`,
		IsTerminal: true,
	}, 0)
	if !strings.Contains(out, "└── usage=") {
		t.Errorf("expected single detail to use └── branch, got %q", out)
	}
	if strings.Contains(out, "├──") {
		t.Errorf("did not expect ├── branch with only one detail, got %q", out)
	}
}

func TestBackgroundTaskDisplay_FailedCapturesError(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{
		TaskID:    "task-1",
		AgentName: "weather-agent",
	}, nil)
	cv.handleA2ATaskFailed(domain.A2ATaskFailedEvent{
		TaskID: "task-1",
		Error:  "connection refused",
		Result: domain.ToolExecutionResult{
			ToolName: "A2A_SubmitTask",
			Success:  false,
		},
	}, nil)

	display := cv.backgroundTasks["task-1"]
	if display.State != "failed" {
		t.Errorf("expected State 'failed', got %q", display.State)
	}
	if display.ErrorMsg != "connection refused" {
		t.Errorf("expected ErrorMsg 'connection refused', got %q", display.ErrorMsg)
	}
	if !display.IsTerminal {
		t.Error("expected IsTerminal to be true after failure")
	}
}

func TestBackgroundTaskDisplay_RemoveTickDeletesEntry(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.backgroundTasks["task-1"] = &BackgroundTaskDisplay{
		TaskID:     "task-1",
		State:      "completed",
		IsTerminal: true,
	}

	cv.handleRemoveBackgroundTask(BackgroundTaskRemovalTickMsg{TaskID: "task-1"}, nil)

	if _, ok := cv.backgroundTasks["task-1"]; ok {
		t.Error("expected backgroundTasks entry for task-1 to be deleted")
	}
}

func TestBackgroundTaskDisplay_RenderLine_StatesAndIcons(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cases := []struct {
		name     string
		display  *BackgroundTaskDisplay
		contains []string
	}{
		{
			name: "submitted",
			display: &BackgroundTaskDisplay{
				TaskID:    "t1",
				AgentName: "weather-agent",
				State:     "submitted",
			},
			contains: []string{"Agent(weather-agent=submitted...)"},
		},
		{
			name: "working",
			display: &BackgroundTaskDisplay{
				TaskID:    "t1",
				AgentName: "weather-agent",
				State:     "working",
			},
			contains: []string{"Agent(weather-agent=working...)"},
		},
		{
			name: "completed with usage",
			display: &BackgroundTaskDisplay{
				TaskID:     "t1",
				AgentName:  "weather-agent",
				State:      "completed",
				UsageJSON:  `{"input_tokens":42,"output_tokens":100,"total_tokens":142}`,
				IsTerminal: true,
			},
			contains: []string{
				"✓",
				"Agent(weather-agent=completed)",
				"└── usage=",
				`"input_tokens":42`,
			},
		},
		{
			name: "failed with error",
			display: &BackgroundTaskDisplay{
				TaskID:     "t1",
				AgentName:  "weather-agent",
				State:      "failed",
				ErrorMsg:   "connection refused",
				IsTerminal: true,
			},
			contains: []string{"✗", "Agent(weather-agent=failed)", "└── error: connection refused"},
		},
		{
			name: "fallback to shortened agent url when no name",
			display: &BackgroundTaskDisplay{
				TaskID:   "t1",
				AgentURL: "http://example.com",
				State:    "working",
			},
			contains: []string{"Agent(example.com=working...)"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := cv.renderBackgroundTaskLine(tc.display, 0)
			for _, substr := range tc.contains {
				if !strings.Contains(line, substr) {
					t.Errorf("expected line to contain %q, got %q", substr, line)
				}
			}
		})
	}
}

func TestBackgroundTaskDisplay_BarHidesWhenNoTasks(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	if cv.HasBackgroundTasks() {
		t.Error("expected HasBackgroundTasks to be false initially")
	}
	if got := cv.RenderBackgroundTasksBar(80); got != "" {
		t.Errorf("expected empty bar with no tasks, got %q", got)
	}
	if got := cv.BackgroundTasksBarHeight(); got != 0 {
		t.Errorf("expected zero bar height with no tasks, got %d", got)
	}
}

func TestBackgroundTaskDisplay_BarRendersOneLinePerTask(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.backgroundTasks["task-a"] = &BackgroundTaskDisplay{
		TaskID:    "task-a",
		AgentName: "weather-agent",
		State:     "working",
	}
	cv.backgroundTasks["task-b"] = &BackgroundTaskDisplay{
		TaskID:    "task-b",
		AgentName: "news-agent",
		State:     "submitted",
	}

	if !cv.HasBackgroundTasks() {
		t.Fatal("expected HasBackgroundTasks true")
	}
	if h := cv.BackgroundTasksBarHeight(); h != 2 {
		t.Errorf("expected bar height 2 for two tasks, got %d", h)
	}

	bar := cv.RenderBackgroundTasksBar(80)
	if !strings.Contains(bar, "Agent(weather-agent=working...)") {
		t.Errorf("expected weather-agent line in bar, got:\n%s", bar)
	}
	if !strings.Contains(bar, "Agent(news-agent=submitted...)") {
		t.Errorf("expected news-agent line in bar, got:\n%s", bar)
	}
	if strings.Count(bar, "\n") != 1 {
		t.Errorf("expected two lines (one newline separator) in bar, got %q", bar)
	}
}

func TestBackgroundTaskDisplay_BarOrderIsStable(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	for _, id := range []string{"task-z", "task-a", "task-m"} {
		cv.backgroundTasks[id] = &BackgroundTaskDisplay{
			TaskID:    id,
			AgentName: id,
			State:     "working",
		}
	}

	bar := cv.RenderBackgroundTasksBar(80)
	idxA := strings.Index(bar, "Agent(task-a")
	idxM := strings.Index(bar, "Agent(task-m")
	idxZ := strings.Index(bar, "Agent(task-z")
	if idxA == -1 || idxM == -1 || idxZ == -1 {
		t.Fatalf("expected all three task names in bar, got:\n%s", bar)
	}
	if idxA >= idxM || idxM >= idxZ {
		t.Errorf("expected lexicographic order task-a < task-m < task-z, got positions a=%d m=%d z=%d", idxA, idxM, idxZ)
	}
}

func TestBackgroundTaskDisplay_BarNotInsideConversationViewport(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetToolFormatter(&stubToolFormatter{})
	cv.backgroundTasks["task-1"] = &BackgroundTaskDisplay{
		TaskID:    "task-1",
		AgentName: "weather-agent",
		State:     "working",
	}

	entry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Tool,
			Content: sdk.NewMessageContent("Task delegated"),
		},
		Time: time.Now(),
		ToolExecution: &domain.ToolExecutionResult{
			ToolName: "A2A_SubmitTask",
			Success:  true,
			Data: map[string]any{
				"task_id":   "task-1",
				"agent_url": "http://example.com",
				"state":     "submitted",
			},
		},
	}
	cv.SetConversation([]domain.ConversationEntry{entry})

	if strings.Contains(cv.renderedContent, "Agent(weather-agent=working") {
		t.Errorf("did not expect background-task line inside the scrollable viewport, got:\n%s", cv.renderedContent)
	}
}

func TestBackgroundTaskDisplay_NormalizesStateString(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cases := []struct {
		raw      string
		contains string
	}{
		{"TASK_STATE_WORKING", "working..."},
		{"TASK_STATE_SUBMITTED", "submitted..."},
		{"TASK_STATE_INPUT_REQUIRED", "input-required..."},
		{"working", "working..."},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			line := cv.renderBackgroundTaskLine(&BackgroundTaskDisplay{
				TaskID:    "t1",
				AgentName: "weather-agent",
				State:     c.raw,
			}, 0)
			if !strings.Contains(line, c.contains) {
				t.Errorf("for raw state %q expected line to contain %q, got %q", c.raw, c.contains, line)
			}
			if strings.Contains(line, "TASK_STATE_") {
				t.Errorf("for raw state %q expected normalisation, got raw enum in %q", c.raw, line)
			}
		})
	}
}

func TestBackgroundTaskDisplay_StatusUpdateCapturesAgentURL(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.handleA2ATaskStatusUpdate(domain.A2ATaskStatusUpdateEvent{
		TaskID:   "task-1",
		AgentURL: "http://localhost:8081",
		Status:   "TASK_STATE_WORKING",
	}, nil)

	display := cv.backgroundTasks["task-1"]
	if display.AgentURL != "http://localhost:8081" {
		t.Errorf("expected AgentURL captured from status update, got %q", display.AgentURL)
	}
}

func TestBackgroundTaskDisplay_SubmittedCapturesAgentURL(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{
		TaskID:   "task-1",
		AgentURL: "http://localhost:8081",
	}, nil)

	display := cv.backgroundTasks["task-1"]
	if display.AgentURL != "http://localhost:8081" {
		t.Errorf("expected AgentURL captured from submitted event, got %q", display.AgentURL)
	}
	if display.State != "submitted" {
		t.Errorf("expected default state 'submitted', got %q", display.State)
	}
}

func TestBackgroundTaskDisplay_AgentNameResolverUsed(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetAgentNameResolver(func(url string) string {
		if url == "http://localhost:8081" {
			return "weather-agent"
		}
		return ""
	})

	line := cv.renderBackgroundTaskLine(&BackgroundTaskDisplay{
		TaskID:   "t1",
		AgentURL: "http://localhost:8081",
		State:    "working",
	}, 0)
	if !strings.Contains(line, "Agent(weather-agent=working...)") {
		t.Errorf("expected resolver-mapped name, got %q", line)
	}
}

func TestBackgroundTaskDisplay_AgentNameResolverFallsBackToShortenedURL(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetAgentNameResolver(func(_ string) string { return "" })

	line := cv.renderBackgroundTaskLine(&BackgroundTaskDisplay{
		TaskID:   "t1",
		AgentURL: "http://localhost:9090",
		State:    "working",
	}, 0)
	if !strings.Contains(line, "Agent(localhost:9090=working...)") {
		t.Errorf("expected fallback to shortened URL when resolver returns empty, got %q", line)
	}
}

// TestBackgroundTaskRemovalTickMsg_TypeNameMatchesDispatcherFilter guards
// against a regression where the message-name didn't match the parent
// dispatcher's "Tick" / "domain." substring filter, causing the auto-
// removal cmd to fire but its message to be silently dropped, leaving
// terminal-state indicators on screen forever.
//
// See internal/app/chat.go updateUIComponentsForUIMessages - only msgs
// whose %T contains "Tick" or starts with "domain." get routed to
// component Update() methods.
func TestBackgroundTaskRemovalTickMsg_TypeNameMatchesDispatcherFilter(t *testing.T) {
	msg := BackgroundTaskRemovalTickMsg{TaskID: "x"}
	typeName := fmt.Sprintf("%T", msg)
	if !strings.Contains(typeName, "Tick") {
		t.Errorf("BackgroundTaskRemovalTickMsg type %q must contain 'Tick' so the app dispatcher routes it; otherwise auto-removal won't fire", typeName)
	}
}

func TestBackgroundTaskDisplay_ShortenAgentURLDropsScheme(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"http://localhost:8081", "localhost:8081"},
		{"https://api.example.com/v1", "api.example.com"},
		{"http://mock-agent:8080/", "mock-agent:8080"},
		{"localhost:8081", "localhost:8081"},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			if got := shortenAgentURL(c.raw); got != c.want {
				t.Errorf("shortenAgentURL(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestBackgroundTaskDisplay_FallsBackToShortenedURL(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	line := cv.renderBackgroundTaskLine(&BackgroundTaskDisplay{
		TaskID:   "t1",
		AgentURL: "http://localhost:8081",
		State:    "working",
	}, 0)
	if strings.Contains(line, "http://") {
		t.Errorf("expected scheme stripped from fallback URL, got %q", line)
	}
	if !strings.Contains(line, "Agent(localhost:8081=working...)") {
		t.Errorf("expected shortened URL, got %q", line)
	}
}

func TestBackgroundTaskDisplay_HasActiveBackgroundTasks(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	if cv.hasActiveBackgroundTasks() {
		t.Error("expected no active tasks initially")
	}

	cv.backgroundTasks["t1"] = &BackgroundTaskDisplay{IsTerminal: false}
	if !cv.hasActiveBackgroundTasks() {
		t.Error("expected active tasks when non-terminal entry present")
	}

	cv.backgroundTasks["t1"].IsTerminal = true
	if cv.hasActiveBackgroundTasks() {
		t.Error("expected no active tasks when all entries are terminal")
	}
}

func TestBackgroundTaskDisplay_RenderLine_IncludesModelAndElapsed(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	display := &BackgroundTaskDisplay{
		TaskID:    "t1",
		AgentName: "browser-agent",
		State:     "working",
		Model:     "deepseek/deepseek-v4-flash",
		StartedAt: time.Now().Add(-17 * time.Second),
	}
	line := cv.renderBackgroundTaskLine(display, 0)

	wantBody := "Agent(browser-agent=working..., model=deepseek/deepseek-v4-flash)"
	if !strings.Contains(line, wantBody) {
		t.Errorf("expected line to contain %q, got %q", wantBody, line)
	}
	if !strings.Contains(line, "17s") {
		t.Errorf("expected elapsed 17s, got %q", line)
	}
	idxModel := strings.Index(line, "model=")
	idxElapsed := strings.LastIndex(line, "17s")
	if idxModel < 0 || idxElapsed < 0 || idxModel >= idxElapsed {
		t.Errorf("expected 'model=' before elapsed in %q (idxModel=%d, idxElapsed=%d)", line, idxModel, idxElapsed)
	}
}

func TestBackgroundTaskDisplay_RenderLine_OmitsModelWhenUnknown(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	display := &BackgroundTaskDisplay{
		TaskID:    "t1",
		AgentName: "browser-agent",
		State:     "working",
		Model:     "",
		StartedAt: time.Now().Add(-5 * time.Second),
	}
	line := cv.renderBackgroundTaskLine(display, 0)

	if !strings.Contains(line, "Agent(browser-agent=working...) 5s") {
		t.Errorf("expected no-model form 'Agent(browser-agent=working...) 5s', got %q", line)
	}
	if strings.Contains(line, "model=") {
		t.Errorf("expected no 'model=' segment when model is unknown, got %q", line)
	}
	if strings.Contains(line, ",") {
		t.Errorf("expected no trailing comma artefact when model is unknown, got %q", line)
	}
}

func TestBackgroundTaskDisplay_RenderLine_FreezesElapsedOnTerminal(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	started := time.Now().Add(-42 * time.Second)
	completed := started.Add(42 * time.Second)
	display := &BackgroundTaskDisplay{
		TaskID:      "t1",
		AgentName:   "browser-agent",
		State:       "completed",
		Model:       "deepseek/deepseek-v4-flash",
		StartedAt:   started,
		CompletedAt: completed,
		IsTerminal:  true,
	}

	line := cv.renderBackgroundTaskLine(display, 0)
	if !strings.Contains(line, "Agent(browser-agent=completed) 42s") {
		t.Errorf("expected frozen elapsed 'Agent(browser-agent=completed) 42s', got %q", line)
	}
	if strings.Contains(line, "model=") {
		t.Errorf("terminal form must not include 'model=', got %q", line)
	}

	time.Sleep(1100 * time.Millisecond)
	line2 := cv.renderBackgroundTaskLine(display, 0)
	if !strings.Contains(line2, "42s") {
		t.Errorf("expected elapsed to stay frozen at 42s on re-render, got %q", line2)
	}
	if strings.Contains(line2, "43s") {
		t.Errorf("expected elapsed to NOT tick past 42s after terminal transition, got %q", line2)
	}
}

func TestBackgroundTaskDisplay_RenderLine_FormatElapsedMinutes(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	display := &BackgroundTaskDisplay{
		TaskID:    "t1",
		AgentName: "x",
		State:     "working",
		StartedAt: time.Now().Add(-83 * time.Second),
	}
	line := cv.renderBackgroundTaskLine(display, 0)
	if !strings.Contains(line, "1m23s") {
		t.Errorf("expected elapsed '1m23s' for 83s, got %q", line)
	}
}

func TestBackgroundTaskDisplay_RenderLine_TruncatesLongModel(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	display := &BackgroundTaskDisplay{
		TaskID:    "t1",
		AgentName: "browser-agent",
		State:     "working",
		Model:     "extremely-long-vendor-name/very-verbose-model-identifier-v42-flash-preview-experimental",
		StartedAt: time.Now().Add(-3 * time.Second),
	}
	const width = 60
	line := cv.renderBackgroundTaskLine(display, width)

	if got := lipgloss.Width(line); got > width {
		t.Errorf("expected visible width <= %d, got %d for line %q", width, got, line)
	}
	if !strings.Contains(line, "Agent(browser-agent=working..., model=") {
		t.Errorf("expected name and state preserved with model prefix, got %q", line)
	}
	if !strings.Contains(line, "...) 3s") {
		t.Errorf("expected truncated model ending with '...) 3s', got %q", line)
	}
	if !strings.Contains(line, "browser-agent") {
		t.Errorf("expected name preserved verbatim, got %q", line)
	}
	if !strings.Contains(line, "working...") {
		t.Errorf("expected state preserved verbatim, got %q", line)
	}
}

func TestBackgroundTaskDisplay_SubmittedPopulatesStartedAtFromTimestamp(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	ts := time.Now().Add(-2 * time.Second)
	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{
		TaskID:    "task-1",
		AgentURL:  "http://localhost:8081",
		Timestamp: ts,
	}, nil)

	display := cv.backgroundTasks["task-1"]
	if display == nil {
		t.Fatal("expected display entry for task-1")
	}
	if !display.StartedAt.Equal(ts) {
		t.Errorf("expected StartedAt=%v from event timestamp, got %v", ts, display.StartedAt)
	}
}

func TestBackgroundTaskDisplay_SubmittedFallsBackToNowWhenTimestampZero(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	before := time.Now()
	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{
		TaskID: "task-1",
	}, nil)
	after := time.Now()

	display := cv.backgroundTasks["task-1"]
	if display == nil {
		t.Fatal("expected display entry for task-1")
	}
	if display.StartedAt.Before(before) || display.StartedAt.After(after) {
		t.Errorf("expected StartedAt within [%v, %v], got %v", before, after, display.StartedAt)
	}
}

func TestBackgroundTaskDisplay_AgentModelResolverPopulatesOnSubmit(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetAgentModelResolver(func(url string) string {
		if url == "http://localhost:8081" {
			return "deepseek/deepseek-v4-flash"
		}
		return ""
	})

	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{
		TaskID:   "task-1",
		AgentURL: "http://localhost:8081",
	}, nil)

	display := cv.backgroundTasks["task-1"]
	if display == nil {
		t.Fatal("expected display entry for task-1")
	}
	if display.Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("expected resolver-populated Model 'deepseek/deepseek-v4-flash', got %q", display.Model)
	}
}

func TestBackgroundTaskDisplay_AgentModelResolverPopulatesOnLateStatusUpdate(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetAgentModelResolver(func(url string) string {
		if url == "http://localhost:8081" {
			return "deepseek/deepseek-v4-flash"
		}
		return ""
	})

	cv.handleA2ATaskSubmitted(domain.A2ATaskSubmittedEvent{TaskID: "task-1"}, nil)
	if got := cv.backgroundTasks["task-1"].Model; got != "" {
		t.Errorf("expected empty Model before AgentURL is known, got %q", got)
	}

	cv.handleA2ATaskStatusUpdate(domain.A2ATaskStatusUpdateEvent{
		TaskID:   "task-1",
		AgentURL: "http://localhost:8081",
		Status:   "TASK_STATE_WORKING",
	}, nil)

	display := cv.backgroundTasks["task-1"]
	if display.Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("expected late resolver to populate Model 'deepseek/deepseek-v4-flash', got %q", display.Model)
	}
}

// TestConversationView_ConcurrentStreamingAccess tests thread-safety of streaming operations
func TestConversationView_ConcurrentStreamingAccess(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetWidth(100)
	cv.SetHeight(30)

	done := make(chan bool)
	go func() {
		for i := 0; i < 1000; i++ {
			cv.appendStreamingContent(fmt.Sprintf("chunk %d ", i), "", "test-model")
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			_ = cv.Render()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	<-done
	<-done

	cv.flushStreamingBuffer()

	cv.streamingMu.RLock()
	isStreaming := cv.isStreaming
	bufLen := cv.streamingBuffer.Len()
	cv.streamingMu.RUnlock()

	if isStreaming {
		t.Error("Expected streaming to be stopped after flush")
	}
	if bufLen != 0 {
		t.Errorf("Expected buffer length 0 after flush, got %d", bufLen)
	}
}

func approvalEntry(status domain.ToolApprovalStatus) domain.ConversationEntry {
	return domain.ConversationEntry{
		PendingToolCall: &sdk.ChatCompletionMessageToolCall{
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "Bash",
				Arguments: `{"command":"git status"}`,
			},
		},
		ToolApprovalStatus: status,
	}
}

func TestRenderPendingToolEntry_ApprovedThemedHeader(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetToolFormatter(&stubToolFormatter{})

	out := cv.renderPendingToolEntry(approvalEntry(domain.ToolApprovalApproved))

	if !strings.Contains(out, "Approved") {
		t.Errorf("expected Approved label, got %q", out)
	}
	if !strings.Contains(out, "Bash") {
		t.Errorf("expected tool name in header, got %q", out)
	}

	if strings.Contains(out, "Tool:") || strings.Contains(out, "Arguments:") {
		t.Errorf("approval header should not contain raw Tool:/Arguments: block, got %q", out)
	}
}

func TestRenderPendingToolEntry_RejectedThemedHeader(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetToolFormatter(&stubToolFormatter{})

	out := cv.renderPendingToolEntry(approvalEntry(domain.ToolApprovalRejected))
	if !strings.Contains(out, "Rejected") {
		t.Errorf("expected Rejected label, got %q", out)
	}
}

func TestRenderPendingToolEntry_PendingRendersNothing(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetToolFormatter(&stubToolFormatter{})

	if out := cv.renderPendingToolEntry(approvalEntry(domain.ToolApprovalPending)); out != "" {
		t.Errorf("pending approval should render nothing, got %q", out)
	}
}

func renderCacheConversation() []domain.ConversationEntry {
	return []domain.ConversationEntry{
		{
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Hello **world**")},
			Time:    time.Unix(1, 0),
		},
		{
			Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("Hi *there*, a reply long enough to wrap somewhere")},
			Model:   "org/model",
			Time:    time.Unix(2, 0),
		},
	}
}

func TestConversationView_RenderCache(t *testing.T) {
	t.Run("cached render matches fresh render", func(t *testing.T) {
		cached := NewConversationView(createMockStyleProvider())
		cached.SetConversation(renderCacheConversation())
		cached.updateViewportContentFull()
		if len(cached.renderCache) == 0 {
			t.Fatal("expected render cache to be populated")
		}

		fresh := NewConversationView(createMockStyleProvider())
		fresh.SetConversation(renderCacheConversation())

		if cached.renderedContent != fresh.renderedContent {
			t.Error("cached rendering diverged from fresh rendering")
		}
	})

	t.Run("width change re-renders entries", func(t *testing.T) {
		cv := NewConversationView(createMockStyleProvider())
		cv.SetConversation(renderCacheConversation())
		before := cv.renderCache[1].fingerprint

		cv.SetWidth(40)
		cv.updateViewportContentFull()

		if cv.renderCache[1].fingerprint == before {
			t.Error("expected fingerprint to change after width change")
		}
	})

	t.Run("raw format toggle re-renders entries", func(t *testing.T) {
		cv := NewConversationView(createMockStyleProvider())
		cv.SetConversation(renderCacheConversation())
		before := cv.renderCache[1].fingerprint

		cv.ToggleRawFormat()

		if cv.renderCache[1].fingerprint == before {
			t.Error("expected fingerprint to change after raw format toggle")
		}
	})

	t.Run("entry state change invalidates that entry only", func(t *testing.T) {
		cv := NewConversationView(createMockStyleProvider())
		cv.SetConversation(renderCacheConversation())
		user := cv.renderCache[0].fingerprint
		assistant := cv.renderCache[1].fingerprint

		conv := renderCacheConversation()
		conv[1].Rejected = true
		cv.SetConversation(conv)

		if cv.renderCache[0].fingerprint != user {
			t.Error("unchanged entry should keep its fingerprint")
		}
		if cv.renderCache[1].fingerprint == assistant {
			t.Error("changed entry should get a new fingerprint")
		}
	})

	t.Run("shrinking conversation clears cache", func(t *testing.T) {
		cv := NewConversationView(createMockStyleProvider())
		cv.SetConversation(renderCacheConversation())

		cv.SetConversation(renderCacheConversation()[:1])

		if len(cv.renderCache) != 1 {
			t.Errorf("expected cache rebuilt with 1 entry, got %d", len(cv.renderCache))
		}
	})

	t.Run("pending plan entries bypass the cache", func(t *testing.T) {
		cv := NewConversationView(createMockStyleProvider())
		conv := renderCacheConversation()
		conv[1].IsPlan = true
		conv[1].PlanApprovalStatus = domain.PlanApprovalPending
		cv.SetConversation(conv)

		if _, ok := cv.renderCache[1]; ok {
			t.Error("pending plan entry must not be cached")
		}
	})
}
