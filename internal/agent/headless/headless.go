package headless

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// ExecFunc matches exec.CommandContext. It is a type alias (not a defined type)
// so callers' own named exec-override types stay assignable without conversion.
type ExecFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd

// Options configures a single `infer headless` subprocess run.
type Options struct {
	BinaryPath string
	Exec       ExecFunc
	SessionID  string
	Prompt     string
	Model      string
	Files      []string
	Heartbeat  bool
	ResultFile string
	ExtraEnv   []string
	Stdin      *os.File
	KeepAlive  bool
	OnLine     func(line []byte)
}

// Result is the outcome of a subprocess run.
type Result struct {
	FinalAssistant string
	Stderr         string
}

// Run spawns `infer headless ...`, streams stdout line-by-line and returns the
// harvested final assistant message. The run logs JSON to stderr, collected
// into this process's log as it runs. The returned error is the subprocess
// setup/exit error. Callers format their own user-facing messages from it.
func Run(ctx context.Context, opts Options) (Result, error) {
	bin := opts.BinaryPath
	if bin == "" {
		bin = os.Args[0]
	}
	execFn := opts.Exec
	if execFn == nil {
		execFn = exec.CommandContext
	}

	cmd := execFn(ctx, bin, buildArgs(opts)...)
	cmd.Env = append(append(os.Environ(), opts.ExtraEnv...), logger.ChildStderrJSONEnv+"=true")

	var result Result

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("stdout pipe: %w", err)
	}

	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}

	stderrReader, stderrWriter := io.Pipe()
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start agent: %w", err)
	}
	projectDir, _ := os.Getwd()
	lastStderrLine := make(chan string, 1)
	go func() {
		lastStderrLine <- logger.CollectChildStderr(stderrReader, "project_dir", projectDir, "conversation_id", opts.SessionID, "worker_pid", cmd.Process.Pid)
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if content, ok := assistantContent(line); ok {
			result.FinalAssistant = content
		}

		if opts.OnLine != nil {
			opts.OnLine(line)
		}
	}

	scanErr := scanner.Err()
	if scanErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	_ = stderrWriter.Close()
	result.Stderr = <-lastStderrLine
	if scanErr != nil {
		return result, fmt.Errorf("read agent output: %w", scanErr)
	}
	if waitErr != nil {
		return result, waitErr
	}
	return result, nil
}

// buildArgs assembles the `headless` subcommand argument vector. The prompt is
// always the final positional argument.
func buildArgs(opts Options) []string {
	args := []string{"headless", "--session-id", opts.SessionID}
	if opts.Heartbeat {
		args = append(args, "--heartbeat")
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	for _, f := range opts.Files {
		args = append(args, "--files", f)
	}
	if opts.ResultFile != "" {
		args = append(args, "--result-file", opts.ResultFile)
	}
	if opts.KeepAlive {
		args = append(args, "--keep-alive")
	}
	return append(args, opts.Prompt)
}

// assistantContent returns the content of a JSON assistant message line when it
// is a conversation message (no "type") with role "assistant" and non-empty content.
func assistantContent(line []byte) (string, bool) {
	var msg struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		return "", false
	}
	if msg.Type != "" || msg.Role != "assistant" || msg.Content == "" {
		return "", false
	}
	return msg.Content, true
}
