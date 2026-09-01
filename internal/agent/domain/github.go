// GitHub ports: cached issue access behind the chat input's "#" autocomplete
// and the git/gh/CI operations behind the install workflow wizard.

package domain

import (
	"context"
	"time"
)

// GitHubIssue is a minimal projection of a GitHub issue, big enough for both
// the autocomplete dropdown (Number, Title, State) and inline expansion into a
// chat-message block (Body, URL, Comments, UpdatedAt). Comments is nil for the
// list variant and populated for the view variant.
type GitHubIssue struct {
	Number    int
	Title     string
	Body      string
	State     string
	URL       string
	UpdatedAt time.Time
	Author    string
	Comments  []GitHubIssueComment
}

// GitHubIssueComment is a single comment on a GitHub issue, sorted by
// CreatedAt ascending.
type GitHubIssueComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

// GitHubIssueService provides cached access to the current repository's GitHub
// issues via the gh CLI. Implementations gracefully degrade (return empty/nil
// with no error) when not in a git repo, when gh is not installed, or when the
// remote/auth is not configured - the chat input's "#" autocomplete and "#N"
// inline expansion simply become no-ops in those environments.
type GitHubIssueService interface {
	// ListIssues returns recent open issues for the current repo, newest first.
	// Results are cached for a short TTL so repeated autocomplete keystrokes
	// don't shell out per character. Returns ([], nil) on environment failures.
	ListIssues(ctx context.Context) ([]GitHubIssue, error)

	// GetIssue fetches an issue with body and the most-recent comments (capped
	// internally). Uncached. Returns (nil, err) on failure so the expansion
	// path can leave the raw token in place.
	GetIssue(ctx context.Context, number int) (*GitHubIssue, error)

	// IsAvailable reports whether the service can serve requests in the
	// current environment. Used by the autocomplete trigger to short-circuit
	// a slow first shell-out when gh / repo / auth are missing.
	IsAvailable() bool
}

// GitHubSetupService handles git/gh/CI operations for the GitHub Action CI setup
// flow triggered from the install-opentask wizard. Every shell invocation carries
// a context so a wedged subprocess cannot hang the UI.
type GitHubSetupService interface {
	GetCurrentRepo() (string, error)
	IsOrgRepo(repo string) (bool, error)
	CheckOrgSecretsExist(orgName string) (bool, error)
	SetOrgSecret(orgName, name, value string) error
	PreparePRCreation(repo, workflowPath string) (string, error)
	WriteWorkflowFile(path, content string) error
	GenerateStandardWorkflowContent() string
	GenerateGithubActionWorkflowContent() string
	InstallWorkflow(ctx context.Context, opts InstallWorkflowOptions) (string, error)
}

// InstallWorkflowOptions parameterizes GitHubSetupService.InstallWorkflow.
type InstallWorkflowOptions struct {
	// Repo is "owner/name"; empty means the current repository.
	Repo string
	// Model runs the install agent and becomes the workflow's default model.
	Model string
	// Context is extra user-supplied guidance appended to the agent prompt.
	Context string
	// GitHubApp selects the GitHub App token variant of the workflow.
	GitHubApp bool
}
