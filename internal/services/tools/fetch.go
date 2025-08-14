package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// FetchTool handles content fetching operations
type FetchTool struct {
	config  *config.Config
	enabled bool
}

// NewFetchTool creates a new fetch tool
func NewFetchTool(cfg *config.Config) *FetchTool {
	return &FetchTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Fetch.Enabled,
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
	if !t.config.Tools.Enabled || !t.config.Fetch.Enabled {
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

	fetchResult, err := t.fetchContent(ctx, url)
	success := err == nil

	result := &domain.ToolExecutionResult{
		ToolName:  "Fetch",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
	}

	if err != nil {
		result.Error = err.Error()
	} else {
		result.Data = fetchResult
	}

	return result, nil
}

// Validate checks if the fetch tool arguments are valid
func (t *FetchTool) Validate(args map[string]interface{}) error {
	if !t.config.Tools.Enabled || !t.config.Fetch.Enabled {
		return fmt.Errorf("fetch tool is not enabled")
	}

	url, ok := args["url"].(string)
	if !ok {
		return fmt.Errorf("url parameter is required and must be a string")
	}

	if err := t.validateURL(url); err != nil {
		return fmt.Errorf("URL validation failed: %w", err)
	}

	if format, ok := args["format"].(string); ok {
		if format != "text" && format != "json" {
			return fmt.Errorf("format must be 'text' or 'json'")
		}
	} else if args["format"] != nil {
		return fmt.Errorf("format parameter must be a string")
	}

	return nil
}

// IsEnabled returns whether the fetch tool is enabled
func (t *FetchTool) IsEnabled() bool {
	return t.enabled
}

// fetchContent is a placeholder implementation
func (t *FetchTool) fetchContent(ctx context.Context, url string) (*domain.FetchResult, error) {
	return nil, fmt.Errorf("fetch functionality not yet implemented in self-contained tool")
}

// validateURL validates URL against security rules and whitelists
func (t *FetchTool) validateURL(url string) error {
	if url == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	if strings.HasPrefix(url, "file://") || strings.HasPrefix(url, "ftp://") {
		return fmt.Errorf("protocol not allowed")
	}

	if strings.HasPrefix(url, "github:") {
		return t.validateGitHubReference(url)
	}

	return t.validateURLDomain(url)
}

// validateGitHubReference validates GitHub reference syntax (github:owner/repo#123)
func (t *FetchTool) validateGitHubReference(reference string) error {
	ref := strings.TrimPrefix(reference, "github:")

	if !strings.Contains(ref, "/") {
		return fmt.Errorf("invalid GitHub reference format")
	}

	parts := strings.Split(ref, "#")
	if len(parts) > 2 {
		return fmt.Errorf("invalid GitHub reference format")
	}

	ownerRepo := parts[0]
	if strings.Count(ownerRepo, "/") != 1 {
		return fmt.Errorf("invalid GitHub reference format")
	}

	return nil
}

// validateURLDomain checks if URL domain is in whitelist
func (t *FetchTool) validateURLDomain(url string) error {
	for _, domain := range t.config.Fetch.WhitelistedDomains {
		if strings.Contains(url, domain) {
			return nil
		}
	}

	return fmt.Errorf("domain not whitelisted")
}
