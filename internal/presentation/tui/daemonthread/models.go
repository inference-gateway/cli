package daemonthread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"

	conversation "github.com/inference-gateway/cli/internal/conversation"
	llm "github.com/inference-gateway/cli/internal/platform/llm"
)

// Models is the chat TUI's model service in daemon mode: the models the
// thread's worker can use, and the one its turns run on.
type Models struct {
	thread *Thread

	mu      sync.Mutex
	models  []string
	current string
}

// NewModels builds the daemon-mode model service over thread.
func NewModels(thread *Thread) *Models {
	return &Models{thread: thread}
}

// ListModels asks the worker for its models, adopting its current one when
// the TUI has not picked one yet. Their context windows, prices and
// modalities, which the worker fetched from its gateway, are recorded here
// too, so the model picker shows them.
func (m *Models) ListModels(ctx context.Context) ([]string, error) {
	data, err := m.thread.Request(ctx, map[string]string{"type": "list_models", "project_dir": m.thread.opts.ProjectDir}, "models")
	if err != nil {
		return nil, err
	}
	var listed struct {
		Models  []string    `json:"models"`
		Current string      `json:"current"`
		Catalog []sdk.Model `json:"catalog"`
	}
	if err := json.Unmarshal(data, &listed); err != nil {
		return nil, err
	}
	if len(listed.Catalog) > 0 {
		conversation.RecordModels(listed.Catalog)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models = listed.Models
	if m.current == "" {
		m.current = listed.Current
	}
	return slices.Clone(listed.Models), nil
}

// SelectModel switches the model the thread's next turns use. The worker
// learns it when the next turn starts, so a pick made offline is not lost.
func (m *Models) SelectModel(modelID string) error {
	if err := m.ValidateModel(modelID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = modelID
	return nil
}

func (m *Models) GetCurrentModel() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// IsModelAvailable reports whether the worker lists modelID. Before the
// first listing every model counts as available.
func (m *Models) IsModelAvailable(modelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.models) == 0 || slices.Contains(m.models, modelID)
}

func (m *Models) ValidateModel(modelID string) error {
	if modelID == "" || !m.IsModelAvailable(modelID) {
		return fmt.Errorf("model %q is not available on the daemon worker", modelID)
	}
	return nil
}

// Completer is the chat TUI's model side call in daemon mode, such as a
// shortcut's snippet. The thread's worker answers it with the session's
// gateway and model, on whichever workspace the session runs.
type Completer struct {
	thread *Thread
}

// NewCompleter builds the daemon-mode side call over thread.
func NewCompleter(thread *Thread) *Completer {
	return &Completer{thread: thread}
}

// completionRequest asks the worker for one side call, answered by the
// completion_result carrying the same id.
type completionRequest struct {
	Type       string `json:"type"`
	ProjectDir string `json:"project_dir"`
	ID         string `json:"id"`
	Model      string `json:"model,omitempty"`
	System     string `json:"system,omitempty"`
	Prompt     string `json:"prompt"`
	MaxTokens  int    `json:"max_tokens,omitempty"`
}

// Complete asks the worker and waits at most requestTimeout, so a daemon too
// old to know the frame fails the call instead of leaving it waiting.
func (c *Completer) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	frame := completionRequest{
		Type: "completion_request", ProjectDir: c.thread.opts.ProjectDir, ID: uuid.NewString(),
		Model: req.Model, System: req.System, Prompt: req.Prompt, MaxTokens: req.MaxTokens,
	}
	data, err := c.thread.request(ctx, frame, "completion_result", frame.ID)
	if err != nil {
		return llm.Response{}, err
	}
	var result struct {
		Content   string `json:"content"`
		Reasoning string `json:"reasoning"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return llm.Response{}, err
	}
	if result.Error != "" {
		return llm.Response{}, errors.New(result.Error)
	}
	return llm.Response{Content: result.Content, Reasoning: result.Reasoning}, nil
}
