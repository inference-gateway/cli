package setup

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CommandRunner is an injectable interface for running subprocesses. Tests
// supply a fake runner so no real gh/git calls are needed.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// RealRunner shells out using exec.CommandContext.
type RealRunner struct{}

// Run returns stdout on success and stderr on failure, so callers that embed
// the bytes in an error surface the real git/gh diagnostic.
func (r *RealRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return exitErr.Stderr, err
	}
	return out, err
}

// Service implements agentdomain.GitHubSetupService.
type Service struct {
	runner   CommandRunner
	runAgent AgentRunFunc
}

// NewService creates a new Service with the given runner.
func NewService(runner CommandRunner) *Service {
	return &Service{runner: runner}
}

// Version pins and defaults for generated GitHub workflows.
const (
	InferActionVersion          = "v0.49.2"
	CheckoutActionVersion       = "v7.0.1"
	AppTokenActionVersion       = "v3.2.0"
	UploadArtifactActionVersion = "v7.0.1"
	DefaultWorkflowModel        = "ollama_cloud/deepseek-v4-flash:preview"
)

// ghTimeoutContext returns a context with a 30-second timeout bounding a single
// git/gh subprocess so a wedged command cannot hang the UI.
func ghTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// GetCurrentRepo returns the current GitHub repository name with owner.
func (s *Service) GetCurrentRepo() (string, error) {
	ctx, cancel := ghTimeoutContext()
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

	ctx, cancel := ghTimeoutContext()
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
	ctx, cancel := ghTimeoutContext()
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
	ctx, cancel := ghTimeoutContext()
	defer cancel()

	output, err := s.runner.Run(ctx, "gh", "secret", "set", name, "--org", orgName, "--visibility", "all", "--body", value)
	if err != nil {
		return fmt.Errorf("%s: %w", string(output), err)
	}
	return nil
}
