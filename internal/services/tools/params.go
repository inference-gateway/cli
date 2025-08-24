package tools

import (
	"fmt"

	"github.com/inference-gateway/cli/internal/domain/filewriter"
)

// WriteParams represents extracted parameters for write operations
type WriteParams struct {
	FilePath    string
	Content     string
	Append      bool
	Overwrite   bool
	Backup      bool
	ChunkIndex  int
	TotalChunks int
	SessionID   string
	IsChunked   bool
}

// ParameterExtractor handles centralized parameter extraction and validation
type ParameterExtractor struct{}

// NewParameterExtractor creates a new ParameterExtractor
func NewParameterExtractor() *ParameterExtractor {
	return &ParameterExtractor{}
}

// ExtractWriteParams extracts and validates write operation parameters
func (p *ParameterExtractor) ExtractWriteParams(params map[string]interface{}) (*WriteParams, error) {
	result := &WriteParams{}

	filePath, err := p.extractString(params, "file_path", true)
	if err != nil {
		return nil, err
	}
	result.FilePath = filePath

	content, err := p.extractString(params, "content", true)
	if err != nil {
		return nil, err
	}
	result.Content = content

	result.Append = p.extractBool(params, "append", false)
	result.Overwrite = p.extractBool(params, "overwrite", true)
	result.Backup = p.extractBool(params, "backup", false)

	chunkIndex := p.extractInt(params, "chunk_index", -1)
	totalChunks := p.extractInt(params, "total_chunks", -1)
	sessionID, _ := p.extractString(params, "session_id", false)

	result.IsChunked = chunkIndex >= 0 || totalChunks > 0 || sessionID != ""

	if result.IsChunked {
		if chunkIndex < 0 {
			return nil, fmt.Errorf("chunk_index is required for chunked operations")
		}
		if sessionID == "" {
			return nil, fmt.Errorf("session_id is required for chunked operations")
		}
		result.ChunkIndex = chunkIndex
		result.TotalChunks = totalChunks
		result.SessionID = sessionID
	}

	if err := p.validateParamCombinations(result); err != nil {
		return nil, err
	}

	return result, nil
}

// ToWriteRequest converts WriteParams to a filewriter.WriteRequest
func (p *ParameterExtractor) ToWriteRequest(params *WriteParams) filewriter.WriteRequest {
	return filewriter.WriteRequest{
		Path:      params.FilePath,
		Content:   params.Content,
		Overwrite: params.Overwrite,
		Backup:    params.Backup,
	}
}

// ToChunkWriteRequest converts WriteParams to a filewriter.ChunkWriteRequest
func (p *ParameterExtractor) ToChunkWriteRequest(params *WriteParams) filewriter.ChunkWriteRequest {
	isLast := params.TotalChunks > 0 && params.ChunkIndex == params.TotalChunks-1
	return filewriter.ChunkWriteRequest{
		SessionID:  params.SessionID,
		ChunkIndex: params.ChunkIndex,
		Data:       []byte(params.Content),
		IsLast:     isLast,
	}
}

// extractString extracts a string parameter
func (p *ParameterExtractor) extractString(params map[string]interface{}, key string, required bool) (string, error) {
	value, exists := params[key]
	if !exists {
		if required {
			return "", fmt.Errorf("missing required parameter: %s", key)
		}
		return "", nil
	}

	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s must be a string, got %T", key, value)
	}

	if required && str == "" {
		return "", fmt.Errorf("parameter %s cannot be empty", key)
	}

	return str, nil
}

// extractBool extracts a boolean parameter
func (p *ParameterExtractor) extractBool(params map[string]interface{}, key string, defaultValue bool) bool {
	value, exists := params[key]
	if !exists {
		return defaultValue
	}

	if b, ok := value.(bool); ok {
		return b
	}

	if str, ok := value.(string); ok {
		switch str {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}

	return defaultValue
}

// extractInt extracts an integer parameter
func (p *ParameterExtractor) extractInt(params map[string]interface{}, key string, defaultValue int) int {
	value, exists := params[key]
	if !exists {
		return defaultValue
	}

	// Handle different numeric types
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if v == "" {
			return defaultValue
		}
		result := 0
		for _, r := range v {
			if r < '0' || r > '9' {
				return defaultValue
			}
			result = result*10 + int(r-'0')
		}
		return result
	}

	return defaultValue
}

// validateParamCombinations validates parameter combinations
func (p *ParameterExtractor) validateParamCombinations(params *WriteParams) error {
	if params.Append && params.IsChunked {
		return fmt.Errorf("append mode is not supported with chunked operations")
	}

	if params.IsChunked && params.ChunkIndex < 0 {
		return fmt.Errorf("chunk_index must be non-negative")
	}

	if params.TotalChunks > 0 && params.ChunkIndex >= params.TotalChunks {
		return fmt.Errorf("chunk_index (%d) must be less than total_chunks (%d)", params.ChunkIndex, params.TotalChunks)
	}

	return nil
}
