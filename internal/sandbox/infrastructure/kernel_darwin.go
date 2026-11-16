package sandboxinfra

import (
	"context"
	"fmt"
	"slices"
	"strings"

	config "github.com/inference-gateway/cli/config"
	process "github.com/inference-gateway/cli/internal/platform/process"
)

const kernelSupported = true

// seatbeltPath is the system binary and never resolved through PATH, so a
// binary planted in the project cannot stand in for it.
const seatbeltPath = "/usr/bin/sandbox-exec"

// confinedChild runs bash under a Seatbelt profile that allows everything
// except writes outside writePaths. Seatbelt matches the resolved path, so
// each path is listed as written and with its symlinks resolved.
func confinedChild(ctx context.Context, writePaths []string, command string) *Child {
	return &Child{Cmd: process.CommandContext(ctx, seatbeltPath, "-p", seatbeltProfile(writePaths), "bash", "-c", command)}
}

// start is a plain start: the profile travels in the command line.
func (c *Child) start() error {
	return c.Cmd.Start()
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
