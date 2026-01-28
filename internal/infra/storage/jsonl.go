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
	basePath        string
	mu              sync.RWMutex
	persistedCounts map[string]int
	persistedMutex  sync.RWMutex
}

// V2 format version constant
const jsonlFormatVersion = 2

// MetadataLine represents the first line in v2 format
type MetadataLine struct {
	Version  int                  `json:"v"`
	Type     string               `json:"type"`
	Metadata ConversationMetadata `json:"metadata"`
}

// EntryLine represents an entry line in v2 format
type EntryLine struct {
	Type  string                   `json:"type"`
	Index int                      `json:"index"`
	Entry domain.ConversationEntry `json:"entry"`
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
		basePath:        path,
		persistedCounts: make(map[string]int),
	}, nil
}

// conversationFilePath returns the path to a conversation's JSONL file
func (s *JsonlStorage) conversationFilePath(conversationID string) string {
	return filepath.Join(s.basePath, conversationID+".jsonl")
}

// saveConversationUnlocked saves a conversation without acquiring the lock
// Caller must hold the lock before calling this method
// Uses append-only optimization: only new entries are appended to existing v2 files
func (s *JsonlStorage) saveConversationUnlocked(_ context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
	filePath := s.conversationFilePath(conversationID)

	state := s.detectFileState(filePath)

	cachedCount, hasCached := s.getPersistedCount(conversationID)

	persistedCount := 0
	if state.exists && state.isV2 {
		if hasCached {
			persistedCount = cachedCount
		} else {
			persistedCount = state.persistedCount
		}
	}

	needsFullRewrite := !state.exists ||
		!state.isV2 ||
		len(entries) < persistedCount

	if needsFullRewrite {
		if err := s.writeFullFileV2(filePath, entries, metadata); err != nil {
			return err
		}
		s.setPersistedCount(conversationID, len(entries))
		return nil
	}

	if len(entries) > persistedCount {
		newEntries := entries[persistedCount:]
		if err := s.appendEntries(filePath, newEntries, persistedCount, metadata); err != nil {
			return err
		}
		s.setPersistedCount(conversationID, len(entries))
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
// Supports both v1 format (2-line: metadata + entries array) and
// v2 format (entry lines + trailing metadata, append-only)
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
			return nil, ConversationMetadata{}, fmt.Errorf("failed to read first line: %w", err)
		}
		return nil, ConversationMetadata{}, fmt.Errorf("failed to read first line: empty file")
	}

	firstLine := make([]byte, len(scanner.Bytes()))
	copy(firstLine, scanner.Bytes())

	var versionCheck struct {
		Version int `json:"v"`
	}
	if json.Unmarshal(firstLine, &versionCheck) == nil && versionCheck.Version == jsonlFormatVersion {
		if _, err := file.Seek(0, 0); err != nil {
			return nil, ConversationMetadata{}, fmt.Errorf("failed to seek to beginning: %w", err)
		}
		entries, metadata, err := s.loadV2Format(file)
		if err != nil {
			return nil, ConversationMetadata{}, err
		}
		s.setPersistedCount(conversationID, len(entries))
		return entries, metadata, nil
	}

	var metadataWrapper struct {
		Metadata ConversationMetadata `json:"metadata"`
	}
	if err := json.Unmarshal(firstLine, &metadataWrapper); err != nil {
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

// readMetadataFromFile reads metadata from a conversation file (v1 or v2 format)
// For v1: metadata is on the first line
// For v2: metadata is on the last "meta" type line
func (s *JsonlStorage) readMetadataFromFile(filePath string) (ConversationMetadata, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return ConversationMetadata{}, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	if !scanner.Scan() {
		return ConversationMetadata{}, fmt.Errorf("empty file")
	}

	firstLine := make([]byte, len(scanner.Bytes()))
	copy(firstLine, scanner.Bytes())

	if !isV2FormatLine(firstLine) {
		return s.parseV1MetadataLine(firstLine)
	}

	return s.scanV2Metadata(firstLine, scanner)
}

// parseV1MetadataLine parses metadata from a v1 format first line
func (s *JsonlStorage) parseV1MetadataLine(line []byte) (ConversationMetadata, error) {
	var metadataWrapper struct {
		Metadata ConversationMetadata `json:"metadata"`
	}
	if err := json.Unmarshal(line, &metadataWrapper); err != nil {
		return ConversationMetadata{}, err
	}
	return metadataWrapper.Metadata, nil
}

// scanV2Metadata scans a v2 format file for the last metadata line
func (s *JsonlStorage) scanV2Metadata(firstLine []byte, scanner *bufio.Scanner) (ConversationMetadata, error) {
	var lastMetadata ConversationMetadata
	hasMetadata := false

	var typeCheck struct {
		Type     string               `json:"type"`
		Metadata ConversationMetadata `json:"metadata"`
	}

	if json.Unmarshal(firstLine, &typeCheck) == nil && typeCheck.Type == "meta" {
		lastMetadata = typeCheck.Metadata
		hasMetadata = true
	}

	for scanner.Scan() {
		if json.Unmarshal(scanner.Bytes(), &typeCheck) == nil && typeCheck.Type == "meta" {
			lastMetadata = typeCheck.Metadata
			hasMetadata = true
		}
	}

	if !hasMetadata {
		return ConversationMetadata{}, fmt.Errorf("no metadata in v2 file")
	}
	return lastMetadata, nil
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

		metadata, err := s.readMetadataFromFile(filePath)
		if err != nil {
			continue
		}

		summaries = append(summaries, ConversationSummary{
			ID:                  metadata.ID,
			Title:               metadata.Title,
			CreatedAt:           metadata.CreatedAt,
			UpdatedAt:           metadata.UpdatedAt,
			MessageCount:        metadata.MessageCount,
			TokenStats:          metadata.TokenStats,
			CostStats:           metadata.CostStats,
			Model:               metadata.Model,
			Tags:                metadata.Tags,
			TitleGenerated:      metadata.TitleGenerated,
			TitleInvalidated:    metadata.TitleInvalidated,
			TitleGenerationTime: metadata.TitleGenerationTime,
		})
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

	s.clearPersistedCount(conversationID)

	return nil
}

// UpdateConversationMetadata updates only the metadata of a conversation
// For both v1 and v2 formats, this requires a full rewrite of the file
// (v2 format is used for the output regardless of input format)
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
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	if !scanner.Scan() {
		_ = file.Close()
		return fmt.Errorf("failed to read first line: empty file")
	}

	firstLine := make([]byte, len(scanner.Bytes()))
	copy(firstLine, scanner.Bytes())

	entries, err := s.loadEntriesFromFile(filePath, firstLine, scanner, file)
	if err != nil {
		return err
	}
	_ = file.Close()

	if err := s.writeFullFileV2(filePath, entries, metadata); err != nil {
		return err
	}

	s.setPersistedCount(conversationID, len(entries))
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

// fileState holds information about an existing conversation file
type fileState struct {
	exists         bool
	isV2           bool
	persistedCount int
}

// detectFileState reads the first line of a file to determine its format version
// and counts entry lines for v2 format
// V2 format supports trailing metadata: entries come first, metadata lines can appear
// anywhere but we use the last one. The first line has v:2 to indicate v2 format.
func (s *JsonlStorage) detectFileState(filePath string) fileState {
	file, err := os.Open(filePath)
	if err != nil {
		return fileState{exists: false}
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	if !scanner.Scan() {
		return fileState{exists: true, isV2: false}
	}

	firstLine := make([]byte, len(scanner.Bytes()))
	copy(firstLine, scanner.Bytes())

	if !isV2FormatLine(firstLine) {
		return fileState{exists: true, isV2: false}
	}

	entryCount := s.countV2Entries(firstLine, scanner)
	return fileState{exists: true, isV2: true, persistedCount: entryCount}
}

// countV2Entries counts entry lines in a v2 format file
func (s *JsonlStorage) countV2Entries(firstLine []byte, scanner *bufio.Scanner) int {
	entryCount := 0

	var typeCheck struct {
		Type string `json:"type"`
	}

	if json.Unmarshal(firstLine, &typeCheck) == nil && typeCheck.Type == "entry" {
		entryCount++
	}

	for scanner.Scan() {
		if json.Unmarshal(scanner.Bytes(), &typeCheck) == nil && typeCheck.Type == "entry" {
			entryCount++
		}
	}

	return entryCount
}

// V2EntryLine represents an entry line in v2 format (first entry has version)
type V2EntryLine struct {
	Version int                      `json:"v,omitempty"`
	Type    string                   `json:"type"`
	Index   int                      `json:"index"`
	Entry   domain.ConversationEntry `json:"entry"`
}

// TrailingMetaLine represents a metadata line without version (used after entries)
type TrailingMetaLine struct {
	Type     string               `json:"type"`
	Metadata ConversationMetadata `json:"metadata"`
}

// writeFullFileV2 writes a complete conversation file in v2 format
// Uses atomic write via temp file + rename
// Format: entry lines first (first entry has v:2), then trailing metadata
func (s *JsonlStorage) writeFullFileV2(filePath string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
	tempPath := filePath + ".tmp"

	file, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = file.Close()
			_ = os.Remove(tempPath)
		}
	}()

	writer := bufio.NewWriter(file)

	if err := s.writeV2Entries(writer, entries); err != nil {
		return err
	}
	if err := s.writeV2Metadata(writer, metadata, len(entries) == 0); err != nil {
		return err
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	success = true
	return nil
}

// writeV2Entries writes entry lines to the writer
func (s *JsonlStorage) writeV2Entries(writer *bufio.Writer, entries []domain.ConversationEntry) error {
	for i, entry := range entries {
		var entryJSON []byte
		var err error

		if i == 0 {
			entryJSON, err = json.Marshal(V2EntryLine{
				Version: jsonlFormatVersion,
				Type:    "entry",
				Index:   i,
				Entry:   entry,
			})
		} else {
			entryJSON, err = json.Marshal(EntryLine{
				Type:  "entry",
				Index: i,
				Entry: entry,
			})
		}

		if err != nil {
			return fmt.Errorf("failed to marshal entry %d: %w", i, err)
		}
		if _, err := writer.Write(entryJSON); err != nil {
			return fmt.Errorf("failed to write entry %d: %w", i, err)
		}
		if _, err := writer.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write newline: %w", err)
		}
	}
	return nil
}

