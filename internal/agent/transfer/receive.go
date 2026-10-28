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
)

// maxPayloadBytes caps an incoming checkpoint: the workspace limit plus room
// for the conversation and the JSON around it.
const maxPayloadBytes = agentinfra.MaxWorkspaceBytes + 64<<20

// pendingImport collects the chunks of one incoming checkpoint.
type pendingImport struct {
	chunks map[int][]byte
	size   int
}

// receive handles the peer frames of an incoming transfer.
func (s *Service) receive(conn *agui.Conn, typ string, frame []byte) {
	switch typ {
	case frameTransferChunk:
		s.receiveChunk(conn, frame)
	case frameTransferImport:
		var f importFrame
		if json.Unmarshal(frame, &f) != nil {
			return
		}
		go s.receiveImport(conn, f)
	case frameTransferAbort:
		var f abortFrame
		if json.Unmarshal(frame, &f) != nil {
			return
		}
		go s.abort(f.TransferID)
	}
}

func (s *Service) receiveChunk(conn *agui.Conn, frame []byte) {
	var chunk chunkFrame
	if err := json.Unmarshal(frame, &chunk); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.imports[chunk.TransferID]
	if pending == nil {
		pending = &pendingImport{chunks: make(map[int][]byte)}
		s.imports[chunk.TransferID] = pending
	}
	pending.size += len(chunk.Data)
	if pending.size > maxPayloadBytes {
		delete(s.imports, chunk.TransferID)
		conn.Deliver(encode(importedFrame{Type: frameTransferImported, TransferID: chunk.TransferID, Error: "the checkpoint is larger than this daemon accepts"}))
		return
	}
	pending.chunks[chunk.Seq] = chunk.Data
}

// receiveImport assembles and validates the checkpoint, restores the
// workspace and the conversation, claims the lease and answers.
func (s *Service) receiveImport(conn *agui.Conn, f importFrame) {
	reply := func(workspace string, err error) {
		frame := importedFrame{Type: frameTransferImported, TransferID: f.TransferID, OK: err == nil, Workspace: workspace}
		if err != nil {
			logger.Warn("transfer refused an incoming session", "transfer_id", f.TransferID, "error", err)
			frame.Error = err.Error()
		}
		conn.Deliver(encode(frame))
	}
	payload, err := s.assemble(f)
	if err != nil {
		reply("", err)
		return
	}
	var checkpoint agentdomain.Checkpoint
	if err := json.Unmarshal(payload, &checkpoint); err != nil {
		reply("", describe(err, "decoding the checkpoint"))
		return
	}
	if checkpoint.SchemaVersion != agentdomain.CheckpointSchemaVersion {
		reply("", fmt.Errorf("checkpoint schema %d is not the %d this daemon speaks", checkpoint.SchemaVersion, agentdomain.CheckpointSchemaVersion))
		return
	}
	if err := checkpoint.Session.Validate(); err != nil {
		reply("", err)
		return
	}
	if f.Environment != s.me {
		reply("", fmt.Errorf("this daemon is environment %q, not %q", s.me, f.Environment))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), replyTimeout)
	defer cancel()
	workspace, err := s.install(ctx, f.TransferID, checkpoint)
	reply(workspace, err)
}

func (s *Service) assemble(f importFrame) ([]byte, error) {
	s.mu.Lock()
	pending := s.imports[f.TransferID]
	delete(s.imports, f.TransferID)
	s.mu.Unlock()
	if pending == nil || len(pending.chunks) != f.Chunks {
		return nil, fmt.Errorf("the checkpoint arrived incomplete")
	}
	payload := make([]byte, 0, pending.size)
	for seq := range f.Chunks {
		chunk, ok := pending.chunks[seq]
		if !ok {
			return nil, fmt.Errorf("checkpoint chunk %d is missing", seq)
		}
		payload = append(payload, chunk...)
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != f.SHA256 {
		return nil, errors.New("the checkpoint's digest does not match")
	}
	return payload, nil
}

// install restores the checkpoint into a fresh workspace, imports its
// conversation through a worker there, opens its credentials and claims the
// lease. A failure undoes what it did.
func (s *Service) install(ctx context.Context, transferID string, checkpoint agentdomain.Checkpoint) (string, error) {
	session := checkpoint.Session
	workspace := s.workspaceFor(session)
	if err := os.RemoveAll(workspace); err != nil {
		return "", describe(err, "clearing the workspace")
	}
	if err := agentinfra.UnpackWorkspace(ctx, checkpoint.Workspace, workspace); err != nil {
		_ = os.RemoveAll(workspace)
		return "", describe(err, "restoring the workspace")
	}
	if len(checkpoint.Conversation) > 0 {
		if err := s.importConversation(ctx, workspace, session.ConversationID, checkpoint); err != nil {
			_ = os.RemoveAll(workspace)
			return "", err
		}
	}
	if len(checkpoint.SealedEnv) > 0 {
		env, err := s.keyring.Open(checkpoint.SealedEnv, transferID)
		if err != nil {
			_ = os.RemoveAll(workspace)
			return "", describe(err, "opening the credentials")
		}
		s.remember(session.ConversationID, env)
	}
	lease := agentdomain.Lease{ConversationID: session.ConversationID, Environment: s.me, Workspace: workspace, TransferID: transferID, Until: time.Now().Add(s.ownedTTL())}
	holder, ok, err := s.leases.ClaimLease(ctx, lease)
	if err != nil {
		_ = os.RemoveAll(workspace)
		return "", describe(err, "claiming the session")
	}
	if !ok {
		_ = os.RemoveAll(workspace)
		return "", fmt.Errorf("the session is owned by environment %s", holder.Environment)
	}
	s.mu.Lock()
	s.installed[transferID] = lease
	s.mu.Unlock()
	return workspace, nil
}

// importConversation stores the checkpoint's conversation through a worker in
// the restored workspace and checks it renders to the checkpoint's digest.
func (s *Service) importConversation(ctx context.Context, workspace, conversationID string, checkpoint agentdomain.Checkpoint) error {
	var conversation map[string]json.RawMessage
	if err := json.Unmarshal(checkpoint.Conversation, &conversation); err != nil {
		return describe(err, "decoding the conversation")
	}
	request := encode(map[string]any{
		"type": frameImportConversation, "project_dir": workspace, "id": conversationID,
		"entries": conversation["entries"], "metadata": conversation["metadata"],
	})
	reply, err := s.registry.Request(ctx, request, frameConversationImported)
	if err != nil {
		return describe(err, "importing the conversation")
	}
	var imported importedConversation
	if json.Unmarshal(reply, &imported) != nil || imported.Error != "" {
		return errors.New("importing the conversation: " + imported.Error)
	}
	if imported.Digest != checkpoint.Digest {
		return errors.New("the imported conversation does not render to the checkpoint's digest")
	}
	return nil
}

// abort undoes an import whose transfer failed afterwards.
func (s *Service) abort(transferID string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s.mu.Lock()
	delete(s.imports, transferID)
	lease, installed := s.installed[transferID]
	delete(s.installed, transferID)
	s.mu.Unlock()
	if !installed {
		return
	}
	s.remember(lease.ConversationID, nil)
	_ = os.RemoveAll(lease.Workspace)
	_ = s.leases.ReleaseLease(ctx, lease.ConversationID, s.me)
}
