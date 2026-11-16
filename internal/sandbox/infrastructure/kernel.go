package sandboxinfra

import (
	"context"
	"os/exec"
	"strings"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	process "github.com/inference-gateway/cli/internal/platform/process"
	sandbox "github.com/inference-gateway/cli/internal/sandbox"
)

// Child is the bash child for one command, with the confinement its platform
// applies: a Landlock ruleset on the thread that forks it on Linux, a Seatbelt
// profile around it on macOS, nothing elsewhere. Cmd is set up like any
// exec.Cmd, and Start is the one call that has to go through Child.
type Child struct {
	Cmd *exec.Cmd
	// WritePaths is what the kernel lets the child write, nil when unconfined.
	WritePaths []string
}

// Start starts the child the way its platform confines it.
func (c *Child) Start() error {
	return c.start()
}

// Unconfined returns the bash child that runs command with the permissions of
// the CLI process, for a command somebody approved.
func Unconfined(ctx context.Context, command string) *Child {
	return &Child{Cmd: process.CommandContext(ctx, "bash", "-c", command)}
}

// Command returns the bash child that runs command. Where the policy enforces
// and the platform can, the kernel confines the child's writes to
// sandbox.WritePaths, so the command cannot write past the policy whatever its
// arguments say. The Bash tool uses it for a command the allow-list runs
// unattended: one somebody approved runs as approved. Reads stay with the path check.
func Command(ctx context.Context, cfg *config.Config, mode agentdomain.AgentMode, command string) *Child {
	if !Enforced(cfg) {
		return Unconfined(ctx, command)
	}
	return confinedChild(ctx, sandbox.WritePaths(cfg, mode), command)
}

// Enforced reports whether Command confines its child on this platform.
func Enforced(cfg *config.Config) bool {
	return cfg.Tools.Sandbox.Filesystem.Enforce && kernelSupported
}

// DeniedHint names the sandbox when a confined command failed on a write the
// kernel refused, so the model asks for the path instead of retrying.
func DeniedHint(output string) string {
	if !strings.Contains(output, "Operation not permitted") && !strings.Contains(output, "Permission denied") {
		return ""
	}
	return "the sandbox confines writes to the writable paths of ~/.infer/" + config.SandboxFileName +
		" (filesystem.allowed): write inside them, or ask the user to approve the command or add the path"
}
