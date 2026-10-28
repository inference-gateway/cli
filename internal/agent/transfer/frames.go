package transfer

import (
	"encoding/json"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// The frames of the executors and transfers vocabulary. Clients send the
// first group to their daemon, peers exchange the second over a daemon
// connection, and the third goes back to clients.
const (
	frameListExecutors   = "list_executors"
	frameTransferSession = "transfer_session"
	frameTransferCancel  = "transfer_cancel"

	frameTransferChunk    = "transfer_chunk"
	frameTransferImport   = "transfer_import"
	frameTransferImported = "transfer_imported"
	frameTransferAbort    = "transfer_abort"

	frameExecutors        = "executors"
	frameTransferProgress = "transfer_progress"
	frameSessionMoved     = "session_moved"

	frameResumeConversation   = "resume_conversation"
	frameMessagesSnapshot     = "MESSAGES_SNAPSHOT"
	frameRunError             = "RUN_ERROR"
	frameExportConversation   = "export_conversation"
	frameConversationExport   = "conversation_export"
	frameImportConversation   = "import_conversation"
	frameConversationImported = "conversation_imported"
)

// clientFrame is what a client's transfer frame declares.
type clientFrame struct {
	Type       string   `json:"type"`
	TransferID string   `json:"transfer_id"`
	ProjectDir string   `json:"project_dir"`
	ID         string   `json:"id"`
	Target     string   `json:"target"`
	Files      []string `json:"files"`
}

type executorsFrame struct {
	Type      string                       `json:"type"`
	Executors []agentdomain.ExecutorStatus `json:"executors"`
}

type progressFrame struct {
	Type string `json:"type"`
	agentdomain.TransferProgress
}

type sessionMovedFrame struct {
	Type string `json:"type"`
	agentdomain.SessionMoved
}

// chunkFrame carries one slice of a checkpoint payload.
type chunkFrame struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	Seq        int    `json:"seq"`
	Data       []byte `json:"data"`
}

// importFrame closes a chunked checkpoint: how many chunks, their digest, and
// which executor the sender believes it is talking to.
type importFrame struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	Executor   string `json:"executor"`
	Chunks     int    `json:"chunks"`
	SHA256     string `json:"sha256"`
}

type importedFrame struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
	OK         bool   `json:"ok"`
	Workspace  string `json:"workspace,omitempty"`
	Error      string `json:"error,omitempty"`
}

type abortFrame struct {
	Type       string `json:"type"`
	TransferID string `json:"transfer_id"`
}

// exportedConversation is the panel's conversation_export reply as the
// checkpoint carries it: the stored entries and metadata.
type exportedConversation struct {
	Type         string          `json:"type"`
	ID           string          `json:"id"`
	Entries      json.RawMessage `json:"entries"`
	Metadata     json.RawMessage `json:"metadata"`
	MessageCount int             `json:"message_count"`
	Digest       string          `json:"digest"`
	Error        string          `json:"error"`
}

type importedConversation struct {
	Type   string `json:"type"`
	Digest string `json:"digest"`
	Error  string `json:"error"`
}

func encode(frame any) []byte {
	data, _ := json.Marshal(frame)
	return data
}

func frameType(frame []byte) string {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(frame, &head)
	return head.Type
}
