package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	utils "github.com/inference-gateway/cli/internal/utils"
)

// Streaming-output coalescing thresholds. readPipeWithBatching accumulates
// command output and flushes it to the streaming callback at most once per
// bashStreamFlushInterval (or sooner once bashStreamFlushBytes have piled up),
// so a high-volume command (e.g. `git diff` on a large branch) emits a bounded
// number of UI events instead of one per line. One event per line otherwise
// stalls the TUI: every chunk makes a full Bubble Tea Update/View round-trip,
// and the terminal completion event that clears the status bar queues behind
// them. The full result output is captured separately and is unaffected.
const (
	bashStreamFlushInterval = 50 * time.Millisecond
	bashStreamFlushBytes    = 64 * 1024
)

// BashTool handles bash command execution with security validation
type BashTool struct {
	config                 *config.Config
	enabled                bool
	formatter              domain.BaseFormatter
	backgroundShellService domain.BackgroundShellService
}

// NewBashTool creates a new bash tool
func NewBashTool(cfg *config.Config, backgroundShellService domain.BackgroundShellService) *BashTool {
	return &BashTool{
		config:                 cfg,
		enabled:                cfg.Tools.Enabled && cfg.Tools.Bash.Enabled,
		formatter:              domain.NewBaseFormatter("Bash"),
		backgroundShellService: backgroundShellService,
	}
}

// Definition returns the tool definition for the LLM. The command parameter is
// intentionally not constrained by an enum: the set of auto-approved commands is
// per-mode (and may be ".*"), so it cannot be expressed as a fixed schema. The
// effective allow-list for the active mode is surfaced in the system prompt
// instead; off-list commands still execute via approval (chat) or are rejected
// with a reason (agent mode).
func (t *BashTool) Definition() sdk.ChatCompletionTool {
	commandDescription := "The bash command to execute. Run ONE command per call - " +
		"pipes and operators (|, &&, ||, ;) are not auto-approved. Which commands run " +
		"without approval depends on the current agent mode and is listed in the system " +
		"prompt; anything off that list requires approval (chat) or is rejected (agent mode)."

	description := t.config.Prompts.Tools.Bash.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Bash",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": commandDescription,
					},
					"format": map[string]any{
						"type":        "string",
						"description": "Output format (text or json)",
						"enum":        []string{"text", "json"},
						"default":     "text",
					},
					"detached": map[string]any{
						"type":        "boolean",
						"description": "Run the command in the background and return immediately. Use BashOutput to read output and Wait to wait for completion. Equivalent to pressing Ctrl+B during execution.",
						"default":     false,
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

// Execute runs the bash tool with given arguments
func (t *BashTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	command, ok := args["command"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Bash",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "command parameter is required and must be a string",
		}, nil
	}

	if detached, ok := args["detached"].(bool); ok && detached {
		detachChan := make(chan struct{}, 1)
		detachChan <- struct{}{}
		ctx = domain.WithBashDetachChannel(ctx, detachChan)
	}

	bashResult, err := t.executeBash(ctx, command)
	success := err == nil && bashResult.ExitCode == 0

	toolData := &domain.BashToolResult{
		Command:  bashResult.Command,
		Output:   bashResult.Output,
		Error:    bashResult.Error,
		ExitCode: bashResult.ExitCode,
		Duration: bashResult.Duration,
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Bash",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	if err != nil {
		result.Error = err.Error()
	} else if !success {
		detail := strings.TrimSpace(bashResult.Output)
		if detail == "" {
			detail = bashResult.Error
		}
		result.Error = fmt.Sprintf("exit status %d: %s", bashResult.ExitCode, detail)
	}

	return result, nil
}

// Validate checks the bash tool arguments. It has no agent-mode context, so the
// allow-check uses standard mode (the interactive default, and what `infer tools
// validate` reports). The authoritative, mode-aware gate is in executeBash.
func (t *BashTool) Validate(args map[string]any) error {
	command, ok := args["command"].(string)
	if !ok {
		return fmt.Errorf("command parameter is required and must be a string")
	}

	if !t.config.IsBashCommandAllowed(command, "standard") {
		return t.notAllowedError(command, "standard")
	}

	return nil
}

// IsEnabled returns whether the bash tool is enabled
func (t *BashTool) IsEnabled() bool {
	return t.enabled
}

// BashResult represents the internal result of a bash command execution
type BashResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
}

