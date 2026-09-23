package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// stubToolFormatter is a minimal ToolFormatter for tests that need the
// view to render entries containing ToolExecution payloads.
type stubToolFormatter struct{}

func (s *stubToolFormatter) FormatToolCall(toolName string, _ map[string]any) string {
	return toolName + "()"
}
func (s *stubToolFormatter) FormatToolResultForUI(result *agentdomain.ToolExecutionResult, _ int) string {
	if result == nil {
		return ""
	}
	return "Tool: " + result.ToolName
}
func (s *stubToolFormatter) FormatToolResultExpanded(result *agentdomain.ToolExecutionResult, _ int) string {
	if result == nil {
		return ""
	}
	return "Tool: " + result.ToolName
}
func (s *stubToolFormatter) FormatToolResultForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return ""
	}
	return "Tool: " + result.ToolName
}
func (s *stubToolFormatter) ShouldAlwaysExpandTool(_ string) bool { return false }
func (s *stubToolFormatter) RenderToolSummary(icon, toolName string, _ map[string]any, trailing string, _ int) string {
	return strings.TrimSpace(icon + " " + toolName + "() " + trailing)
}

// createMockStyleProvider creates a mock styles provider for testing
func createMockStyleProvider() *styles.Provider {
	fakeTheme := &tuimocks.FakeTheme{}
	fakeThemeService := &tuimocks.FakeThemeService{}
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

	conversation := []convdomain.ConversationEntry{
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

	conversation := []convdomain.ConversationEntry{
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

	conversation := []convdomain.ConversationEntry{
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

	cv.SetConversation([]convdomain.ConversationEntry{
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("edited")},
			ToolExecution: &agentdomain.ToolExecutionResult{ToolName: "Edit"},
			Time:          time.Now(),
		},
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("ran")},
			ToolExecution: &agentdomain.ToolExecutionResult{ToolName: "Bash"},
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

	cv.SetConversation([]convdomain.ConversationEntry{
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("edited")},
			ToolExecution: &agentdomain.ToolExecutionResult{ToolName: "MultiEdit"},
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

// TestConversationView_ToggleAllExpandsCollapsedAmongExpanded guards the rejected-write
// bug: with a default-expanded Edit next to a collapsed result, the first ctrl+o must
// expand everything (so the collapsed card opens), not collapse-first and leave it shut.
func TestConversationView_ToggleAllExpandsCollapsedAmongExpanded(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetToolFormatter(&stubToolFormatter{})

	cv.SetConversation([]convdomain.ConversationEntry{
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("edited")},
			ToolExecution: &agentdomain.ToolExecutionResult{ToolName: "Edit"}, // default-expanded
			Time:          time.Now(),
		},
		{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("rejected")},
			ToolExecution: &agentdomain.ToolExecutionResult{ToolName: "Write", Rejected: true}, // collapsed
			Time:          time.Now(),
		},
	})

	if !cv.IsToolResultExpanded(0) || cv.IsToolResultExpanded(1) {
		t.Fatal("precondition: Edit expanded, rejected Write collapsed")
	}

	cv.ToggleAllToolResultsExpansion()
	if !cv.IsToolResultExpanded(1) {
		t.Error("expected first ToggleAll to expand the collapsed rejected write")
	}
	if !cv.IsToolResultExpanded(0) {
		t.Error("expected the already-expanded Edit to stay expanded")
	}

	cv.ToggleAllToolResultsExpansion()
	if cv.IsToolResultExpanded(0) || cv.IsToolResultExpanded(1) {
		t.Error("expected second ToggleAll to collapse everything")
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

	conversation := []convdomain.ConversationEntry{
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

// TestConversationView_StreamingLifecycle exercises the append/render/flush
// streaming flow. ConversationView is confined to the Bubble Tea event loop,
// so the flow is sequential by contract.
func TestConversationView_StreamingLifecycle(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetWidth(100)
	cv.SetHeight(30)

	for i := range 1000 {
		cv.appendStreamingContent(fmt.Sprintf("chunk %d ", i), "", "test-model")
		_ = cv.Render()
	}

	if !cv.isStreaming {
		t.Error("Expected streaming to be active while chunks arrive")
	}

	cv.flushStreamingBuffer()

	if cv.isStreaming {
		t.Error("Expected streaming to be stopped after flush")
	}
	if cv.streamingBuffer.Len() != 0 {
		t.Errorf("Expected buffer length 0 after flush, got %d", cv.streamingBuffer.Len())
	}
}

// TestConversationView_StreamingRenderCoalesced pins the issue #888 fix: streamed
// deltas must not each trigger a full viewport rebuild. Instead they mark the view
// dirty and a single coalescing tick performs one rebuild, re-arming until the
// stream ends. Per-token rebuilds are what scrambled the screen mid-generation.
func TestConversationView_StreamingRenderCoalesced(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetWidth(100)
	cv.SetHeight(30)

	const marker = "STREAMED_MARKER"

	_, cmd := cv.handleStreamingContentEvent(tui.StreamingContentEvent{Content: marker + " one "}, nil)
	if cmd == nil {
		t.Fatal("first streamed delta should arm the render tick (non-nil cmd)")
	}

	if _, cmd2 := cv.handleStreamingContentEvent(tui.StreamingContentEvent{Content: "two "}, nil); cmd2 != nil {
		t.Fatal("subsequent streamed deltas must not arm a second render tick")
	}

	if strings.Contains(cv.renderedContent, marker) {
		t.Fatal("streamed content must not be rendered synchronously on every delta")
	}
	if !cv.streamingDirty {
		t.Fatal("streamed content should mark the view dirty")
	}

	_, tickCmd := cv.handleStreamingRenderTick(nil)
	if !strings.Contains(cv.renderedContent, marker) {
		t.Fatal("render tick should rebuild the viewport with the streamed content")
	}
	if cv.streamingDirty {
		t.Fatal("render tick should clear the dirty flag")
	}
	if tickCmd == nil {
		t.Fatal("render tick should re-arm while streaming is active")
	}

	cv.flushStreamingBuffer()
	if _, stopCmd := cv.handleStreamingRenderTick(nil); stopCmd != nil {
		t.Fatal("render tick should stop re-arming once streaming ends")
	}
	if cv.streamingRenderArmed {
		t.Fatal("render tick should disarm once streaming ends")
	}
}

func approvalEntry(status convdomain.ToolApprovalStatus) convdomain.ConversationEntry {
	return convdomain.ConversationEntry{
		PendingToolCall: &sdk.ChatCompletionMessageToolCall{
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "Bash",
				Arguments: `{"command":"git status"}`,
			},
		},
		ToolApprovalStatus: status,
	}
}

func TestRenderPendingToolEntry(t *testing.T) {
	cases := []struct {
		name         string
		status       convdomain.ToolApprovalStatus
		wantContains []string
		wantAbsent   []string
		wantEmpty    bool
	}{
		{
			name:         "approved themed header",
			status:       convdomain.ToolApprovalApproved,
			wantContains: []string{"Approved", "Bash"},
			wantAbsent:   []string{"Tool:", "Arguments:"},
		},
		{
			name:         "rejected themed header",
			status:       convdomain.ToolApprovalRejected,
			wantContains: []string{"Rejected"},
		},
		{
			name:      "pending renders nothing",
			status:    convdomain.ToolApprovalPending,
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cv := NewConversationView(createMockStyleProvider())
			cv.SetToolFormatter(&stubToolFormatter{})
			out := cv.renderPendingToolEntry(approvalEntry(tc.status))
			if tc.wantEmpty {
				if out != "" {
					t.Errorf("pending approval should render nothing, got %q", out)
				}
				return
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q, got %q", want, out)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(out, absent) {
					t.Errorf("expected output to not contain %q, got %q", absent, out)
				}
			}
		})
	}
}

func renderCacheConversation() []convdomain.ConversationEntry {
	return []convdomain.ConversationEntry{
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
		conv[1].PlanApprovalStatus = convdomain.PlanApprovalPending
		cv.SetConversation(conv)

		if _, ok := cv.renderCache[1]; ok {
			t.Error("pending plan entry must not be cached")
		}
	})
}

