package teleport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// The teleport's time budgets: the whole saga, one peer reply, the quiesce of
// the source worker, and the cleanup of a failed target.
const (
	teleportTimeout = 30 * time.Minute
	replyTimeout    = 5 * time.Minute
	quiesceTimeout  = 2 * time.Minute
	discardTimeout  = time.Minute
	chunkBytes      = 4 << 20
)

// requester is the connection a teleport reports to.
type requester interface {
	Deliver(frame []byte)
}

// startTeleport validates a teleport_session frame and runs the saga under a
// cancellable context registered by teleport id.
func (s *Service) startTeleport(to requester, f clientFrame) {
	if f.TeleportID == "" || f.ProjectDir == "" || f.ID == "" || f.Target == "" {
		to.Deliver(runError("teleport_session needs teleport_id, project_dir, id and target"))
		return
	}
	session := s.session(sessionsdomain.ThreadKey{ProjectDir: f.ProjectDir, ConversationID: f.ID})
	if err := session.Validate(); err != nil {
		refuseTeleport(to, f.TeleportID, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), teleportTimeout)
	s.mu.Lock()
	if _, running := s.teleports[f.TeleportID]; running {
		s.mu.Unlock()
		cancel()
		refuseTeleport(to, f.TeleportID, "a teleport with this id is already running")
		return
	}
	if s.conversations[f.ID] != "" {
		s.mu.Unlock()
		cancel()
		refuseTeleport(to, f.TeleportID, "a teleport of this session is already running")
		return
	}
	s.teleports[f.TeleportID] = cancel
	s.conversations[f.ID] = f.TeleportID
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.teleports, f.TeleportID)
		delete(s.conversations, f.ID)
		s.mu.Unlock()
	}()
	req := agentdomain.TeleportRequest{
		TeleportID: f.TeleportID,
		Session:    session,
		Target:     f.Target,
		Files:      f.Files,
	}
	s.teleport(ctx, to, req)
}

