package conversation

import (
	"encoding/json"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func entry(role sdk.MessageRole, content string, hidden bool) convdomain.ConversationEntry {
	return convdomain.ConversationEntry{
		Message: sdk.Message{Role: role, Content: sdk.NewMessageContent(content)},
		Time:    time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
		Hidden:  hidden,
	}
}

func newExportRepo(t *testing.T) *InMemoryConversationRepository {
	t.Helper()
	repo := NewInMemoryConversationRepository(nil, nil)

	toolCallID := "call-1"
	entries := []convdomain.ConversationEntry{
		entry(sdk.User, "hello there", false),
		func() convdomain.ConversationEntry {
			e := entry(sdk.Assistant, "assistant reply", false)
			e.Model = "openai/gpt-4o"
			e.Message.ToolCalls = &[]sdk.ChatCompletionMessageToolCall{{
				ID: toolCallID,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Read",
					Arguments: `{"path":"/tmp/x"}`,
				},
			}}
			return e
		}(),
		entry(sdk.System, "secret system prompt", true),
		func() convdomain.ConversationEntry {
			e := entry(sdk.Tool, "tool output", false)
			e.Message.ToolCallID = &toolCallID
			return e
		}(),
	}
	for _, e := range entries {
		assert.NoError(t, repo.AddMessage(e))
	}
	return repo
}

func TestInMemoryConversationRepository_Export(t *testing.T) {
	tests := []struct {
		name        string
		format      convdomain.ExportFormat
		contains    []string
		notContains []string
		wantErr     bool
	}{
		{
			name:   "markdown",
			format: convdomain.ExportMarkdown,
			contains: []string{
				"# Chat Session Export",
				"**Total Messages:** 3",
				"👤 **You**",
				"hello there",
				"🤖 **Assistant (openai/gpt-4o)**",
				"assistant reply",
				"### Tool Calls",
				`{"path":"/tmp/x"}`,
				"🔧 **Tool Result**",
				"*Tool Call ID: call-1*",
			},
			notContains: []string{"secret system prompt", "⚙️ **System**"},
		},
		{
			name:   "text",
			format: convdomain.ExportText,
			contains: []string{
				"Chat Session Export",
				"[15:04:05] You:",
				"[15:04:05] Assistant (openai/gpt-4o):",
				"[15:04:05] Tool:",
			},
			notContains: []string{"secret system prompt"},
		},
		{
			name:        "json filters hidden",
			format:      convdomain.ExportJSON,
			notContains: []string{"secret system prompt"},
		},
		{
			name:    "unsupported format",
			format:  convdomain.ExportFormat("yaml"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newExportRepo(t)
			data, err := repo.Export(tt.format)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			out := string(data)
			for _, want := range tt.contains {
				assert.Contains(t, out, want)
			}
			for _, banned := range tt.notContains {
				assert.NotContains(t, out, banned)
			}

			if tt.format == convdomain.ExportJSON {
				var entries []json.RawMessage
				assert.NoError(t, json.Unmarshal(data, &entries))
				assert.Len(t, entries, 3, "hidden entry must be filtered out")
			}
		})
	}
}

func TestInMemoryConversationRepository_ExportMarkdownIncludesTokenStats(t *testing.T) {
	repo := newExportRepo(t)
	assert.NoError(t, repo.AddTokenUsage("", 10, 20, 30, 0, 0))

	data, err := repo.Export(convdomain.ExportMarkdown)
	assert.NoError(t, err)
	out := string(data)
	assert.Contains(t, out, "**Total Input Tokens:** 10")
	assert.Contains(t, out, "**Total Output Tokens:** 20")
	assert.Contains(t, out, "**Total Tokens:** 30")
	assert.Contains(t, out, "**API Requests:** 1")
}

