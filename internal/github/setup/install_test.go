package setup

import (
	"strings"
	"testing"
)

func TestInstallChatPrompt(t *testing.T) {
	p := InstallChatPrompt("octo/repo", "Timeout: 25 minutes")
	for _, want := range []string{"/opentask", "octo/repo", InstallBranch, "tasks.yml", "bash-allow-append must start with `task( .*)?`", "Workflow configuration:", "Timeout: 25 minutes"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
	if p2 := InstallChatPrompt("", ""); !strings.Contains(p2, "gh repo view") || strings.Contains(p2, "Workflow configuration:") {
		t.Fatalf("empty-arg prompt wrong:\n%s", p2)
	}
}
