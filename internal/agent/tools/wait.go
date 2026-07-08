package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"

	fsnotify "github.com/fsnotify/fsnotify"
)

// WaitTool blocks inside a single tool execution until a condition is met
// (shells exit, file event, or check command succeeds), then returns once
// with the outcome. Waiting costs zero chat completions.
type WaitTool struct {
	config       *config.Config
	enabled      bool
	formatter    domain.BaseFormatter
	shellService domain.BackgroundShellService
}

// NewWaitTool creates a new Wait tool.
func NewWaitTool(cfg *config.Config, shellService domain.BackgroundShellService) *WaitTool {
	return &WaitTool{
		config:       cfg,
		enabled:      cfg.Tools.Enabled && cfg.Tools.Wait.Enabled,
		formatter:    domain.NewBaseFormatter("Wait"),
		shellService: shellService,
	}
}

// Definition returns the tool definition for the LLM.
func (t *WaitTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.Wait.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Wait",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"condition": map[string]any{
						"type": "string",
						"description": "The condition to wait for: 'shells' (background shell(s) exit), " +
							"'file' (file path created/modified/removed), or 'command' (check command exits 0).",
						"enum": []string{"shells", "file", "command"},
					},
					"shell_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Shell ID(s) to wait for (condition=shells). Omit to wait for all pending background shells.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "File path to watch (condition=file).",
					},
					"event": map[string]any{
						"type":        "string",
						"description": "File event to wait for: 'create', 'modify', 'remove', or 'any' (default). Only used with condition=file.",
						"enum":        []string{"create", "modify", "remove", "any"},
					},
					"command": map[string]any{
						"type":        "string",
						"description": "Check command to re-run until it exits 0 (condition=command). Goes through the same bash allow-list as the Bash tool.",
					},
					"pending_exit_codes": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "number"},
						"description": "Non-zero exit codes that mean 'still pending, keep polling' (condition=command). " +
							"Any other non-zero exit ends the wait immediately with reason 'check_failed'. " +
							"Omit to keep polling on every non-zero exit. Example: [8] for 'gh pr checks'.",
					},
					"timeout_seconds": map[string]any{
						"type":        "number",
						"description": "Maximum time to wait in seconds (bounded by the config ceiling). Required.",
					},
				},
				"required":             []string{"condition", "timeout_seconds"},
				"additionalProperties": false,
			},
		},
	}
}

// Execute runs the Wait tool with given arguments.
func (t *WaitTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.Validate(args); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "Wait",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	condition, _ := args["condition"].(string)
	timeoutSec, _ := args["timeout_seconds"].(float64)

	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	maxTimeout := float64(t.config.Tools.Wait.MaxTimeoutSeconds)
	if maxTimeout <= 0 {
		maxTimeout = 600
	}
	if timeoutSec > maxTimeout {
		timeoutSec = maxTimeout
	}

	timeout := time.Duration(timeoutSec) * time.Second

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var result map[string]any

	switch condition {
	case "shells":
		result = t.waitShells(waitCtx, args)
	case "file":
		result = t.waitFile(waitCtx, args)
	case "command":
		result = t.waitCommand(waitCtx, args)
	default:
		return &domain.ToolExecutionResult{
			ToolName:  "Wait",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("unknown condition: %s (must be 'shells', 'file', or 'command')", condition),
		}, nil
	}

	elapsed := time.Since(start)
	result["elapsed_seconds"] = elapsed.Seconds()

	success := true
	errMsg := ""
	switch result["reason"] {
	case "timeout":
		success = false
		errMsg = fmt.Sprintf("timed out after %.0fs waiting for condition '%s'", elapsed.Seconds(), condition)
	case "cancelled":
		success = false
		errMsg = "wait was cancelled"
	case "check_failed":
		success = false
		exitCode, _ := result["last_exit_code"].(int)
		errMsg = fmt.Sprintf("check command failed with exit code %d (not in pending_exit_codes)", exitCode)
	}

	return &domain.ToolExecutionResult{
		ToolName:  "Wait",
		Arguments: args,
		Success:   success,
		Duration:  elapsed,
		Data:      result,
		Error:     errMsg,
	}, nil
}

