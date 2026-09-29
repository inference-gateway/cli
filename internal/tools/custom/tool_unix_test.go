//go:build unix

package custom

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// scriptTool builds Echo around a shell script written into a temp directory.
// The script runs in the session working directory, so it gets that temp
// directory as $DIR.
func scriptTool(t *testing.T, script string) (*Tool, string) {
	t.Helper()
	dir := t.TempDir()
	body := "#!/bin/sh\nDIR=" + dir + "\n" + script
	if err := os.WriteFile(filepath.Join(dir, "tool.sh"), []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(echoManifest, "  - bin/echo-tool\n  - --verbose\n", "  - ./tool.sh\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "Echo.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := parseManifest(filepath.Join(dir, "Echo.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return newTool(m, dir), dir
}

func TestTool_Execute(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		wantSuccess bool
		wantOutput  string
		wantError   string
	}{
		{name: "stdin arguments become stdout result", script: "cat", wantSuccess: true, wantOutput: `{"text":"hi"}`},
		{name: "non-zero exit reports stderr", script: "echo out; echo boom >&2; exit 3", wantError: "exit status 3: boom"},
		{name: "empty stderr falls back to stdout", script: "echo only-stdout; exit 1", wantError: "exit status 1: only-stdout"},
		{name: "silent failure reports the exit status", script: "exit 2", wantError: "exit status 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, _ := scriptTool(t, tt.script)
			result, err := tool.Execute(context.Background(), map[string]any{"text": "hi"})
			if err != nil {
				t.Fatalf("Execute returned an error: %v", err)
			}
			if result.Success != tt.wantSuccess || result.Data != tt.wantOutput || result.Error != tt.wantError {
				t.Errorf("result = {Success: %v, Data: %q, Error: %q}, want {%v, %q, %q}", result.Success, result.Data, result.Error, tt.wantSuccess, tt.wantOutput, tt.wantError)
			}
		})
	}
}

func TestTool_ExecuteRejectsInvalidArgumentsBeforeStarting(t *testing.T) {
	tool, dir := scriptTool(t, `touch "$DIR/started"`)
	result, _ := tool.Execute(context.Background(), map[string]any{})
	if result.Success || !strings.Contains(result.Error, `required field "text" is missing`) {
		t.Errorf("result = %+v, want the missing argument reported", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
		t.Error("the command ran despite invalid arguments")
	}
}

func TestTool_ExecuteTimeoutKillsTheProcessTree(t *testing.T) {
	tool, dir := scriptTool(t, "sleep 30 &\necho $! > \"$DIR/child.pid\"\nwait\n")
	tool.timeout = 500 * time.Millisecond

	start := time.Now()
	result, _ := tool.Execute(context.Background(), map[string]any{"text": "hi"})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Execute took %s, want it to return soon after the timeout", elapsed)
	}
	if result.Success || result.Error != "timed out after 500ms" {
		t.Errorf("result = %+v, want a timeout", result)
	}

	data, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for utils.ProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if utils.ProcessAlive(pid) {
		t.Errorf("child process %d outlived the timed-out tool", pid)
	}
}

func TestTool_ExecuteKeepsARunThatFinishedDuringThePipeGrace(t *testing.T) {
	tool, _ := scriptTool(t, "( sleep 2 ) & echo done")
	tool.timeout = 500 * time.Millisecond

	result, _ := tool.Execute(context.Background(), map[string]any{"text": "hi"})
	if !result.Success || result.Data != "done\n" {
		t.Errorf("result = %+v, want the completed run kept with its output", result)
	}
}

func TestTool_ExecuteKeepsAFailedRunThatFinishedDuringThePipeGrace(t *testing.T) {
	tool, _ := scriptTool(t, "( sleep 2 ) & echo done; exit 3")
	tool.timeout = 500 * time.Millisecond

	result, _ := tool.Execute(context.Background(), map[string]any{"text": "hi"})
	if result.Success || result.Error != "exit status 3 (a child kept the output open): done" {
		t.Errorf("result = %+v, want the run's own failure reported", result)
	}
}

func TestTool_ExecuteCancelled(t *testing.T) {
	tool, _ := scriptTool(t, "sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	result, _ := tool.Execute(ctx, map[string]any{"text": "hi"})
	if result.Success || result.Error != context.Canceled.Error() {
		t.Errorf("result = %+v, want the call cancelled", result)
	}
}
