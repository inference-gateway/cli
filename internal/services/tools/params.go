package tools

import (
	"fmt"

	"github.com/inference-gateway/cli/internal/domain/filewriter"
)

// WriteParams represents extracted parameters for write operations
type WriteParams struct {
	FilePath string
	Content  string
}

// ParameterExtractor handles centralized parameter extraction and validation
type ParameterExtractor struct{}

// NewParameterExtractor creates a new ParameterExtractor
func NewParameterExtractor() *ParameterExtractor {
	return &ParameterExtractor{}
}

// ExtractWriteParams extracts and validates write operation parameters
func (p *ParameterExtractor) ExtractWriteParams(params map[string]any) (*WriteParams, error) {
	filePath, err := p.extractString(params, "file_path", true)
	if err != nil {
		return nil, err
	}

	content, err := p.extractString(params, "content", true)
	if err != nil {
		return nil, err
	}

	return &WriteParams{
		FilePath: filePath,
		Content:  content,
	}, nil
}

// ToWriteRequest converts WriteParams to a filewriter.WriteRequest
func (p *ParameterExtractor) ToWriteRequest(params *WriteParams) filewriter.WriteRequest {
	return filewriter.WriteRequest{
		Path:      params.FilePath,
		Content:   params.Content,
		Overwrite: true,
		Backup:    false,
	}
}

// extractString extracts a string parameter
func (p *ParameterExtractor) extractString(params map[string]any, key string, required bool) (string, error) {
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
