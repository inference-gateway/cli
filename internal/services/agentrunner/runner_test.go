package agentrunner

import (
	"context"
	"os/exec"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestBuildArgs(t *testing.T) {
	args := buildArgs(Options{
		SessionID:       "sess-1",
		Prompt:          "do the thing",
		Model:           "openai/gpt-4",
		Files:           []string{"a.png", "b.go"},
		Remote:          true,
		Heartbeat:       true,
		RequireApproval: true,
		ResultFile:      "/tmp/r.json",
	})

	joined := args
	want := []string{
		"agent", "--session-id", "sess-1", "--remote", "--heartbeat",
		"--require-approval", "--model", "openai/gpt-4",
		"--files", "a.png", "--files", "b.go",
		"--result-file", "/tmp/r.json", "do the thing",
	}
	if len(joined) != len(want) {
		t.Fatalf("got %v, want %v", joined, want)
	}
	for i := range want {
		if joined[i] != want[i] {
			t.Fatalf("arg %d: got %q want %q (full: %v)", i, joined[i], want[i], joined)
		}
	}
	if joined[len(joined)-1] != "do the thing" {
		t.Fatalf("prompt must be the final positional arg, got %q", joined[len(joined)-1])
	}
}

func TestAssistantContent(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		want   string
		wantOK bool
	}{
		{"assistant with content", `{"role":"assistant","content":"hello"}`, "hello", true},
		{"assistant empty content", `{"role":"assistant","content":""}`, "", false},
		{"status line ignored", `{"type":"session_stats","message":"done"}`, "", false},
		{"user line ignored", `{"role":"user","content":"hi"}`, "", false},
		{"tool line ignored", `{"role":"tool","content":"out"}`, "", false},
		{"invalid json", `not json`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := assistantContent([]byte(tt.line))
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("assistantContent(%q) = (%q,%v), want (%q,%v)", tt.line, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestParseApprovalRequest(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{"valid", `{"type":"approval_request","tool_name":"Bash","tool_args":"{}","tool_call_id":"c1"}`, true},
		{"different type", `{"type":"info","message":"x"}`, false},
		{"assistant", `{"role":"assistant","content":"hi"}`, false},
		{"invalid json", `nope`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, ok := parseApprovalRequest([]byte(tt.input))
			if ok != tt.ok {
				t.Fatalf("ok = %v want %v", ok, tt.ok)
			}
			if ok && req.ToolName != "Bash" {
				t.Fatalf("tool_name = %q want Bash", req.ToolName)
			}
		})
	}
}

func TestRunHarvestsFinalAssistantAndStreamsLines(t *testing.T) {
	script := `printf '%s\n' \
'{"role":"user","content":"hi"}' \
'{"role":"assistant","content":"first"}' \
'{"role":"assistant","content":"final answer"}' \
'{"type":"session_stats","message":"done"}'`

	var lines int
	res, err := Run(context.Background(), Options{
		Exec: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", script)
		},
		SessionID: "s1",
		Prompt:    "do",
		OnLine:    func(b []byte) { lines++ },
	})
	if err != nil {
		t.Fatalf("Run error: %v (stderr=%q)", err, res.Stderr)
	}
	if res.FinalAssistant != "final answer" {
		t.Fatalf("FinalAssistant = %q want %q", res.FinalAssistant, "final answer")
	}
	if lines != 4 {
		t.Fatalf("OnLine called %d times, want 4", lines)
	}
}

func TestRunBrokersApprovalOverStdin(t *testing.T) {
	// The agent prints an approval request, blocks on stdin, mirrors what it
	// read to stderr, then prints a fixed assistant line. Reaching that line
	// proves the runner wrote the approval response to stdin (otherwise `read`
	// would block forever and Run would hang).
	script := `printf '%s\n' '{"type":"approval_request","tool_name":"Bash","tool_args":"{}","tool_call_id":"c1"}'
read line
printf 'received:%s\n' "$line" 1>&2
printf '%s\n' '{"role":"assistant","content":"approved"}'`

	var gotReq bool
	res, err := Run(context.Background(), Options{
		Exec: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", script)
		},
		SessionID:       "s1",
		Prompt:          "do",
		RequireApproval: true,
		Approval: func(req domain.ApprovalRequest) domain.ApprovalResponse {
			gotReq = req.ToolCallID == "c1"
			return domain.ApprovalResponse{Approved: true}
		},
	})
	if err != nil {
		t.Fatalf("Run error: %v (stderr=%q)", err, res.Stderr)
	}
	if !gotReq {
		t.Fatalf("Approval callback not invoked with the request")
	}
	if res.FinalAssistant != "approved" {
		t.Fatalf("FinalAssistant = %q, want approved (stdin response not delivered?)", res.FinalAssistant)
	}
}
