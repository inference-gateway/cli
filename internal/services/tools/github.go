package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// GithubTool handles GitHub API operations
type GithubTool struct {
	config  *config.Config
	enabled bool
	client  *http.Client
}

// NewGithubTool creates a new GitHub tool
func NewGithubTool(cfg *config.Config) *GithubTool {
	return &GithubTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Github.Enabled,
		client: &http.Client{
			Timeout: time.Duration(cfg.Tools.Github.Safety.Timeout) * time.Second,
		},
	}
}

// Definition returns the tool definition for the LLM
func (t *GithubTool) Definition() domain.ToolDefinition {
	required := []string{}

	if t.config.Tools.Github.Owner == "" {
		required = append(required, "owner")
	}

	if t.config.Tools.Github.Repo == "" {
		required = append(required, "repo")
	}

	ownerDescription := "Repository owner (username or organization)"
	repoDescription := "Repository name"

	if t.config.Tools.Github.Owner != "" {
		ownerDescription += fmt.Sprintf(" (defaults to: %s)", t.config.Tools.Github.Owner)
	}
	if t.config.Tools.Github.Repo != "" {
		repoDescription += fmt.Sprintf(" (defaults to: %s)", t.config.Tools.Github.Repo)
	}

	return domain.ToolDefinition{
		Name:        "Github",
		Description: "Interact with GitHub API to fetch issues, pull requests, and other data",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"owner": map[string]any{
					"type":        "string",
					"description": ownerDescription,
				},
				"repo": map[string]any{
					"type":        "string",
					"description": repoDescription,
				},
				"issue_number": map[string]any{
					"type":        []string{"integer", "string"},
					"description": "Issue or pull request number (can be provided as integer or string)",
				},
				"resource": map[string]any{
					"type":        "string",
					"description": "Resource type to fetch",
					"enum":        []string{"issue", "issues", "pull_request", "comments"},
					"default":     "issue",
				},
				"state": map[string]any{
					"type":        "string",
					"description": "Filter by state (for issues/PRs list)",
					"enum":        []string{"open", "closed", "all"},
					"default":     "open",
				},
				"per_page": map[string]any{
					"type":        "integer",
					"description": "Number of items per page (max 100)",
					"default":     30,
					"maximum":     100,
				},
			},
			"required": required,
		},
	}
}

// Execute runs the GitHub tool with given arguments
func (t *GithubTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled || !t.config.Tools.Github.Enabled {
		return nil, fmt.Errorf("GitHub tool is not enabled")
	}

	if t.config.Tools.Github.Owner == "" {
		if owner, ok := args["owner"].(string); !ok || owner == "" {
			return &domain.ToolExecutionResult{
				ToolName:  "Github",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     "GitHub tool requires owner to be configured in settings for security",
			}, nil
		}
	}

	owner, ok := args["owner"].(string)
	if !ok || owner == "" {
		if t.config.Tools.Github.Owner != "" {
			owner = t.config.Tools.Github.Owner
		} else {
			return &domain.ToolExecutionResult{
				ToolName:  "Github",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     "owner parameter is required and must be a string, or set owner in config",
			}, nil
		}
	}

	repo, ok := args["repo"].(string)
	if !ok || repo == "" {
		if t.config.Tools.Github.Repo != "" {
			repo = t.config.Tools.Github.Repo
		} else {
			return &domain.ToolExecutionResult{
				ToolName:  "Github",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     "repo parameter is required and must be a string, or set repo in config",
			}, nil
		}
	}

	resource := "issue"
	if r, ok := args["resource"].(string); ok {
		resource = r
	}

	var githubResult any
	var err error

	switch resource {
	case "issue":
		if issueNum, ok := parseIssueNumber(args["issue_number"]); ok {
			githubResult, err = t.fetchIssue(ctx, owner, repo, issueNum)
		} else {
			return &domain.ToolExecutionResult{
				ToolName:  "Github",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     "issue_number parameter is required for fetching a specific issue",
			}, nil
		}
	case "issues":
		state := "open"
		if s, ok := args["state"].(string); ok {
			state = s
		}
		perPage := 30
		if p, ok := args["per_page"].(float64); ok {
			perPage = int(p)
		}
		githubResult, err = t.fetchIssues(ctx, owner, repo, state, perPage)
	case "pull_request":
		if prNum, ok := parseIssueNumber(args["issue_number"]); ok {
			githubResult, err = t.fetchPullRequest(ctx, owner, repo, prNum)
		} else {
			return &domain.ToolExecutionResult{
				ToolName:  "Github",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     "issue_number parameter is required for fetching a specific pull request",
			}, nil
		}
	case "comments":
		if issueNum, ok := parseIssueNumber(args["issue_number"]); ok {
			githubResult, err = t.fetchIssueComments(ctx, owner, repo, issueNum)
		} else {
			return &domain.ToolExecutionResult{
				ToolName:  "Github",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     "issue_number parameter is required for fetching comments",
			}, nil
		}
	default:
		return &domain.ToolExecutionResult{
			ToolName:  "Github",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("unsupported resource type: %s", resource),
		}, nil
	}

	success := err == nil
	result := &domain.ToolExecutionResult{
		ToolName:  "Github",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
	}

	if err != nil {
		result.Error = err.Error()
	} else {
		result.Data = githubResult
	}

	return result, nil
}

