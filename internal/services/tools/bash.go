package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	utils "github.com/inference-gateway/cli/internal/utils"
	sdk "github.com/inference-gateway/sdk"
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

// Definition returns the tool definition for the LLM
func (t *BashTool) Definition() sdk.ChatCompletionTool {
	var allowedCommands []string

	for _, cmd := range t.config.Tools.Bash.Whitelist.Commands {
		allowedCommands = append(allowedCommands, cmd)
		switch cmd {
		case "ls":
			allowedCommands = append(allowedCommands, "ls -l", "ls -la", "ls -a")
		case "git":
		case "grep":
			allowedCommands = append(allowedCommands, "grep -r", "grep -n", "grep -i")
		}
	}

	patternExamples := []string{
		"git status",
		"git log --oneline -n 5",
		"git log --oneline -n 10",
		"docker ps",
		"kubectl get pods",
	}
	allowedCommands = append(allowedCommands, patternExamples...)

	commandDescription := "The bash command to execute. Must be from the whitelist of allowed commands."
	if len(allowedCommands) > 0 {
		commandDescription += " Available commands include: " + strings.Join(allowedCommands, ", ")
	}

	description := "Execute whitelisted bash commands securely. Only pre-approved commands from the whitelist can be executed."
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
						"enum":        allowedCommands,
					},
					"format": map[string]any{
						"type":        "string",
						"description": "Output format (text or json)",
						"enum":        []string{"text", "json"},
						"default":     "text",
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
	}

	return result, nil
}

// Validate checks if the bash tool arguments are valid
func (t *BashTool) Validate(args map[string]any) error {
	command, ok := args["command"].(string)
	if !ok {
		return fmt.Errorf("command parameter is required and must be a string")
	}

	if !t.isCommandAllowed(command) {
		return fmt.Errorf("command not whitelisted: %s", command)
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

	if !wasApproved && !t.isCommandAllowed(command) {
		result.ExitCode = -1
		result.Duration = time.Since(start).String()
		result.Error = fmt.Sprintf("command not whitelisted: %s", command)
		return result, fmt.Errorf("command not whitelisted: %s", command)
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

			time.Sleep(100 * time.Millisecond)

			shellID, err := t.backgroundShellService.DetachToBackground(ctx, cmd, result.Command, outputBuffer)
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
			logger.Debug("bash: command completed before detach signal")
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

		detachedMux.Lock()
		isDetached := *detached
		detachedMux.Unlock()

		if !isDetached {
			callback(line)
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Debug("bash: scanner error", "error", err)
	}
}

// isCommandAllowed checks if a command is whitelisted
func (t *BashTool) isCommandAllowed(command string) bool {
	command = strings.TrimSpace(command)

	for _, allowed := range t.config.Tools.Bash.Whitelist.Commands {
		if command == allowed || strings.HasPrefix(command, allowed+" ") {
			return true
		}
	}

	for _, pattern := range t.config.Tools.Bash.Whitelist.Patterns {
		matched, err := regexp.MatchString(pattern, command)
		if err == nil && matched {
			return true
		}
	}

	return false
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

// FormatForUI formats the result for UI display
func (t *BashTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))

	previewLines := strings.Split(preview, "\n")
	for i, line := range previewLines {
		if i == 0 {
			output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, line))
		} else {
			output.WriteString(fmt.Sprintf("\n     %s", line))
		}
	}

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *BashTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatBashData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatBashData formats bash-specific data
func (t *BashTool) formatBashData(data any) string {
	bashResult, ok := data.(*domain.BashToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Exit Code: %d\n", bashResult.ExitCode))
	if bashResult.Error != "" {
		output.WriteString(fmt.Sprintf("Error: %s\n", bashResult.Error))
	}
	if bashResult.Output != "" {
		output.WriteString(fmt.Sprintf("Output:\n%s\n", bashResult.Output))
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
