package components

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
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

	e, readCmd, err := startPTYEditor(path, dir, 40, 10, true)
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

func TestEditorColorArgs(t *testing.T) {
	dark := []string{"-c", "set background=dark", "-c", "syntax enable", "-c", "set number"}
	light := []string{"-c", "set background=light", "-c", "syntax enable", "-c", "set number"}

	tests := []struct {
		bin  string
		dark bool
		want []string
	}{
		{"vim", true, dark},
		{"nvim", true, dark},
		{"/usr/bin/vim", true, dark},
		{"gvim", true, dark},
		{"vi", true, dark},
		{"vim", false, light},
		{"cat", true, nil},
		{"code", true, nil},
		{"nano", true, nil},
		{"view", true, nil}, // read-only vim: not in the family, left untouched
	}
	for _, tt := range tests {
		if got := editorColorArgs(tt.bin, tt.dark); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("editorColorArgs(%q, %v) = %v, want %v", tt.bin, tt.dark, got, tt.want)
		}
	}
}

func TestBuildEditorArgv(t *testing.T) {
	got := buildEditorArgv([]string{"nvim", "-p"}, "/tmp/f.go", true)
	want := []string{"nvim", "-p", "-c", "set background=dark", "-c", "syntax enable", "-c", "set number", "/tmp/f.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vim-family argv = %v, want %v", got, want)
	}

	// non-vim: no flags injected, file appended directly.
	got = buildEditorArgv([]string{"nano"}, "/tmp/f.go", true)
	want = []string{"nano", "/tmp/f.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("non-vim argv = %v, want %v", got, want)
	}
}

// sgrColorRe matches an SGR foreground-color parameter (basic 30-37, bright 90-97,
// or 256/truecolor 38;…) bounded by a CSI introducer/separator on each side.
var sgrColorRe = regexp.MustCompile(`(\x1b\[|;)(3[0-7]|9[0-7]|38)(;|m)`)

// TestPTYEditor_VimEmitsColor is the end-to-end guard: with the forced
// background/syntax flags, a real vim editing a .go file must emit SGR color that
// survives the emulator. Best-effort - skips when vim is unavailable (or -short) so
// it never blocks minimal CI.
func TestPTYEditor_VimEmitsColor(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns vim; skipped in -short")
	}
	if _, err := exec.LookPath("vim"); err != nil {
		t.Skip("vim not available")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vim")

	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	src := "package main\n\n// a comment\nfunc main() { _ = \"hi\" }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	e, readCmd, err := startPTYEditor(path, dir, 80, 24, true)
	if err != nil {
		t.Skipf("PTY unavailable: %v", err)
	}
	defer e.close()

	// vim draws then idles waiting for input; the re-arming reader would block once
	// it goes idle. Pump PTY output over a channel from a reader goroutine (which
	// never touches the emulator), and feed the emulator only here in the main
	// goroutine until a quiet period - so there is no concurrent emulator access.
	out := make(chan []byte, 64)
	done := make(chan struct{})
	go func() {
		for readCmd != nil {
			o, ok := readCmd().(ptyOutputMsg)
			if !ok {
				return // ptyExitMsg
			}
			select {
			case out <- o.data:
				readCmd = e.readCmd()
			case <-done:
				return
			}
		}
	}()
	defer close(done)

	got := false
	quiet := time.NewTimer(800 * time.Millisecond)
	defer quiet.Stop()
	hard := time.After(4 * time.Second)
loop:
	for {
		select {
		case data := <-out:
			e.term.write(data)
			got = true
			if !quiet.Stop() {
				<-quiet.C
			}
			quiet.Reset(800 * time.Millisecond)
		case <-quiet.C:
			if got {
				break loop // vim finished drawing
			}
			quiet.Reset(800 * time.Millisecond)
		case <-hard:
			break loop
		}
	}

	if !got {
		t.Skip("no PTY output from vim")
	}
	if rendered := e.View(80, 24); !sgrColorRe.MatchString(rendered) {
		t.Errorf("expected vim to emit SGR color, found none in render:\n%q", rendered)
	}
}
