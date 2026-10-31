package headless

import (
	"context"
	"encoding/json"
	"errors"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	projects "github.com/inference-gateway/cli/internal/projects"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// The panel frames a session teleport uses to teleport a conversation between
// storage backends: the worker exports from and imports into the store of the
// project it runs in, so per-project storage layouts resolve.
const (
	inboundExportConversation = "export_conversation"
	inboundImportConversation = "import_conversation"

	outboundConversationExport   = "conversation_export"
	outboundConversationImported = "conversation_imported"
)

var errNoConversationStorage = errors.New("the worker has no conversation storage")

// conversationExportFrame answers export_conversation with the stored entries
// and metadata plus the digest of the snapshot they render to.
type conversationExportFrame struct {
	Type         string                          `json:"type"`
	ID           string                          `json:"id"`
	Entries      []convdomain.ConversationEntry  `json:"entries,omitempty"`
	Metadata     convdomain.ConversationMetadata `json:"metadata,omitzero"`
	MessageCount int                             `json:"message_count"`
	Digest       string                          `json:"digest,omitempty"`
	Error        string                          `json:"error,omitempty"`
}

// importConversationFrame carries the entries and metadata to store under id.
type importConversationFrame struct {
	ID       string                          `json:"id"`
	Entries  []convdomain.ConversationEntry  `json:"entries"`
	Metadata convdomain.ConversationMetadata `json:"metadata"`
}

// conversationImportedFrame answers import_conversation with what was stored.
type conversationImportedFrame struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	MessageCount int    `json:"message_count"`
	Digest       string `json:"digest,omitempty"`
	Error        string `json:"error,omitempty"`
}

// teleports answers the conversation export and import frames from the
// worker's conversation storage.
type teleports struct {
	write frameWriter
	store storage.ConversationStorage
}

// export answers export_conversation with the conversation as stored. A chat
// with no message sent has nothing stored yet and exports as empty, so a fresh
// session still teleports.
func (t *teleports) export(id string) {
	if t.store == nil {
		t.write(conversationExportFrame{Type: outboundConversationExport, ID: id, Error: errNoConversationStorage.Error()})
		return
	}
	entries, metadata, err := t.store.LoadConversation(context.Background(), id)
	if err != nil && !errors.Is(err, convdomain.ErrConversationNotFound) {
		t.write(conversationExportFrame{Type: outboundConversationExport, ID: id, Error: err.Error()})
		return
	}
	messages := snapshotMessages(entries)
	t.write(conversationExportFrame{
		Type: outboundConversationExport, ID: id, Entries: entries, Metadata: metadata,
		MessageCount: len(messages), Digest: agui.SnapshotDigest(messages),
	})
}

// importFrom stores the conversation an import_conversation line carries under
// this worker's project, so the project's listings include it.
func (t *teleports) importFrom(line []byte) {
	var frame importConversationFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		t.write(conversationImportedFrame{Type: outboundConversationImported, Error: err.Error()})
		return
	}
	if t.store == nil {
		t.write(conversationImportedFrame{Type: outboundConversationImported, ID: frame.ID, Error: errNoConversationStorage.Error()})
		return
	}
	frame.Metadata.ID = frame.ID
	frame.Metadata.Project = projects.Path()
	frame.Metadata.MessageCount = len(frame.Entries)
	if err := t.save(frame); err != nil {
		t.write(conversationImportedFrame{Type: outboundConversationImported, ID: frame.ID, Error: err.Error()})
		return
	}
	messages := snapshotMessages(frame.Entries)
	t.write(conversationImportedFrame{
		Type: outboundConversationImported, ID: frame.ID, MessageCount: len(messages), Digest: agui.SnapshotDigest(messages),
	})
}

// save stores an imported conversation. An empty one stores nothing, so the
// listings never show a conversation with no message. Its first message does.
func (t *teleports) save(frame importConversationFrame) error {
	if len(frame.Entries) == 0 {
		return nil
	}
	return t.store.SaveConversation(context.Background(), frame.ID, frame.Entries, frame.Metadata)
}
