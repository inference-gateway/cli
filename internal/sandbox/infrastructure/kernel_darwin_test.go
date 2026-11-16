package sandboxinfra

import (
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestSeatbeltProfile(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  string
	}{
		{
			name: "no paths denies every write",
			want: `(version 1)(allow default)(deny file-write*)`,
		},
		{
			name:  "paths are quoted and escaped",
			paths: []string{"/a b", `/q"x`},
			want:  `(version 1)(allow default)(deny file-write* (require-not (require-any (subpath "/a b") (subpath "/q\"x"))))`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seatbeltProfile(tt.paths); got != tt.want {
				t.Fatalf("seatbeltProfile(%q)\n got %s\nwant %s", tt.paths, got, tt.want)
			}
		})
	}
}

// TestSeatbeltProfile_ListsResolvedSpelling: /tmp is a symlink to /private/tmp
// on macOS and Seatbelt matches the resolved path, so both spellings appear.
func TestSeatbeltProfile_ListsResolvedSpelling(t *testing.T) {
	got := seatbeltProfile([]string{"/tmp"})
	for _, want := range []string{`(subpath "/tmp")`, `(subpath "/private/tmp")`} {
		if !strings.Contains(got, want) {
			t.Errorf("seatbeltProfile([/tmp]) = %s, missing %s", got, want)
		}
	}
}

func TestCommand_RunsUnderSeatbelt(t *testing.T) {
	cmd := Command(t.Context(), config.DefaultConfig(), agentdomain.AgentModeStandard, "echo hi")

	if cmd.Args[0] != seatbeltPath || cmd.Args[1] != "-p" || cmd.Args[len(cmd.Args)-1] != "echo hi" {
		t.Fatalf("Command args = %q, want %s -p <profile> bash -c 'echo hi'", cmd.Args, seatbeltPath)
	}
}