// writeV2Metadata writes metadata line to the writer
// If includeVersion is true, the version is included (for empty entry files)
func (s *JsonlStorage) writeV2Metadata(writer *bufio.Writer, metadata ConversationMetadata, includeVersion bool) error {
	var metaJSON []byte
	var err error

	if includeVersion {
		metaJSON, err = json.Marshal(MetadataLine{
			Version:  jsonlFormatVersion,
			Type:     "meta",
			Metadata: metadata,
		})
	} else {
		metaJSON, err = json.Marshal(TrailingMetaLine{
			Type:     "meta",
			Metadata: metadata,
		})
	}

	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if _, err := writer.Write(metaJSON); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}
	if _, err := writer.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}
	return nil
}

// appendEntries appends new entries and updated metadata to an existing v2 format file
func (s *JsonlStorage) appendEntries(filePath string, entries []domain.ConversationEntry, startIndex int, metadata ConversationMetadata) error {
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for append: %w", err)
	}
	defer func() { _ = file.Close() }()

	writer := bufio.NewWriter(file)

	for i, entry := range entries {
		entryLine := EntryLine{
			Type:  "entry",
			Index: startIndex + i,
			Entry: entry,
		}
		entryJSON, err := json.Marshal(entryLine)
		if err != nil {
			return fmt.Errorf("failed to marshal entry %d: %w", startIndex+i, err)
		}
		if _, err := writer.Write(entryJSON); err != nil {
			return fmt.Errorf("failed to write entry %d: %w", startIndex+i, err)
		}
		if _, err := writer.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write newline: %w", err)
		}
	}

	metaLine := struct {
		Type     string               `json:"type"`
		Metadata ConversationMetadata `json:"metadata"`
	}{
		Type:     "meta",
		Metadata: metadata,
	}
	metaJSON, err := json.Marshal(metaLine)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if _, err := writer.Write(metaJSON); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}
	if _, err := writer.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	return nil
}

