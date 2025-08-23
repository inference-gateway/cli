package filewriter

import (
	"context"
	"io"
)

// WriteRequest represents a file write operation request
type WriteRequest struct {
	Path      string
	Content   string
	Overwrite bool
	Backup    bool
}

// WriteResult represents the result of a file write operation
type WriteResult struct {
	Path       string
	BytesWritten int64
	BackupPath string
	Created    bool
}

// ChunkWriteRequest represents a chunk write operation
type ChunkWriteRequest struct {
	SessionID  string
	ChunkIndex int
	Data       []byte
	IsLast     bool
}

// FileWriter defines the interface for file writing operations
type FileWriter interface {
	Write(ctx context.Context, req WriteRequest) (*WriteResult, error)
	ValidatePath(path string) error
}

// ChunkManager defines the interface for handling chunked file writes
type ChunkManager interface {
	WriteChunk(ctx context.Context, req ChunkWriteRequest) error
	FinalizeChunks(ctx context.Context, sessionID string, targetPath string) (*WriteResult, error)
	CleanupSession(sessionID string) error
	GetSessionInfo(sessionID string) (*ChunkSessionInfo, error)
}

// ChunkSessionInfo provides information about an active chunk session
type ChunkSessionInfo struct {
	SessionID    string
	TotalChunks  int
	ReceivedChunks int
	TempPath     string
	Created      bool
}

// PathValidator validates file paths for security and accessibility
type PathValidator interface {
	Validate(path string) error
	IsWritable(path string) bool
	IsInSandbox(path string) bool
}

// BackupManager handles file backup operations
type BackupManager interface {
	CreateBackup(ctx context.Context, originalPath string) (string, error)
	RestoreBackup(ctx context.Context, backupPath string, originalPath string) error
	CleanupBackup(backupPath string) error
}

// StreamWriter provides streaming write capabilities
type StreamWriter interface {
	io.Writer
	io.Closer
	Sync() error
	Path() string
}
