//go:build !unix

package customtools

import "os/exec"

// killProcessGroupOnCancel keeps exec's default cancel, which kills only the
// direct child.
func killProcessGroupOnCancel(cmd *exec.Cmd) {}