// Validate checks the tool arguments.
func (t *WaitTool) Validate(args map[string]any) error {
	condition, ok := args["condition"].(string)
	if !ok || condition == "" {
		return fmt.Errorf("condition is required and must be one of: shells, file, command")
	}

	switch condition {
	case "shells", "file", "command":
		// valid
	default:
		return fmt.Errorf("condition must be one of: shells, file, command (got %q)", condition)
	}

	timeoutSec, ok := args["timeout_seconds"].(float64)
	if !ok || timeoutSec <= 0 {
		return fmt.Errorf("timeout_seconds is required and must be a positive number")
	}

	maxTimeout := float64(t.config.Tools.Wait.MaxTimeoutSeconds)
	if maxTimeout > 0 && timeoutSec > maxTimeout {
		return fmt.Errorf("timeout_seconds %.0f exceeds maximum of %.0f", timeoutSec, maxTimeout)
	}

	if condition == "file" {
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return fmt.Errorf("path is required when condition is 'file'")
		}
	}

	if condition == "command" {
		cmd, ok := args["command"].(string)
		if !ok || cmd == "" {
			return fmt.Errorf("command is required when condition is 'command'")
		}
		if err := validatePendingExitCodes(args); err != nil {
			return err
		}
	}

	return nil
}

// validatePendingExitCodes checks that pending_exit_codes, when present, is
// an array of numbers.
func validatePendingExitCodes(args map[string]any) error {
	raw, present := args["pending_exit_codes"]
	if !present {
		return nil
	}
	codes, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("pending_exit_codes must be an array of numbers")
	}
	for _, c := range codes {
		if _, ok := c.(float64); !ok {
			return fmt.Errorf("pending_exit_codes must be an array of numbers")
		}
	}
	return nil
}

// IsEnabled returns whether the tool is enabled.
func (t *WaitTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results for different contexts.
func (t *WaitTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *WaitTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	if !result.Success {
		return fmt.Sprintf("Wait failed: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Wait completed"
	}
	condition, _ := data["condition"].(string)
	reason, _ := data["reason"].(string)
	if reason == "condition_met" {
		return fmt.Sprintf("Wait(%s) condition met after %.1fs", condition, result.Duration.Seconds())
	}
	return fmt.Sprintf("Wait(%s) %s after %.1fs", condition, reason, result.Duration.Seconds())
}

// FormatForUI formats the result for UI display.
func (t *WaitTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", toolCall)
	fmt.Fprintf(&output, "└─ %s %s", statusIcon, preview)

	return output.String()
}

// FormatForLLM formats the result for LLM consumption. Condition details
// (last output, exit codes, shell states) are included even on failure so
// the model can see why the wait ended and decide what to do next.
func (t *WaitTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		if !result.Success {
			return fmt.Sprintf("Error: %s", result.Error)
		}
		return "Wait completed"
	}

	var b strings.Builder
	condition, _ := data["condition"].(string)
	reason, _ := data["reason"].(string)
	elapsed, _ := data["elapsed_seconds"].(float64)

	if !result.Success && result.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", result.Error)
	}
	fmt.Fprintf(&b, "Wait condition: %s\n", condition)
	fmt.Fprintf(&b, "Outcome: %s\n", reason)
	fmt.Fprintf(&b, "Elapsed: %.1fs\n", elapsed)
	t.formatConditionDetails(data, condition, &b)

	return b.String()
}

// formatConditionDetails appends condition-specific details to the builder.
func (t *WaitTool) formatConditionDetails(data map[string]any, condition string, b *strings.Builder) {
	switch condition {
	case "shells":
		t.formatShellsResult(data, b)
	case "file":
		t.formatFileResult(data, b)
	case "command":
		t.formatCommandResult(data, b)
	}
}

// formatShellsResult appends shell result details to the builder.
func (t *WaitTool) formatShellsResult(data map[string]any, b *strings.Builder) {
	shells, ok := data["shells"].([]any)
	if !ok {
		return
	}
	fmt.Fprintf(b, "\nShell results:\n")
	for _, s := range shells {
		sh, ok := s.(map[string]any)
		if !ok {
			continue
		}
		id, _ := sh["shell_id"].(string)
		ec, _ := sh["exit_code"].(float64)
		output, _ := sh["output"].(string)
		fmt.Fprintf(b, "  %s: exit %d\n", id, int(ec))
		if output != "" {
			lines := strings.Split(strings.TrimSpace(output), "\n")
			limit := 10
			if len(lines) < limit {
				limit = len(lines)
			}
			for _, line := range lines[:limit] {
				fmt.Fprintf(b, "    %s\n", line)
			}
			if len(lines) > limit {
				fmt.Fprintf(b, "    ... (%d more lines)\n", len(lines)-limit)
			}
		}
	}
}

// formatFileResult appends file event details to the builder.
func (t *WaitTool) formatFileResult(data map[string]any, b *strings.Builder) {
	if path, _ := data["path"].(string); path != "" {
		fmt.Fprintf(b, "Path: %s\n", path)
	}
	if event, _ := data["event"].(string); event != "" {
		fmt.Fprintf(b, "Event: %s\n", event)
	}
}