func (s *Service) cancelTeleport(teleportID string) {
	s.mu.Lock()
	cancel := s.teleports[teleportID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// refuseTeleport answers a teleport request the service will not run.
func refuseTeleport(to requester, teleportID, reason string) {
	to.Deliver(encode(progressFrame{Type: frameTeleportProgress, TeleportProgress: agentdomain.TeleportProgress{
		TeleportID: teleportID, Stage: agentdomain.TeleportFailed, Message: reason,
	}}))
}

// teleport is the saga: precondition, provisioning, checkpointing, restoring,
// ready. Any failure before ready leaves the session here.
func (s *Service) teleport(ctx context.Context, to requester, req agentdomain.TeleportRequest) {
	key := sessionsdomain.ThreadKey{ProjectDir: req.Session.ProjectDir, ConversationID: req.Session.ConversationID}
	report := func(stage agentdomain.TeleportStage, message string) { s.report(to, req, stage, message) }
	fail := func(err error) { s.failTeleport(to, req, err) }
	if req.Target == s.me {
		s.teleportBack(ctx, to, req)
		return
	}

	environment, ok := s.environments[req.Target]
	if !ok || environment.Kind() == agentdomain.EnvironmentKindLocal {
		fail(fmt.Errorf("environment %q is not a remote environment registered in daemon.yaml", req.Target))
		return
	}
	if backend := s.cfg.LeaseBackend; backend == config.StorageTypeJsonl || backend == config.StorageTypeMemory {
		logger.Warn("teleport runs on per-machine lease storage - a crash mid-saga can leave both daemons believing they own the session, point storage.type at a shared backend to close the window", "backend", backend, "target", req.Target, "conversation_id", req.Session.ConversationID)
	}
	done, err := s.precondition(ctx, req)
	if err != nil {
		fail(err)
		return
	}
	if done != nil {
		report(agentdomain.TeleportReady, "the session already lives on "+req.Target)
		s.announce(to, key, req, *done, "", nil)
		return
	}

	report(agentdomain.TeleportProvisioning, "")
	endpoint, err := environment.Ensure(ctx, req.Session)
	if err != nil {
		fail(describe(err, "provisioning %s", req.Target))
		return
	}
	defer endpoint.Release()
	teleported := false
	defer func() {
		if !teleported {
			s.discard(ctx, environment, req)
		}
	}()
	frames := newPeerFrames()
	conn, info, err := s.dial.Dial(ctx, endpoint, frames)
	if err != nil {
		fail(err)
		return
	}
	defer conn.Close()
	if info.Environment != req.Target {
		fail(fmt.Errorf("the daemon reached for %s identifies as %q", req.Target, info.Environment))
		return
	}

	report(agentdomain.TeleportCheckpointing, "")
	checkpoint, err := s.checkpoint(ctx, req, info, conn)
	if err != nil {
		fail(err)
		return
	}
	fingerprint, _, err := agentinfra.CheckoutState(ctx, req.Session.ProjectDir, s.excludes(req.Session.ProjectDir))
	if err != nil {
		logger.Warn("teleport could not fingerprint the checkout the session leaves", "conversation_id", req.Session.ConversationID, "error", err)
	}

	report(agentdomain.TeleportRestoring, "")
	workspace, err := s.restore(ctx, conn, frames, req, checkpoint)
	if err != nil {
		_ = conn.Send(encode(abortFrame{Type: frameTeleportAbort, TeleportID: req.TeleportID}))
		fail(err)
		return
	}

	lease := agentdomain.Lease{
		ConversationID: req.Session.ConversationID, Environment: req.Target, Workspace: workspace,
		TeleportID: req.TeleportID, Until: time.Now().Add(s.entries[req.Target].EffectiveLeaseTTL()),
	}
	teleported = s.handOver(ctx, conn, to, req, lease, endpoint.Addr, checkpoint.Interrupts)
	if teleported && fingerprint != "" {
		s.rememberOrigin(req.Session.ConversationID, req.Session.ProjectDir, fingerprint)
	}
}

// discard removes what a failed teleport left on its target: the runtime it
// provisioned and any claim the target holds on the session, so the next
// teleport starts clean. It outlives a cancelled teleport.
func (s *Service) discard(ctx context.Context, environment agentdomain.Environment, req agentdomain.TeleportRequest) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discardTimeout)
	defer cancel()
	if err := environment.Delete(ctx, req.Session); err != nil {
		logger.Warn("teleport could not delete the runtime a failed teleport left", "teleport_id", req.TeleportID, "target", req.Target, "error", err)
	}
	if err := s.leases.ReleaseLease(ctx, req.Session.ConversationID, req.Target); err != nil {
		logger.Warn("teleport could not release the target's claim after a failed teleport", "teleport_id", req.TeleportID, "target", req.Target, "error", err)
	}
}

// handOver writes the target's lease row and announces the teleport, or aborts
// the restore there when the row will not yield. It reports whether the
// session teleported.
func (s *Service) handOver(ctx context.Context, peer *agui.Conn, to requester, req agentdomain.TeleportRequest, lease agentdomain.Lease, addr string, interrupts json.RawMessage) bool {
	holder, ok, err := s.leases.ClaimLease(ctx, lease)
	if err == nil && !ok {
		err = fmt.Errorf("environment %s holds the session's lease", holder.Environment)
	}
	if err != nil {
		_ = peer.Send(encode(abortFrame{Type: frameTeleportAbort, TeleportID: req.TeleportID}))
		s.failTeleport(to, req, describe(err, "recording the new owner"))
		return false
	}
	s.report(to, req, agentdomain.TeleportReady, "")
	s.announce(to, sessionsdomain.ThreadKey{ProjectDir: req.Session.ProjectDir, ConversationID: req.Session.ConversationID}, req, lease, addr, interrupts)
	return true
}

// report hands the requester one progress frame of the teleport.
func (s *Service) report(to requester, req agentdomain.TeleportRequest, stage agentdomain.TeleportStage, message string) {
	to.Deliver(encode(progressFrame{Type: frameTeleportProgress, TeleportProgress: agentdomain.TeleportProgress{
		TeleportID: req.TeleportID, Stage: stage, Message: message,
	}}))
}

// failTeleport logs why the teleport failed and reports it to the requester.
func (s *Service) failTeleport(to requester, req agentdomain.TeleportRequest, err error) {
	logger.Warn("teleport failed", "teleport_id", req.TeleportID, "conversation_id", req.Session.ConversationID, "target", req.Target, "error", err)
	s.report(to, req, agentdomain.TeleportFailed, err.Error())
}

