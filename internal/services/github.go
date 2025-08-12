package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// GitHubIssue represents a GitHub issue
type GitHubIssue struct {
	Number     int     `json:"number"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	State      string  `json:"state"`
	HTMLURL    string  `json:"html_url"`
	User       User    `json:"user"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	Labels     []Label `json:"labels"`
	Comments   int     `json:"comments"`
	Repository string  `json:"-"` // Will be set manually
}

// User represents a GitHub user
type User struct {
	Login string `json:"login"`
}

// Label represents a GitHub label
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// GitHubService handles GitHub API operations
type GitHubService struct {
	client *resty.Client
}

// NewGitHubService creates a new GitHub service
func NewGitHubService() *GitHubService {
	client := resty.New().
		SetTimeout(30 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second)

	return &GitHubService{
		client: client,
	}
}

// FetchIssue fetches a GitHub issue by repository and issue number
func (g *GitHubService) FetchIssue(ctx context.Context, repository string, issueNumber int) (*GitHubIssue, error) {
	if repository == "" {
		return nil, fmt.Errorf("repository cannot be empty")
	}
	if issueNumber <= 0 {
		return nil, fmt.Errorf("issue number must be positive")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d", repository, issueNumber)

	resp, err := g.client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/vnd.github.v3+json").
		SetHeader("User-Agent", "inference-gateway-cli/1.0").
		Get(url)

	if err != nil {
		return nil, fmt.Errorf("failed to fetch issue: %w", err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		return nil, fmt.Errorf("issue #%d not found in repository %s", issueNumber, repository)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var issue GitHubIssue
	if err := json.Unmarshal(resp.Body(), &issue); err != nil {
		return nil, fmt.Errorf("failed to parse issue response: %w", err)
	}

	issue.Repository = repository
	return &issue, nil
}

// ParseIssueReference parses various GitHub issue reference formats
func (g *GitHubService) ParseIssueReference(reference string) (repository string, issueNumber int, err error) {
	reference = strings.TrimSpace(reference)

	if reference == "" {
		return "", 0, fmt.Errorf("issue reference cannot be empty")
	}

	// Handle different formats:
	// 1. "123" - just number (requires repository context)
	// 2. "owner/repo#123" - full reference
	// 3. "https://github.com/owner/repo/issues/123" - full URL
	// 4. "#123" - number with hash (requires repository context)

	// Handle URL format
	if strings.HasPrefix(reference, "https://github.com/") {
		return g.parseFromURL(reference)
	}

	// Handle owner/repo#123 format
	if strings.Contains(reference, "/") && strings.Contains(reference, "#") {
		parts := strings.Split(reference, "#")
		if len(parts) != 2 {
			return "", 0, fmt.Errorf("invalid issue reference format: %s", reference)
		}

		repository = parts[0]
		number, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", 0, fmt.Errorf("invalid issue number: %s", parts[1])
		}

		return repository, number, nil
	}

	// Handle #123 or 123 format (number only)
	numberStr := strings.TrimPrefix(reference, "#")
	number, err := strconv.Atoi(numberStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid issue number: %s", numberStr)
	}

	// Return empty repository - caller needs to provide context
	return "", number, nil
}

// parseFromURL parses issue information from a GitHub URL
func (g *GitHubService) parseFromURL(url string) (repository string, issueNumber int, err error) {
	// Expected format: https://github.com/owner/repo/issues/123
	url = strings.TrimPrefix(url, "https://github.com/")
	parts := strings.Split(url, "/")

	if len(parts) < 4 || parts[2] != "issues" {
		return "", 0, fmt.Errorf("invalid GitHub issue URL format")
	}

	repository = fmt.Sprintf("%s/%s", parts[0], parts[1])
	number, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", 0, fmt.Errorf("invalid issue number in URL: %s", parts[3])
	}

	return repository, number, nil
}

// FormatIssueForPrompt formats a GitHub issue into a prompt-friendly string
func (g *GitHubService) FormatIssueForPrompt(issue *GitHubIssue) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("# GitHub Issue #%d: %s\n\n", issue.Number, issue.Title))
	builder.WriteString(fmt.Sprintf("**Repository:** %s\n", issue.Repository))
	builder.WriteString(fmt.Sprintf("**Status:** %s\n", issue.State))
	builder.WriteString(fmt.Sprintf("**Created by:** %s\n", issue.User.Login))
	builder.WriteString(fmt.Sprintf("**URL:** %s\n\n", issue.HTMLURL))

	if len(issue.Labels) > 0 {
		builder.WriteString("**Labels:** ")
		for i, label := range issue.Labels {
			if i > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(label.Name)
		}
		builder.WriteString("\n\n")
	}

	builder.WriteString("## Description\n\n")
	if issue.Body != "" {
		builder.WriteString(issue.Body)
	} else {
		builder.WriteString("*No description provided*")
	}
	builder.WriteString("\n")

	return builder.String()
}
