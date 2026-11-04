package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sessions "github.com/inference-gateway/cli/internal/sessions"
)

// screenFrame asks the worker of a session for its latest screen frame through
// the registry, in the daemon's own project dir where the web terminal's chats
// run. A session that teleported answers through its relay worker.
func screenFrame(registry *sessions.Registry) func(ctx context.Context, sessionID string) (*agentdomain.Frame, error) {
	projectDir, _ := os.Getwd()
	return func(ctx context.Context, sessionID string) (*agentdomain.Frame, error) {
		request, _ := json.Marshal(map[string]string{"type": "read_frame", "project_dir": projectDir, "id": sessionID})
		reply, err := registry.Request(ctx, request, "frame")
		if err != nil {
			return nil, err
		}
		var frame struct {
			Frame *agentdomain.Frame `json:"frame"`
			Error string             `json:"error"`
		}
		if err := json.Unmarshal(reply, &frame); err != nil {
			return nil, err
		}
		if frame.Frame == nil {
			return nil, errors.New(frame.Error)
		}
		return frame.Frame, nil
	}
}
