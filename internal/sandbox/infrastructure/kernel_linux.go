package sandboxinfra

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	landlock "github.com/landlock-lsm/go-landlock/landlock"

	process "github.com/inference-gateway/cli/internal/platform/process"
)

const kernelSupported = true

// confinedCommand re-runs this binary so the Landlock ruleset is applied in
// the child before it becomes bash: a ruleset binds the calling process and is
// inherited, so the parent cannot apply it on the child's behalf.
func confinedCommand(ctx context.Context, writePaths []string, command string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return process.CommandContext(ctx, "bash", "-c", command)
	}
	args := []string{ReexecCommand}
	for _, path := range writePaths {
		args = append(args, "--write", path)
	}
	args = append(args, "--", "bash", "-c", command)
	return process.CommandContext(ctx, exe, args...)
}

// Restrict confines this process and its descendants: everything stays
// readable and executable, writes land only under writePaths. Best effort, so
// a kernel without Landlock runs unconfined rather than failing the command.
func Restrict(writePaths []string) error {
	var dirs, files []string
	for _, path := range writePaths {
		info, err := os.Stat(path)
		switch {
		case err != nil:
			continue
		case info.IsDir():
			dirs = append(dirs, path)
		default:
			files = append(files, path)
		}
	}
	err := landlock.V11.BestEffort().RestrictPaths(
		landlock.RODirs("/"),
		landlock.RWDirs(dirs...).IgnoreIfMissing(),
		landlock.RWFiles(files...).IgnoreIfMissing(),
	)
	if err != nil {
		return fmt.Errorf("applying the kernel sandbox: %w", err)
	}
	return nil
}
