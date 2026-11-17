package sandboxinfra

import (
	"context"
	"os/exec"
	"strings"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	process "github.com/inference-gateway/cli/internal/platform/process"
	sandbox "github.com/inference-gateway/cli/internal/sandbox"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

// Child is the bash child for one command, with the confinement its platform
// applies: Landlock on Linux, Seatbelt on macOS, nothing elsewhere. Cmd is set
// up like any exec.Cmd and must Start through Child. WritePaths is what the
// kernel lets it write, nil when unconfined.
type Child struct {
	Cmd        *exec.Cmd
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
// sandbox.WritePaths and its reads of the blocking deny entries, so the command
// cannot reach past the policy whatever its arguments say. The Bash tool uses
// it unattended for allow-listed commands: approved ones run as approved.
func Command(ctx context.Context, cfg *config.Config, mode agentdomain.AgentMode, command string) *Child {
	if !Enforced(cfg) {
		return Unconfined(ctx, command)
	}
	return confinedChild(ctx, sandbox.WritePaths(cfg, mode), sandbox.BlockingDenials(cfg), command)
}

// Enforced reports whether Command confines its child on this platform.
func Enforced(cfg *config.Config) bool {
	return cfg.Tools.Sandbox.Filesystem.Enforce && kernelSupported
}

// KernelName names the confinement this platform applies, empty where there is none.
func KernelName() string {
	return kernelName
}

// KernelDenied lists the blocking deny entries the kernel itself refuses to
// read, rename or delete under Command, empty where the platform cannot.
func KernelDenied(cfg *config.Config) []sandboxdomain.Denied {
	return kernelDenied(sandbox.BlockingDenials(cfg))
}

// DeniedHint names the sandbox when a confined command failed on a write or
// a read the kernel refused, so the model asks for the path instead of retrying.
func DeniedHint(output string) string {
	if !strings.Contains(output, "Operation not permitted") && !strings.Contains(output, "Permission denied") {
		return ""
	}
	return "the sandbox confines writes to the writable paths of ~/.infer/" + config.SandboxFileName +
		" (filesystem.allowed) and refuses to read, rename or delete its denied files (filesystem.denied):" +
		" work inside them, or ask the user to approve the command or add the path"
}
