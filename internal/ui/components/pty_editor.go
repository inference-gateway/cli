package components

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	vt "github.com/charmbracelet/x/vt"
	pty "github.com/creack/pty"
)

// ptyOutputMsg carries a chunk of the editor child's terminal output.
type ptyOutputMsg struct{ data []byte }

// ptyExitMsg signals the editor child exited (or the PTY was closed).
type ptyExitMsg struct{ err error }

// vtTerm wraps the x/vt emulator. It isolates the (pre-release) dependency - to
// swap emulators, change only this type. The emulator's Render() draws cells but
// no visible cursor, so render() overlays a reverse-video block at the cursor
// (vim relies on the terminal's hardware cursor, which we don't have in a pane).
type vtTerm struct{ em *vt.Emulator }

func newVTTerm(cols, rows int) *vtTerm { return &vtTerm{em: vt.NewEmulator(cols, rows)} }

func (t *vtTerm) write(p []byte)        { _, _ = t.em.Write(p) }
func (t *vtTerm) resize(cols, rows int) { t.em.Resize(cols, rows) }

func (t *vtTerm) render() string {
	pos := t.em.CursorPosition()
	cell := t.em.CellAt(pos.X, pos.Y)
	if cell == nil {
		return t.em.Render()
	}
	saved := cell.Style.Attrs
	cell.Style.Attrs |= uv.AttrReverse
	out := t.em.Render()
	cell.Style.Attrs = saved
	return out
}

// readReply reads escape-sequence replies the emulator generated in response to
// the child's terminal queries (device attributes, cursor/status reports, …).
func (t *vtTerm) readReply(p []byte) (int, error) { return t.em.Read(p) }

// closeEmulator unblocks readReply so the reply-forwarding goroutine can exit.
func (t *vtTerm) closeEmulator() { _ = t.em.Close() }

// forwardReplies pumps the emulator's query replies back to the child's PTY.
// The emulator buffers replies in an unbuffered pipe, so without draining it the
// first reply blocks the write that produced it - freezing the editor (and the
// whole UI) on a child that is itself waiting for our answer.
func forwardReplies(term *vtTerm, w io.Writer) {
	buf := make([]byte, 256)
	for {
		n, err := term.readReply(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// ptyEditor runs the user's editor in a pseudo-terminal and renders it into the
// diff pane. Output is streamed via a re-arming read command; key input is
// re-encoded and written to the PTY; the pane size drives the PTY window size.
type ptyEditor struct {
	pty  *os.File
	cmd  *exec.Cmd
	term *vtTerm
	cols int
	rows int
}

// resolveEditor returns the editor argv: $VISUAL, then $EDITOR, else vim.
func resolveEditor() []string {
	for _, v := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if f := strings.Fields(v); len(f) > 0 {
			return f
		}
	}
	return []string{"vim"}
}

// editorColorArgs returns the flags that force on-screen syntax highlighting for
// vim-family editors. A bare vim spawned with no vimrc can't detect the emulator's
// background (it doesn't answer vim's OSC-11 query), so the default colorscheme
// renders washed-out - looking like no highlighting at all. We pin the background to
// the TUI theme and enable syntax. The flags run after the user's own vimrc (if any),
// so they only force these two settings. Non-vim editors get nil (they do their own
// highlighting, or don't accept `-c`).
func editorColorArgs(editorBin string, dark bool) []string {
	switch strings.ToLower(filepath.Base(editorBin)) {
	case "vim", "nvim", "vi", "gvim", "mvim":
		bg := "dark"
		if !dark {
			bg = "light"
		}
		return []string{"-c", "set background=" + bg, "-c", "syntax enable"}
	}
	return nil
}

// buildEditorArgv assembles the editor command line: the resolved editor (and its own
// args), then the color flags for vim-family editors, then the file. It builds a fresh
// slice so it never aliases resolveEditor's backing array.
func buildEditorArgv(editor []string, absPath string, dark bool) []string {
	flags := editorColorArgs(editor[0], dark)
	argv := make([]string, 0, len(editor)+len(flags)+1)
	argv = append(argv, editor...)
	argv = append(argv, flags...)
	argv = append(argv, absPath)
	return argv
}

// startPTYEditor launches the resolved editor on absPath in a PTY sized
// cols×rows (rooted at workdir) and returns the editor with its initial read cmd.
// dark selects the editor background (matching the active TUI theme) for vim-family
// editors so their syntax colors read well against the pane.
func startPTYEditor(absPath, workdir string, cols, rows int, dark bool) (*ptyEditor, tea.Cmd, error) {
	cols, rows = max(cols, 1), max(rows, 1)
	argv := buildEditorArgv(resolveEditor(), absPath, dark)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, nil, err
	}
	e := &ptyEditor{pty: f, cmd: cmd, term: newVTTerm(cols, rows), cols: cols, rows: rows}
	go forwardReplies(e.term, f)
	return e, e.readCmd(), nil
}

// readCmd blocks on the next chunk of PTY output, returning it as a message (or
// ptyExitMsg when the child exits / the PTY closes). It is re-armed after each
// ptyOutputMsg so the stream continues.
func (e *ptyEditor) readCmd() tea.Cmd {
	f := e.pty
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := f.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			return ptyOutputMsg{data: data}
		}
		return ptyExitMsg{err: err}
	}
}

// write forwards encoded key input to the editor's PTY.
func (e *ptyEditor) write(b []byte) {
	if len(b) > 0 {
		_, _ = e.pty.Write(b)
	}
}

// resize matches the PTY (and emulator) to the pane, sending the child SIGWINCH.
func (e *ptyEditor) resize(cols, rows int) {
	cols, rows = max(cols, 1), max(rows, 1)
	if cols == e.cols && rows == e.rows {
		return
	}
	e.cols, e.rows = cols, rows
	_ = pty.Setsize(e.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	e.term.resize(cols, rows)
}

// View resizes to the pane and renders the current terminal screen.
func (e *ptyEditor) View(width, height int) string {
	e.resize(width, height)
	return e.term.render()
}

// close kills the child and closes the PTY (unblocking any in-flight read) and
// the emulator (unblocking the reply-forwarding goroutine).
func (e *ptyEditor) close() {
	e.term.closeEmulator()
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
	if e.pty != nil {
		_ = e.pty.Close()
	}
	if e.cmd != nil {
		_ = e.cmd.Wait()
	}
}