// heightFormatter renders a tool result as `collapsed` lines when collapsed and
// `expanded` lines when expanded, giving scroll-anchoring math a real height delta.
type heightFormatter struct{ collapsed, expanded int }

func (heightFormatter) FormatToolCall(name string, _ map[string]any) string { return name + "()" }
func (f heightFormatter) FormatToolResultForUI(_ *agentdomain.ToolExecutionResult, _ int) string {
	return strings.TrimRight(strings.Repeat("c\n", f.collapsed), "\n")
}
func (f heightFormatter) FormatToolResultExpanded(_ *agentdomain.ToolExecutionResult, _ int) string {
	return strings.TrimRight(strings.Repeat("e\n", f.expanded), "\n")
}
func (heightFormatter) FormatToolResultForLLM(_ *agentdomain.ToolExecutionResult) string { return "" }
func (heightFormatter) ShouldAlwaysExpandTool(string) bool                               { return false }
func (heightFormatter) RenderToolSummary(icon, name string, _ map[string]any, trailing string, _ int) string {
	return strings.TrimSpace(icon + " " + name + "() " + trailing)
}

func scrollTestView(t *testing.T) *ConversationView {
	t.Helper()
	cv := NewConversationView(createMockStyleProvider())
	cv.SetToolFormatter(heightFormatter{collapsed: 2, expanded: 6})
	cv.SetWidth(80)
	cv.SetHeight(8)
	conv := make([]convdomain.ConversationEntry, 0, 6)
	for i := 0; i < 6; i++ {
		conv = append(conv, convdomain.ConversationEntry{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("x")},
			ToolExecution: &agentdomain.ToolExecutionResult{ToolName: "Bash"},
			Time:          time.Now(),
		})
	}
	cv.SetConversation(conv)
	return cv
}

