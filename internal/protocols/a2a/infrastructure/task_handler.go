package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	adkserver "github.com/inference-gateway/adk/server"
	adk "github.com/inference-gateway/adk/types"

	agentheadless "github.com/inference-gateway/cli/internal/agent/headless"
)

// stderrTail is how much of a failed run's stderr the failed task's status message keeps.
const stderrTail = 2048

// HeadlessTaskHandler runs every task it gets as one `infer headless
// --prompt-only` subprocess. The task's text parts are the prompt, the run's
// final assistant message is the completed task's status message, and a
// tasks/cancel reaches the run as the cancellation of its context. Timeout
// bounds one run so a hung one cannot hold the one-at-a-time queue forever.
type HeadlessTaskHandler struct {
	Runner  func(ctx context.Context, opts agentheadless.Options) (agentheadless.Result, error)
	Mode    string
	Timeout time.Duration
}

// NewHeadlessTaskHandler builds the handler for tasks served in the given
// headless agent mode, each run bounded by timeout (zero means unbounded).
func NewHeadlessTaskHandler(mode string, timeout time.Duration) *HeadlessTaskHandler {
	return &HeadlessTaskHandler{Runner: agentheadless.Run, Mode: mode, Timeout: timeout}
}

// SetAgent is the TaskHandler contract; a served task never calls the ADK's OpenAI-compatible agent.
func (*HeadlessTaskHandler) SetAgent(adkserver.OpenAICompatibleAgent) {}

// GetAgent is the TaskHandler contract; see SetAgent.
func (*HeadlessTaskHandler) GetAgent() adkserver.OpenAICompatibleAgent { return nil }

// HandleTask validates the task and runs it. Validation and run failures land
// in the task's status rather than in the error return, so the state set here
// is what the client polls. A task whose context was canceled keeps the
// canceled state the task manager already recorded.
func (h *HeadlessTaskHandler) HandleTask(ctx context.Context, task *adk.Task, message *adk.Message) (*adk.Task, error) {
	prompt, err := taskPrompt(message)
	if err != nil {
		return respondTask(task, adk.TaskStateFailed, err.Error()), nil
	}
	sessionID, err := sessionForContextID(task.GetContextID())
	if err != nil {
		return respondTask(task, adk.TaskStateFailed, err.Error()), nil
	}

	runCtx, cancel := h.boundedContext(ctx)
	defer cancel()
	res, runErr := h.Runner(runCtx, agentheadless.Options{
		Prompt:     prompt,
		Mode:       h.Mode,
		SessionID:  sessionID,
		PromptOnly: true,
	})
	if ctx.Err() != nil {
		task.Status.State = adk.TaskStateCanceled
		return task, nil
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return respondTask(task, adk.TaskStateFailed, fmt.Sprintf("the run exceeded the %s task timeout", h.Timeout)), nil
	}
	if runErr != nil {
		return respondTask(task, adk.TaskStateFailed, runFailure(runErr, res.Stderr)), nil
	}
	if res.FinalAssistant == "" {
		return respondTask(task, adk.TaskStateFailed, "the run produced no final assistant message"), nil
	}
	return respondTask(task, adk.TaskStateCompleted, res.FinalAssistant), nil
}

// boundedContext derives the run context, with the handler's timeout when it has one.
func (h *HeadlessTaskHandler) boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if h.Timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, h.Timeout)
}

// taskPrompt joins the task message's text parts into the prompt. A message
// with file or data parts is refused: served tasks are always text.
func taskPrompt(message *adk.Message) (string, error) {
	var texts []string
	for _, part := range message.Parts {
		if part.Text == nil {
			return "", errors.New("only text parts are supported; send the task as a text/plain message")
		}
		texts = append(texts, *part.Text)
	}
	if len(texts) == 0 {
		return "", errors.New("the task message carries no text")
	}
	return strings.Join(texts, "\n\n"), nil
}

// contextIDPattern is the charset a contextId may use: it becomes a storage
// key on the daemon side, so path and shell metacharacters are refused.
var contextIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// sessionForContextID maps the caller-chosen contextId onto the session ID the
// headless run stores the conversation under.
func sessionForContextID(contextID string) (string, error) {
	if !contextIDPattern.MatchString(contextID) {
		return "", errors.New("the task's contextId must be 1 to 128 letters, digits, dashes or underscores")
	}
	return "a2a-" + contextID, nil
}

// runFailure describes a failed run: the run error plus the end of stderr when it has one.
func runFailure(err error, stderr string) string {
	text := err.Error()
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return text
	}
	if len(stderr) > stderrTail {
		stderr = stderr[len(stderr)-stderrTail:]
	}
	return fmt.Sprintf("%s; the run's stderr ended with: %s", text, stderr)
}

// respondTask sets the task's state and status message text, the shape A2A
// clients read the result from.
func respondTask(task *adk.Task, state adk.TaskState, text string) *adk.Task {
	task.Status.State = state
	task.Status.Message = &adk.Message{
		MessageID: fmt.Sprintf("%s-%s", state, task.ID),
		Role:      adk.RoleAgent,
		TaskID:    &task.ID,
		ContextID: task.ContextID,
		Parts:     []adk.Part{adk.CreateTextPart(text)},
	}
	return task
}
