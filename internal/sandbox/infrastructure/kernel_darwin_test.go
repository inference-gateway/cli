package sandboxinfra

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

func TestSeatbeltProfile(t *testing.T) {
	tests := []struct {
		name   string
		paths  []string
		denied []sandboxdomain.Denied
		want   string
	}{
		{
			name: "no paths denies every write",
			want: "(version 1)(allow default)(deny file-write*)",
		},
		{
			name:  "paths are quoted",
			paths: []string{"/a b", `/q"x`},
			want:  `(version 1)(allow default)(deny file-write* (require-not (require-any (subpath "/a b") (subpath "/q\"x"))))`,
		},
		{
			name:   "denied directory entries stay with the path check",
			denied: []sandboxdomain.Denied{{Path: ".git/"}, {Path: "secrets/*"}},
			want:   `(version 1)(allow default)(deny file-write*)`,
		},
		{
			name:   "denied exact name matches at any depth",
			denied: []sandboxdomain.Denied{{Path: "auth.yaml"}},
			want:   `(version 1)(allow default)(deny file-write*)(deny file-read-data file-write-unlink (require-any (regex #"(^|/)auth\.yaml$")))`,
		},
		{
			name:   "denied glob carves out its excepts",
			denied: []sandboxdomain.Denied{{Path: "*.env.*", Except: []string{".env.example"}}},
			want:   `(version 1)(allow default)(deny file-write*)(deny file-read-data file-write-unlink (require-all (require-any (regex #"(^|/)[^/]*\.env\.[^/]*$")) (require-not (require-any (regex #"(^|/)\.env\.example$")))))`,
		},
		{
			name:   "a glob the regex cannot say exactly is skipped",
			denied: []sandboxdomain.Denied{{Path: "id_?sa"}},
			want:   `(version 1)(allow default)(deny file-write*)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seatbeltProfile(tt.paths, tt.denied); got != tt.want {
				t.Fatalf("seatbeltProfile(%q, %+v)\n got %s\nwant %s", tt.paths, tt.denied, got, tt.want)
			}
		})
	}
}

// TestSeatbeltProfile_ListsResolvedSpelling: /tmp is a symlink to /private/tmp
// on macOS and Seatbelt matches the resolved path, so both spellings appear.
func TestSeatbeltProfile_ListsResolvedSpelling(t *testing.T) {
	got := seatbeltProfile([]string{"/tmp"}, nil)
	for _, want := range []string{`(subpath "/tmp")`, `(subpath "/private/tmp")`} {
		if !strings.Contains(got, want) {
			t.Errorf("seatbeltProfile([/tmp]) = %s, missing %s", got, want)
		}
	}
}

// TestSeatbeltProfile_DeniedAnchoredEntry: an anchored deny entry is denied as
// its expanded path, in the written and resolved spellings like a write path,
// unless it is a directory on disk, which stays with the path check.
func TestSeatbeltProfile_DeniedAnchoredEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	secret := filepath.Join(home, "secrets.yaml")
	if err := os.MkdirAll(filepath.Join(home, "vault"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := seatbeltProfile(nil, []sandboxdomain.Denied{{Path: "~/secrets.yaml"}, {Path: "~/vault"}})
	want := "(version 1)(allow default)(deny file-write*)(deny file-read-data file-write-unlink (require-any"
	spellings := []string{secret}
	if resolved := config.RealPath(secret); resolved != secret {
		spellings = append(spellings, resolved)
	}
	for _, spelling := range spellings {
		want += fmt.Sprintf(" (subpath %q)", spelling)
	}
	want += "))"
	if got != want {
		t.Fatalf("seatbeltProfile(nil, [~/secrets.yaml ~/vault])\n got %s\nwant %s", got, want)
	}
}

func TestCommand_RunsUnderSeatbelt(t *testing.T) {
	args := Command(t.Context(), config.DefaultConfig(), agentdomain.AgentModeStandard, "echo hi").Cmd.Args

	if args[0] != seatbeltPath || args[1] != "-p" || args[len(args)-1] != "echo hi" {
		t.Fatalf("Command args = %q, want %s -p <profile> bash -c 'echo hi'", args, seatbeltPath)
	}
}

// TestStart_DeniesReadingBlockedFiles: a confined child can neither read, copy,
// rename nor reach through a symlink a file a blocking deny entry names, while
// a plain file beside it stays readable. The temp dir is writable, so only the
// secret itself is what the kernel refuses.
func TestStart_DeniesReadingBlockedFiles(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "auth.yaml")
	if err := os.WriteFile(secret, []byte("mocked-credential"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		command string
		refused bool
	}{
		{name: "read", command: "cat " + secret, refused: true},
		{name: "copy", command: fmt.Sprintf("cp %s %s/copy.txt", secret, dir), refused: true},
		{name: "read through a symlink", command: fmt.Sprintf("ln -s %s %s/link.txt && cat %s/link.txt", secret, dir, dir), refused: true},
		{name: "rename", command: fmt.Sprintf("mv %s %s/moved.txt", secret, dir), refused: true},
		{name: "a plain file beside it", command: "cat " + filepath.Join(dir, "plain.txt")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child := Command(t.Context(), config.DefaultConfig(), agentdomain.AgentModeStandard, tt.command)
			var output bytes.Buffer
			child.Cmd.Stdout, child.Cmd.Stderr = &output, &output
			if err := child.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			err := child.Cmd.Wait()
			if refused := err != nil && strings.Contains(output.String(), "Operation not permitted"); refused != tt.refused {
				t.Fatalf("%s: wait err = %v, output %q, want refused %v", tt.command, err, output.String(), tt.refused)
			}
			if strings.Contains(output.String(), "mocked-credential") {
				t.Fatalf("%s leaked the secret: %q", tt.command, output.String())
			}
		})
	}

	for _, leftover := range []string{"copy.txt", "moved.txt"} {
		if _, err := os.Stat(filepath.Join(dir, leftover)); err == nil {
			t.Fatalf("%s exists, the kernel let the secret out", leftover)
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("the secret must stay in place: %v", err)
	}
}
