package tools

import (
	"bytes"
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
					"description": "Resource type to fetch or create",
					"enum":        []string{"issue", "issues", "pull_request", "comments", "create_comment"},
					"default":     "issue",
				},
				"comment_body": map[string]any{
					"type":        "string",
					"description": "Comment body text (required for create_comment resource)",
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

	if err := t.validateEnabled(); err != nil {
		return nil, err
	}

	owner, repo, err := t.extractOwnerAndRepo(args)
	if err != nil {
		return t.createErrorResult(args, start, err.Error()), nil
	}

	resource := t.extractResource(args)
	githubResult, err := t.executeResource(ctx, resource, owner, repo, args)
	if err != nil {
		if toolErr, ok := err.(*ToolExecutionError); ok {
			return t.createErrorResult(args, start, toolErr.Message), nil
		}
		return t.createResult(args, start, nil, err.Error()), nil
	}

	return t.createResult(args, start, githubResult, ""), nil
}

// ToolExecutionError represents an error during tool execution
type ToolExecutionError struct {
	Message string
}

func (e *ToolExecutionError) Error() string {
	return e.Message
}

// validateEnabled checks if the GitHub tool is enabled
func (t *GithubTool) validateEnabled() error {
	if !t.config.Tools.Enabled || !t.config.Tools.Github.Enabled {
		return fmt.Errorf("GitHub tool is not enabled")
	}
	return nil
}

// extractOwnerAndRepo extracts and validates owner and repo parameters
func (t *GithubTool) extractOwnerAndRepo(args map[string]any) (string, string, error) {
	if t.config.Tools.Github.Owner == "" {
		if owner, ok := args["owner"].(string); !ok || owner == "" {
			return "", "", fmt.Errorf("GitHub tool requires owner to be configured in settings for security")
		}
	}

	owner, ok := args["owner"].(string)
	if !ok || owner == "" {
		if t.config.Tools.Github.Owner != "" {
			owner = t.config.Tools.Github.Owner
		} else {
			return "", "", fmt.Errorf("owner parameter is required and must be a string, or set owner in config")
		}
	}

	repo, ok := args["repo"].(string)
	if !ok || repo == "" {
		if t.config.Tools.Github.Repo != "" {
			repo = t.config.Tools.Github.Repo
		} else {
			return "", "", fmt.Errorf("repo parameter is required and must be a string, or set repo in config")
		}
	}

	return owner, repo, nil
}

// extractResource extracts the resource type from arguments
func (t *GithubTool) extractResource(args map[string]any) string {
	resource := "issue"
	if r, ok := args["resource"].(string); ok {
		resource = r
	}
	return resource
}

// executeResource executes the appropriate GitHub API operation based on resource type
func (t *GithubTool) executeResource(ctx context.Context, resource, owner, repo string, args map[string]any) (any, error) {
	switch resource {
	case "issue":
		return t.handleIssueResource(ctx, owner, repo, args)
	case "issues":
		return t.handleIssuesResource(ctx, owner, repo, args)
	case "pull_request":
		return t.handlePullRequestResource(ctx, owner, repo, args)
	case "comments":
		return t.handleCommentsResource(ctx, owner, repo, args)
	case "create_comment":
		return t.handleCreateCommentResource(ctx, owner, repo, args)
	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resource)
	}
}

// handleIssueResource handles fetching a specific issue
func (t *GithubTool) handleIssueResource(ctx context.Context, owner, repo string, args map[string]any) (any, error) {
	issueNum, ok := parseIssueNumber(args["issue_number"])
	if !ok {
		return nil, &ToolExecutionError{"issue_number parameter is required for fetching a specific issue"}
	}
	return t.fetchIssue(ctx, owner, repo, issueNum)
}

// handleIssuesResource handles fetching a list of issues
func (t *GithubTool) handleIssuesResource(ctx context.Context, owner, repo string, args map[string]any) (any, error) {
	state := "open"
	if s, ok := args["state"].(string); ok {
		state = s
	}
	perPage := 30
	if p, ok := args["per_page"].(float64); ok {
		perPage = int(p)
	}
	return t.fetchIssues(ctx, owner, repo, state, perPage)
}