// getPersistedCount returns the cached persisted count for a conversation
func (s *JsonlStorage) getPersistedCount(conversationID string) (int, bool) {
	s.persistedMutex.RLock()
	defer s.persistedMutex.RUnlock()
	count, ok := s.persistedCounts[conversationID]
	return count, ok
}

// setPersistedCount updates the cached persisted count for a conversation
func (s *JsonlStorage) setPersistedCount(conversationID string, count int) {
	s.persistedMutex.Lock()
	defer s.persistedMutex.Unlock()
	s.persistedCounts[conversationID] = count
}

// clearPersistedCount removes the cached persisted count for a conversation
func (s *JsonlStorage) clearPersistedCount(conversationID string) {
	s.persistedMutex.Lock()
	defer s.persistedMutex.Unlock()
	delete(s.persistedCounts, conversationID)
}

// isV2FormatLine checks if the first line indicates v2 format
func isV2FormatLine(firstLine []byte) bool {
	var versionCheck struct {
		Version int `json:"v"`
	}
	if err := json.Unmarshal(firstLine, &versionCheck); err != nil {
		return false
	}
	return versionCheck.Version == jsonlFormatVersion
}

// loadEntriesFromFile loads entries from a file, handling both v1 and v2 formats
// The caller is responsible for closing the file after this function returns
func (s *JsonlStorage) loadEntriesFromFile(filePath string, firstLine []byte, scanner *bufio.Scanner, file *os.File) ([]domain.ConversationEntry, error) {
	if isV2FormatLine(firstLine) {
		_ = file.Close()
		reopenedFile, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to reopen file: %w", err)
		}
		entries, _, err := s.loadV2Format(reopenedFile)
		_ = reopenedFile.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to load v2 entries: %w", err)
		}
		return entries, nil
	}

	if !scanner.Scan() {
		return nil, fmt.Errorf("failed to read entries line")
	}
	entriesLine := make([]byte, len(scanner.Bytes()))
	copy(entriesLine, scanner.Bytes())

	var entriesWrapper struct {
		Entries []domain.ConversationEntry `json:"entries"`
	}
	if err := json.Unmarshal(entriesLine, &entriesWrapper); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entries: %w", err)
	}
	return entriesWrapper.Entries, nil
}

