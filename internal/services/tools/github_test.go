package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestGithubTool_DefinitionWithDefaults(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
				Owner:   "testowner",
				Repo:    "testrepo",
			},
		},
	}

	tool := NewGithubTool(cfg)
	def := tool.Definition()

	params := def.Parameters.(map[string]any)
	required := params["required"].([]string)

	if len(required) != 0 {
		t.Errorf("Expected no required parameters when defaults are set, got %v", required)
	}

	props := params["properties"].(map[string]any)
	ownerProp := props["owner"].(map[string]any)
	repoProp := props["repo"].(map[string]any)

	ownerDesc := ownerProp["description"].(string)
	repoDesc := repoProp["description"].(string)

	if !strings.Contains(ownerDesc, "testowner") {
		t.Errorf("Expected owner description to contain default 'testowner', got %s", ownerDesc)
	}

	if !strings.Contains(repoDesc, "testrepo") {
		t.Errorf("Expected repo description to contain default 'testrepo', got %s", repoDesc)
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
		{
			name: "valid pull request creation",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "create_pull_request",
				"title":    "Add new feature",
				"body":     "This PR adds a new feature",
				"head":     "feature-branch",
				"base":     "main",
			},
		},
		{
			name: "valid pull request creation minimal",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "create_pull_request",
				"title":    "Add new feature",
				"head":     "feature-branch",
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

func TestGithubTool_Validate_SecurityRequirement(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewGithubTool(cfg)

	args := map[string]any{
		"repo": "testrepo",
	}

	err := tool.Validate(args)
	if err == nil {
		t.Error("Expected validation error when no owner configured and no owner provided")
	}

	expectedErrMsg := "GitHub tool requires owner to be configured in settings for security"
	if !strings.Contains(err.Error(), expectedErrMsg) {
		t.Errorf("Expected error to contain '%s', got '%s'", expectedErrMsg, err.Error())
	}

	cfgWithDefaults := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
				Owner:   "secureowner",
				Repo:    "securerepo",
			},
		},
	}

	toolWithDefaults := NewGithubTool(cfgWithDefaults)
	argsWithoutOwner := map[string]any{
		"resource": "issues",
	}

	err = toolWithDefaults.Validate(argsWithoutOwner)
	if err != nil {
		t.Errorf("Expected no validation error when defaults configured, got %v", err)
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

	issues, ok := result.Data.([]domain.GitHubIssue)
	if !ok {
		t.Error("Expected data to be a slice of GitHubIssue")
	} else {
		if len(issues) != 2 {
			t.Errorf("Expected 2 issues, got %d", len(issues))
		}
	}
}

func TestGithubTool_CreateComment(t *testing.T) {
	mockComment := domain.GitHubComment{
		ID:   123,
		Body: "This is a test comment",
		User: domain.GitHubUser{Login: "testuser"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/repos/testowner/testrepo/issues/1/comments" {
			var requestData map[string]string
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &requestData)

			if requestData["body"] != "This is a test comment" {
				t.Errorf("Expected comment body 'This is a test comment', got '%s'", requestData["body"])
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(mockComment)
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
		"resource":     "create_comment",
		"issue_number": 1,
		"comment_body": "This is a test comment",
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Expected Success = true, got false with error: %s", result.Error)
	}

	comment, ok := result.Data.(*domain.GitHubComment)
	if !ok {
		t.Error("Expected data to be a GitHubComment pointer")
	} else {
		if comment.ID != 123 {
			t.Errorf("Expected comment ID 123, got %d", comment.ID)
		}
		if comment.Body != "This is a test comment" {
			t.Errorf("Expected comment body 'This is a test comment', got '%s'", comment.Body)
		}
	}
}

func TestGithubTool_CreateCommentValidation(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
				BaseURL: "https://api.github.com",
			},
		},
	}

	tool := NewGithubTool(cfg)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name: "missing comment_body",
			args: map[string]any{
				"owner":        "testowner",
				"repo":         "testrepo",
				"resource":     "create_comment",
				"issue_number": 1,
			},
			wantErr: "comment_body parameter is required for create_comment resource",
		},
		{
			name: "empty comment_body",
			args: map[string]any{
				"owner":        "testowner",
				"repo":         "testrepo",
				"resource":     "create_comment",
				"issue_number": 1,
				"comment_body": "",
			},
			wantErr: "comment_body parameter is required for create_comment resource",
		},
		{
			name: "missing issue_number",
			args: map[string]any{
				"owner":        "testowner",
				"repo":         "testrepo",
				"resource":     "create_comment",
				"comment_body": "Test comment",
			},
			wantErr: "issue_number parameter is required for resource type 'create_comment'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if err == nil {
				t.Errorf("Expected validation error, got nil")
			} else if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.wantErr, err.Error())
			}
		})
	}
}

func TestGithubTool_CreatePullRequest(t *testing.T) {
	mockPR := domain.GitHubPullRequest{
		ID:     456,
		Number: 42,
		Title:  "Add new feature",
		Body:   "This PR adds a new feature",
		State:  "open",
		User:   domain.GitHubUser{Login: "testuser"},
		Head: domain.GitHubBranch{
			Ref: "feature-branch",
		},
		Base: domain.GitHubBranch{
			Ref: "main",
		},
		HTMLURL: "https://github.com/testowner/testrepo/pull/42",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/repos/testowner/testrepo/pulls" {
			var requestData map[string]string
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &requestData)

			if requestData["title"] != "Add new feature" {
				t.Errorf("Expected PR title 'Add new feature', got '%s'", requestData["title"])
			}
			if requestData["head"] != "feature-branch" {
				t.Errorf("Expected head 'feature-branch', got '%s'", requestData["head"])
			}
			if requestData["base"] != "main" {
				t.Errorf("Expected base 'main', got '%s'", requestData["base"])
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(mockPR)
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
		"resource": "create_pull_request",
		"title":    "Add new feature",
		"body":     "This PR adds a new feature",
		"head":     "feature-branch",
		"base":     "main",
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !result.Success {
		t.Errorf("Expected Success = true, got false with error: %s", result.Error)
	}

	pr, ok := result.Data.(*domain.GitHubPullRequest)
	if !ok {
		t.Error("Expected data to be a GitHubPullRequest pointer")
	} else {
		if pr.Number != 42 {
			t.Errorf("Expected PR number 42, got %d", pr.Number)
		}
		if pr.Title != "Add new feature" {
			t.Errorf("Expected PR title 'Add new feature', got '%s'", pr.Title)
		}
	}
}

func TestGithubTool_CreatePullRequestValidation(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Github: config.GithubToolConfig{
				Enabled: true,
				BaseURL: "https://api.github.com",
			},
		},
	}

	tool := NewGithubTool(cfg)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name: "missing title",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "create_pull_request",
				"head":     "feature-branch",
			},
			wantErr: "title parameter is required for create_pull_request resource",
		},
		{
			name: "empty title",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "create_pull_request",
				"title":    "",
				"head":     "feature-branch",
			},
			wantErr: "title parameter is required for create_pull_request resource",
		},
		{
			name: "missing head",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "create_pull_request",
				"title":    "Test PR",
			},
			wantErr: "head parameter is required for create_pull_request resource",
		},
		{
			name: "empty head",
			args: map[string]any{
				"owner":    "testowner",
				"repo":     "testrepo",
				"resource": "create_pull_request",
				"title":    "Test PR",
				"head":     "",
			},
			wantErr: "head parameter is required for create_pull_request resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if err == nil {
				t.Errorf("Expected validation error, got nil")
			} else if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.wantErr, err.Error())
			}
		})
	}
}
