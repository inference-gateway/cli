package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// JsonlStorage implements ConversationStorage using JSONL files
type JsonlStorage struct {
	basePath string
	mu       sync.RWMutex
}

// NewJsonlStorage creates a new JSONL storage instance
func NewJsonlStorage(config JsonlStorageConfig) (*JsonlStorage, error) {
	path := config.Path
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create conversations directory: %w", err)
	}

	testFile := filepath.Join(path, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return nil, fmt.Errorf("conversations directory not writable: %w", err)
	}
	_ = os.Remove(testFile)

	return &JsonlStorage{
		basePath: path,
	}, nil
}

// conversationFilePath returns the path to a conversation's JSONL file
func (s *JsonlStorage) conversationFilePath(conversationID string) string {
	return filepath.Join(s.basePath, conversationID+".jsonl")
}

// saveConversationUnlocked saves a conversation without acquiring the lock
// Caller must hold the lock before calling this method
func (s *JsonlStorage) saveConversationUnlocked(_ context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
	metadataJSON, err := json.Marshal(map[string]any{"metadata": metadata})
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	entriesJSON, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		return fmt.Errorf("failed to marshal entries: %w", err)
	}

	filePath := s.conversationFilePath(conversationID)
	tempPath := filePath + ".tmp"

	file, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	writer := bufio.NewWriter(file)
	if _, err := writer.Write(metadataJSON); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write metadata: %w", err)
	}
	if _, err := writer.WriteString("\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write newline: %w", err)
	}
	if _, err := writer.Write(entriesJSON); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write entries: %w", err)
	}
	if _, err := writer.WriteString("\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write final newline: %w", err)
	}

	if err := writer.Flush(); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// SaveConversation saves a conversation to a JSONL file
func (s *JsonlStorage) SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveConversationUnlocked(ctx, conversationID, entries, metadata)
}

// LoadConversation loads a conversation from a JSONL file
func (s *JsonlStorage) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filePath := s.conversationFilePath(conversationID)
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ConversationMetadata{}, fmt.Errorf("conversation not found: %s", conversationID)
		}
		return nil, ConversationMetadata{}, fmt.Errorf("failed to open conversation file: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, ConversationMetadata{}, fmt.Errorf("failed to read metadata line: %w", err)
		}
		return nil, ConversationMetadata{}, fmt.Errorf("failed to read metadata line: empty file")
	}
	var metadataWrapper struct {
		Metadata ConversationMetadata `json:"metadata"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &metadataWrapper); err != nil {
		return nil, ConversationMetadata{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, ConversationMetadata{}, fmt.Errorf("failed to read entries line: %w", err)
		}
		return nil, ConversationMetadata{}, fmt.Errorf("failed to read entries line: missing entries")
	}
	var entriesWrapper struct {
		Entries []domain.ConversationEntry `json:"entries"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &entriesWrapper); err != nil {
		return nil, ConversationMetadata{}, fmt.Errorf("failed to unmarshal entries: %w", err)
	}

	return entriesWrapper.Entries, metadataWrapper.Metadata, nil
}

