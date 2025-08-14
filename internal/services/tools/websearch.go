package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// WebSearchTool handles web search operations
type WebSearchTool struct {
	config           *config.Config
	webSearchService domain.WebSearchService
	enabled          bool
}

// NewWebSearchTool creates a new web search tool
func NewWebSearchTool(cfg *config.Config, webSearchService domain.WebSearchService) *WebSearchTool {
	return &WebSearchTool{
		config:           cfg,
		webSearchService: webSearchService,
		enabled:          cfg.Tools.Enabled && cfg.WebSearch.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *WebSearchTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "WebSearch",
		Description: "Search the web using Google or DuckDuckGo search engines",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query to execute",
				},
				"engine": map[string]interface{}{
					"type":        "string",
					"description": fmt.Sprintf("The search engine to use (%s). %s is recommended for reliable results.", strings.Join(t.config.WebSearch.Engines, " or "), t.config.WebSearch.DefaultEngine),
					"enum":        t.config.WebSearch.Engines,
					"default":     t.config.WebSearch.DefaultEngine,
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of search results to return",
					"minimum":     1,
					"maximum":     50,
					"default":     t.config.WebSearch.MaxResults,
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Output format (text or json)",
					"enum":        []string{"text", "json"},
					"default":     "text",
				},
			},
			"required": []string{"query"},
		},
	}
}

// Execute runs the web search tool with given arguments
func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.WebSearch.Enabled {
		return nil, fmt.Errorf("web search tool is not enabled")
	}

	query, ok := args["query"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WebSearch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "query parameter is required and must be a string",
		}, nil
	}

	engine, ok := args["engine"].(string)
	if !ok {
		engine = t.config.WebSearch.DefaultEngine
	}

	var limit int
	if limitFloat, ok := args["limit"].(float64); ok {
		limit = int(limitFloat)
	} else {
		limit = t.config.WebSearch.MaxResults
	}

	var searchResult *domain.WebSearchResponse
	var err error

	switch engine {
	case "google":
		searchResult, err = t.webSearchService.SearchGoogle(ctx, query, limit)
	case "duckduckgo":
		searchResult, err = t.webSearchService.SearchDuckDuckGo(ctx, query, limit)
	default:
		return &domain.ToolExecutionResult{
			ToolName:  "WebSearch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("unsupported search engine: %s", engine),
		}, nil
	}

	success := err == nil

	result := &domain.ToolExecutionResult{
		ToolName:  "WebSearch",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data:      searchResult,
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result, nil
}

// Validate checks if the web search tool arguments are valid
func (t *WebSearchTool) Validate(args map[string]interface{}) error {
	if !t.config.WebSearch.Enabled {
		return fmt.Errorf("web search tool is not enabled")
	}

	query, ok := args["query"].(string)
	if !ok {
		return fmt.Errorf("query parameter is required and must be a string")
	}

	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("query cannot be empty")
	}

	if engine, ok := args["engine"].(string); ok {
		validEngines := make(map[string]bool)
		for _, eng := range t.config.WebSearch.Engines {
			validEngines[eng] = true
		}
		if !validEngines[engine] {
			return fmt.Errorf("unsupported search engine: %s", engine)
		}
	}

	if limitFloat, ok := args["limit"].(float64); ok {
		limit := int(limitFloat)
		if limit < 1 || limit > 50 {
			return fmt.Errorf("limit must be between 1 and 50")
		}
	}

	return nil
}

// IsEnabled returns whether the web search tool is enabled
func (t *WebSearchTool) IsEnabled() bool {
	return t.enabled
}