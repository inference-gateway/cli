package headless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	directexec "github.com/inference-gateway/cli/internal/presentation/tui/directexec"
	tools "github.com/inference-gateway/cli/internal/tools"
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
		command, background := bashCommand(task)
		if command == "" {
			return fn, true, errors.New("no bash command provided; use !<command>")
		}
		args := map[string]any{"command": command}
		if background {
			args["detached"] = true
		}
		raw, _ := json.Marshal(args)
		return sdk.ChatCompletionMessageToolCallFunction{Name: tools.ToolBash, Arguments: string(raw)}, true, nil
	}
	return fn, false, nil
}

// bashCommand is the command of a `!cmd` task, without the `&` that asks for a
// background shell, and whether it had one.
func bashCommand(task string) (string, bool) {
	return directexec.SplitBackground(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(task), "!")))
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
		emitPreRunError(os.Stdout, opts.Format, err)
		return true, err
	}
	err = runDirectCall(ctx, opts.Format, toolService, repo, sessionID, model, cfg, opts.Task, fn)
	if opts.ResultFile != "" {
		writeResultFile(opts.ResultFile, repo, sessionID, err)
	}
	return true, err
}

// runDirectCall executes fn through the run's own tool service - gateway, A2A
// agents and MCP servers already wired - and renders it in --format while it
// runs, the way an agent turn would. It records what the chat TUI's direct-exec
// path records. No model is called.
func runDirectCall(ctx context.Context, format string, toolService agentdomain.ToolService, repo convdomain.ConversationRepository, sessionID, model string, cfg *config.Config, task string, fn sdk.ChatCompletionMessageToolCallFunction) error {
	if !toolService.IsToolEnabled(fn.Name) {
		return fmt.Errorf("tool %s is not enabled", fn.Name)
	}
	run := &directRun{
		tools: toolService, repo: repo, sessionID: sessionID, task: task,
		call:   sdk.ChatCompletionMessageToolCall{ID: directCallID(task), Type: "function", Function: fn},
		events: newDirectEvents(),
	}
	run.record(convdomain.ConversationEntry{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(strings.TrimSpace(task))}, Time: time.Now()})
	go run.execute(ctx)
	return renderStream(format, run.events.out, nil, sessionID, model, cfg, repo, nil, nil, nil)
}

// directCallID names a direct task's tool call, which says how the conversation
// treats it: a `!cmd` pair is dropped from the model's history.
func directCallID(task string) string {
	if isBashTask(task) {
		return convdomain.NewUserBashCallID()
	}
	return convdomain.NewUserToolCallID()
}

// directEvents is one direct run's event stream. A tool can call back after it
// returned, as a detached command's output reader can, so every send goes
// through send, which drops what arrives once the stream closed. The renderer
// reads until close, so a blocked send always finishes and close gets its lock.
type directEvents struct {
	out chan agentdomain.ChatEvent

	mu     sync.RWMutex
	closed bool
}

func newDirectEvents() *directEvents {
	return &directEvents{out: make(chan agentdomain.ChatEvent)}
}

func (e *directEvents) send(event agentdomain.ChatEvent) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.closed {
		e.out <- event
	}
}

func (e *directEvents) close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	close(e.out)
}

// directRun is one `!cmd` or `!!Tool(...)` task on its way through the tool
// service.
type directRun struct {
	tools     agentdomain.ToolService
	repo      convdomain.ConversationRepository
	sessionID string
	task      string
	call      sdk.ChatCompletionMessageToolCall
	events    *directEvents
}

// execute runs the tool, reports it the way an agent turn reports a call and
// closes the events. A cancelled ctx ends the run as cancelled.
func (r *directRun) execute(ctx context.Context) {
	defer r.events.close()
	r.events.send(agentdomain.ChatCompleteEvent{RequestID: r.sessionID, Timestamp: time.Now(), ToolCalls: []sdk.ChatCompletionMessageToolCall{r.call}})
	r.progress("running", "Executing: "+r.label())

	result := r.runTool(ctx)
	r.recordResult(result)

	completed := agentdomain.ToolExecutionCompletedEvent{SessionID: r.sessionID, RequestID: r.sessionID, Timestamp: time.Now(), TotalExecuted: 1, Results: []*agentdomain.ToolExecutionResult{result}}
	if result.Success {
		completed.SuccessCount = 1
		r.progress("completed", "Completed successfully")
	} else {
		completed.FailureCount = 1
		r.progress("failed", "Execution failed: "+result.Error)
	}
	r.events.send(completed)
	if ctx.Err() != nil {
		r.events.send(agentdomain.ChatCompleteEvent{RequestID: r.sessionID, Timestamp: time.Now(), Cancelled: true})
	}
}

// label names what runs in the status line: a `!cmd`'s command or a tool.
func (r *directRun) label() string {
	if !isBashTask(r.task) {
		return r.call.Function.Name
	}
	command, _ := bashCommand(r.task)
	return command
}

// runTool executes the call as the user's own, streaming what it reports, and
// always returns a result to record.
func (r *directRun) runTool(ctx context.Context) *agentdomain.ToolExecutionResult {
	ctx = agentdomain.WithDirectExecution(agentdomain.WithToolApproved(ctx))
	ctx = agentdomain.WithToolProgressCallback(ctx, func(message string) { r.progress("running", message) })
	ctx = agentdomain.WithBashOutputCallback(ctx, func(output string) {
		r.events.send(agentdomain.BashOutputChunkEvent{
			BaseChatEvent: agentdomain.BaseChatEvent{RequestID: r.sessionID, Timestamp: time.Now()},
			ToolCallID:    r.call.ID, Output: output,
		})
	})
	result, err := r.tools.ExecuteToolDirect(ctx, r.call.Function)
	if err != nil || result == nil {
		result = &agentdomain.ToolExecutionResult{ToolName: r.call.Function.Name, Success: false}
		if err != nil {
			result.Error = err.Error()
		}
	}
	result.ToolCallID = r.call.ID
	if result.Arguments == nil {
		_ = json.Unmarshal([]byte(r.call.Function.Arguments), &result.Arguments)
	}
	return result
}

// recordResult stores the call and its result, and for a `!cmd` the output the
// model reads in place of them.
func (r *directRun) recordResult(result *agentdomain.ToolExecutionResult) {
	now := time.Now()
	formatted := r.repo.FormatToolResultForLLM(result)
	assistantEntry, toolEntry := convdomain.NewToolCallEntries(r.call, result, formatted, now)
	r.record(assistantEntry)
	r.record(toolEntry)
	if isBashTask(r.task) {
		command, _ := bashCommand(r.task)
		r.record(convdomain.NewUserBashOutputEntry(command, formatted, now))
	}
}

func (r *directRun) record(entry convdomain.ConversationEntry) {
	if err := r.repo.AddMessage(entry); err != nil {
		logger.Warn("failed to persist direct tool call", "error", err, "session_id", r.sessionID)
	}
}

func (r *directRun) progress(status, message string) {
	r.events.send(agentdomain.ToolExecutionProgressEvent{
		BaseChatEvent: agentdomain.BaseChatEvent{RequestID: r.sessionID, Timestamp: time.Now()},
		ToolCallID:    r.call.ID, ToolName: r.call.Function.Name, Arguments: r.call.Function.Arguments,
		Status: status, Message: message,
	})
}
