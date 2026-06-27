// Package agentrunner centralizes spawning `infer agent` as a subprocess and
// streaming its stdout. It is the single implementation shared by the channel
// manager, scheduler, heartbeat, and the Agent tool (local subagents), so the
// spawn/scan/approval-IPC loop lives in exactly one place.
//
// It is a leaf package (imports only domain + stdlib) so callers in the
// services, services/scheduler, and services/heartbeat packages can all depend
// on it without an import cycle.
package agentrunner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// ExecFunc matches exec.CommandContext. It is a type alias (not a defined type)
// so callers' own named exec-override types stay assignable without conversion.
type ExecFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd

// Options configures a single `infer agent` subprocess run.
type Options struct {
	// BinaryPath is the infer binary to spawn; defaults to os.Args[0] when empty.
	BinaryPath string
	// Exec overrides command construction (tests); defaults to exec.CommandContext.
	Exec ExecFunc

	SessionID string
	Prompt    string
	Model     string
	Files     []string

	RequireApproval bool
	Remote          bool
	Heartbeat       bool
	// ResultFile, when set, passes --result-file so the agent writes its final
	// assistant message to a JSON file on exit (used by detached/tmux runs).
	ResultFile string
	// ExtraEnv is appended to os.Environ() for the subprocess (e.g. depth guard).
	ExtraEnv []string

	// OnLine is called for each non-empty raw stdout line. Approval-request
	// lines handled internally (see Approval) are not passed to OnLine.
	OnLine func(line []byte)
	// Approval, when set and RequireApproval is true, resolves a tool approval
	// request into a response that is written back to the agent's stdin. It
	// blocks until the decision is made.
	Approval func(domain.ApprovalRequest) domain.ApprovalResponse
}

// Result is the outcome of a subprocess run.
type Result struct {
	// FinalAssistant is the last non-empty assistant message content seen on
	// stdout - the harvested "answer" of the run.
	FinalAssistant string
	// Stderr is the full captured standard error (also mirrored live to os.Stderr).
	Stderr string
}

// Run spawns `infer agent ...`, streams stdout line-by-line, optionally brokers
// tool approval over stdin, and returns the harvested final assistant message
// plus captured stderr. The returned error is the subprocess setup/exit error;
// callers format their own user-facing messages from it.
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
	cmd.Env = append(os.Environ(), opts.ExtraEnv...)

	var result Result

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("stdout pipe: %w", err)
	}

	brokerApproval := opts.RequireApproval && opts.Approval != nil
	var stdinWriter io.WriteCloser
	if brokerApproval {
		stdinWriter, err = cmd.StdinPipe()
		if err != nil {
			return result, fmt.Errorf("stdin pipe: %w", err)
		}
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start agent: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if brokerApproval {
			if req, ok := parseApprovalRequest(line); ok {
				writeApprovalResponse(stdinWriter, *req, opts.Approval(*req))
				continue
			}
		}

		if content, ok := assistantContent(line); ok {
			result.FinalAssistant = content
		}

		if opts.OnLine != nil {
			opts.OnLine(line)
		}
	}

	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	result.Stderr = stderrBuf.String()
	if waitErr != nil {
		return result, waitErr
	}
	if scanErr != nil {
		return result, fmt.Errorf("read agent output: %w", scanErr)
	}
	return result, nil
}

// buildArgs assembles the `agent` subcommand argument vector. The prompt is
// always the final positional argument.
func buildArgs(opts Options) []string {
	args := []string{"agent", "--session-id", opts.SessionID}
	if opts.Remote {
		args = append(args, "--remote")
	}
	if opts.Heartbeat {
		args = append(args, "--heartbeat")
	}
	if opts.RequireApproval {
		args = append(args, "--require-approval")
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
	return append(args, opts.Prompt)
}

func writeApprovalResponse(w io.Writer, req domain.ApprovalRequest, resp domain.ApprovalResponse) {
	resp.Type = "approval_response"
	resp.ToolCallID = req.ToolCallID
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = w.Write(append(data, '\n'))
}

// parseApprovalRequest attempts to parse a JSON line as an ApprovalRequest.
func parseApprovalRequest(line []byte) (*domain.ApprovalRequest, bool) {
	var req domain.ApprovalRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, false
	}
	if req.Type != "approval_request" {
		return nil, false
	}
	return &req, true
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
