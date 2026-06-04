package components

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := resolveEditor(); len(got) != 1 || got[0] != "vim" {
		t.Errorf("default = %v, want [vim]", got)
	}

	t.Setenv("EDITOR", "nvim -p")
	if got := resolveEditor(); len(got) != 2 || got[0] != "nvim" || got[1] != "-p" {
		t.Errorf("EDITOR split = %v, want [nvim -p]", got)
	}

	t.Setenv("VISUAL", "code --wait")
	if got := resolveEditor(); got[0] != "code" {
		t.Errorf("VISUAL should take precedence, got %v", got)
	}
}

func TestVTTerm_RenderShowsOutput(t *testing.T) {
	term := newVTTerm(40, 5)
	term.write([]byte("hello world"))
	if got := stripANSI(term.render()); !strings.Contains(got, "hello world") {
		t.Errorf("render = %q, want it to contain 'hello world'", got)
	}
}

// TestPTYEditor_SpawnRenderExit exercises the real PTY path with `cat <file>`,
// which prints the file then exits (so it never blocks on input).
func TestPTYEditor_SpawnRenderExit(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "cat")

	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("HELLO_PTY\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e, readCmd, err := startPTYEditor(path, dir, 40, 10)
	if err != nil {
		t.Skipf("PTY unavailable: %v", err)
	}
	defer e.close()

	sawOutput := false
	for i := 0; i < 200 && readCmd != nil; i++ {
		switch m := readCmd().(type) {
		case ptyOutputMsg:
			e.term.write(m.data)
			sawOutput = true
			readCmd = e.readCmd()
		case ptyExitMsg:
			readCmd = nil
		}
	}

	if !sawOutput {
		t.Fatal("expected PTY output from cat")
	}
	if got := stripANSI(e.View(40, 10)); !strings.Contains(got, "HELLO_PTY") {
		t.Errorf("rendered screen missing file content:\n%s", got)
	}
}

// TestVTTerm_AnswersQueries guards the freeze fix: a terminal query (DSR cursor
// position) must produce a reply readable via readReply, so forwardReplies can
// send it to the child. Without draining, the reply write blocks forever.
func TestVTTerm_AnswersQueries(t *testing.T) {
	term := newVTTerm(20, 5)
	defer term.closeEmulator()

	go term.write([]byte("\x1b[6n"))

	buf := make([]byte, 64)
	n, err := term.readReply(buf)
	if n == 0 {
		t.Fatalf("expected a query reply, got none (err=%v)", err)
	}
	if buf[n-1] != 'R' {
		t.Errorf("expected a cursor-position report ending in 'R', got %q", buf[:n])
	}
}
