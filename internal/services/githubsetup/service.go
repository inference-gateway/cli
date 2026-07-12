// Package githubsetup implements domain.GitHubSetupService for the GitHub Action CI
// setup flow. Every subprocess call carries a context so a wedged command cannot
// hang the UI. Git commands use gitdiff.RunGit; gh commands use the injected
// CommandRunner with a 30-second timeout context.
package githubsetup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	constants "github.com/inference-gateway/cli/internal/constants"
	gitdiff "github.com/inference-gateway/cli/internal/services/gitdiff"
)

// CommandRunner is an injectable interface for running subprocesses. Tests
// supply a fake runner so no real gh/git calls are needed.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// RealRunner shells out using exec.CommandContext.
type RealRunner struct{}

// Run executes the command using exec.CommandContext.
func (r *RealRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

// Service implements domain.GitHubSetupService.
type Service struct {
	runner CommandRunner
}

// NewService creates a new Service with the given runner.
func NewService(runner CommandRunner) *Service {
	return &Service{runner: runner}
}

// Version pins and defaults for the generated .github/workflows/infer.yml.
// Bumping any of these is a one-line change picked up by both templates.
const (
	inferActionVersion     = "v0.29.0"
	checkoutActionVersion  = "v7.0.0"
	appTokenActionVersion  = "v3.2.0"
	workflowDefaultModel   = "ollama_cloud/deepseek-v4-flash"
	workflowTimeoutMinutes = 15
)

// workflowHeader is the shared prologue of the generated workflow: header
// comment, triggers (issue/comment mentions + manual workflow_dispatch),
// permissions, and the gated job preamble with concurrency and timeout.
func workflowHeader(extraNote string) string {
	return fmt.Sprintf(`---
# Infer Agent CI
#
# Runs the Infer agent (inference-gateway/infer-action) in three ways:
#
# 1. Issue-driven: mention `+"`@infer`"+` in an issue title, body, or comment and
#    the agent picks up the task, works on it, and opens a draft PR.
# 2. Review-comment-driven: mention `+"`@infer`"+` in a pull request review comment
#    (inline or thread reply) and the agent works on the focused file/diff hunk.
# 3. Manual (workflow_dispatch): run it from the Actions tab with a free-text
#    prompt — useful for ad-hoc tasks like "find bugs and report them".
#    Optionally tick "browser-agent" to spin up the A2A
#    inference-gateway/browser-agent container so the agent can browse the web.
#
# Notes:
# - Jobs are capped at %d minutes (timeout-minutes).
# - Runs are deduplicated per issue, PR, or dispatch run via concurrency.%s
name: Infer

on:
  issues:
    types:
      - opened
      - edited
  issue_comment:
    types:
      - created
  pull_request_review_comment:
    types:
      - created
  workflow_dispatch:
    inputs:
      prompt:
        description: 'Free-text task for the agent (e.g. "find bugs and report them")'
        type: string
        required: true
      browser-agent:
        description: 'Start the A2A browser-agent (inference-gateway/browser-agent) so the agent can browse the web'
        type: boolean
        required: false
        default: false
      debug:
        description: 'Enable debug output and mirror agent logs'
        type: boolean
        required: false
        default: false

permissions:
  issues: write
  contents: write
  pull-requests: write

jobs:
  infer:
    concurrency:
      group: ${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}
      cancel-in-progress: true
    if: |
      github.event_name == 'workflow_dispatch' ||
      (
        github.event.sender.type != 'Bot' &&
        !endsWith(github.actor, '[bot]') &&
        (
          (github.event_name == 'issue_comment' && contains(github.event.comment.body, '@infer')) ||
          (github.event_name == 'pull_request_review_comment' && contains(github.event.comment.body, '@infer')) ||
          (github.event_name == 'issues' && (contains(github.event.issue.body, '@infer') || contains(github.event.issue.title, '@infer')))
        )
      )
    runs-on: ubuntu-24.04
    timeout-minutes: %d
    steps:
`, workflowTimeoutMinutes, extraNote, workflowTimeoutMinutes)
}

// workflowAgentInputs is the shared tail of the "Run Infer Agent" step: the
// agent defaults and the provider API key pass-throughs.
const workflowAgentInputs = `          trigger-phrase: '@infer'
          model: ` + workflowDefaultModel + `
          direct-prompt: ${{ inputs.prompt }}
          agents: ${{ inputs.browser-agent && 'browser-agent' || '' }}
          debug: ${{ inputs.debug }}
          mirror-agent-logs: ${{ inputs.debug }}
          anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
          openai-api-key: ${{ secrets.OPENAI_API_KEY }}
          google-api-key: ${{ secrets.GOOGLE_API_KEY }}
          deepseek-api-key: ${{ secrets.DEEPSEEK_API_KEY }}
          groq-api-key: ${{ secrets.GROQ_API_KEY }}
          mistral-api-key: ${{ secrets.MISTRAL_API_KEY }}
          cloudflare-api-key: ${{ secrets.CLOUDFLARE_API_KEY }}
          cohere-api-key: ${{ secrets.COHERE_API_KEY }}
          ollama-api-key: ${{ secrets.OLLAMA_API_KEY }}
          ollama-cloud-api-key: ${{ secrets.OLLAMA_CLOUD_API_KEY }}
          moonshot-api-key: ${{ secrets.MOONSHOT_API_KEY }}
          minimax-api-key: ${{ secrets.MINIMAX_API_KEY }}
          nvidia-api-key: ${{ secrets.NVIDIA_API_KEY }}
          zai-api-key: ${{ secrets.ZAI_API_KEY }}
`

// ghTimeoutContext returns a context with a 30-second timeout for gh subprocess
// calls, derived from the passed context.
func ghTimeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 30*time.Second)
}

