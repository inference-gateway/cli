package infrastructure

import (
	"context"
	"errors"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	uuid "github.com/google/uuid"

	adk "github.com/inference-gateway/adk/types"

	agentheadless "github.com/inference-gateway/cli/internal/agent/headless"
)

func newTask(contextID string) *adk.Task {
	return &adk.Task{ID: uuid.NewString(), ContextID: &contextID, Status: adk.TaskStatus{State: adk.TaskStateSubmitted}}
}

func newMessage(parts ...adk.Part) *adk.Message {
	return &adk.Message{MessageID: uuid.NewString(), Role: adk.RoleUser, Parts: parts}
}

// statusText flattens the task's status message parts back to text.
func statusText(task *adk.Task) string {
	var parts []string
	for _, part := range task.Status.Message.Parts {
		if part.Text != nil {
			parts = append(parts, *part.Text)
		}
	}
	return strings.Join(parts, "")
}

func TestHandleTask(t *testing.T) {
	contextID := uuid.NewString()
	tests := []struct {
		name       string
		task       *adk.Task
		message    *adk.Message
		result     agentheadless.Result
		runErr     error
		cancel     bool
		wantState  adk.TaskState
		wantPrompt string
		wantStatus string
		wantRun    bool
	}{
		{
			name:       "text becomes the prompt and the final answer completes the task",
			task:       newTask(contextID),
			message:    newMessage(adk.CreateTextPart("ship it")),
			result:     agentheadless.Result{FinalAssistant: "done"},
			wantRun:    true,
			wantPrompt: "ship it",
			wantState:  adk.TaskStateCompleted,
			wantStatus: "done",
		},
		{
			name:       "multi text parts join into one prompt",
			task:       newTask(contextID),
			message:    newMessage(adk.CreateTextPart("part one"), adk.CreateTextPart("part two")),
			result:     agentheadless.Result{FinalAssistant: "done"},
			wantRun:    true,
			wantPrompt: "part one\n\npart two",
			wantState:  adk.TaskStateCompleted,
			wantStatus: "done",
		},
		{
			name:       "a bang task stays text and is never executed directly",
			task:       newTask(contextID),
			message:    newMessage(adk.CreateTextPart("!rm -rf /")),
			result:     agentheadless.Result{FinalAssistant: "as text"},
			wantRun:    true,
			wantPrompt: "!rm -rf /",
			wantState:  adk.TaskStateCompleted,
			wantStatus: "as text",
		},
		{
			name:       "a slash task stays text and is never run as a shortcut",
			task:       newTask(contextID),
			message:    newMessage(adk.CreateTextPart("/clear")),
			result:     agentheadless.Result{FinalAssistant: "as text"},
			wantRun:    true,
			wantPrompt: "/clear",
			wantState:  adk.TaskStateCompleted,
			wantStatus: "as text",
		},
		{
			name:       "a data part fails without a run",
			task:       newTask(contextID),
			message:    newMessage(adk.CreateDataPart(map[string]any{"blob": true})),
			wantState:  adk.TaskStateFailed,
			wantStatus: "only text parts are supported; send the task as a text/plain message",
		},
		{
			name:       "a message without text fails without a run",
			task:       newTask(contextID),
			message:    newMessage(),
			wantState:  adk.TaskStateFailed,
			wantStatus: "the task message carries no text",
		},
		{
			name:       "a non-UUID context fails without a run",
			task:       newTask("not-a-uuid"),
			message:    newMessage(adk.CreateTextPart("hi")),
			wantState:  adk.TaskStateFailed,
			wantStatus: "the task's contextId must be a UUID",
		},
		{
			name:       "a failed run fails the task with the stderr tail",
			task:       newTask(contextID),
			message:    newMessage(adk.CreateTextPart("go")),
			result:     agentheadless.Result{Stderr: "the gateway did not answer"},
			runErr:     errors.New("exit status 1"),
			wantRun:    true,
			wantPrompt: "go",
			wantState:  adk.TaskStateFailed,
			wantStatus: "exit status 1; the run's stderr ended with: the gateway did not answer",
		},
		{
			name:       "a run with no final answer fails the task",
			task:       newTask(contextID),
			message:    newMessage(adk.CreateTextPart("go")),
			result:     agentheadless.Result{},
			wantRun:    true,
			wantPrompt: "go",
			wantState:  adk.TaskStateFailed,
			wantStatus: "the run produced no final assistant message",
		},
		{
			name:       "a canceled run keeps the canceled state the manager recorded",
			task:       newTask(contextID),
			message:    newMessage(adk.CreateTextPart("go")),
			cancel:     true,
			wantRun:    true,
			wantPrompt: "go",
			wantState:  adk.TaskStateCanceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotOpts agentheadless.Options
			ran := false
			handler := &HeadlessTaskHandler{
				Mode: "plan",
				Runner: func(_ context.Context, opts agentheadless.Options) (agentheadless.Result, error) {
					ran = true
					gotOpts = opts
					if tt.runErr != nil {
						return tt.result, tt.runErr
					}
					return tt.result, nil
				},
			}

			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			out, err := handler.HandleTask(ctx, tt.task, tt.message)
			require.NoError(t, err)
			require.Equal(t, string(tt.wantState), string(out.Status.State), "task state")
			require.Equal(t, tt.wantRun, ran, "the headless run started")
			if tt.wantRun {
				require.Equal(t, tt.wantPrompt, gotOpts.Prompt, "prompt")
				require.Equal(t, "plan", gotOpts.Mode, "agent mode")
				require.True(t, gotOpts.PromptOnly, "prompt-only")
				require.Equal(t, "a2a-"+tt.task.GetContextID(), gotOpts.SessionID, "session id")
			} else {
				require.Zero(t, gotOpts.Prompt, "no run options set")
			}
			if tt.wantStatus != "" {
				require.Equal(t, tt.wantStatus, statusText(out), "status message")
			}
		})
	}
}
