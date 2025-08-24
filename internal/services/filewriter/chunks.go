package filewriter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/inference-gateway/cli/internal/domain/filewriter"
)

// ChunkSession represents an active chunked writing session
type ChunkSession struct {
	sessionID      string
	tempFile       *os.File
	writer         *bufio.Writer
	expectedChunks int
	receivedChunks int
	mutex          sync.Mutex
	tempPath       string
}

// StreamingChunkManager implements filewriter.ChunkManager with safe streaming
type StreamingChunkManager struct {
	sessions map[string]*ChunkSession
	mutex    sync.RWMutex
	tempDir  string
	writer   filewriter.FileWriter
}

// NewStreamingChunkManager creates a new StreamingChunkManager
func NewStreamingChunkManager(tempDir string, writer filewriter.FileWriter) filewriter.ChunkManager {
	return &StreamingChunkManager{
		sessions: make(map[string]*ChunkSession),
		tempDir:  tempDir,
		writer:   writer,
	}
}

// WriteChunk writes a chunk to the session's temp file
func (cm *StreamingChunkManager) WriteChunk(ctx context.Context, req filewriter.ChunkWriteRequest) error {
	cm.mutex.Lock()
	session, exists := cm.sessions[req.SessionID]
	if !exists {
		var err error
		session, err = cm.createSession(req.SessionID)
		if err != nil {
			cm.mutex.Unlock()
			return fmt.Errorf("failed to create session: %w", err)
		}
		cm.sessions[req.SessionID] = session
	}
	cm.mutex.Unlock()

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if req.ChunkIndex != session.receivedChunks {
		return fmt.Errorf("chunk index mismatch: expected %d, got %d", session.receivedChunks, req.ChunkIndex)
	}

	if _, err := session.writer.Write(req.Data); err != nil {
		return fmt.Errorf("failed to write chunk data: %w", err)
	}

	if err := session.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush chunk data: %w", err)
	}

	session.receivedChunks++

	if req.IsLast {
		session.expectedChunks = req.ChunkIndex + 1
	}

	return nil
}

// FinalizeChunks completes a chunked write session and moves to target location
func (cm *StreamingChunkManager) FinalizeChunks(ctx context.Context, sessionID string, targetPath string) (*filewriter.WriteResult, error) {
	cm.mutex.Lock()
	session, exists := cm.sessions[sessionID]
	if !exists {
		cm.mutex.Unlock()
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	delete(cm.sessions, sessionID)
	cm.mutex.Unlock()

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if session.expectedChunks > 0 && session.receivedChunks != session.expectedChunks {
		return nil, fmt.Errorf("incomplete session: expected %d chunks, received %d", session.expectedChunks, session.receivedChunks)
	}

	if err := session.writer.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush session data: %w", err)
	}

	if err := session.tempFile.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := session.tempFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	tempInfo, err := os.Stat(session.tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat temp file: %w", err)
	}

	tempContent, err := os.ReadFile(session.tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp file content: %w", err)
	}

	defer func() { _ = os.Remove(session.tempPath) }()

	writeReq := filewriter.WriteRequest{
		Path:      targetPath,
		Content:   string(tempContent),
		Overwrite: true,
		Backup:    false,
	}

	result, err := cm.writer.Write(ctx, writeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to write finalized content: %w", err)
	}

	result.BytesWritten = tempInfo.Size()

	return result, nil
}

// CleanupSession removes an active session and cleans up resources
func (cm *StreamingChunkManager) CleanupSession(sessionID string) error {
	cm.mutex.Lock()
	session, exists := cm.sessions[sessionID]
	if !exists {
		cm.mutex.Unlock()
		return nil
	}
	delete(cm.sessions, sessionID)
	cm.mutex.Unlock()

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if session.tempFile != nil {
		_ = session.tempFile.Close()
	}

	if session.tempPath != "" {
		_ = os.Remove(session.tempPath)
	}

	return nil
}

// GetSessionInfo returns information about an active session
func (cm *StreamingChunkManager) GetSessionInfo(sessionID string) (*filewriter.ChunkSessionInfo, error) {
	cm.mutex.RLock()
	session, exists := cm.sessions[sessionID]
	cm.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	return &filewriter.ChunkSessionInfo{
		SessionID:      sessionID,
		TotalChunks:    session.expectedChunks,
		ReceivedChunks: session.receivedChunks,
		TempPath:       session.tempPath,
		Created:        true,
	}, nil
}

// createSession creates a new chunk session
func (cm *StreamingChunkManager) createSession(sessionID string) (*ChunkSession, error) {
	if err := os.MkdirAll(cm.tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	tempFile, err := os.CreateTemp(cm.tempDir, fmt.Sprintf("chunk_session_%s_", sessionID))
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	return &ChunkSession{
		sessionID:      sessionID,
		tempFile:       tempFile,
		writer:         bufio.NewWriter(tempFile),
		expectedChunks: -1,
		receivedChunks: 0,
		tempPath:       tempFile.Name(),
	}, nil
}
