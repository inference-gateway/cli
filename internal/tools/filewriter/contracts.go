package filewriter

// WriteRequest represents a file write operation request
type WriteRequest struct {
	Path      string
	Content   string
	Overwrite bool
	Backup    bool
}

// WriteResult represents the result of a file write operation
type WriteResult struct {
	Path         string
	BytesWritten int64
	BackupPath   string
	Created      bool
}

// ChunkWriteRequest represents a chunk write operation
type ChunkWriteRequest struct {
	SessionID  string
	ChunkIndex int
	Data       []byte
	IsLast     bool
}

// ChunkSessionInfo provides information about an active chunk session
type ChunkSessionInfo struct {
	SessionID   string
	TotalChunks int
	Created     bool
}