// handlePullRequestResource handles fetching a specific pull request
func (t *GithubTool) handlePullRequestResource(ctx context.Context, owner, repo string, args map[string]any) (any, error) {
	prNum, ok := parseIssueNumber(args["issue_number"])
	if !ok {
		return nil, &ToolExecutionError{"issue_number parameter is required for fetching a specific pull request"}
	}
	return t.fetchPullRequest(ctx, owner, repo, prNum)
}

// handleCommentsResource handles fetching comments
func (t *GithubTool) handleCommentsResource(ctx context.Context, owner, repo string, args map[string]any) (any, error) {
	issueNum, ok := parseIssueNumber(args["issue_number"])
	if !ok {
		return nil, &ToolExecutionError{"issue_number parameter is required for fetching comments"}
	}
	return t.fetchIssueComments(ctx, owner, repo, issueNum)
}

// handleCreateCommentResource handles creating a comment
func (t *GithubTool) handleCreateCommentResource(ctx context.Context, owner, repo string, args map[string]any) (any, error) {
	issueNum, ok := parseIssueNumber(args["issue_number"])
	if !ok {
		return nil, &ToolExecutionError{"issue_number parameter is required for creating a comment"}
	}

	commentBody, ok := args["comment_body"].(string)
	if !ok || commentBody == "" {
		return nil, &ToolExecutionError{"comment_body parameter is required for creating a comment"}
	}

	return t.createIssueComment(ctx, owner, repo, issueNum, commentBody)
}

// createResult creates a ToolExecutionResult
func (t *GithubTool) createResult(args map[string]any, start time.Time, data any, errorMsg string) *domain.ToolExecutionResult {
	result := &domain.ToolExecutionResult{
		ToolName:  "Github",
		Arguments: args,
		Success:   errorMsg == "",
		Duration:  time.Since(start),
	}

	if errorMsg != "" {
		result.Error = errorMsg
	} else {
		result.Data = data
	}

	return result
}

// createErrorResult creates an error ToolExecutionResult
func (t *GithubTool) createErrorResult(args map[string]any, start time.Time, errorMsg string) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  "Github",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     errorMsg,
	}
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

	body, err := t.makeAPIRequest(ctx, "GET", url, nil)
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

	body, err := t.makeAPIRequest(ctx, "GET", url, nil)
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

	body, err := t.makeAPIRequest(ctx, "GET", url, nil)
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

	body, err := t.makeAPIRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var comments []domain.GitHubComment
	if err := json.Unmarshal(body, &comments); err != nil {
		return nil, fmt.Errorf("failed to unmarshal comments: %w", err)
	}

	return comments, nil
}

// createIssueComment creates a comment on an issue or pull request
func (t *GithubTool) createIssueComment(ctx context.Context, owner, repo string, issueNumber int, body string) (*domain.GitHubComment, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments",
		t.config.Tools.Github.BaseURL, owner, repo, issueNumber)

	commentData := map[string]string{
		"body": body,
	}

	jsonData, err := json.Marshal(commentData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal comment data: %w", err)
	}

	respBody, err := t.makeAPIRequest(ctx, "POST", url, jsonData)
	if err != nil {
		return nil, err
	}

	var comment domain.GitHubComment
	if err := json.Unmarshal(respBody, &comment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal comment response: %w", err)
	}

	return &comment, nil
}

// makeAPIRequest makes an API request to GitHub and returns the response body
func (t *GithubTool) makeAPIRequest(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewBuffer(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "inference-gateway-cli")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	token := config.ResolveEnvironmentVariables(t.config.Tools.Github.Token)
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, t.config.Tools.Github.Safety.MaxSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errorResp domain.GitHubError
		if err := json.Unmarshal(respBody, &errorResp); err == nil && errorResp.Message != "" {
			return nil, fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, errorResp.Message)
		}
		return nil, fmt.Errorf("GitHub API error: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	return respBody, nil
}

// validateResourceType validates the resource type and its requirements
func (t *GithubTool) validateResourceType(resource string, args map[string]any) error {
	validResources := []string{"issue", "issues", "pull_request", "comments", "create_comment"}
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

	if resource == "issue" || resource == "pull_request" || resource == "comments" || resource == "create_comment" {
		if err := t.validateIssueNumber(args, resource); err != nil {
			return err
		}
	}

	if resource == "create_comment" {
		if commentBody, ok := args["comment_body"].(string); !ok || commentBody == "" {
			return fmt.Errorf("comment_body parameter is required for create_comment resource")
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
