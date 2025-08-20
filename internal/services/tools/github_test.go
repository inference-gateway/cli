package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

func TestGithubTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGithubTool(cfg)
	def := tool.Definition()

	if def.Name != "Github" {
		t.Errorf("Expected tool name 'Github', got %s", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestGithubTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name          string
		toolsEnabled  bool
		githubEnabled bool
		expectedState bool
	}{
		{
			name:          "enabled when both tools and github enabled",
			toolsEnabled:  true,
			githubEnabled: true,
			expectedState: true,
		},
		{
			name:          "disabled when tools disabled",
			toolsEnabled:  false,
			githubEnabled: true,
			expectedState: false,
		},
		{
			name:          "disabled when github disabled",
			toolsEnabled:  true,
			githubEnabled: false,
			expectedState: false,
		},
		{
			name:          "disabled when both disabled",
			toolsEnabled:  false,
			githubEnabled: false,
			expectedState: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
					Github: config.GithubToolConfig{
						Enabled: tt.githubEnabled,
					},
				},
			}

			tool := NewGithubTool(cfg)
			if tool.IsEnabled() != tt.expectedState {
				t.Errorf("Expected IsEnabled() = %v, got %v", tt.expectedState, tool.IsEnabled())
			}
		})
	}
}

func TestGithubTool_Validate_ValidCases(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGithubTool(cfg)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "valid issue fetch",
			args: map[string]any{
				"owner":        "testowner",
				"repo":         "testrepo",
				"resource":     "issue",
				"issue_number": float64(123),
			},
		},
		{
			name: "valid issues list",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "issues",
				"state":    "open",
				"per_page": float64(10),
			},
		},
		{
			name: "valid issue fetch with string number",
			args: map[string]any{
				"owner":        "testowner",
				"repo":         "testrepo",
				"resource":     "issue",
				"issue_number": "123",
			},
		},
		{
			name: "valid issue fetch with GitHub reference",
			args: map[string]any{
				"owner":        "testowner",
				"repo":         "testrepo",
				"resource":     "issue",
				"issue_number": "#123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if err != nil {
				t.Errorf("Validate() error = %v, expected no error", err)
			}
		})
	}
}

func TestGithubTool_Validate_InvalidCases(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGithubTool(cfg)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "missing owner",
			args: map[string]any{"repo": "testrepo"},
		},
		{
			name: "missing repo",
			args: map[string]any{"owner": "testowner"},
		},
		{
			name: "empty owner",
			args: map[string]any{
				"owner": "",
				"repo":  "testrepo",
			},
		},
		{
			name: "invalid resource",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "invalid",
			},
		},
		{
			name: "per_page too large",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "issues",
				"per_page": float64(101),
			},
		},
		{
			name: "invalid issue_number string",
			args: map[string]any{
				"owner":        "testowner",
				"repo":         "testrepo",
				"resource":     "issue",
				"issue_number": "not-a-number",
			},
		},
		{
			name: "invalid issue_number type",
			args: map[string]any{
				"owner":        "testowner",
				"repo":         "testrepo",
				"resource":     "issue",
				"issue_number": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if err == nil {
				t.Errorf("Validate() expected error but got none")
			}
		})
	}
}

func TestGithubTool_Execute_Disabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
			Github: config.GithubToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGithubTool(cfg)
	ctx := context.Background()

	args := map[string]any{
		"owner":        "testowner",
		"repo":         "testrepo",
		"resource":     "issue",
		"issue_number": float64(123),
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when tool is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when tool is disabled")
	}
}

func TestGithubTool_Execute_GithubDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: false,
			},
		},
	}

	tool := NewGithubTool(cfg)
	ctx := context.Background()

	args := map[string]any{
		"owner":        "testowner",
		"repo":         "testrepo",
		"resource":     "issue",
		"issue_number": float64(123),
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when GitHub tool is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when GitHub tool is disabled")
	}
}

