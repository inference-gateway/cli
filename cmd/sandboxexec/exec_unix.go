//go:build unix

package sandboxexec

import (
	"fmt"
	"os"
	"os/exec"

	unix "golang.org/x/sys/unix"
)

// execProgram replaces this process with the program, so the confinement
// applied to this process carries over without a wrapper left behind.
func execProgram(args []string) error {
	program, err := exec.LookPath(args[0])
	if err != nil {
		return fmt.Errorf("resolving %q: %w", args[0], err)
	}
	return unix.Exec(program, args, os.Environ())
}