// precondition refuses a session another environment owns, answers a completed
// teleport idempotently, and stamps this daemon's own row with the teleport
// id so the target's claim matches it.
func (s *Service) precondition(ctx context.Context, req agentdomain.TeleportRequest) (*agentdomain.Lease, error) {
	lease, found, err := s.leases.GetLease(ctx, req.Session.ConversationID)
	if err != nil {
		return nil, describe(err, "reading the session's lease")
	}
	if found && lease.Environment == req.Target && lease.TeleportID == req.TeleportID {
		return &lease, nil
	}
	if found && lease.Environment != s.me && lease.Live(time.Now()) {
		return nil, fmt.Errorf("session %s is owned by environment %s", req.Session.ConversationID, lease.Environment)
	}
	own := agentdomain.Lease{
		ConversationID: req.Session.ConversationID, Environment: s.me, Workspace: req.Session.ProjectDir,
		TeleportID: req.TeleportID, Until: time.Now().Add(s.ownedTTL()),
	}
	if _, ok, err := s.leases.ClaimLease(ctx, own); err != nil || !ok {
		if err == nil {
			err = errors.New("another environment claimed the session first")
		}
		return nil, describe(err, "claiming the session for the teleport")
	}
	return nil, nil
}

// checkpoint quiesces the source worker and captures the session: its
// conversation unless the target shares this daemon's storage, its open
// interrupts, its workspace and its sealed credentials.
func (s *Service) checkpoint(ctx context.Context, req agentdomain.TeleportRequest, peer agentinfra.PeerInfo, conn *agui.Conn) (agentdomain.Checkpoint, error) {
	key := sessionsdomain.ThreadKey{ProjectDir: req.Session.ProjectDir, ConversationID: req.Session.ConversationID}
	quiesceCtx, cancel := context.WithTimeout(ctx, quiesceTimeout)
	defer cancel()
	interrupts, err := s.registry.Quiesce(quiesceCtx, key)
	if err != nil {
		return agentdomain.Checkpoint{}, describe(err, "quiescing the session")
	}
	exported, err := s.export(ctx, req.Session)
	if err != nil {
		return agentdomain.Checkpoint{}, err
	}
	checkpoint := agentdomain.Checkpoint{
		SchemaVersion: agentdomain.CheckpointSchemaVersion,
		Session:       req.Session,
		MessageCount:  exported.MessageCount,
		Digest:        exported.Digest,
		Interrupts:    interrupts,
	}
	if peer.StorageID == "" || peer.StorageID != s.cfg.StorageID {
		checkpoint.Conversation = encode(map[string]json.RawMessage{"entries": exported.Entries, "metadata": exported.Metadata})
	}
	checkpoint.Workspace, err = agentinfra.PackWorkspace(ctx, req.Session.ProjectDir, req.Files, s.excludes(req.Session.ProjectDir))
	if err != nil {
		return agentdomain.Checkpoint{}, describe(err, "packing the workspace")
	}
	checkpoint.SealedEnv, err = s.sealCredentials(req, conn)
	if err != nil {
		return agentdomain.Checkpoint{}, err
	}
	return checkpoint, nil
}

// export asks a worker in the project for the stored conversation.
func (s *Service) export(ctx context.Context, session agentdomain.Session) (exportedConversation, error) {
	ctx, cancel := context.WithTimeout(ctx, replyTimeout)
	defer cancel()
	request := encode(map[string]string{"type": frameExportConversation, "project_dir": session.ProjectDir, "id": session.ConversationID})
	reply, err := s.registry.Request(ctx, request, frameConversationExport)
	if err != nil {
		return exportedConversation{}, describe(err, "exporting the conversation")
	}
	var exported exportedConversation
	if err := json.Unmarshal(reply, &exported); err != nil {
		return exportedConversation{}, describe(err, "decoding the exported conversation")
	}
	if exported.Error != "" {
		return exportedConversation{}, errors.New("exporting the conversation: " + exported.Error)
	}
	return exported, nil
}

// sealCredentials encrypts the environment's forward_env values to the peer's
// certificate, so they travel only to the daemon that presented it.
func (s *Service) sealCredentials(req agentdomain.TeleportRequest, conn *agui.Conn) ([]byte, error) {
	names := s.entries[req.Target].ForwardEnv
	if len(names) == 0 {
		return nil, nil
	}
	certs := conn.PeerCertificates()
	if len(certs) == 0 {
		return nil, errors.New("forward_env needs a TLS connection to seal the credentials to")
	}
	env := make(map[string]string, len(names))
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			env[name] = value
		}
	}
	sealed, err := agentinfra.SealEnv(certs[0], env, req.TeleportID)
	if err != nil {
		return nil, describe(err, "sealing the credentials")
	}
	return sealed, nil
}

