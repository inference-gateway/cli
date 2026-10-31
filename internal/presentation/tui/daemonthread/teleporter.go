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

// teleportFrame asks the daemon to teleport the followed session.
type teleportFrame struct {
	Type       string   `json:"type"`
	TeleportID string   `json:"teleport_id"`
	ProjectDir string   `json:"project_dir,omitempty"`
	ID         string   `json:"id,omitempty"`
	Target     string   `json:"target,omitempty"`
	Files      []string `json:"files,omitempty"`
}

// ListWorkspaces asks the daemon for every workspace it knows, the local
// one first, with whether each is reachable.
func (t *Thread) ListWorkspaces(ctx context.Context) ([]agentdomain.WorkspaceStatus, error) {
	data, err := t.Request(ctx, map[string]string{"type": "list_workspaces"}, "workspaces")
	if err != nil {
		return nil, err
	}
	var listed struct {
		Workspaces []agentdomain.WorkspaceStatus `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &listed); err != nil {
		return nil, err
	}
	return listed.Workspaces, nil
}

// Teleport starts teleporting the followed session to target. Progress arrives as
// TeleportProgressEvent, and the teleport as a SessionPlacementChangedEvent.
func (t *Thread) Teleport(target string, files []string) error {
	t.mu.Lock()
	switch {
	case t.conn == nil:
		t.mu.Unlock()
		return ErrOffline
	case t.teleport != "":
		t.mu.Unlock()
		return errors.New("a teleport of this session is already running")
	case t.placement.Remote() && target != t.placement.Local:
		where := t.placement.Workspace
		t.mu.Unlock()
		return errors.New("the session runs on " + where + " and teleports only back to " + t.placement.Local + " from there")
	}
	frame := teleportFrame{Type: "teleport_session", TeleportID: uuid.NewString(), ProjectDir: t.opts.ProjectDir, ID: t.following, Target: target, Files: files}
	t.teleport = frame.TeleportID
	t.placement.Teleporting = target
	placement := t.placement
	t.mu.Unlock()
	if err := t.Send(frame); err != nil {
		t.endTeleport()
		return err
	}
	t.post(tui.SessionPlacementChangedEvent{Placement: placement})
	return nil
}

// CancelTeleport stops the running teleport, which leaves the session here.
func (t *Thread) CancelTeleport() error {
	t.mu.Lock()
	id := t.teleport
	t.mu.Unlock()
	if id == "" {
		return nil
	}
	return t.Send(teleportFrame{Type: "teleport_cancel", TeleportID: id})
}

func (t *Thread) endTeleport() tui.SessionPlacement {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.teleport = ""
	t.placement.Teleporting = ""
	return t.placement
}

// onProgress reports a stage of the running teleport. A failed teleport
// leaves the session where it was.
func (t *Thread) onProgress(frame []byte) {
	var progress agentdomain.TeleportProgress
	if json.Unmarshal(frame, &progress) != nil {
		return
	}
	t.mu.Lock()
	ours := progress.TeleportID != "" && progress.TeleportID == t.teleport
	t.mu.Unlock()
	if !ours {
		return
	}
	t.post(tui.TeleportProgressEvent{Progress: progress})
	if progress.Stage == agentdomain.TeleportFailed {
		t.post(tui.SessionPlacementChangedEvent{Placement: t.endTeleport()})
	}
}

// onTeleported follows the session to where it teleported. The daemon tells the
// requester and every follower, so the second notice is a duplicate.
func (t *Thread) onTeleported(frame []byte) {
	var teleported agentdomain.SessionTeleported
	if json.Unmarshal(frame, &teleported) != nil {
		return
	}
	t.mu.Lock()
	duplicate := teleported.Session.ConversationID != t.following || t.teleport == "" && t.placement.Workspace == teleported.Workspace
	if duplicate {
		t.mu.Unlock()
		return
	}
	t.teleport = ""
	t.placement.Teleporting = ""
	t.placement.Workspace, t.placement.WorkspaceDir = teleported.Workspace, teleported.WorkspaceDir
	id := t.following
	t.mu.Unlock()
	var cancelled []json.RawMessage
	_ = json.Unmarshal(teleported.CancelledInterrupts, &cancelled)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		_ = t.Follow(ctx, id, sessionsdomain.ThreadOptions{})
		t.post(tui.SessionPlacementChangedEvent{Placement: t.Placement(), Teleported: true, CancelledInterrupts: len(cancelled)})
	}()
}
