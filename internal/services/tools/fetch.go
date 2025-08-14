package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// FetchTool handles content fetching operations
type FetchTool struct {
	config       *config.Config
	fetchService domain.FetchService
	enabled      bool
}

// NewFetchTool creates a new fetch tool
func NewFetchTool(cfg *config.Config, fetchService domain.FetchService) *FetchTool {
	return &FetchTool{
		config:       cfg,
		fetchService: fetchService,
		enabled:      cfg.Tools.Enabled && cfg.Fetch.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *FetchTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "Fetch",
		Description: "Fetch content from whitelisted URLs or GitHub references. Supports 'github:owner/repo#123' syntax for GitHub issues/PRs.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL to fetch content from, or GitHub reference (github:owner/repo#123)",
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Output format (text or json)",
					"enum":        []string{"text", "json"},
					"default":     "text",
				},
			},
			"required": []string{"url"},
		},
	}
}

// Execute runs the fetch tool with given arguments
func (t *FetchTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Fetch.Enabled {
		return nil, fmt.Errorf("fetch tool is not enabled")
	}

	url, ok := args["url"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Fetch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "url parameter is required and must be a string",
		}, nil
	}

	fetchResult, err := t.fetchService.FetchContent(ctx, url)
	success := err == nil

	result := &domain.ToolExecutionResult{
		ToolName:  "Fetch",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data:      fetchResult,
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result, nil
}

// Validate checks if the fetch tool arguments are valid
func (t *FetchTool) Validate(args map[string]interface{}) error {
	if !t.config.Fetch.Enabled {
		return fmt.Errorf("fetch tool is not enabled")
	}

	url, ok := args["url"].(string)
	if !ok {
		return fmt.Errorf("url parameter is required and must be a string")
	}

	if err := t.fetchService.ValidateURL(url); err != nil {
		return fmt.Errorf("URL validation failed: %w", err)
	}

	return nil
}

// IsEnabled returns whether the fetch tool is enabled
func (t *FetchTool) IsEnabled() bool {
	return t.enabled
}