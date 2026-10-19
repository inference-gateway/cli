//go:build windows

package process

import (
	"context"
	"os/exec"
	"testing"

	windows "golang.org/x/sys/windows"
)

func TestCommandsSuppressTheConsoleWindow(t *testing.T) {
	cases := map[string]*exec.Cmd{
		"Command":        Command("cmd", "/c", "exit"),
		"CommandContext": CommandContext(context.Background(), "cmd", "/c", "exit"),
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
				t.Fatal("HideWindow not set")
			}
			if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
				t.Fatal("CREATE_NO_WINDOW not set")
			}
		})
	}
}
