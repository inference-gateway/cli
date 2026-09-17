package headless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	render "github.com/inference-gateway/cli/internal/platform/render"
	directexec "github.com/inference-gateway/cli/internal/presentation/tui/directexec"
)

// directCall maps a `!!Tool(arg="v")` or `!cmd` task to the tool call the
// chat TUI's direct-exec path would run. direct is false for any other task.
func directCall(task string) (fn sdk.ChatCompletionMessageToolCallFunction, direct bool, err error) {
	task = strings.TrimSpace(task)
	switch {
	case strings.HasPrefix(task, "!!"):
		name, args, perr := directexec.ParseToolCall(strings.TrimSpace(strings.TrimPrefix(task, "!!")))
		if perr != nil {
			return fn, true, fmt.Errorf("invalid tool call: %w (expected !!ToolName(arg=\"value\"))", perr)
		}
		raw, _ := json.Marshal(args)
		return sdk.ChatCompletionMessageToolCallFunction{Name: name, Arguments: string(raw)}, true, nil
	case strings.HasPrefix(task, "!"):
		command := strings.TrimSpace(strings.TrimPrefix(task, "!"))
		if command == "" {
			return fn, true, errors.New("no bash command provided; use !<command>")
		}
		raw, _ := json.Marshal(map[string]string{"command": command})
		return sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: string(raw)}, true, nil
	}
	return fn, false, nil
}

// isBashTask reports whether task is a `!cmd` direct bash run, which never
// needs local agents. `!!Tool(...)` is excluded since it may call A2A tools.
func isBashTask(task string) bool {
	task = strings.TrimSpace(task)
	return strings.HasPrefix(task, "!") && !strings.HasPrefix(task, "!!")
}

// runDirect handles a `!!Tool(...)` or `!cmd` task end to end; direct is false
// when the task is an ordinary prompt. Everything it reports is already
// rendered in --format, a parse error included.
func runDirect(ctx context.Context, opts Options, toolService agentdomain.ToolService, repo convdomain.ConversationRepository, sessionID, model string, cfg *config.Config) (direct bool, err error) {
	fn, direct, err := directCall(opts.Task)
	if !direct {
		return false, nil
	}
	if err != nil {
		render.EmitPreRunError(os.Stdout, opts.Format, err)
		return true, err
	}
	err = runDirectCall(ctx, opts.Format, toolService, repo, sessionID, model, cfg, fn)
	if opts.ResultFile != "" {
		writeResultFile(opts.ResultFile, repo, sessionID, err)
	}
	return true, err
}

// runDirectCall executes fn through the run's own tool service - gateway, A2A
// agents and MCP servers already wired - records the assistant tool_call and
// tool result entries in the conversation, and streams both in --format the
// way an agent turn would. No model is called.
func runDirectCall(ctx context.Context, format string, toolService agentdomain.ToolService, repo convdomain.ConversationRepository, sessionID, model string, cfg *config.Config, fn sdk.ChatCompletionMessageToolCallFunction) error {
	if !toolService.IsToolEnabled(fn.Name) {
		return fmt.Errorf("tool %s is not enabled", fn.Name)
	}
	call := sdk.ChatCompletionMessageToolCall{ID: "call_" + uuid.New().String(), Type: "function", Function: fn}
	result, err := toolService.ExecuteToolDirect(agentdomain.WithToolApproved(ctx), fn)
	if err != nil || result == nil {
		result = &agentdomain.ToolExecutionResult{ToolName: fn.Name, Success: false}
		if err != nil {
			result.Error = err.Error()
		}
	}
	result.ToolCallID = call.ID
	if result.Arguments == nil {
		_ = json.Unmarshal([]byte(fn.Arguments), &result.Arguments)
	}

	now := time.Now()
	assistantEntry, toolEntry := convdomain.NewToolCallEntries(call, result, repo.FormatToolResultForLLM(result), now)
	for _, entry := range []convdomain.ConversationEntry{assistantEntry, toolEntry} {
		if err := repo.AddMessage(entry); err != nil {
			logger.Warn("failed to persist direct tool call", "error", err, "session_id", sessionID)
		}
	}

	completed := agentdomain.ToolExecutionCompletedEvent{SessionID: sessionID, RequestID: sessionID, Timestamp: now, TotalExecuted: 1, Results: []*agentdomain.ToolExecutionResult{result}}
	if result.Success {
		completed.SuccessCount = 1
	} else {
		completed.FailureCount = 1
	}
	events := make(chan agentdomain.ChatEvent, 2)
	events <- agentdomain.ChatCompleteEvent{RequestID: sessionID, Timestamp: now, ToolCalls: []sdk.ChatCompletionMessageToolCall{call}}
	events <- completed
	close(events)
	return renderStream(format, events, nil, nil, sessionID, model, cfg, repo, nil)
}
