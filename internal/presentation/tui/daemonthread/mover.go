package daemonthread

import (
	"context"
	"encoding/json"
	"errors"

	uuid "github.com/google/uuid"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// transferFrame asks the daemon to move the followed session.
type transferFrame struct {
	Type       string   `json:"type"`
	TransferID string   `json:"transfer_id"`
	ProjectDir string   `json:"project_dir,omitempty"`
	ID         string   `json:"id,omitempty"`
	Target     string   `json:"target,omitempty"`
	Files      []string `json:"files,omitempty"`
}

// ListEnvironments asks the daemon for every environment it knows, the local
// one first, with whether each is reachable.
func (t *Thread) ListEnvironments(ctx context.Context) ([]agentdomain.EnvironmentStatus, error) {
	data, err := t.Request(ctx, map[string]string{"type": "list_environments"}, "environments")
	if err != nil {
		return nil, err
	}
	var listed struct {
		Environments []agentdomain.EnvironmentStatus `json:"environments"`
	}
	if err := json.Unmarshal(data, &listed); err != nil {
		return nil, err
	}
	return listed.Environments, nil
}

// Transfer starts moving the followed session to target. Progress arrives as
// TransferProgressEvent, and the move as a SessionPlacementChangedEvent.
func (t *Thread) Transfer(target string, files []string) error {
	t.mu.Lock()
	switch {
	case t.conn == nil:
		t.mu.Unlock()
		return ErrOffline
	case t.transfer != "":
		t.mu.Unlock()
		return errors.New("a transfer of this session is already running")
	case t.placement.Remote() && target != t.placement.Local:
		where := t.placement.Environment
		t.mu.Unlock()
		return errors.New("the session runs on " + where + " and moves only back to " + t.placement.Local + " from there")
	}
	frame := transferFrame{Type: "transfer_session", TransferID: uuid.NewString(), ProjectDir: t.opts.ProjectDir, ID: t.following, Target: target, Files: files}
	t.transfer = frame.TransferID
	t.placement.Transferring = target
	placement := t.placement
	t.mu.Unlock()
	if err := t.Send(frame); err != nil {
		t.endTransfer()
		return err
	}
	t.post(tui.SessionPlacementChangedEvent{Placement: placement})
	return nil
}

// CancelTransfer stops the running transfer, which leaves the session here.
func (t *Thread) CancelTransfer() error {
	t.mu.Lock()
	id := t.transfer
	t.mu.Unlock()
	if id == "" {
		return nil
	}
	return t.Send(transferFrame{Type: "transfer_cancel", TransferID: id})
}

func (t *Thread) endTransfer() tui.SessionPlacement {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.transfer = ""
	t.placement.Transferring = ""
	return t.placement
}

// onProgress reports a stage of the running transfer. A failed transfer
// leaves the session where it was.
func (t *Thread) onProgress(frame []byte) {
	var progress agentdomain.TransferProgress
	if json.Unmarshal(frame, &progress) != nil {
		return
	}
	t.mu.Lock()
	ours := progress.TransferID != "" && progress.TransferID == t.transfer
	t.mu.Unlock()
	if !ours {
		return
	}
	t.post(tui.TransferProgressEvent{Progress: progress})
	if progress.Stage == agentdomain.TransferFailed {
		t.post(tui.SessionPlacementChangedEvent{Placement: t.endTransfer()})
	}
}

// onMoved follows the session to where it moved. The daemon tells the
// requester and every follower, so the second notice is a duplicate.
func (t *Thread) onMoved(frame []byte) {
	var moved agentdomain.SessionMoved
	if json.Unmarshal(frame, &moved) != nil {
		return
	}
	t.mu.Lock()
	duplicate := moved.Session.ConversationID != t.following || t.transfer == "" && t.placement.Environment == moved.Environment
	if duplicate {
		t.mu.Unlock()
		return
	}
	t.transfer = ""
	t.placement.Transferring = ""
	t.placement.Environment, t.placement.Workspace = moved.Environment, moved.Workspace
	id := t.following
	t.mu.Unlock()
	var cancelled []json.RawMessage
	_ = json.Unmarshal(moved.CancelledInterrupts, &cancelled)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		_ = t.Follow(ctx, id, sessionsdomain.ThreadOptions{})
		t.post(tui.SessionPlacementChangedEvent{Placement: t.Placement(), Moved: true, CancelledInterrupts: len(cancelled)})
	}()
}
