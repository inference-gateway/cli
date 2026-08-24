package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestRequestSandboxApproval(t *testing.T) {
	tests := []struct {
		name       string
		response   agentdomain.ApprovalAction
		wantAllow  bool
		wantAlways bool
	}{
		{"approve grants session-only", agentdomain.ApprovalApprove, true, false},
		{"auto-accept grants and persists", agentdomain.ApprovalAutoAccept, true, true},
		{"reject denies", agentdomain.ApprovalReject, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := make(chan agentdomain.ChatEvent, 1)
			pub := newEventPublisher("req-1", events)
			svc := &AgentServiceImpl{}
			tc := sdk.ChatCompletionMessageToolCall{
				ID:       "call-1",
				Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"},
			}

			go func() {
				ev := (<-events).(agentdomain.ToolApprovalRequestedEvent)
				assert.Equal(t, "SandboxAccess", ev.ToolCall.Function.Name)
				assert.Equal(t, "call-1-sandbox", ev.ToolCall.ID)
				var args map[string]string
				require.NoError(t, json.Unmarshal([]byte(ev.ToolCall.Function.Arguments), &args))
				assert.Equal(t, "/granted/dir", args["path"])
				assert.Equal(t, "Read", args["tool"])
				ev.ResponseChan <- tt.response
			}()

			allow, always := svc.requestSandboxApproval(context.Background(), tc, pub, "/granted/dir")
			assert.Equal(t, tt.wantAllow, allow)
			assert.Equal(t, tt.wantAlways, always)
		})
	}
}

func TestSandboxGrantDir(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, dir, sandboxGrantDir(dir))
	assert.Equal(t, dir, sandboxGrantDir(filepath.Join(dir, "missing.txt")))
}