func TestGithubTool_Execute_InvalidArgs(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
				BaseURL: "https://api.github.com",
				Safety: config.GithubSafetyConfig{
					MaxSize: 1048576,
					Timeout: 30,
				},
			},
		},
	}

	tool := NewGithubTool(cfg)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "missing owner",
			args: map[string]any{
				"repo":         "testrepo",
				"resource":     "issue",
				"issue_number": float64(123),
			},
		},
		{
			name: "missing repo",
			args: map[string]any{
				"owner":        "testowner",
				"resource":     "issue",
				"issue_number": float64(123),
			},
		},
		{
			name: "missing issue_number for issue",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "issue",
			},
		},
		{
			name: "unsupported resource",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "unsupported",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(ctx, tt.args)
			if err != nil {
				t.Fatal("Execute should not return error for invalid args, should return failed result")
			}

			if result == nil {
				t.Fatal("Expected result object")
			}

			if result.Success {
				t.Error("Expected Success = false for invalid args")
			}

			if result.Error == "" {
				t.Error("Expected error message for invalid args")
			}
		})
	}
}

func TestGithubTool_Execute_Success(t *testing.T) {
	// Create mock GitHub API server
	mockIssue := domain.GitHubIssue{
		ID:     123456,
		Number: 123,
		Title:  "Test Issue",
		Body:   "This is a test issue",
		State:  "open",
		User: domain.GitHubUser{
			ID:    789,
			Login: "testuser",
		},
		Comments:  5,
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now().Add(-time.Minute),
		HTMLURL:   "https://github.com/testowner/testrepo/issues/123",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/testowner/testrepo/issues/123" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockIssue)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
				BaseURL: server.URL,
				Safety: config.GithubSafetyConfig{
					MaxSize: 1048576,
					Timeout: 30,
				},
			},
		},
	}

	tool := NewGithubTool(cfg)
	ctx := context.Background()

	args := map[string]any{
		"owner":        "testowner",
		"repo":         "testrepo",
		"resource":     "issue",
		"issue_number": float64(123),
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if result == nil {
		t.Fatal("Expected result object")
	}

	if !result.Success {
		t.Errorf("Expected Success = true, got false with error: %s", result.Error)
	}

	if result.Data == nil {
		t.Error("Expected data to be present")
	}

	// Verify the result data
	issue, ok := result.Data.(*domain.GitHubIssue)
	if !ok {
		t.Error("Expected data to be a GitHubIssue")
	} else {
		if issue.Number != 123 {
			t.Errorf("Expected issue number 123, got %d", issue.Number)
		}
		if issue.Title != "Test Issue" {
			t.Errorf("Expected title 'Test Issue', got %s", issue.Title)
		}
	}
}

func TestGithubTool_Execute_APIError(t *testing.T) {
	// Create mock server that returns API error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "Not Found",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
				BaseURL: server.URL,
				Safety: config.GithubSafetyConfig{
					MaxSize: 1048576,
					Timeout: 30,
				},
			},
		},
	}

	tool := NewGithubTool(cfg)
	ctx := context.Background()

	args := map[string]any{
		"owner":        "testowner",
		"repo":         "testrepo",
		"resource":     "issue",
		"issue_number": float64(123),
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute() should not return error, got %v", err)
	}

	if result == nil {
		t.Fatal("Expected result object")
	}

	if result.Success {
		t.Error("Expected Success = false for API error")
	}

	if result.Error == "" {
		t.Error("Expected error message for API error")
	}
}

func TestGithubTool_Execute_IssuesList(t *testing.T) {
	// Create mock issues list
	mockIssues := []domain.GitHubIssue{
		{
			ID:     1,
			Number: 1,
			Title:  "First Issue",
			State:  "open",
			User:   domain.GitHubUser{Login: "user1"},
		},
		{
			ID:     2,
			Number: 2,
			Title:  "Second Issue",
			State:  "open",
			User:   domain.GitHubUser{Login: "user2"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/testowner/testrepo/issues" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockIssues)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
				BaseURL: server.URL,
				Safety: config.GithubSafetyConfig{
					MaxSize: 1048576,
					Timeout: 30,
				},
			},
		},
	}

	tool := NewGithubTool(cfg)
	ctx := context.Background()

	args := map[string]any{
		"owner":    "testowner",
		"repo":     "testrepo",
		"resource": "issues",
		"state":    "open",
		"per_page": float64(10),
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Expected Success = true, got false with error: %s", result.Error)
	}

	// Verify the result data
	issues, ok := result.Data.([]domain.GitHubIssue)
	if !ok {
		t.Error("Expected data to be a slice of GitHubIssue")
	} else {
		if len(issues) != 2 {
			t.Errorf("Expected 2 issues, got %d", len(issues))
		}
	}
}
