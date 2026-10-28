package domain

import "encoding/json"

// CheckpointSchemaVersion is the shape of Checkpoint a receiving daemon
// understands. A mismatch refuses the transfer before anything is restored.
const CheckpointSchemaVersion = 1

// TransferStage is one step of a transfer as reported to its requester.
type TransferStage string

const (
	TransferProvisioning  TransferStage = "provisioning"
	TransferCheckpointing TransferStage = "checkpointing"
	TransferRestoring     TransferStage = "restoring"
	TransferReady         TransferStage = "ready"
	TransferFailed        TransferStage = "failed"
)

// Workspace is the project's git state: a bundle of HEAD, a binary patch of
// the uncommitted changes, a tarball of the untracked files that respect
// .gitignore and the sandbox's denied paths, and the explicitly listed extra
// files. Branch is the checked-out branch the restore checks out again.
type Workspace struct {
	Branch    string `json:"branch"`
	Bundle    []byte `json:"bundle,omitempty"`
	Patch     []byte `json:"patch,omitempty"`
	Untracked []byte `json:"untracked,omitempty"`
	Extra     []byte `json:"extra,omitempty"`
}

// Checkpoint is everything a session needs to continue elsewhere: its
// conversation unless both executors share a store, the interrupts its last
// run was suspended on, the workspace, and the credentials sealed to the
// target. SchemaVersion and Digest let the target validate before it claims.
type Checkpoint struct {
	SchemaVersion int             `json:"schema_version"`
	Session       Session         `json:"session"`
	MessageCount  int             `json:"message_count"`
	Digest        string          `json:"digest"`
	Conversation  json.RawMessage `json:"conversation,omitempty"`
	Interrupts    json.RawMessage `json:"interrupts,omitempty"`
	Workspace     Workspace       `json:"workspace"`
	SealedEnv     []byte          `json:"sealed_env,omitempty"`
}

// TransferRequest is what a client asks for: move the session to the named
// executor, carrying the listed extra files. TransferID makes retries
// idempotent.
type TransferRequest struct {
	TransferID string   `json:"transfer_id"`
	Session    Session  `json:"session"`
	Target     string   `json:"target"`
	Files      []string `json:"files,omitempty"`
}

// TransferProgress reports one stage of a transfer to its requester.
type TransferProgress struct {
	TransferID string        `json:"transfer_id"`
	Stage      TransferStage `json:"stage"`
	Message    string        `json:"message,omitempty"`
}

// SessionMoved tells a thread's clients where the session lives now and which
// interrupts the move cancelled, so they resume there.
type SessionMoved struct {
	Session             Session         `json:"session"`
	Executor            string          `json:"executor"`
	Addr                string          `json:"addr,omitempty"`
	Workspace           string          `json:"workspace"`
	CancelledInterrupts json.RawMessage `json:"cancelled_interrupts,omitempty"`
}
