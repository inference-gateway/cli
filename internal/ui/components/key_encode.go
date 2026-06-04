package components

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// encodeKey converts a parsed Bubble Tea key event back into the raw terminal
// input bytes to feed a child process (vim) running in a PTY. Bubble Tea owns
// stdin and delivers structured key events, so to drive the child we re-encode
// them. This covers the keys an editor needs (text, control/alt combos, and the
// common special keys); it is intentionally a pragmatic subset, not a full
// terminal-input encoder.
func encodeKey(k tea.KeyPressMsg) []byte {
	alt := k.Mod&tea.ModAlt != 0

	if k.Mod&tea.ModCtrl != 0 {
		if b, ok := ctrlBytes(k.Code); ok {
			return withAlt(alt, b)
		}
	}
	if seq, ok := specialKey(k.Code); ok {
		return withAlt(alt, seq)
	}
	if k.Text != "" {
		return withAlt(alt, []byte(k.Text))
	}
	if k.Code != 0 && unicode.IsPrint(k.Code) {
		return withAlt(alt, []byte(string(k.Code)))
	}
	return nil
}

// withAlt prefixes the sequence with ESC when Alt (Meta) is held, the standard
// terminal convention for alt-modified keys.
func withAlt(alt bool, b []byte) []byte {
	if !alt || len(b) == 0 {
		return b
	}
	return append([]byte{0x1b}, b...)
}

// specialKey maps a non-printable key code to its terminal escape sequence.
func specialKey(code rune) ([]byte, bool) {
	switch code {
	case tea.KeyEnter:
		return []byte{'\r'}, true
	case tea.KeyTab:
		return []byte{'\t'}, true
	case tea.KeyEscape:
		return []byte{0x1b}, true
	case tea.KeyBackspace:
		return []byte{0x7f}, true
	case tea.KeySpace:
		return []byte{' '}, true
	case tea.KeyUp:
		return []byte("\x1b[A"), true
	case tea.KeyDown:
		return []byte("\x1b[B"), true
	case tea.KeyRight:
		return []byte("\x1b[C"), true
	case tea.KeyLeft:
		return []byte("\x1b[D"), true
	case tea.KeyHome:
		return []byte("\x1b[H"), true
	case tea.KeyEnd:
		return []byte("\x1b[F"), true
	case tea.KeyPgUp:
		return []byte("\x1b[5~"), true
	case tea.KeyPgDown:
		return []byte("\x1b[6~"), true
	case tea.KeyDelete:
		return []byte("\x1b[3~"), true
	case tea.KeyInsert:
		return []byte("\x1b[2~"), true
	}
	return nil, false
}

// ctrlBytes maps a Ctrl-modified key to its control byte (e.g. ctrl+a → 0x01).
func ctrlBytes(code rune) ([]byte, bool) {
	switch {
	case code >= 'a' && code <= 'z':
		return []byte{byte(code-'a') + 1}, true
	case code >= 'A' && code <= 'Z':
		return []byte{byte(code-'A') + 1}, true
	case code == ' ', code == '@':
		return []byte{0x00}, true
	case code == '[':
		return []byte{0x1b}, true
	case code == '\\':
		return []byte{0x1c}, true
	case code == ']':
		return []byte{0x1d}, true
	case code == '^':
		return []byte{0x1e}, true
	case code == '_':
		return []byte{0x1f}, true
	}
	return nil, false
}
