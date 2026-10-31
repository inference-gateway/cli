package teleport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// returnAckTimeout bounds the owner's answer to a teleport back, which it sends
// before it quiesces and packs.
const returnAckTimeout = 15 * time.Second

// teleportBack teleports a session that runs on a remote workspace back to this
// daemon: the owner packs it, its repository lands on the checkout it left and
// its conversation is stored here again, remembering the runtime it left
// behind. A failure leaves it where it was.
func (s *Service) teleportBack(ctx context.Context, to requester, req agentdomain.TeleportRequest) {
	fail := func(err error) { s.failTeleport(to, req, err) }
	key := sessionsdomain.ThreadKey{ProjectDir: req.Session.ProjectDir, ConversationID: req.Session.ConversationID}
	remote, found, err := s.leases.GetLease(ctx, req.Session.ConversationID)
	switch {
	case err != nil:
		fail(describe(err, "reading the session's lease"))
		return
	case !found || remote.Workspace == s.me:
		fail(fmt.Errorf("session %s already runs on %s", req.Session.ConversationID, s.me))
		return
	}
	workspace, ok := s.workspaces[remote.Workspace]
	if !ok {
		fail(fmt.Errorf("workspace %s is not registered in daemon.yaml", remote.Workspace))
		return
	}
	if err := s.checkoutReady(ctx, req.Session); err != nil {
		fail(err)
		return
	}

	s.report(to, req, agentdomain.TeleportProvisioning, "")
	endpoint, err := workspace.Reach(ctx, req.Session)
	if err != nil {
		fail(describe(err, "reaching %s", remote.Workspace))
		return
	}
	defer endpoint.Release()
	frames := newPeerFrames()
	conn, info, err := s.dial.Dial(ctx, endpoint, frames)
	if err != nil {
		fail(err)
		return
	}
	defer conn.Close()
	if info.Workspace != remote.Workspace {
		fail(fmt.Errorf("the daemon reached for %s identifies as %q", remote.Workspace, info.Workspace))
		return
	}

	s.report(to, req, agentdomain.TeleportCheckpointing, "")
	quiesceCtx, cancel := context.WithTimeout(ctx, quiesceTimeout)
	defer cancel()
	if _, err := s.registry.Quiesce(quiesceCtx, key); err != nil {
		fail(describe(err, "quiescing the session's relay"))
		return
	}
	checkpoint, err := s.fetchCheckpoint(ctx, conn, frames, req, remote)
	if err != nil {
		fail(err)
		return
	}

	s.report(to, req, agentdomain.TeleportRestoring, "")
	if err := s.land(ctx, req, remote, checkpoint); err != nil {
		fail(err)
		return
	}
	_ = conn.Send(encode(returnFrame{Type: frameTeleportReturned, TeleportID: req.TeleportID}))
	s.forgetOrigin(req.Session.ConversationID)
	s.rememberRuntime(req.Session.ConversationID, remote.Workspace)
	s.registry.TeleportedHere(key)
	s.report(to, req, agentdomain.TeleportReady, "")
	teleported := encode(sessionTeleportedFrame{Type: frameSessionTeleported, SessionTeleported: agentdomain.SessionTeleported{
		Session: req.Session, Workspace: s.me, WorkspaceDir: req.Session.ProjectDir, CancelledInterrupts: checkpoint.Interrupts,
	}})
	to.Deliver(teleported)
	s.registry.Deliver(key, teleported)
}

// checkoutReady says why the session's repository cannot land on the checkout
// it left, or nil: the checkout is clean, or unchanged since the session left.
func (s *Service) checkoutReady(ctx context.Context, session agentdomain.Session) error {
	fingerprint, clean, err := agentinfra.CheckoutState(ctx, session.ProjectDir, s.excludes(session.ProjectDir))
	if err != nil {
		return describe(err, "reading the checkout at %s", session.ProjectDir)
	}
	left, origin := s.origin(session.ConversationID)
	if left != "" && left != session.ProjectDir {
		return fmt.Errorf("the session left the checkout at %s: teleport it back from a chat in that project", left)
	}
	if clean || fingerprint == origin {
		return nil
	}
	return fmt.Errorf("the checkout at %s changed since the session left it: stash those changes (git stash -u), teleport the session back, then git stash pop", session.ProjectDir)
}

