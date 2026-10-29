package daemonthread

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	sdk "github.com/inference-gateway/sdk"

	conversation "github.com/inference-gateway/cli/internal/conversation"
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

// SelectModel switches the model the thread's next turns use. It returns at
// once, since it runs on the TUI's update loop, and the worker learns it
// asynchronously.
func (m *Models) SelectModel(modelID string) error {
	if err := m.ValidateModel(modelID); err != nil {
		return err
	}
	m.mu.Lock()
	m.current = modelID
	m.mu.Unlock()
	go func() {
		_ = m.thread.Send(map[string]string{"type": "select_model", "model": modelID, "project_dir": m.thread.opts.ProjectDir})
	}()
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