func TestRebuildPreservingScroll_AnchorsAboveViewportEntry(t *testing.T) {
	cv := scrollTestView(t)

	spans := cv.entryLineSpans()
	cv.Viewport.SetYOffset(spans[2][0])
	before := cv.Viewport.YOffset()
	beforeH0 := spans[0][1]

	cv.ToggleToolResultExpansion(0)

	afterH0 := cv.entryLineSpans()[0][1]
	if afterH0 <= beforeH0 {
		t.Fatalf("expanded entry should be taller: collapsed=%d expanded=%d", beforeH0, afterH0)
	}
	if got, want := cv.Viewport.YOffset(), before+(afterH0-beforeH0); got != want {
		t.Errorf("YOffset not anchored: got %d, want %d (delta %d)", got, want, afterH0-beforeH0)
	}
}

func TestRebuildPreservingScroll_IgnoresBelowViewportEntry(t *testing.T) {
	cv := scrollTestView(t)

	cv.Viewport.SetYOffset(0) // viewport top at the very top; entry 5 is below it
	before := cv.Viewport.YOffset()

	cv.ToggleToolResultExpansion(5)

	if got := cv.Viewport.YOffset(); got != before {
		t.Errorf("toggling a below-viewport entry must not move the offset: got %d, want %d", got, before)
	}
}

func TestConversationView_AutoFollow(t *testing.T) {
	appendEntry := func(cv *ConversationView) {
		conv := append(cv.conversation, convdomain.ConversationEntry{
			Message:       sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("x")},
			ToolExecution: &agentdomain.ToolExecutionResult{ToolName: "Bash"},
			Time:          time.Now(),
		})
		cv.SetConversation(conv)
	}

	tests := []struct {
		name       string
		arrange    func(cv *ConversationView)
		wantBottom bool
		wantOffset int
	}{
		{
			name:       "at bottom follows new content",
			arrange:    func(cv *ConversationView) { appendEntry(cv) },
			wantBottom: true,
		},
		{
			name: "scrolled up holds position",
			arrange: func(cv *ConversationView) {
				cv.Viewport.SetYOffset(1)
				appendEntry(cv)
			},
			wantOffset: 1,
		},
		{
			name: "ResetUserScroll pins to bottom",
			arrange: func(cv *ConversationView) {
				cv.Viewport.SetYOffset(1)
				cv.ResetUserScroll()
			},
			wantBottom: true,
		},
		{
			name:       "shrinking height keeps following the tail",
			arrange:    func(cv *ConversationView) { cv.SetHeight(5) },
			wantBottom: true,
		},
		{
			name: "shrinking height while scrolled up holds position",
			arrange: func(cv *ConversationView) {
				cv.Viewport.SetYOffset(1)
				cv.SetHeight(5)
			},
			wantOffset: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv := scrollTestView(t)
			if !cv.Viewport.AtBottom() {
				t.Fatal("fixture should start at bottom")
			}
			tt.arrange(cv)
			if cv.Viewport.AtBottom() != tt.wantBottom {
				t.Fatalf("AtBottom() = %v, want %v", cv.Viewport.AtBottom(), tt.wantBottom)
			}
			if !tt.wantBottom && cv.Viewport.YOffset() != tt.wantOffset {
				t.Fatalf("YOffset() = %d, want %d", cv.Viewport.YOffset(), tt.wantOffset)
			}
		})
	}
}