// loadV2Format loads a conversation in the v2 format
// Format: entry lines (first has v:2) followed by trailing metadata lines
// The last metadata line is used (supports append-only updates)
func (s *JsonlStorage) loadV2Format(file *os.File) ([]domain.ConversationEntry, ConversationMetadata, error) {
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var entries []domain.ConversationEntry
	var metadata ConversationMetadata
	hasMetadata := false

	for scanner.Scan() {
		lineBytes := scanner.Bytes()

		var typeCheck struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(lineBytes, &typeCheck); err != nil {
			return nil, ConversationMetadata{}, fmt.Errorf("failed to parse line type: %w", err)
		}

		switch typeCheck.Type {
		case "entry":
			var entryLine struct {
				Entry domain.ConversationEntry `json:"entry"`
			}
			if err := json.Unmarshal(lineBytes, &entryLine); err != nil {
				return nil, ConversationMetadata{}, fmt.Errorf("failed to unmarshal entry: %w", err)
			}
			entries = append(entries, entryLine.Entry)
		case "meta":
			var metaLine struct {
				Metadata ConversationMetadata `json:"metadata"`
			}
			if err := json.Unmarshal(lineBytes, &metaLine); err != nil {
				return nil, ConversationMetadata{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
			metadata = metaLine.Metadata
			hasMetadata = true
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, ConversationMetadata{}, fmt.Errorf("failed to read file: %w", err)
	}

	if !hasMetadata {
		return nil, ConversationMetadata{}, fmt.Errorf("no metadata found in v2 file")
	}

	return entries, metadata, nil
}
