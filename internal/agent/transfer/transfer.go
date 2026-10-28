package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// The transfer's time budgets: the whole saga, one peer reply, and the
// quiesce of the source worker.
const (
	transferTimeout = 30 * time.Minute
	replyTimeout    = 5 * time.Minute
	quiesceTimeout  = 2 * time.Minute
	chunkBytes      = 4 << 20
)

// requester is the connection a transfer reports to.
type requester interface {
	Deliver(frame []byte)
}

// startTransfer validates a transfer_session frame and runs the saga under a
// cancellable context registered by transfer id.
func (s *Service) startTransfer(conn *agui.Conn, f clientFrame) {
	if f.TransferID == "" || f.ProjectDir == "" || f.ID == "" || f.Target == "" {
		conn.Deliver(runError("transfer_session needs transfer_id, project_dir, id and target"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), transferTimeout)
	s.mu.Lock()
	if _, running := s.transfers[f.TransferID]; running {
		s.mu.Unlock()
		cancel()
		conn.Deliver(encode(progressFrame{Type: frameTransferProgress, TransferProgress: agentdomain.TransferProgress{
			TransferID: f.TransferID, Stage: agentdomain.TransferFailed, Message: "a transfer with this id is already running",
		}}))
		return
	}
	s.transfers[f.TransferID] = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.transfers, f.TransferID)
		s.mu.Unlock()
	}()
	req := agentdomain.TransferRequest{
		TransferID: f.TransferID,
		Session:    s.session(sessionsdomain.ThreadKey{ProjectDir: f.ProjectDir, ConversationID: f.ID}),
		Target:     f.Target,
		Files:      f.Files,
	}
	s.transfer(ctx, conn, req)
}