// GetCurrentRepo returns the current GitHub repository name with owner.
func (s *Service) GetCurrentRepo() (string, error) {
	ctx, cancel := ghTimeoutContext(context.Background())
	defer cancel()

	output, err := s.runner.Run(ctx, "gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		return "", fmt.Errorf("failed to get current repository: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// IsOrgRepo checks whether the given repo belongs to a GitHub organization.
func (s *Service) IsOrgRepo(repo string) (bool, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid repo format: %s", repo)
	}
	owner := parts[0]

	ctx, cancel := ghTimeoutContext(context.Background())
	defer cancel()

	_, err := s.runner.Run(ctx, "gh", "api", fmt.Sprintf("/orgs/%s", owner))
	if err != nil {
		return false, nil
	}
	return true, nil
}

// CheckOrgSecretsExist checks whether INFER_APP_ID and INFER_APP_PRIVATE_KEY
// secrets exist for the given org.
func (s *Service) CheckOrgSecretsExist(orgName string) (bool, error) {
	ctx, cancel := ghTimeoutContext(context.Background())
	defer cancel()

	output, err := s.runner.Run(ctx, "gh", "secret", "list", "--org", orgName)
	if err != nil {
		return false, fmt.Errorf("failed to list org secrets: %w", err)
	}

	secrets := string(output)
	hasAppID := strings.Contains(secrets, "INFER_APP_ID")
	hasPrivateKey := strings.Contains(secrets, "INFER_APP_PRIVATE_KEY")

	return hasAppID && hasPrivateKey, nil
}

// SetOrgSecret sets a GitHub organization-level secret.
func (s *Service) SetOrgSecret(orgName, name, value string) error {
	ctx, cancel := ghTimeoutContext(context.Background())
	defer cancel()

	output, err := s.runner.Run(ctx, "gh", "secret", "set", name, "--org", orgName, "--visibility", "all", "--body", value)
	if err != nil {
		return fmt.Errorf("%s: %w", string(output), err)
	}
	return nil
}

// WriteWorkflowFile writes the workflow content to the specified path.
func (s *Service) WriteWorkflowFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// GenerateStandardWorkflowContent generates the standard (github-actions[bot])
// workflow YAML content.
func (s *Service) GenerateStandardWorkflowContent() string {
	return workflowHeader("") + fmt.Sprintf(`      - name: Checkout repository
        uses: actions/checkout@%s

      - name: Run Infer Agent
        uses: inference-gateway/infer-action@%s
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
`, checkoutActionVersion, inferActionVersion) + workflowAgentInputs
}

// GenerateGithubActionWorkflowContent generates the GitHub App-based workflow
// YAML content with org-level secrets.
func (s *Service) GenerateGithubActionWorkflowContent() string {
	extraNote := `
# - The GitHub App used for the token needs the "Workflows" (read & write)
#   repository permission so the agent can push changes to .github/workflows.`
	return workflowHeader(extraNote) + fmt.Sprintf(`      - name: Generate GitHub App token
        id: app-token
        uses: actions/create-github-app-token@%s
        with:
          client-id: ${{ secrets.INFER_APP_ID }}
          private-key: ${{ secrets.INFER_APP_PRIVATE_KEY }}
          owner: ${{ github.repository_owner }}
          repositories: |
            ${{ github.event.repository.name }}

      - name: Get GitHub App User ID
        id: get-user-id
        run: echo "user-id=$(gh api "/users/${{ steps.app-token.outputs.app-slug }}[bot]" --jq .id)" >> "$GITHUB_OUTPUT"
        env:
          GH_TOKEN: ${{ steps.app-token.outputs.token }}

      - name: Set up Git
        run: |
          git config --global user.name '${{ steps.app-token.outputs.app-slug }}[bot]'
          git config --global user.email '${{ steps.get-user-id.outputs.user-id }}+${{ steps.app-token.outputs.app-slug }}[bot]@users.noreply.github.com'
          git config --global commit.gpgsign false
          git config --global commit.signoff true

      - name: Checkout repository
        uses: actions/checkout@%s
        with:
          token: ${{ steps.app-token.outputs.token }}

      - name: Run Infer Agent
        uses: inference-gateway/infer-action@%s
        with:
          github-token: ${{ steps.app-token.outputs.token }}
          github-app-slug: ${{ steps.app-token.outputs.app-slug }}
`, appTokenActionVersion, checkoutActionVersion, inferActionVersion) + workflowAgentInputs
}

// PreparePRCreation creates a branch, commits the workflow file, pushes it,
// and opens a PR creation page. It returns the compare URL.
func (s *Service) PreparePRCreation(repo, workflowPath string) (string, error) {
	var baseBranch string

	ctxGitDefault, cancelDefault := ghTimeoutContext(context.Background())
	defer cancelDefault()

	output, err := s.runner.Run(ctxGitDefault, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(output)), "/")
		if len(parts) > 0 {
			baseBranch = parts[len(parts)-1]
		}
	}
	if baseBranch == "" {
		baseBranch = "main"
	}

	gitCtx, cancelGit := context.WithTimeout(context.Background(), constants.GitCommandTimeout)
	defer cancelGit()
	currentBranch, err := gitdiff.RunGit(gitCtx, "", "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	branch := strings.TrimSpace(string(currentBranch))

	if branch == "main" || branch == "master" {
		baseBranch = branch
		branch = "ci/setup-infer-github-action"

		ctxBranch, cancelBranch := ghTimeoutContext(context.Background())
		defer cancelBranch()
		output, err := s.runner.Run(ctxBranch, "git", "checkout", "-b", branch)
		if err != nil {
			return "", fmt.Errorf("failed to create branch: %s: %w", string(output), err)
		}
	}

	ctxAdd, cancelAdd := ghTimeoutContext(context.Background())
	defer cancelAdd()
	output, err = s.runner.Run(ctxAdd, "git", "add", workflowPath)
	if err != nil {
		return "", fmt.Errorf("failed to add file: %s: %w", string(output), err)
	}

	ctxCommit, cancelCommit := ghTimeoutContext(context.Background())
	defer cancelCommit()
	output, err = s.runner.Run(ctxCommit, "git", "commit", "-m", "feat(ci): Setup infer workflow")
	if err != nil {
		return "", fmt.Errorf("failed to commit: %s: %w", string(output), err)
	}

	ctxPush, cancelPush := ghTimeoutContext(context.Background())
	defer cancelPush()
	output, err = s.runner.Run(ctxPush, "git", "push", "-u", "origin", branch)
	if err != nil {
		return "", fmt.Errorf("failed to push: %s: %w", string(output), err)
	}

	title := "feat(ci): Setup infer workflow"
	body := `## Summary

This PR sets up the infer workflow for automated code review and assistance.

## Changes

- Added infer workflow configuration
- Configured to trigger on @infer mentions in issues

## Testing

After merging, @infer mentions in issues will trigger the bot.

🤖 Generated with infer`

	ctxPR, cancelPR := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelPR()
	_, err = s.runner.Run(ctxPR, "gh", "pr", "create",
		"--base", baseBranch,
		"--head", branch,
		"--title", title,
		"--body", body,
		"--web")
	if err != nil {
		return "", fmt.Errorf("failed to open PR creation page: %w", err)
	}

	return fmt.Sprintf("https://github.com/%s/compare/%s...%s", repo, baseBranch, branch), nil
}