// fetchCheckpoint asks the session's owner to pack it and collects the
// checkpoint it streams back. An owner that does not answer at once runs an
// infer that cannot send sessions back.
func (s *Service) fetchCheckpoint(ctx context.Context, conn *agui.Conn, frames *peerFrames, req agentdomain.TeleportRequest, remote agentdomain.Lease) (agentdomain.Checkpoint, error) {
	owner := remote.Workspace
	ask := returnFrame{Type: frameTeleportReturn, TeleportID: req.TeleportID, ID: req.Session.ConversationID, WorkspaceDir: remote.WorkspaceDir, Workspace: s.me}
	if err := conn.Send(encode(ask)); err != nil {
		return agentdomain.Checkpoint{}, describe(err, "asking %s for the session", owner)
	}
	ackCtx, cancelAck := context.WithTimeout(ctx, returnAckTimeout)
	defer cancelAck()
	replyCtx, cancel := context.WithTimeout(ctx, replyTimeout)
	defer cancel()
	wait := ackCtx
	pending := newPendingImport()
	for {
		frame, err := frames.await(wait, frameTeleportReturning, frameTeleportChunk, frameTeleportImport)
		if err != nil && wait == ackCtx && errors.Is(err, context.DeadlineExceeded) {
			return agentdomain.Checkpoint{}, fmt.Errorf("%s's daemon did not answer the teleport back, it may run an infer too old to send sessions back", owner)
		}
		if err != nil {
			return agentdomain.Checkpoint{}, describe(err, "waiting for %s to pack the session", owner)
		}
		wait = replyCtx
		switch frameType(frame) {
		case frameTeleportReturning:
			continue
		case frameTeleportChunk:
			var chunk chunkFrame
			if err := json.Unmarshal(frame, &chunk); err != nil {
				return agentdomain.Checkpoint{}, describe(err, "reading a checkpoint chunk from %s", owner)
			}
			if err := pending.add(chunk); err != nil {
				return agentdomain.Checkpoint{}, err
			}
			continue
		}
		var closing importFrame
		if err := json.Unmarshal(frame, &closing); err != nil {
			return agentdomain.Checkpoint{}, describe(err, "reading the checkpoint from %s", owner)
		}
		payload, err := pending.payload(closing)
		if err != nil {
			return agentdomain.Checkpoint{}, err
		}
		checkpoint, err := decodeCheckpoint(payload)
		if err != nil {
			return agentdomain.Checkpoint{}, err
		}
		if checkpoint.Session.ConversationID != req.Session.ConversationID || len(checkpoint.Conversation) == 0 {
			return agentdomain.Checkpoint{}, fmt.Errorf("%s sent a checkpoint that is not session %s's", owner, req.Session.ConversationID)
		}
		return checkpoint, nil
	}
}

// land puts the checkpoint's repository on the session's checkout, takes the
// session's lease back and stores its conversation here. A failure undoes
// all three, so the session stays with its remote owner.
func (s *Service) land(ctx context.Context, req agentdomain.TeleportRequest, remote agentdomain.Lease, checkpoint agentdomain.Checkpoint) error {
	landing, err := agentinfra.LandRepository(ctx, checkpoint.Repository, req.Session.ProjectDir, s.excludes(req.Session.ProjectDir))
	if err != nil {
		return describe(err, "landing the repository on %s", req.Session.ProjectDir)
	}
	if err := s.reclaim(ctx, req, remote, checkpoint); err != nil {
		if undoErr := landing.Undo(ctx); undoErr != nil {
			return errors.Join(err, describe(undoErr, "undoing the landing"))
		}
		return err
	}
	if err := landing.Keep(ctx); err != nil {
		logger.Warn("teleport could not drop the stash a landing kept", "project_dir", req.Session.ProjectDir, "error", err)
	}
	return nil
}