// executeBash executes a bash command with security validation
func (t *BashTool) executeBash(ctx context.Context, command string) (*BashResult, error) {
	start := time.Now()
	result := &BashResult{
		Command: command,
	}

	wasApproved := domain.IsToolApproved(ctx)
	modeKey := domain.BashAllowModeKey(ctx)

	if !wasApproved && !t.config.IsBashCommandAllowed(command, modeKey) {
		err := t.notAllowedError(command, modeKey)
		result.ExitCode = -1
		result.Duration = time.Since(start).String()
		result.Error = err.Error()
		return result, err
	}

	outputCallback := domain.GetBashOutputCallback(ctx)
	hasCallback := domain.HasBashOutputCallback(ctx)
	detachChan := domain.GetBashDetachChannel(ctx)
	hasDetachChan := domain.HasBashDetachChannel(ctx)

	var cmdCtx context.Context
	var cancel context.CancelFunc

	if hasDetachChan && detachChan != nil && t.backgroundShellService != nil {
		cmdCtx, cancel = context.WithCancel(context.Background())
		cmdCtx = domain.WithBashDetachChannel(cmdCtx, detachChan)
		cmdCtx = domain.WithBashOutputCallback(cmdCtx, outputCallback)
	} else {
		timeout := time.Duration(t.config.Tools.Bash.Timeout) * time.Second
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		cmdCtx, cancel = context.WithTimeout(ctx, timeout)
	}

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)

	if hasCallback && outputCallback != nil {
		return t.executeBashWithStreaming(cmdCtx, cmd, outputCallback, cancel, start)
	}

	defer cancel()

	output, err := cmd.CombinedOutput()
	result.Duration = time.Since(start).String()
	result.Output = string(output)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
		} else {
			result.ExitCode = -1
		}
		result.Error = err.Error()
	}

	return result, nil
}

// executeBashWithStreaming executes a bash command and streams output through the callback
func (t *BashTool) executeBashWithStreaming(ctx context.Context, cmd *exec.Cmd, callback domain.BashOutputCallback, cancel context.CancelFunc, start time.Time) (*BashResult, error) {
	result := &BashResult{
		Command: cmd.Args[len(cmd.Args)-1],
	}

	shouldCancel := true
	defer func() {
		if shouldCancel {
			cancel()
		}
	}()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start).String()
		result.Error = fmt.Sprintf("failed to create stdout pipe: %v", err)
		return result, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start).String()
		result.Error = fmt.Sprintf("failed to create stderr pipe: %v", err)
		return result, err
	}

	if err := cmd.Start(); err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start).String()
		result.Error = fmt.Sprintf("failed to start command: %v", err)
		return result, err
	}

	detachChan, hasDetachChan := ctx.Value(domain.BashDetachChannelKey).(<-chan struct{})

	var outputBuilder strings.Builder
	var outputBuffer domain.OutputRingBuffer
	var wg sync.WaitGroup
	var outputMux sync.Mutex

	if hasDetachChan && detachChan != nil && t.backgroundShellService != nil {
		outputBuffer = utils.NewOutputRingBuffer(1024 * 1024)
	}

	detached := false
	detachedMux := sync.Mutex{}

	wg.Add(2)
	go t.readPipeWithBatching(stdout, callback, outputBuffer, &outputBuilder, &outputMux, &detached, &detachedMux, &wg)
	go t.readPipeWithBatching(stderr, callback, outputBuffer, &outputBuilder, &outputMux, &detached, &detachedMux, &wg)

	if hasDetachChan && detachChan != nil && t.backgroundShellService != nil && outputBuffer != nil {
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-detachChan:
			detachedMux.Lock()
			detached = true
			detachedMux.Unlock()

			shellID, err := t.backgroundShellService.DetachToBackground(ctx, cmd, result.Command, outputBuffer, done)
			if err != nil {
				logger.Debug("bash: DetachToBackground failed", "error", err)
				result.ExitCode = -1
				result.Duration = time.Since(start).String()
				result.Error = fmt.Sprintf("failed to detach to background: %v", err)
				return result, err
			}

			result.Duration = time.Since(start).String()
			result.Output = fmt.Sprintf("Command detached to background (shell ID: %s)\nUse 'BashOutput(shell_id=\"%s\")' to view output.", shellID, shellID)
			result.ExitCode = 0
			shouldCancel = false
			return result, nil

		case <-done:
			// TODO: run some cleanups perhaps
		}
	} else {
		wg.Wait()
	}

	err = cmd.Wait()
	result.Duration = time.Since(start).String()

	if outputBuffer != nil {
		result.Output = outputBuffer.String()
	} else {
		result.Output = outputBuilder.String()
	}

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
		} else {
			result.ExitCode = -1
		}
		result.Error = err.Error()
	}

	return result, nil
}

