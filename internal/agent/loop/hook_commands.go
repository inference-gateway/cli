package loop

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	streamevent "github.com/inference-gateway/cli/internal/platform/streamevent"
	sandbox "github.com/inference-gateway/cli/internal/sandbox"
)

// hookCommandOutputLimit caps the command output carried in a hook_command
// stream event so a chatty command can't emit an unbounded line.
const hookCommandOutputLimit = 4096

// runCommandHooks runs the command hooks due at a hook point. Each command is
// gated on the per-mode bash allow-list, the same matcher a model-proposed
// command faces, so hooks open no new bypass. Allowed commands run
// fire-and-observe: their output becomes a hook_command stream event and a log
// line, never conversation input. Off-list commands are skipped with the hint.
func runCommandHooks(ctx context.Context, cfg *config.Config, provider agentdomain.HookCommandProvider, mode agentdomain.AgentMode, hook agentdomain.HookPoint, turn int, sessionID string) {
	if provider == nil {
		if cfg == nil {
			return
		}
		provider = cfg.Hooks
	}
	due := provider.CommandsDue(hook)
	if len(due) == 0 {
		return
	}
	for _, hc := range due {
		if cfg == nil || !sandbox.IsBashCommandAllowed(cfg, hc.Command, mode) {
			hint := sandbox.BashCommandRejectionHint(cfg, hc.Command)
			logger.Warn("hook command not allow-listed; skipping",
				"name", hc.Name, "hook", string(hook), "command", hc.Command, "mode", mode.ModeKey(), "hint", hint)
			streamevent.EmitDebugEvent("hook_command_skipped", map[string]any{
				"name": hc.Name, "hook": string(hook), "command": hc.Command,
				"mode": mode.ModeKey(), "reason": "not_allowlisted", "hint": hint,
			})
			continue
		}
		runHookCommand(ctx, hook, turn, sessionID, hc)
	}
}

// runHookCommand executes one allow-listed command hook fire-and-observe: it runs
// `bash -c <command>` under the command's timeout, feeds a small JSON context on
// stdin (hook, turn, session id - Claude-Code style), captures combined output,
// and emits a hook_command stream event carrying the exit code, duration and
// (truncated) output.
func runHookCommand(ctx context.Context, hook agentdomain.HookPoint, turn int, sessionID string, hc agentdomain.HookCommand) {
	if ctx == nil {
		ctx = context.Background()
	}
	if hc.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, hc.Timeout)
		defer cancel()
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, "bash", "-c", hc.Command)
	cmd.Stdin = strings.NewReader(hookCommandStdin(hook, turn, sessionID))
	out, err := cmd.CombinedOutput()
	durMs := time.Since(start).Milliseconds()

	exitCode, errStr := classifyCommandError(err)
	logger.Info("hook command executed",
		"name", hc.Name, "hook", string(hook), "command", hc.Command,
		"exit_code", exitCode, "duration_ms", durMs, "error", errStr)
	streamevent.EmitDebugEvent("hook_command", map[string]any{
		"name": hc.Name, "hook": string(hook), "command": hc.Command, "turn": turn,
		"exit_code": exitCode, "duration_ms": durMs,
		"output": truncateHookOutput(string(out)), "error": errStr,
	})
}

// hookCommandStdin builds the JSON context fed to a hook command on stdin.
func hookCommandStdin(hook agentdomain.HookPoint, turn int, sessionID string) string {
	payload, err := json.Marshal(map[string]any{
		"hook":       string(hook),
		"turn":       turn,
		"session_id": sessionID,
	})
	if err != nil {
		return "{}"
	}
	return string(payload)
}

// classifyCommandError maps an exec error to an (exit code, message): 0/"" on
// success, the process exit code on a non-zero exit, and -1 on a start/timeout
// failure (no exit code available).
func classifyCommandError(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), err.Error()
	}
	return -1, err.Error()
}

func truncateHookOutput(s string) string {
	if len(s) <= hookCommandOutputLimit {
		return s
	}
	return s[:hookCommandOutputLimit] + "\n... (truncated)"
}
