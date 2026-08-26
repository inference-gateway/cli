package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentrunner "github.com/inference-gateway/cli/internal/agent/application/agentrunner"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// InstallBranch is the fixed head branch for agent-driven workflow installs.
// Keeping it stable means a re-install pushes onto the same branch and updates
// the already-open PR instead of opening a duplicate.
const InstallBranch = "infer/install-github-action"

// installNetworkTimeout bounds clone/push/PR subprocesses; installAgentTimeout
// bounds the LLM agent run, which explores the repo and can take minutes.
const (
	installShortTimeout   = 30 * time.Second
	installNetworkTimeout = 3 * time.Minute
	installAgentTimeout   = 20 * time.Minute
)

// AgentRunFunc matches agentrunner.Run so tests can stub the LLM step.
type AgentRunFunc func(ctx context.Context, opts agentrunner.Options) (agentrunner.Result, error)

// SetAgentRunner overrides the agent runner (tests). Nil restores the default.
func (s *Service) SetAgentRunner(run AgentRunFunc) { s.runAgent = run }

func (s *Service) agentRunner() AgentRunFunc {
	if s.runAgent != nil {
		return s.runAgent
	}
	return agentrunner.Run
}

// InstallWorkflow clones the target repo, has an LLM agent create or update the
// infer-action workflow inside the checkout (preserving repo-specific
// customizations), then commits, pushes and opens - or updates - the install
// PR. It returns the PR URL.
func (s *Service) InstallWorkflow(ctx context.Context, opts agentdomain.InstallWorkflowOptions) (string, error) {
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		current, err := s.GetCurrentRepo()
		if err != nil {
			return "", err
		}
		repo = current
	}

	// Clone under /tmp: the default tool sandbox allows /tmp, so the headless
	// agent can read and write the checkout by absolute path.
	dir, err := os.MkdirTemp("/tmp", "infer-install-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if out, err := s.runTimed(ctx, installNetworkTimeout, "gh", "repo", "clone", repo, dir, "--", "--depth", "1"); err != nil {
		return "", fmt.Errorf("clone %s: %s: %w", repo, strings.TrimSpace(string(out)), err)
	}

	branchExisted := s.checkoutInstallBranch(ctx, dir)
	languages := s.repoLanguages(ctx, repo)
	prompt := installPrompt(installPromptParams{
		Dir:       dir,
		Repo:      repo,
		Languages: languages,
		Existing:  findInferWorkflows(dir),
		CIPath:    findCIWorkflow(dir),
		GitHubApp: opts.GitHubApp,
		Context:   opts.Context,
	})

	agentCtx, cancel := context.WithTimeout(ctx, installAgentTimeout)
	defer cancel()
	res, err := s.agentRunner()(agentCtx, agentrunner.Options{
		SessionID: fmt.Sprintf("install-github-action-%d", time.Now().UnixNano()),
		Prompt:    prompt,
		Model:     opts.Model,
	})
	if err != nil {
		return "", fmt.Errorf("workflow agent failed: %w (stderr: %s)", err, strings.TrimSpace(res.Stderr))
	}
	title, body := parseAgentPR(res.FinalAssistant)

	return s.publishInstall(ctx, dir, repo, title, body, branchExisted)
}

// checkoutInstallBranch checks out InstallBranch in the clone, on top of the
// remote branch when a previous install already pushed it (so the open PR is
// continued), or as a fresh local branch otherwise. Returns whether the remote
// branch existed.
func (s *Service) checkoutInstallBranch(ctx context.Context, dir string) bool {
	if _, err := s.runTimed(ctx, installNetworkTimeout, "git", "-C", dir, "fetch", "origin", InstallBranch); err == nil {
		if _, err := s.runTimed(ctx, installShortTimeout, "git", "-C", dir, "checkout", "-B", InstallBranch, "origin/"+InstallBranch); err == nil {
			return true
		}
	}
	_, _ = s.runTimed(ctx, installShortTimeout, "git", "-C", dir, "checkout", "-b", InstallBranch)
	return false
}

// repoLanguages returns the repository languages reported by GitHub,
// best-effort ("" on any error).
func (s *Service) repoLanguages(ctx context.Context, repo string) string {
	out, err := s.runTimed(ctx, installShortTimeout, "gh", "api", "repos/"+repo+"/languages", "--jq", "keys | join(\", \")")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// publishInstall commits everything under .github/workflows, pushes the
// install branch, and creates the PR (or reuses the one already open on the
// branch). Returns the PR URL.
func (s *Service) publishInstall(ctx context.Context, dir, repo, title, body string, branchExisted bool) (string, error) {
	if out, err := s.runTimed(ctx, installShortTimeout, "git", "-C", dir, "add", ".github/workflows"); err != nil {
		return "", fmt.Errorf("git add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// diff --cached --quiet exits 0 when nothing is staged.
	if _, err := s.runTimed(ctx, installShortTimeout, "git", "-C", dir, "diff", "--cached", "--quiet"); err == nil {
		return "", fmt.Errorf("the agent made no workflow changes - the workflow is already up to date")
	}
	if out, err := s.runTimed(ctx, installShortTimeout, "git", "-C", dir, "commit", "-m", title); err != nil {
		return "", fmt.Errorf("git commit: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := s.runTimed(ctx, installNetworkTimeout, "git", "-C", dir, "push", "-u", "origin", InstallBranch); err != nil {
		return "", fmt.Errorf("git push: %s: %w", strings.TrimSpace(string(out)), err)
	}

	if branchExisted {
		if out, err := s.runTimed(ctx, installShortTimeout, "gh", "pr", "view", InstallBranch, "--repo", repo, "--json", "url", "--jq", ".url"); err == nil {
			if url := strings.TrimSpace(string(out)); url != "" {
				return url, nil
			}
		}
	}
	out, err := s.runTimed(ctx, installShortTimeout, "gh", "pr", "create",
		"--repo", repo, "--head", InstallBranch, "--title", title, "--body", body)
	if err != nil {
		return "", fmt.Errorf("create PR: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *Service) runTimed(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.runner.Run(ctx, name, args...)
}

// findInferWorkflows lists workflow files in the checkout that already use
// inference-gateway/infer-action, relative to the checkout root.
func findInferWorkflows(dir string) []string {
	var found []string
	entries, err := os.ReadDir(filepath.Join(dir, ".github", "workflows"))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, ".github", "workflows", name))
		if err == nil && strings.Contains(string(data), "inference-gateway/infer-action") {
			found = append(found, filepath.Join(".github", "workflows", name))
		}
	}
	return found
}

// findCIWorkflow returns the repo-relative path of a conventional CI workflow
// to take inspiration from, or "".
func findCIWorkflow(dir string) string {
	for _, name := range []string{"ci.yml", "ci.yaml", "build.yml", "test.yml"} {
		if _, err := os.Stat(filepath.Join(dir, ".github", "workflows", name)); err == nil {
			return filepath.Join(".github", "workflows", name)
		}
	}
	return ""
}

// parseAgentPR extracts {"title","body"} from the agent's final message,
// tolerating code fences and surrounding prose. Falls back to a generic
// conventional title with the raw message as body so a successful edit is
// never thrown away over a formatting miss.
func parseAgentPR(answer string) (string, string) {
	title := "ci: update opentask agent workflow"
	body := strings.TrimSpace(answer)
	start := strings.Index(answer, "{")
	end := strings.LastIndex(answer, "}")
	if start >= 0 && end > start {
		var pr struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err := json.Unmarshal([]byte(answer[start:end+1]), &pr); err == nil && pr.Title != "" {
			return pr.Title, pr.Body
		}
	}
	return title, body
}
