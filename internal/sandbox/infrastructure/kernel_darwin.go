package sandboxinfra

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	config "github.com/inference-gateway/cli/config"
	process "github.com/inference-gateway/cli/internal/platform/process"
)

const kernelSupported = true

// seatbeltPath is the system binary and never resolved through PATH, so a
// binary planted in the project cannot stand in for it.
const seatbeltPath = "/usr/bin/sandbox-exec"

// confinedCommand runs bash under a Seatbelt profile that allows everything
// except writes outside writePaths. Seatbelt matches the resolved path, so
// each path is listed as written and with its symlinks resolved.
func confinedCommand(ctx context.Context, writePaths []string, command string) *exec.Cmd {
	return process.CommandContext(ctx, seatbeltPath, "-p", seatbeltProfile(writePaths), "bash", "-c", command)
}

func seatbeltProfile(writePaths []string) string {
	var spellings []string
	for _, path := range writePaths {
		for _, spelling := range []string{path, config.RealPath(path)} {
			if !slices.Contains(spellings, spelling) {
				spellings = append(spellings, spelling)
			}
		}
	}
	if len(spellings) == 0 {
		return "(version 1)(allow default)(deny file-write*)"
	}
	var profile strings.Builder
	profile.WriteString("(version 1)(allow default)(deny file-write* (require-not (require-any")
	for _, spelling := range spellings {
		fmt.Fprintf(&profile, " (subpath %q)", spelling)
	}
	profile.WriteString(")))")
	return profile.String()
}

// Restrict is the Linux re-exec step. macOS confines the child from the parent.
func Restrict([]string) error {
	return errors.New("the kernel sandbox re-exec step is Linux only")
}