// reclaim moves the session's lease from its remote owner to this daemon and
// imports the conversation through a worker here, handing the lease back when
// the import fails.
func (s *Service) reclaim(ctx context.Context, req agentdomain.TeleportRequest, remote agentdomain.Lease, checkpoint agentdomain.Checkpoint) error {
	conversationID := req.Session.ConversationID
	if err := s.leases.ReleaseLease(ctx, conversationID, remote.Workspace); err != nil {
		return describe(err, "releasing %s's lease", remote.Workspace)
	}
	own := agentdomain.Lease{ConversationID: conversationID, Workspace: s.me, WorkspaceDir: req.Session.ProjectDir, TeleportID: req.TeleportID, Until: time.Now().Add(s.ownedTTL())}
	giveBack := func(err error) error {
		_ = s.leases.ReleaseLease(ctx, conversationID, s.me)
		remote.Until = time.Now().Add(s.entries[remote.Workspace].EffectiveLeaseTTL())
		if _, _, claimErr := s.leases.ClaimLease(ctx, remote); claimErr != nil {
			return errors.Join(err, describe(claimErr, "handing the lease back to %s", remote.Workspace))
		}
		return err
	}
	if holder, ok, err := s.leases.ClaimLease(ctx, own); err != nil || !ok {
		if err == nil {
			err = fmt.Errorf("workspace %s claimed the session first", holder.Workspace)
		}
		return giveBack(describe(err, "claiming the session"))
	}
	if err := s.importConversation(ctx, req.Session.ProjectDir, conversationID, checkpoint); err != nil {
		return giveBack(err)
	}
	return nil
}

// packForReturn answers teleport_return from the daemon a session came from:
// it brings the session to rest here, packs it and streams it back.
func (s *Service) packForReturn(conn *agui.Conn, f returnFrame) {
	_ = conn.Send(encode(returnFrame{Type: frameTeleportReturning, TeleportID: f.TeleportID, ID: f.ID}))
	ctx, cancel := context.WithTimeout(context.Background(), replyTimeout)
	defer cancel()
	checkpoint, key, err := s.packOwned(ctx, f)
	if err == nil {
		s.mu.Lock()
		s.returns[f.TeleportID] = key
		s.mu.Unlock()
		err = sendCheckpoint(conn, f.TeleportID, f.Workspace, checkpoint)
	}
	if err != nil {
		logger.Warn("teleport could not send a session back", "teleport_id", f.TeleportID, "conversation_id", f.ID, "error", err)
		_ = conn.Send(runError(err.Error()))
	}
}

// packOwned quiesces a session restored here and captures it whole, its
// conversation always included, since it goes back to where it left. The
// workspace dir comes from the requester, since a restarted daemon forgets its
// leases, so only one under this daemon's projects root is packed.
func (s *Service) packOwned(ctx context.Context, f returnFrame) (agentdomain.Checkpoint, sessionsdomain.ThreadKey, error) {
	lease, found, err := s.leases.GetLease(ctx, f.ID)
	if err != nil {
		return agentdomain.Checkpoint{}, sessionsdomain.ThreadKey{}, describe(err, "reading the session's lease")
	}
	if found && lease.Workspace != s.me {
		return agentdomain.Checkpoint{}, sessionsdomain.ThreadKey{}, fmt.Errorf("session %s runs on %s, not %s", f.ID, lease.Workspace, s.me)
	}
	if rel, err := filepath.Rel(s.cfg.ProjectsRoot, f.WorkspaceDir); f.WorkspaceDir == "" || err != nil || !filepath.IsLocal(rel) {
		return agentdomain.Checkpoint{}, sessionsdomain.ThreadKey{}, fmt.Errorf("%q is not a workspace dir %s restored", f.WorkspaceDir, s.me)
	}
	key := sessionsdomain.ThreadKey{ProjectDir: filepath.Clean(f.WorkspaceDir), ConversationID: f.ID}
	quiesceCtx, cancel := context.WithTimeout(ctx, quiesceTimeout)
	defer cancel()
	interrupts, err := s.registry.Quiesce(quiesceCtx, key)
	if err != nil {
		return agentdomain.Checkpoint{}, key, describe(err, "quiescing the session")
	}
	session := s.session(key)
	exported, err := s.export(ctx, session)
	if err != nil {
		return agentdomain.Checkpoint{}, key, err
	}
	repository, err := agentinfra.PackRepository(ctx, key.ProjectDir, nil, s.excludes(key.ProjectDir))
	if err != nil {
		return agentdomain.Checkpoint{}, key, describe(err, "packing the repository")
	}
	return agentdomain.Checkpoint{
		SchemaVersion: agentdomain.CheckpointSchemaVersion,
		Session:       session,
		MessageCount:  exported.MessageCount,
		Digest:        exported.Digest,
		Conversation:  encode(map[string]json.RawMessage{"entries": exported.Entries, "metadata": exported.Metadata}),
		Interrupts:    interrupts,
		Repository:    repository,
	}, key, nil
}

