//go:build windows

package process

import (
	"context"
	"os/exec"
	"strings"
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

const consoleProbe = `Add-Type -TypeDefinition 'using System;using System.Runtime.InteropServices;public class ConsoleProbe{[DllImport("kernel32.dll")]public static extern IntPtr GetConsoleWindow();}'; [ConsoleProbe]::GetConsoleWindow().Equals([IntPtr]::Zero)`

func TestSpawnedChildReportsNoConsoleWindow(t *testing.T) {
	out, err := Command("powershell", "-NoProfile", "-NonInteractive", "-Command", consoleProbe).Output()
	if err != nil {
		t.Fatalf("powershell probe: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "True" {
		t.Fatalf("child reported a console window: %q", got)
	}
}
