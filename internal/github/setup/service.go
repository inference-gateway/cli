package setup

import (
	"context"
	"errors"
	"os/exec"

	process "github.com/inference-gateway/cli/internal/platform/process"
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
	cmd := process.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return exitErr.Stderr, err
	}
	return out, err
}

// Version pins and defaults for generated GitHub workflows, consumed by the
// scheduler's githubscheduler store and rendered workflow.
const (
	InferActionVersion          = "v0.49.2"
	CheckoutActionVersion       = "v7.0.1"
	AppTokenActionVersion       = "v3.2.0"
	UploadArtifactActionVersion = "v7.0.1"
	DefaultWorkflowModel        = "ollama_cloud/deepseek-v4-flash:preview"
)