func TestInMemoryConversationRepository_FormatToolCall(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	tests := []struct {
		name     string
		toolCall sdk.ChatCompletionMessageToolCall
		want     string
	}{
		{
			name: "invalid json args falls back to bare name",
			toolCall: sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read", Arguments: "not-json"},
			},
			want: "Read()",
		},
		{
			name: "valid args formatted via platform formatter",
			toolCall: sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read", Arguments: `{"path":"/tmp/x"}`},
			},
			want: "Read(path=/tmp/x)",
		},
		{
			name: "empty args object",
			toolCall: sdk.ChatCompletionMessageToolCall{
				Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{}`},
			},
			want: "Bash()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, repo.formatToolCall(tt.toolCall))
		})
	}
}

func TestInMemoryConversationRepository_UpdateToolApprovalStatus(t *testing.T) {
	tests := []struct {
		name       string
		hasPending bool
		action     agentdomain.ApprovalAction
		want       convdomain.ToolApprovalStatus
	}{
		{name: "approve", hasPending: true, action: agentdomain.ApprovalApprove, want: convdomain.ToolApprovalApproved},
		{name: "auto accept", hasPending: true, action: agentdomain.ApprovalAutoAccept, want: convdomain.ToolApprovalApproved},
		{name: "reject", hasPending: true, action: agentdomain.ApprovalReject, want: convdomain.ToolApprovalRejected},
		{name: "no pending tool call is a no-op", hasPending: false, action: agentdomain.ApprovalApprove},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewInMemoryConversationRepository(nil, nil)
			assert.NoError(t, repo.AddMessage(entry(sdk.User, "hi", false)))
			if tt.hasPending {
				assert.NoError(t, repo.AddPendingToolCall(sdk.ChatCompletionMessageToolCall{ID: "call-1"}, make(chan agentdomain.ApprovalAction)))
			}

			repo.UpdateToolApprovalStatus(tt.action)

			msgs := repo.GetMessages()
			if tt.hasPending {
				assert.Equal(t, tt.want, msgs[len(msgs)-1].ToolApprovalStatus)
			} else {
				for _, m := range msgs {
					assert.Empty(t, m.ToolApprovalStatus)
				}
			}
		})
	}
}

func TestInMemoryConversationRepository_MarkMessageAsPlanByIndex(t *testing.T) {
	tests := []struct {
		name       string
		index      int
		wantMarked bool
	}{
		{name: "valid index", index: 1, wantMarked: true},
		{name: "first index", index: 0, wantMarked: true},
		{name: "negative index ignored", index: -1},
		{name: "index equal to length ignored", index: 2},
		{name: "index far out of range ignored", index: 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewInMemoryConversationRepository(nil, nil)
			assert.NoError(t, repo.AddMessage(entry(sdk.User, "hi", false)))
			assert.NoError(t, repo.AddMessage(entry(sdk.Assistant, "plan text", false)))

			repo.MarkMessageAsPlanByIndex(tt.index)

			msgs := repo.GetMessages()
			markedCount := 0
			for i, m := range msgs {
				if m.IsPlan {
					markedCount++
					assert.Equal(t, tt.index, i)
					assert.Equal(t, convdomain.PlanApprovalPending, m.PlanApprovalStatus)
				}
			}
			if tt.wantMarked {
				assert.Equal(t, 1, markedCount)
			} else {
				assert.Zero(t, markedCount)
			}
		})
	}
}

func TestInMemoryConversationRepository_UpdatePlanStatus(t *testing.T) {
	tests := []struct {
		name    string
		hasPlan bool
		action  agentdomain.PlanApprovalAction
		want    convdomain.PlanApprovalStatus
	}{
		{name: "accept", hasPlan: true, action: agentdomain.PlanApprovalAccept, want: convdomain.PlanApprovalAccepted},
		{name: "accept standard", hasPlan: true, action: agentdomain.PlanApprovalAcceptStandard, want: convdomain.PlanApprovalAccepted},
		{name: "reject", hasPlan: true, action: agentdomain.PlanApprovalReject, want: convdomain.PlanApprovalRejected},
		{name: "no pending plan is a no-op", hasPlan: false, action: agentdomain.PlanApprovalAccept},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewInMemoryConversationRepository(nil, nil)
			assert.NoError(t, repo.AddMessage(entry(sdk.Assistant, "plan text", false)))
			if tt.hasPlan {
				repo.MarkLastMessageAsPlan()
			}

			repo.UpdatePlanStatus(tt.action)

			msg := repo.GetMessages()[0]
			if tt.hasPlan {
				assert.Equal(t, tt.want, msg.PlanApprovalStatus)
			} else {
				assert.Empty(t, msg.PlanApprovalStatus)
			}
		})
	}
}