// Validate checks if the GitHub tool arguments are valid
func (t *GithubTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled || !t.config.Tools.Github.Enabled {
		return fmt.Errorf("GitHub tool is not enabled")
	}

	if t.config.Tools.Github.Owner == "" {
		owner, ok := args["owner"].(string)
		if !ok || owner == "" {
			return fmt.Errorf("GitHub tool requires owner to be configured in settings for security")
		}
	}

	owner, ok := args["owner"].(string)
	if !ok || owner == "" {
		if t.config.Tools.Github.Owner == "" {
			return fmt.Errorf("owner parameter is required and must be a string, or set owner in config")
		}
	}

	repo, ok := args["repo"].(string)
	if !ok || repo == "" {
		if t.config.Tools.Github.Repo == "" {
			return fmt.Errorf("repo parameter is required and must be a string, or set repo in config")
		}
	}

	resource, hasResource := args["resource"].(string)
	if hasResource {
		if err := t.validateResourceType(resource, args); err != nil {
			return err
		}
	}

	if state, ok := args["state"].(string); ok {
		validStates := []string{"open", "closed", "all"}
		valid := false
		for _, validState := range validStates {
			if state == validState {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("state must be one of: %v", validStates)
		}
	}

	if perPage, ok := args["per_page"].(float64); ok {
		if perPage < 1 || perPage > 100 {
			return fmt.Errorf("per_page must be between 1 and 100")
		}
	}

	return nil
}

// IsEnabled returns whether the GitHub tool is enabled
func (t *GithubTool) IsEnabled() bool {
	return t.enabled
}

// fetchIssue fetches a specific issue
func (t *GithubTool) fetchIssue(ctx context.Context, owner, repo string, issueNumber int) (*domain.GitHubIssue, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d", t.config.Tools.Github.BaseURL, owner, repo, issueNumber)

	body, err := t.makeAPIRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	var issue domain.GitHubIssue
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("failed to unmarshal issue: %w", err)
	}

	return &issue, nil
}

// fetchIssues fetches a list of issues
func (t *GithubTool) fetchIssues(ctx context.Context, owner, repo, state string, perPage int) ([]domain.GitHubIssue, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues?state=%s&per_page=%d",
		t.config.Tools.Github.BaseURL, owner, repo, state, perPage)

	body, err := t.makeAPIRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	var issues []domain.GitHubIssue
	if err := json.Unmarshal(body, &issues); err != nil {
		return nil, fmt.Errorf("failed to unmarshal issues: %w", err)
	}

	return issues, nil
}

// fetchPullRequest fetches a specific pull request
func (t *GithubTool) fetchPullRequest(ctx context.Context, owner, repo string, prNumber int) (*domain.GitHubPullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", t.config.Tools.Github.BaseURL, owner, repo, prNumber)

	body, err := t.makeAPIRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	var pr domain.GitHubPullRequest
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pull request: %w", err)
	}

	return &pr, nil
}

// fetchIssueComments fetches comments for an issue or pull request
func (t *GithubTool) fetchIssueComments(ctx context.Context, owner, repo string, issueNumber int) ([]domain.GitHubComment, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments",
		t.config.Tools.Github.BaseURL, owner, repo, issueNumber)

	body, err := t.makeAPIRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	var comments []domain.GitHubComment
	if err := json.Unmarshal(body, &comments); err != nil {
		return nil, fmt.Errorf("failed to unmarshal comments: %w", err)
	}

	return comments, nil
}

// makeAPIRequest makes an API request to GitHub and returns the response body
func (t *GithubTool) makeAPIRequest(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "inference-gateway-cli")

	token := config.ResolveEnvironmentVariables(t.config.Tools.Github.Token)
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, t.config.Tools.Github.Safety.MaxSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errorResp domain.GitHubError
		if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Message != "" {
			return nil, fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, errorResp.Message)
		}
		return nil, fmt.Errorf("GitHub API error: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	return body, nil
}

// validateResourceType validates the resource type and its requirements
func (t *GithubTool) validateResourceType(resource string, args map[string]any) error {
	validResources := []string{"issue", "issues", "pull_request", "comments"}
	valid := false
	for _, validRes := range validResources {
		if resource == validRes {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("resource must be one of: %v", validResources)
	}

	if resource == "issue" || resource == "pull_request" || resource == "comments" {
		if err := t.validateIssueNumber(args, resource); err != nil {
			return err
		}
	}

	return nil
}

// validateIssueNumber validates the issue_number parameter for resources that require it
func (t *GithubTool) validateIssueNumber(args map[string]any, resource string) error {
	if issueNum, ok := args["issue_number"]; ok {
		if _, valid := parseIssueNumber(issueNum); !valid {
			return fmt.Errorf("issue_number must be a valid number for resource type '%s'", resource)
		}
	} else {
		return fmt.Errorf("issue_number parameter is required for resource type '%s'", resource)
	}
	return nil
}

// parseIssueNumber converts various types to int for issue number
func parseIssueNumber(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case string:
		str := strings.TrimSpace(v)
		str = strings.TrimPrefix(str, "#")
		if num, err := strconv.Atoi(str); err == nil {
			return num, true
		}
		return 0, false
	default:
		return 0, false
	}
}