// formatCommandResult appends command result details to the builder.
func (t *WaitTool) formatCommandResult(data map[string]any, b *strings.Builder) {
	if cmd, _ := data["command"].(string); cmd != "" {
		fmt.Fprintf(b, "Command: %s\n", cmd)
	}
	if code, ok := data["last_exit_code"].(int); ok {
		fmt.Fprintf(b, "Last exit code: %d\n", code)
	}
	if output, _ := data["last_output"].(string); output != "" {
		fmt.Fprintf(b, "Last output: %s\n", strings.TrimSpace(output))
	}
}

// ShouldCollapseArg determines if an argument should be collapsed in display.
func (t *WaitTool) ShouldCollapseArg(key string) bool {
	return key == "command" || key == "shell_ids"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI.
func (t *WaitTool) ShouldAlwaysExpand() bool {
	return false
}

// waitShells blocks until the referenced background shell(s) exit.
func (t *WaitTool) waitShells(ctx context.Context, args map[string]any) map[string]any {
	shellIDs, _ := args["shell_ids"].([]any)
	var targetIDs []string
	for _, id := range shellIDs {
		if s, ok := id.(string); ok {
			targetIDs = append(targetIDs, s)
		}
	}

	if len(targetIDs) == 0 && t.shellService != nil {
		allShells := t.shellService.GetAllShells()
		for _, s := range allShells {
			if s.State == domain.ShellStateRunning {
				targetIDs = append(targetIDs, s.ShellID)
			}
		}
	}

	if len(targetIDs) == 0 {
		return map[string]any{
			"condition": "shells",
			"reason":    "no_shells",
			"message":   "No background shells to wait for.",
		}
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			reason := "timeout"
			if ctx.Err() == context.Canceled {
				reason = "cancelled"
			}
			return t.shellResults(targetIDs, reason)
		case <-ticker.C:
			allDone := true
			for _, id := range targetIDs {
				shell := t.shellService.GetShell(id)
				if shell == nil || !shell.State.IsTerminal() {
					allDone = false
					break
				}
			}
			if allDone {
				return t.shellResults(targetIDs, "condition_met")
			}
		}
	}
}

// shellResults collects exit codes and tail output for the given shell IDs.
func (t *WaitTool) shellResults(ids []string, reason string) map[string]any {
	if t.shellService == nil {
		return map[string]any{
			"condition": "shells",
			"reason":    "no_shells",
			"message":   "Shell service not available.",
		}
	}

	results := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		shell := t.shellService.GetShell(id)
		if shell == nil {
			results = append(results, map[string]any{
				"shell_id":  id,
				"exit_code": -1,
				"output":    "shell not found",
			})
			continue
		}
		exitCode := -1
		if shell.ExitCode != nil {
			exitCode = *shell.ExitCode
		}
		tailOutput := ""
		if shell.OutputBuffer != nil {
			tailOutput = shell.OutputBuffer.Recent(4096)
		}
		results = append(results, map[string]any{
			"shell_id":  id,
			"command":   shell.Command,
			"exit_code": exitCode,
			"state":     string(shell.State),
			"output":    tailOutput,
		})
	}

	return map[string]any{
		"condition": "shells",
		"reason":    reason,
		"shells":    results,
	}
}

