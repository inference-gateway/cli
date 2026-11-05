package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	web "github.com/inference-gateway/cli/internal/presentation/web"
	sessions "github.com/inference-gateway/cli/internal/sessions"
)

// screenResultReply is the reply type both screen relays below wait for.
const screenResultReply = "screen_result"

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

// screenControl relays the Screen view's take over and hand back to the
// session's worker, through the same registry the frames travel, so a
// teleported session answers through its relay worker unchanged.
func screenControl(registry *sessions.Registry) func(ctx context.Context, sessionID, action string) (*web.ScreenOutcome, error) {
	projectDir, _ := os.Getwd()
	return func(ctx context.Context, sessionID, action string) (*web.ScreenOutcome, error) {
		request, _ := json.Marshal(map[string]string{
			"type": "screen_control", "project_dir": projectDir, "id": sessionID, "action": action,
		})
		return screenOutcome(registry.Request(ctx, request, screenResultReply))
	}
}

// screenInput relays one person input on a held screen to the session's
// worker, in the frame space the Screen view's image is served in.
func screenInput(registry *sessions.Registry) func(ctx context.Context, sessionID string, input json.RawMessage) (*web.ScreenOutcome, error) {
	projectDir, _ := os.Getwd()
	return func(ctx context.Context, sessionID string, input json.RawMessage) (*web.ScreenOutcome, error) {
		request, err := json.Marshal(struct {
			Type       string          `json:"type"`
			ProjectDir string          `json:"project_dir"`
			ID         string          `json:"id"`
			Input      json.RawMessage `json:"input"`
		}{Type: "screen_input", ProjectDir: projectDir, ID: sessionID, Input: input})
		if err != nil {
			return nil, err
		}
		return screenOutcome(registry.Request(ctx, request, screenResultReply))
	}
}

// screenOutcome decodes the worker's screen_result into the web handler's
// outcome, or returns the error the relay raised before any reply.
func screenOutcome(reply []byte, requestErr error) (*web.ScreenOutcome, error) {
	if requestErr != nil {
		return nil, requestErr
	}
	var result struct {
		Held  bool   `json:"held"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(reply, &result); err != nil {
		return nil, errors.New("the session's screen reply is unreadable")
	}
	return &web.ScreenOutcome{Held: result.Held, Error: result.Error}, nil
}