// readPipeWithBatching reads from a pipe and streams output line by line
func (t *BashTool) readPipeWithBatching(
	pipe io.ReadCloser,
	callback domain.BashOutputCallback,
	outputBuffer domain.OutputRingBuffer,
	outputBuilder *strings.Builder,
	outputMux *sync.Mutex,
	detached *bool,
	detachedMux *sync.Mutex,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	scanner := bufio.NewScanner(pipe)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var pending strings.Builder
	var lastFlush time.Time

	flush := func() {
		if pending.Len() == 0 {
			return
		}
		detachedMux.Lock()
		isDetached := *detached
		detachedMux.Unlock()
		if !isDetached {
			callback(strings.TrimRight(pending.String(), "\n"))
		}
		pending.Reset()
		lastFlush = time.Now()
	}

	for scanner.Scan() {
		line := scanner.Text()

		outputMux.Lock()
		if outputBuffer != nil {
			_, _ = outputBuffer.Write([]byte(line + "\n"))
		} else {
			outputBuilder.WriteString(line)
			outputBuilder.WriteString("\n")
		}
		outputMux.Unlock()

		pending.WriteString(line)
		pending.WriteByte('\n')

		if pending.Len() >= bashStreamFlushBytes || time.Since(lastFlush) >= bashStreamFlushInterval {
			flush()
		}
	}

	flush()

	if err := scanner.Err(); err != nil {
		logger.Debug("bash: scanner error", "error", err)
	}
}

// notAllowedError builds the rejection error for a command that is not in the
// bash allow-list for mode, appending the actionable hint from
// config.BashCommandRejectionHint (run one command at a time, drop a redirect,
// avoid leaking a $VAR, ...) so the model can correct course rather than retrying
// blindly. The Bash tool, the approval policy, and agent auto-approval all share
// config.IsBashCommandAllowed, so they agree on exactly what runs without prompting.
func (t *BashTool) notAllowedError(command, mode string) error {
	if hint := config.BashCommandRejectionHint(command); hint != "" {
		return fmt.Errorf("command not allowed: %s - %s", command, hint)
	}
	return fmt.Errorf("command not allowed: %s (%s mode)", command, mode)
}

// FormatResult formats tool execution results for different contexts
func (t *BashTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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

// FormatPreview returns a short preview of the result for UI display
func (t *BashTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	bashResult, ok := result.Data.(*domain.BashToolResult)
	if !ok {
		if result.Success {
			return "Execution completed successfully"
		}
		return "Execution failed"
	}

	if bashResult.ExitCode == 0 && bashResult.Output != "" {
		output := strings.TrimSpace(bashResult.Output)
		lines := strings.Split(output, "\n")

		if len(lines) <= 4 {
			return output
		}

		preview := strings.Join(lines[:4], "\n")
		return preview + "\n..."
	} else if bashResult.ExitCode != 0 {
		return fmt.Sprintf("Exit code: %d", bashResult.ExitCode)
	}
	return "Command completed"
}

// FormatResultBody returns the command's primary output for the collapsed preview
// and the full-on-failure view. Unlike FormatPreview it never truncates, and on a
// non-zero exit it surfaces the exit code and error so failures are fully visible.
func (t *BashTool) FormatResultBody(result *domain.ToolExecutionResult) string {
	if result == nil {
		return ""
	}

	bashResult, ok := result.Data.(*domain.BashToolResult)
	if !ok {
		return ""
	}

	output := strings.TrimRight(bashResult.Output, "\n")
	if bashResult.ExitCode == 0 {
		return output
	}

	header := fmt.Sprintf("exit %d", bashResult.ExitCode)
	if bashResult.Error != "" {
		header += ": " + bashResult.Error
	}
	if output == "" {
		return header
	}
	return header + "\n" + output
}

// FormatForUI formats the result for UI display
func (t *BashTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", toolCall)

	previewLines := strings.Split(preview, "\n")
	for i, line := range previewLines {
		if i == 0 {
			fmt.Fprintf(&output, "└─ %s %s", statusIcon, line)
		} else {
			fmt.Fprintf(&output, "\n     %s", line)
		}
	}

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *BashTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var dataContent string
	if result.Data != nil {
		dataContent = t.formatBashData(result.Data)
	}
	return t.formatter.FormatExpanded(result, dataContent)
}

// formatBashData formats bash-specific data
func (t *BashTool) formatBashData(data any) string {
	bashResult, ok := data.(*domain.BashToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	fmt.Fprintf(&output, "Exit Code: %d\n", bashResult.ExitCode)
	if bashResult.Error != "" {
		fmt.Fprintf(&output, "Error: %s\n", bashResult.Error)
	}
	if bashResult.Output != "" {
		fmt.Fprintf(&output, "Output:\n%s\n", bashResult.Output)
	}
	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *BashTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *BashTool) ShouldAlwaysExpand() bool {
	return false
}
