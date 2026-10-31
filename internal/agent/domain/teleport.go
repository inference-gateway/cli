package domain

import "encoding/json"

// CheckpointSchemaVersion is the shape of Checkpoint a receiving daemon
// understands. A mismatch refuses the teleport before anything is restored.
const CheckpointSchemaVersion = 2

// TeleportStage is one step of a teleport as reported to its requester.
type TeleportStage string

const (
	TeleportProvisioning  TeleportStage = "provisioning"
	TeleportCheckpointing TeleportStage = "checkpointing"
	TeleportRestoring     TeleportStage = "restoring"
	TeleportReady         TeleportStage = "ready"
	TeleportFailed        TeleportStage = "failed"
)

// Repository is the project's git state: a bundle of HEAD, a binary patch of
// the uncommitted changes, a tarball of the untracked files that respect
// .gitignore and the sandbox's denied paths, and the explicitly listed extra
// files. Branch is the branch the restore checks out again, and Origin the
// credential-free origin remote the restored clone pushes to.
type Repository struct {
	Branch    string `json:"branch"`
	Origin    string `json:"origin,omitempty"`
	Bundle    []byte `json:"bundle,omitempty"`
	Patch     []byte `json:"patch,omitempty"`
	Untracked []byte `json:"untracked,omitempty"`
	Extra     []byte `json:"extra,omitempty"`
}

// Checkpoint is everything a session needs to continue elsewhere: its
// conversation unless both workspaces share a store, the interrupts its last
// run was suspended on, the repository, and the credentials sealed to the
// target. SchemaVersion and Digest let the target validate before it claims.
type Checkpoint struct {
	SchemaVersion int             `json:"schema_version"`
	Session       Session         `json:"session"`
	MessageCount  int             `json:"message_count"`
	Digest        string          `json:"digest"`
	Conversation  json.RawMessage `json:"conversation,omitempty"`
	Interrupts    json.RawMessage `json:"interrupts,omitempty"`
	Repository    Repository      `json:"repository"`
	SealedEnv     []byte          `json:"sealed_env,omitempty"`
}

// TeleportRequest is what a client asks for: teleport the session to the named
// workspace, carrying the listed extra files. TeleportID makes retries
// idempotent.
type TeleportRequest struct {
	TeleportID string   `json:"teleport_id"`
	Session    Session  `json:"session"`
	Target     string   `json:"target"`
	Files      []string `json:"files,omitempty"`
}

// TeleportProgress reports one stage of a teleport to its requester.
type TeleportProgress struct {
	TeleportID string        `json:"teleport_id"`
	Stage      TeleportStage `json:"stage"`
	Message    string        `json:"message,omitempty"`
}

// SessionTeleported tells a thread's clients where the session lives now and which
// interrupts the teleport cancelled, so they resume there.
type SessionTeleported struct {
	Session             Session         `json:"session"`
	Workspace           string          `json:"workspace"`
	Addr                string          `json:"addr,omitempty"`
	WorkspaceDir        string          `json:"workspace_dir"`
	CancelledInterrupts json.RawMessage `json:"cancelled_interrupts,omitempty"`
}
