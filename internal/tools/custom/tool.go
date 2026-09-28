package custom

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// ponytail: 1 MiB tail per stream, the model sees at most tools.max_result_bytes
// anyway. Raise it if that cap is ever configured past it.
const maxCapturedBytes = 1 << 20

// pipeGrace bounds how long a call waits for the pipes to close after the
// process exits or is killed, in case a child it spawned still holds them.
const pipeGrace = time.Second

// Tool runs a custom tool's command: the call's arguments go to stdin as one
// JSON object and stdout is the result.
type Tool struct {
	manifest  agentdomain.ToolManifest
	command   []string
	timeout   time.Duration
	formatter agentinfra.BaseFormatter
}

func newTool(m manifest, dir string) *Tool {
	return &Tool{
		manifest:  m.ToolManifest,
		command:   resolveCommand(m.Command, dir),
		timeout:   cmp.Or(time.Duration(m.Timeout)*time.Second, defaultTimeout),
		formatter: agentinfra.NewBaseFormatter(m.Name),
	}
}

// Manifest returns the manifest as declared, so the registry answers the
// tool's modes and approval like a built-in's.
func (t *Tool) Manifest() agentdomain.ToolManifest {
	return t.manifest
}

// Definition returns the manifest's description and JSON Schema.
func (t *Tool) Definition() sdk.ChatCompletionTool {
	return t.manifest.Definition()
}

// Validate checks the arguments against the manifest's parameters.
func (t *Tool) Validate(args map[string]any) error {
	return agentdomain.ValidateArguments(t.manifest.Parameters, args)
}

// IsEnabled is always true: a disabled manifest never loads.
func (t *Tool) IsEnabled() bool {
	return true
}

// Execute runs the command and reports a failed call in the result, not as
// an error, so the model reads why it failed.
func (t *Tool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()
	output, err := t.run(ctx, args)
	result := &agentdomain.ToolExecutionResult{
		ToolName:  t.manifest.Name,
		Arguments: args,
		Success:   err == nil,
		Duration:  time.Since(start),
		Data:      output,
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result, nil
}

func (t *Tool) run(ctx context.Context, args map[string]any) (string, error) {
	if err := t.Validate(args); err != nil {
		return "", err
	}
	input, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("encoding arguments: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.command[0], t.command[1:]...)
	cmd.Stdin = bytes.NewReader(input)
	stdout := utils.NewOutputRingBuffer(maxCapturedBytes)
	stderr := utils.NewOutputRingBuffer(maxCapturedBytes)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = pipeGrace
	killProcessGroupOnCancel(cmd)

	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil || (errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState.ExitCode() == 0):
		return stdout.String(), nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() > 0 && ctx.Err() != nil:
		return "", failure(fmt.Errorf("%s (a child kept the output open)", cmd.ProcessState), stderr.String(), stdout.String())
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "", fmt.Errorf("timed out after %s", t.timeout)
	case ctx.Err() != nil:
		return "", ctx.Err()
	case errors.Is(err, exec.ErrWaitDelay):
		return "", failure(fmt.Errorf("%s (a child kept the output open)", cmd.ProcessState), stderr.String(), stdout.String())
	default:
		return "", failure(err, stderr.String(), stdout.String())
	}
}

// failure explains a failed run with stderr, or stdout when stderr is empty.
func failure(err error, stderr, stdout string) error {
	if output := cmp.Or(strings.TrimSpace(stderr), strings.TrimSpace(stdout)); output != "" {
		return fmt.Errorf("%w: %s", err, output)
	}
	return err
}

// FormatResult formats the result for the UI, the model or a preview.
func (t *Tool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return t.formatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.formatForUI(result)
	}
}

// FormatPreview returns the first lines of the output, or the error.
func (t *Tool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	if !result.Success {
		return result.Error
	}
	output, _ := result.Data.(string)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > 3 {
		return strings.Join(lines[:3], "\n") + "\n..."
	}
	return strings.Join(lines, "\n")
}

func (t *Tool) formatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	lines := strings.Split(t.FormatPreview(result), "\n")
	var output strings.Builder
	fmt.Fprintf(&output, "%s\n└─ %s %s", t.formatter.FormatToolCall(result.Arguments, false), t.formatter.FormatStatusIcon(result.Success), lines[0])
	for _, line := range lines[1:] {
		fmt.Fprintf(&output, "\n     %s", line)
	}
	return output.String()
}

func (t *Tool) formatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	output, _ := result.Data.(string)
	return t.formatter.FormatExpanded(result, output)
}

// ShouldCollapseArg keeps every argument visible.
func (t *Tool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand leaves the result collapsed by default.
func (t *Tool) ShouldAlwaysExpand() bool {
	return false
}

// projectTool is a custom tool a project ships in .infer/tools or
// .agents/tools. The repository supplies it, so every call needs approval
// whatever its manifest says, unless the agent runs in auto mode.
type projectTool struct {
	*Tool
}

func newProjectTool(tool *Tool) projectTool {
	requireApproval := true
	tool.manifest.RequireApproval = &requireApproval
	return projectTool{tool}
}

// RequiresApproval asks for approval in every mode the tool runs in but auto.
// The approval policy consults it ahead of the mode, so even readonly mode,
// which runs its tools unapproved, asks. Outside its modes the call is refused
// without asking.
func (t projectTool) RequiresApproval(_ map[string]any, mode agentdomain.AgentMode) bool {
	return mode != agentdomain.AgentModeAutoAccept && t.manifest.AvailableIn(mode)
}

var _ agentdomain.Tool = (*Tool)(nil)
var _ agentdomain.ManifestTool = (*Tool)(nil)
var _ agentdomain.CallApprover = projectTool{}
