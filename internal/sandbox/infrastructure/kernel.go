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

// ReexecCommand is the hidden infer subcommand Linux re-runs itself through to
// apply Restrict before becoming bash. The other platforms never run it.
const ReexecCommand = "__sandbox"

// Command returns the bash child that runs command. Where the policy enforces
// and the platform can, the kernel confines the child's writes to
// sandbox.WritePaths, so the command cannot write past the policy whatever its
// arguments say. The Bash tool uses it for a command the allow-list runs
// unattended: one somebody approved runs as approved. Reads stay with the path check.
func Command(ctx context.Context, cfg *config.Config, mode agentdomain.AgentMode, command string) *exec.Cmd {
	if !Enforced(cfg) {
		return process.CommandContext(ctx, "bash", "-c", command)
	}
	return confinedCommand(ctx, sandbox.WritePaths(cfg, mode), command)
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