// restore streams the checkpoint to the peer, waits for it to import, then
// resumes the conversation there and checks the snapshot matches.
func (s *Service) restore(ctx context.Context, conn *agui.Conn, frames *peerFrames, req agentdomain.TeleportRequest, checkpoint agentdomain.Checkpoint) (string, error) {
	if err := sendCheckpoint(conn, req.TeleportID, req.Target, checkpoint); err != nil {
		return "", err
	}
	replyCtx, cancel := context.WithTimeout(ctx, replyTimeout)
	defer cancel()
	reply, err := frames.await(replyCtx, frameTeleportImported)
	if err != nil {
		return "", describe(err, "waiting for %s to restore the session", req.Target)
	}
	var imported importedFrame
	if err := json.Unmarshal(reply, &imported); err != nil {
		return "", describe(err, "reading %s's restore reply", req.Target)
	}
	if !imported.OK {
		return "", fmt.Errorf("%s refused the checkpoint: %s", req.Target, imported.Error)
	}
	if err := conn.Send(encode(map[string]string{"type": frameResumeConversation, "project_dir": imported.Workspace, "id": req.Session.ConversationID})); err != nil {
		return "", describe(err, "resuming the conversation on %s", req.Target)
	}
	snapshotCtx, snapshotCancel := context.WithTimeout(ctx, replyTimeout)
	defer snapshotCancel()
	snapshot, err := frames.await(snapshotCtx, frameMessagesSnapshot)
	if err != nil {
		return "", describe(err, "waiting for the conversation to resume on %s", req.Target)
	}
	if digest := snapshotDigest(snapshot); digest != checkpoint.Digest {
		return "", fmt.Errorf("the conversation on %s renders to digest %s, not the checkpoint's %s", req.Target, digest, checkpoint.Digest)
	}
	return imported.Workspace, nil
}

// sendCheckpoint streams a checkpoint to conn in chunks and closes it with the
// import frame naming the environment it is meant for.
func sendCheckpoint(conn *agui.Conn, teleportID, environment string, checkpoint agentdomain.Checkpoint) error {
	payload := encode(checkpoint)
	sum := sha256.Sum256(payload)
	chunks := 0
	for start := 0; start < len(payload); start += chunkBytes {
		end := min(start+chunkBytes, len(payload))
		if err := conn.Send(encode(chunkFrame{Type: frameTeleportChunk, TeleportID: teleportID, Seq: chunks, Data: payload[start:end]})); err != nil {
			return describe(err, "sending the checkpoint")
		}
		chunks++
	}
	if err := conn.Send(encode(importFrame{Type: frameTeleportImport, TeleportID: teleportID, Environment: environment, Chunks: chunks, SHA256: hex.EncodeToString(sum[:])})); err != nil {
		return describe(err, "sending the checkpoint")
	}
	return nil
}

// snapshotDigest fingerprints a MESSAGES_SNAPSHOT the way the worker does.
func snapshotDigest(frame []byte) string {
	var snapshot struct {
		Messages []agui.Message `json:"messages"`
	}
	_ = json.Unmarshal(frame, &snapshot)
	return agui.SnapshotDigest(snapshot.Messages)
}

// announce tells the requester and the thread's followers where the session
// lives now and which interrupts the teleport cancelled, and frees the thread's
// last run here from holding back this daemon's Sandbox retention.
func (s *Service) announce(to requester, key sessionsdomain.ThreadKey, req agentdomain.TeleportRequest, lease agentdomain.Lease, addr string, interrupts json.RawMessage) {
	teleported := encode(sessionTeleportedFrame{Type: frameSessionTeleported, SessionTeleported: agentdomain.SessionTeleported{
		Session: req.Session, Environment: lease.Environment, Addr: addr, Workspace: lease.Workspace, CancelledInterrupts: interrupts,
	}})
	to.Deliver(teleported)
	if s.registry != nil {
		s.registry.TeleportedAway(key)
		s.registry.Deliver(key, teleported)
	}
}