func (s *Service) cancelTransfer(transferID string) {
	s.mu.Lock()
	cancel := s.transfers[transferID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// transfer is the saga: precondition, provisioning, checkpointing, restoring,
// ready. Any failure before ready leaves the session here.
func (s *Service) transfer(ctx context.Context, to requester, req agentdomain.TransferRequest) {
	key := sessionsdomain.ThreadKey{ProjectDir: req.Session.ProjectDir, ConversationID: req.Session.ConversationID}
	report := func(stage agentdomain.TransferStage, message string) {
		to.Deliver(encode(progressFrame{Type: frameTransferProgress, TransferProgress: agentdomain.TransferProgress{
			TransferID: req.TransferID, Stage: stage, Message: message,
		}}))
	}
	fail := func(err error) {
		logger.Warn("transfer failed", "transfer_id", req.TransferID, "conversation_id", req.Session.ConversationID, "target", req.Target, "error", err)
		report(agentdomain.TransferFailed, err.Error())
	}

	executor, ok := s.executors[req.Target]
	if !ok || executor.Kind() == agentdomain.ExecutorKindLocal {
		fail(fmt.Errorf("executor %q is not a remote executor registered in daemon.yaml", req.Target))
		return
	}
	done, err := s.precondition(ctx, req)
	if err != nil {
		fail(err)
		return
	}
	if done != nil {
		report(agentdomain.TransferReady, "the session already lives on "+req.Target)
		s.announce(to, key, req, *done, "", nil)
		return
	}

	report(agentdomain.TransferProvisioning, "")
	endpoint, err := executor.Ensure(ctx, req.Session)
	if err != nil {
		fail(describe(err, "provisioning %s", req.Target))
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
	if info.Executor != req.Target {
		fail(fmt.Errorf("the daemon reached for %s identifies as %q", req.Target, info.Executor))
		return
	}

	report(agentdomain.TransferCheckpointing, "")
	checkpoint, err := s.checkpoint(ctx, req, info, conn)
	if err != nil {
		fail(err)
		return
	}

	report(agentdomain.TransferRestoring, "")
	workspace, err := s.restore(ctx, conn, frames, req, checkpoint)
	if err != nil {
		_ = conn.Send(encode(abortFrame{Type: frameTransferAbort, TransferID: req.TransferID}))
		fail(err)
		return
	}

	lease := agentdomain.Lease{
		ConversationID: req.Session.ConversationID, Executor: req.Target, Workspace: workspace,
		TransferID: req.TransferID, Until: time.Now().Add(s.entries[req.Target].EffectiveLeaseTTL()),
	}
	if _, _, err := s.leases.ClaimLease(ctx, lease); err != nil {
		fail(describe(err, "recording the new owner"))
		return
	}
	report(agentdomain.TransferReady, "")
	s.announce(to, key, req, lease, endpoint.Addr, checkpoint.Interrupts)
}

// precondition refuses a session another executor owns, answers a completed
// transfer idempotently, and stamps this daemon's own row with the transfer
// id so the target's claim matches it.
func (s *Service) precondition(ctx context.Context, req agentdomain.TransferRequest) (*agentdomain.Lease, error) {
	lease, found, err := s.leases.GetLease(ctx, req.Session.ConversationID)
	if err != nil {
		return nil, describe(err, "reading the session's lease")
	}
	if found && lease.Executor == req.Target && lease.TransferID == req.TransferID {
		return &lease, nil
	}
	if found && lease.Executor != s.me && lease.Live(time.Now()) {
		return nil, fmt.Errorf("session %s is owned by executor %s", req.Session.ConversationID, lease.Executor)
	}
	own := agentdomain.Lease{
		ConversationID: req.Session.ConversationID, Executor: s.me, Workspace: req.Session.ProjectDir,
		TransferID: req.TransferID, Until: time.Now().Add(s.ownedTTL()),
	}
	if _, ok, err := s.leases.ClaimLease(ctx, own); err != nil || !ok {
		if err == nil {
			err = errors.New("another executor claimed the session first")
		}
		return nil, describe(err, "claiming the session for the transfer")
	}
	return nil, nil
}

// checkpoint quiesces the source worker and captures the session: its
// conversation unless the target shares this daemon's storage, its open
// interrupts, its workspace and its sealed credentials.
func (s *Service) checkpoint(ctx context.Context, req agentdomain.TransferRequest, peer agentinfra.PeerInfo, conn *agui.Conn) (agentdomain.Checkpoint, error) {
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
	denies := func(abs string) bool { return s.cfg.Denies != nil && s.cfg.Denies(req.Session.ProjectDir, abs) }
	checkpoint.Workspace, err = agentinfra.PackWorkspace(ctx, req.Session.ProjectDir, req.Files, denies)
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

// sealCredentials encrypts the executor's forward_env values to the peer's
// certificate, so they travel only to the daemon that presented it.
func (s *Service) sealCredentials(req agentdomain.TransferRequest, conn *agui.Conn) ([]byte, error) {
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
	sealed, err := agentinfra.SealEnv(certs[0], env, req.TransferID)
	if err != nil {
		return nil, describe(err, "sealing the credentials")
	}
	return sealed, nil
}

// restore streams the checkpoint to the peer, waits for it to import, then
// resumes the conversation there and checks the snapshot matches.
func (s *Service) restore(ctx context.Context, conn *agui.Conn, frames *peerFrames, req agentdomain.TransferRequest, checkpoint agentdomain.Checkpoint) (string, error) {
	payload := encode(checkpoint)
	sum := sha256.Sum256(payload)
	chunks := 0
	for start := 0; start < len(payload); start += chunkBytes {
		end := min(start+chunkBytes, len(payload))
		if err := conn.Send(encode(chunkFrame{Type: frameTransferChunk, TransferID: req.TransferID, Seq: chunks, Data: payload[start:end]})); err != nil {
			return "", describe(err, "sending the checkpoint")
		}
		chunks++
	}
	if err := conn.Send(encode(importFrame{Type: frameTransferImport, TransferID: req.TransferID, Executor: req.Target, Chunks: chunks, SHA256: hex.EncodeToString(sum[:])})); err != nil {
		return "", describe(err, "sending the checkpoint")
	}
	replyCtx, cancel := context.WithTimeout(ctx, replyTimeout)
	defer cancel()
	reply, err := frames.await(replyCtx, frameTransferImported)
	if err != nil {
		return "", describe(err, "waiting for %s to restore the session", req.Target)
	}
	var imported importedFrame
	if json.Unmarshal(reply, &imported) != nil || !imported.OK {
		return "", fmt.Errorf("%s refused the checkpoint: %s", req.Target, imported.Error)
	}
	if err := conn.Send(encode(map[string]string{"type": frameResumeConversation, "project_dir": imported.Workspace, "id": req.Session.ConversationID})); err != nil {
		return "", describe(err, "resuming the conversation on %s", req.Target)
	}
	snapshot, err := frames.await(replyCtx, frameMessagesSnapshot)
	if err != nil {
		return "", describe(err, "waiting for the conversation to resume on %s", req.Target)
	}
	if digest, count := snapshotDigest(snapshot); digest != checkpoint.Digest {
		return "", fmt.Errorf("the conversation on %s has %d messages and does not match the checkpoint's %d", req.Target, count, checkpoint.MessageCount)
	}
	return imported.Workspace, nil
}

// snapshotDigest fingerprints a MESSAGES_SNAPSHOT the way the worker does.
func snapshotDigest(frame []byte) (string, int) {
	var snapshot struct {
		Messages []agui.Message `json:"messages"`
	}
	_ = json.Unmarshal(frame, &snapshot)
	return agui.SnapshotDigest(snapshot.Messages), len(snapshot.Messages)
}

// announce tells the requester and the thread's followers where the session
// lives now and which interrupts the move cancelled.
func (s *Service) announce(to requester, key sessionsdomain.ThreadKey, req agentdomain.TransferRequest, lease agentdomain.Lease, addr string, interrupts json.RawMessage) {
	moved := encode(sessionMovedFrame{Type: frameSessionMoved, SessionMoved: agentdomain.SessionMoved{
		Session: req.Session, Executor: lease.Executor, Addr: addr, Workspace: lease.Workspace, CancelledInterrupts: interrupts,
	}})
	to.Deliver(moved)
	if s.registry != nil {
		s.registry.Deliver(key, moved)
	}
}
