package teleport

import (
	"encoding/json"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// The frames of the workspaces and teleports vocabulary. Clients send the
// first group to their daemon, peers exchange the second over a daemon
// connection, and the third goes back to clients.
const (
	frameListWorkspaces  = "list_workspaces"
	frameTeleportSession = "teleport_session"
	frameTeleportCancel  = "teleport_cancel"

	frameTeleportChunk     = "teleport_chunk"
	frameTeleportImport    = "teleport_import"
	frameTeleportImported  = "teleport_imported"
	frameTeleportAbort     = "teleport_abort"
	frameTeleportReturn    = "teleport_return"
	frameTeleportReturning = "teleport_returning"
	frameTeleportReturned  = "teleport_returned"

	frameWorkspaces        = "workspaces"
	frameTeleportProgress  = "teleport_progress"
	frameSessionTeleported = "session_teleported"
	frameThreadStopped     = "thread_stopped"

	frameResumeConversation   = "resume_conversation"
	frameMessagesSnapshot     = "MESSAGES_SNAPSHOT"
	frameRunError             = "RUN_ERROR"
	frameExportConversation   = "export_conversation"
	frameConversationExport   = "conversation_export"
	frameImportConversation   = "import_conversation"
	frameConversationImported = "conversation_imported"
)

// clientFrame is what a client's teleport frame declares.
type clientFrame struct {
	Type       string   `json:"type"`
	TeleportID string   `json:"teleport_id"`
	ProjectDir string   `json:"project_dir"`
	ID         string   `json:"id"`
	Target     string   `json:"target"`
	Files      []string `json:"files"`
}

type workspacesFrame struct {
	Type       string                        `json:"type"`
	Workspaces []agentdomain.WorkspaceStatus `json:"workspaces"`
}

type progressFrame struct {
	Type string `json:"type"`
	agentdomain.TeleportProgress
}

type sessionTeleportedFrame struct {
	Type string `json:"type"`
	agentdomain.SessionTeleported
}

// threadStoppedFrame answers stop_thread: the workspace whose runtime of the
// session was deleted, empty when it ran here, or why it could not be.
type threadStoppedFrame struct {
	Type       string `json:"type"`
	ProjectDir string `json:"project_dir"`
	ID         string `json:"id"`
	Workspace  string `json:"workspace,omitempty"`
	Error      string `json:"error,omitempty"`
}

// chunkFrame carries one slice of a checkpoint payload.
type chunkFrame struct {
	Type       string `json:"type"`
	TeleportID string `json:"teleport_id"`
	Seq        int    `json:"seq"`
	Data       []byte `json:"data"`
}

// importFrame closes a chunked checkpoint: how many chunks, their digest, and
// which workspace the sender believes it is talking to.
type importFrame struct {
	Type       string `json:"type"`
	TeleportID string `json:"teleport_id"`
	Workspace  string `json:"workspace"`
	Chunks     int    `json:"chunks"`
	SHA256     string `json:"sha256"`
}

type importedFrame struct {
	Type         string `json:"type"`
	TeleportID   string `json:"teleport_id"`
	OK           bool   `json:"ok"`
	WorkspaceDir string `json:"workspace_dir,omitempty"`
	Error        string `json:"error,omitempty"`
}

type abortFrame struct {
	Type       string `json:"type"`
	TeleportID string `json:"teleport_id"`
}

// returnFrame asks the daemon that owns a session to pack it and stream it
// back to Workspace, the daemon it came from. The owner answers
// teleport_returning at once, and teleport_returned, carrying only the
// teleport id, tells it the session landed.
type returnFrame struct {
	Type         string `json:"type"`
	TeleportID   string `json:"teleport_id"`
	ID           string `json:"id"`
	WorkspaceDir string `json:"workspace_dir,omitempty"`
	Workspace    string `json:"workspace"`
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