// returned lets go of a session that landed back where it came from: its
// last run here stops holding the runtime back and its lease row goes.
func (s *Service) returned(teleportID string) {
	s.mu.Lock()
	key, ok := s.returns[teleportID]
	delete(s.returns, teleportID)
	s.mu.Unlock()
	if !ok {
		return
	}
	s.registry.TeleportedAway(key)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := s.leases.ReleaseLease(ctx, key.ConversationID, s.me); err != nil {
		logger.Warn("teleport could not release a session that went back", "conversation_id", key.ConversationID, "error", err)
	}
}

// excludes is the sandbox policy over a project's files, which keeps them out
// of a checkpoint and out of a landing.
func (s *Service) excludes(projectDir string) func(abs string) bool {
	return func(abs string) bool { return s.cfg.Denies != nil && s.cfg.Denies(projectDir, abs) }
}

// What the daemon remembers per session under its state dir.
const (
	originsState  = "origins"
	runtimesState = "runtimes"
)

// rememberOrigin keeps the checkout a session left and its fingerprint, which
// lets it come back onto that checkout, and only that one, while unchanged.
func (s *Service) rememberOrigin(conversationID, projectDir, fingerprint string) {
	if !s.writeState(originsState, conversationID, projectDir+"\n"+fingerprint) {
		logger.Warn("teleport could not remember the checkout a session left", "conversation_id", conversationID)
	}
}

// origin is the checkout a session left and its fingerprint then, or empty
// strings when the daemon remembers none.
func (s *Service) origin(conversationID string) (string, string) {
	projectDir, fingerprint, _ := strings.Cut(s.readState(originsState, conversationID), "\n")
	return projectDir, fingerprint
}

func (s *Service) forgetOrigin(conversationID string) {
	s.removeState(originsState, conversationID)
}

// rememberRuntime keeps the remote workspace a session came home from, whose
// runtime it left behind, so stopping the session deletes that runtime too.
func (s *Service) rememberRuntime(conversationID, workspace string) {
	if !s.writeState(runtimesState, conversationID, workspace) {
		logger.Warn("teleport could not remember the runtime a session left", "conversation_id", conversationID, "workspace", workspace)
	}
}

// runtimeLeftOn is the remote workspace a session left its runtime on when it
// came home, "" when the daemon remembers none.
func (s *Service) runtimeLeftOn(conversationID string) string {
	return s.readState(runtimesState, conversationID)
}

func (s *Service) forgetRuntime(conversationID string) {
	s.removeState(runtimesState, conversationID)
}

func (s *Service) writeState(kind, conversationID, value string) bool {
	path := s.stateFile(kind, conversationID)
	if path == "" {
		return true
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false
	}
	return os.WriteFile(path, []byte(value), 0o600) == nil
}

func (s *Service) readState(kind, conversationID string) string {
	path := s.stateFile(kind, conversationID)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (s *Service) removeState(kind, conversationID string) {
	if path := s.stateFile(kind, conversationID); path != "" {
		_ = os.Remove(path)
	}
}

// stateFile is where the daemon keeps one kind of state about a session, ""
// when it remembers none or the conversation id is not a plain file name.
func (s *Service) stateFile(kind, conversationID string) string {
	name := filepath.Base(conversationID)
	if s.cfg.StateDir == "" || name != conversationID || name == "." || strings.Contains(name, "..") {
		return ""
	}
	return filepath.Join(s.cfg.StateDir, kind, name)
}