// waitFile blocks on fsnotify events for a given path.
func (t *WaitTool) waitFile(ctx context.Context, args map[string]any) map[string]any {
	path, _ := args["path"].(string)
	eventStr, _ := args["event"].(string)
	if eventStr == "" {
		eventStr = "any"
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return map[string]any{
			"condition": "file",
			"reason":    "error",
			"error":     fmt.Sprintf("failed to resolve path: %v", err),
		}
	}

	if eventStr == "create" {
		if _, err := os.Stat(absPath); err == nil {
			return map[string]any{
				"condition": "file",
				"reason":    "condition_met",
				"path":      absPath,
				"event":     eventStr,
				"op":        "exists",
			}
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return map[string]any{
			"condition": "file",
			"reason":    "error",
			"error":     fmt.Sprintf("failed to create file watcher: %v", err),
		}
	}
	defer func() { _ = watcher.Close() }()

	parentDir := filepath.Dir(absPath)
	if err := watcher.Add(parentDir); err != nil {
		return map[string]any{
			"condition": "file",
			"reason":    "error",
			"error":     fmt.Sprintf("failed to watch directory %s: %v", parentDir, err),
		}
	}

	targetName := filepath.Base(absPath)

	for {
		select {
		case <-ctx.Done():
			reason := "timeout"
			if ctx.Err() == context.Canceled {
				reason = "cancelled"
			}
			return map[string]any{
				"condition": "file",
				"reason":    reason,
				"path":      absPath,
			}
		case ev, ok := <-watcher.Events:
			if !ok {
				return map[string]any{
					"condition": "file",
					"reason":    "error",
					"error":     "file watcher closed",
				}
			}
			if filepath.Base(ev.Name) != targetName {
				continue
			}
			if eventMatches(ev.Op, eventStr) {
				return map[string]any{
					"condition": "file",
					"reason":    "condition_met",
					"path":      absPath,
					"event":     eventStr,
					"op":        ev.Op.String(),
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return map[string]any{
					"condition": "file",
					"reason":    "error",
					"error":     "file watcher closed",
				}
			}
			logger.Debug("wait file watcher error", "error", err)
		}
	}
}

// eventMatches checks if the given fsnotify operation matches the requested event.
func eventMatches(op fsnotify.Op, eventStr string) bool {
	switch eventStr {
	case "create":
		return op&fsnotify.Create != 0
	case "modify":
		return op&fsnotify.Write != 0 || op&fsnotify.Chmod != 0
	case "remove":
		return op&fsnotify.Remove != 0 || op&fsnotify.Rename != 0
	case "any":
		return true
	default:
		return false
	}
}

// waitCommand re-runs a check command at a fixed interval until it exits 0.
// Exit codes listed in pending_exit_codes mean "still pending, keep polling";
// any other non-zero exit ends the wait immediately with reason check_failed
// so a check that distinguishes pending from failed (e.g. gh pr checks:
// 0=passed, 8=pending, else failed) returns the moment the outcome is known.
func (t *WaitTool) waitCommand(ctx context.Context, args map[string]any) map[string]any {
	cmdStr, _ := args["command"].(string)

	if cmdStr == "" {
		return map[string]any{
			"condition": "command",
			"reason":    "error",
			"error":     "command is required when condition is 'command'",
		}
	}

	if runtime.GOOS == "windows" {
		logger.Warn("wait command condition requires bash which is not available on Windows")
		return map[string]any{
			"condition": "command",
			"reason":    "error",
			"error":     "command condition requires bash (not available on Windows)",
		}
	}

	if !t.config.IsBashCommandAllowed(cmdStr, domain.BashAllowModeKey(ctx)) {
		return map[string]any{
			"condition": "command",
			"reason":    "not_allowed",
			"command":   cmdStr,
			"error":     fmt.Sprintf("command not allowed by bash allow-list: %s", cmdStr),
		}
	}

	pendingCodes := map[int]bool{}
	if raw, ok := args["pending_exit_codes"].([]any); ok {
		for _, c := range raw {
			if f, ok := c.(float64); ok {
				pendingCodes[int(f)] = true
			}
		}
	}

	buildResult := func(reason, lastOutput string, exitCode, attempts int) map[string]any {
		return map[string]any{
			"condition":      "command",
			"reason":         reason,
			"command":        cmdStr,
			"last_output":    lastOutput,
			"last_exit_code": exitCode,
			"attempts":       attempts,
		}
	}

	classify := func(exitCode int) string {
		if exitCode == 0 {
			return "condition_met"
		}
		if len(pendingCodes) > 0 && !pendingCodes[exitCode] {
			return "check_failed"
		}
		return ""
	}

	pollInterval := t.config.Tools.Wait.CommandPollIntervalMs
	if pollInterval <= 0 {
		pollInterval = 2000
	}
	interval := time.Duration(pollInterval) * time.Millisecond

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	attempts := 1
	lastOutput, exitCode := t.runCheckCommand(ctx, cmdStr)
	if reason := classify(exitCode); reason != "" {
		return buildResult(reason, lastOutput, exitCode, attempts)
	}

	for {
		select {
		case <-ctx.Done():
			reason := "timeout"
			if ctx.Err() == context.Canceled {
				reason = "cancelled"
			}
			return buildResult(reason, lastOutput, exitCode, attempts)
		case <-ticker.C:
			lastOutput, exitCode = t.runCheckCommand(ctx, cmdStr)
			attempts++
			if reason := classify(exitCode); reason != "" {
				return buildResult(reason, lastOutput, exitCode, attempts)
			}
		}
	}
}

// runCheckCommand executes a check command and returns its output and exit code.
func (t *WaitTool) runCheckCommand(ctx context.Context, cmdStr string) (string, int) {
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, "bash", "-c", cmdStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return string(output), exitErr.ExitCode()
		}
		return string(output), -1
	}
	return string(output), 0
}

// Ensure WaitTool implements domain.Tool
var _ domain.Tool = (*WaitTool)(nil)