// ListConversations returns a list of conversation summaries
func (s *JsonlStorage) ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read conversations directory: %w", err)
	}

	var summaries []ConversationSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		conversationID := strings.TrimSuffix(entry.Name(), ".jsonl")
		filePath := s.conversationFilePath(conversationID)

		file, err := os.Open(filePath)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(file)
		if scanner.Scan() {
			var metadataWrapper struct {
				Metadata ConversationMetadata `json:"metadata"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &metadataWrapper); err == nil {
				summaries = append(summaries, ConversationSummary{
					ID:                  metadataWrapper.Metadata.ID,
					Title:               metadataWrapper.Metadata.Title,
					CreatedAt:           metadataWrapper.Metadata.CreatedAt,
					UpdatedAt:           metadataWrapper.Metadata.UpdatedAt,
					MessageCount:        metadataWrapper.Metadata.MessageCount,
					TokenStats:          metadataWrapper.Metadata.TokenStats,
					CostStats:           metadataWrapper.Metadata.CostStats,
					Model:               metadataWrapper.Metadata.Model,
					Tags:                metadataWrapper.Metadata.Tags,
					TitleGenerated:      metadataWrapper.Metadata.TitleGenerated,
					TitleInvalidated:    metadataWrapper.Metadata.TitleInvalidated,
					TitleGenerationTime: metadataWrapper.Metadata.TitleGenerationTime,
				})
			}
		}
		_ = file.Close()
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	if offset >= len(summaries) {
		return []ConversationSummary{}, nil
	}
	summaries = summaries[offset:]
	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
	}

	return summaries, nil
}

// ListConversationsNeedingTitles returns conversations that need title generation
func (s *JsonlStorage) ListConversationsNeedingTitles(ctx context.Context, limit int) ([]ConversationSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allSummaries, err := s.ListConversations(ctx, 0, 0)
	if err != nil {
		return nil, err
	}

	var needingTitles []ConversationSummary
	for _, summary := range allSummaries {
		if (!summary.TitleGenerated || summary.TitleInvalidated) && summary.MessageCount >= 2 {
			needingTitles = append(needingTitles, summary)
		}
	}

	if limit > 0 && len(needingTitles) > limit {
		needingTitles = needingTitles[:limit]
	}

	return needingTitles, nil
}

// DeleteConversation removes a conversation file
func (s *JsonlStorage) DeleteConversation(ctx context.Context, conversationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := s.conversationFilePath(conversationID)
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("conversation not found: %s", conversationID)
		}
		return fmt.Errorf("failed to delete conversation: %w", err)
	}

	return nil
}

// UpdateConversationMetadata updates only the metadata of a conversation
func (s *JsonlStorage) UpdateConversationMetadata(ctx context.Context, conversationID string, metadata ConversationMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := s.conversationFilePath(conversationID)
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("conversation not found: %s", conversationID)
		}
		return fmt.Errorf("failed to open conversation file: %w", err)
	}

	scanner := bufio.NewScanner(file)

	if !scanner.Scan() {
		_ = file.Close()
		return fmt.Errorf("failed to read metadata line")
	}

	if !scanner.Scan() {
		_ = file.Close()
		return fmt.Errorf("failed to read entries line")
	}
	entriesLine := make([]byte, len(scanner.Bytes()))
	copy(entriesLine, scanner.Bytes())
	_ = file.Close()

	var entriesWrapper struct {
		Entries []domain.ConversationEntry `json:"entries"`
	}
	if err := json.Unmarshal(entriesLine, &entriesWrapper); err != nil {
		return fmt.Errorf("failed to unmarshal entries: %w", err)
	}

	metadataJSON, err := json.Marshal(map[string]any{"metadata": metadata})
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	entriesJSON, err := json.Marshal(map[string]any{"entries": entriesWrapper.Entries})
	if err != nil {
		return fmt.Errorf("failed to marshal entries: %w", err)
	}

	tempPath := filePath + ".tmp"
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	writer := bufio.NewWriter(tempFile)
	if _, err := writer.Write(metadataJSON); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write metadata: %w", err)
	}
	if _, err := writer.WriteString("\n"); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write newline: %w", err)
	}
	if _, err := writer.Write(entriesJSON); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write entries: %w", err)
	}
	if _, err := writer.WriteString("\n"); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write final newline: %w", err)
	}

	if err := writer.Flush(); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// Close closes the storage (no-op for JSONL)
func (s *JsonlStorage) Close() error {
	return nil
}

// Health checks if the storage is accessible
func (s *JsonlStorage) Health(ctx context.Context) error {
	if _, err := os.Stat(s.basePath); err != nil {
		return fmt.Errorf("conversations directory not accessible: %w", err)
	}

	testFile := filepath.Join(s.basePath, ".health_check")
	if err := os.WriteFile(testFile, []byte("health check"), 0644); err != nil {
		return fmt.Errorf("conversations directory not writable: %w", err)
	}
	_ = os.Remove(testFile)

	return nil
}
