//go:build !windows

package process

import "os/exec"

func hideConsole(*exec.Cmd) {}
