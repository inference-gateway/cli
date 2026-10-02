package headless

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestBuildArgs(t *testing.T) {
	args := buildArgs(Options{
		SessionID:  "sess-1",
		Prompt:     "do the thing",
		Model:      "openai/gpt-4",
		Files:      []string{"a.png", "b.go"},
		Heartbeat:  true,
		ResultFile: "/tmp/r.json",
	})

	joined := args
	want := []string{
		"headless", "--session-id", "sess-1", "--heartbeat",
		"--model", "openai/gpt-4",
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

func TestRunReturnsThePlainStderrOutput(t *testing.T) {
	script := `printf '%s\n' '{"level":"info","msg":"working"}' 'first plain line' 'the run failed' 1>&2
exit 3`

	res, err := Run(context.Background(), Options{
		Exec: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", script)
		},
		SessionID: "s1",
		Prompt:    "do",
	})
	if err == nil {
		t.Fatal("Run succeeded for a failing subprocess")
	}
	if res.Stderr != "first plain line\nthe run failed" {
		t.Fatalf("Stderr = %q, want the plain lines", res.Stderr)
	}
}

func TestRunPassesStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	var lines []string
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), Options{
			Exec: func(ctx context.Context, name string, args ...string) *exec.Cmd {
				return exec.CommandContext(ctx, "sh", "-c", "cat")
			},
			SessionID: "s1", Prompt: "do", Stdin: r, KeepAlive: true,
			OnLine: func(b []byte) { lines = append(lines, string(b)) },
		})
		done <- err
	}()

	if _, err := w.Write([]byte(`{"type":"run_agent_input"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after stdin closed")
	}
	if len(lines) != 1 || lines[0] != `{"type":"run_agent_input"}` {
		t.Fatalf("child echoed %q", lines)
	}
}

func TestRunStopsTheChildOnAnOversizedLine(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, err := Run(ctx, Options{
		Exec: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", "exec head -c 11000000 /dev/zero")
		},
		SessionID: "s1",
		Prompt:    "do",
	})
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("Run error = %v, want bufio.ErrTooLong before the deadline", err)
	}
}
