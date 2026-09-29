package infrastructure

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// workerStopGrace is how long a worker gets after its hang-up to shut its
// gateway, MCP servers and containers down before it is killed.
// ponytail: no process group, so a kill after the grace orphans the worker's
// own children. Setpgid plus a group kill if that ever shows up.
const workerStopGrace = 25 * time.Second

// processWorker is a running `infer headless --serve` process: frames go to
// its stdin one line each, and its stdout lines come back on lines.
type processWorker struct {
	ctx    context.Context
	cancel context.CancelFunc
	stdin  io.WriteCloser
	sendMu sync.Mutex
	lines  chan []byte
	done   chan struct{}
}

// LaunchWorker starts the session worker for a thread: this binary in serve
// mode, in the thread's project dir so the project's config, storage, skills
// and sandbox apply, with the thread's options as flags and env overrides.
func LaunchWorker(key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions) (sessionsdomain.Worker, error) {
	bin, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolving the infer binary: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, workerArgs(key, opts)...)
	cmd.Dir = key.ProjectDir
	cmd.Env = append(os.Environ(), workerEnv(key, opts)...)
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = workerStopGrace

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	w := &processWorker{ctx: ctx, cancel: cancel, stdin: stdin, lines: make(chan []byte, 64), done: make(chan struct{})}
	cmd.Cancel = w.hangUp
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("starting the session worker: %w", err)
	}

	go w.read(cmd, stdout, key)
	return w, nil
}

// workerArgs are the serve-mode flags. --require-approval routes approvals to
// stdin, where the daemon relays them.
func workerArgs(key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions) []string {
	args := []string{"headless", "--serve", "--require-approval", "--session-id", key.ConversationID}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Mode != "" {
		args = append(args, "--mode", opts.Mode)
	}
	return args
}

// workerEnv carries the options headless has no flag for as INFER_ overrides.
// PWD follows the project dir, since exec with an explicit Env leaves the
// daemon's PWD in place and the project slug is derived from the working dir.
func workerEnv(key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions) []string {
	env := []string{"PWD=" + key.ProjectDir}
	if opts.SystemPrompt != "" {
		env = append(env, "INFER_PROMPTS_AGENT_SYSTEM_PROMPT="+opts.SystemPrompt)
	}
	if opts.CustomInstructions != "" {
		env = append(env, "INFER_PROMPTS_AGENT_CUSTOM_INSTRUCTIONS="+opts.CustomInstructions)
	}
	if len(opts.SandboxDirectories) > 0 {
		env = append(env, "INFER_TOOLS_SANDBOX_DIRECTORIES="+strings.Join(opts.SandboxDirectories, "\n"))
	}
	if opts.MaxTurns > 0 {
		env = append(env, "INFER_AGENT_MAX_TURNS="+strconv.Itoa(opts.MaxTurns))
	}
	return env
}

// read forwards stdout lines until the worker exits. Lines are unbounded on
// purpose: a long conversation's MESSAGES_SNAPSHOT is one line, and a capped
// scanner would stop reading and deadlock the worker on a full pipe.
func (w *processWorker) read(cmd *exec.Cmd, stdout io.Reader, key sessionsdomain.ThreadKey) {
	defer close(w.done)
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			w.lines <- line
		}
		if err != nil {
			break
		}
	}
	close(w.lines)
	if err := cmd.Wait(); err != nil && w.ctx.Err() == nil {
		logger.Warn("session worker exited", "project_dir", key.ProjectDir, "conversation_id", key.ConversationID, "error", err)
	}
}

// Send writes frame to the worker's stdin as one compact line, so a frame a
// client pretty-printed cannot split into several.
func (w *processWorker) Send(frame []byte) error {
	var line bytes.Buffer
	if err := json.Compact(&line, frame); err != nil {
		return err
	}
	line.WriteByte('\n')
	w.sendMu.Lock()
	defer w.sendMu.Unlock()
	_, err := w.stdin.Write(line.Bytes())
	return err
}

func (w *processWorker) Lines() <-chan []byte {
	return w.lines
}

// hangUp asks the worker to stop the same way on every platform: an interrupt
// frame ends the running turn, and stdin EOF makes the worker run its own
// cleanup and exit. The grace period's kill covers a worker that does not.
func (w *processWorker) hangUp() error {
	_ = w.Send([]byte(`{"type":"interrupt"}`))
	w.sendMu.Lock()
	defer w.sendMu.Unlock()
	return w.stdin.Close()
}

// Stop hangs the worker up, which runs its own cleanup, and waits for it to
// exit, killing it after the grace period.
func (w *processWorker) Stop() {
	w.cancel()
	<-w.done
}
