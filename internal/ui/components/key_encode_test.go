package components

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEncodeKey(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyPressMsg
		want []byte
	}{
		{"letter", tea.KeyPressMsg{Code: 'a', Text: "a"}, []byte("a")},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, []byte{'\r'}},
		{"escape", tea.KeyPressMsg{Code: tea.KeyEscape}, []byte{0x1b}},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}, []byte{'\t'}},
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, []byte{0x7f}},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, []byte("\x1b[A")},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, []byte("\x1b[B")},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}, []byte("\x1b[C")},
		{"left", tea.KeyPressMsg{Code: tea.KeyLeft}, []byte("\x1b[D")},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}, []byte("\x1b[3~")},
		{"pgup", tea.KeyPressMsg{Code: tea.KeyPgUp}, []byte("\x1b[5~")},
		{"ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, []byte{0x03}},
		{"ctrl+w", tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}, []byte{0x17}},
		{"alt+b", tea.KeyPressMsg{Code: 'b', Text: "b", Mod: tea.ModAlt}, []byte{0x1b, 'b'}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodeKey(tc.key); !bytes.Equal(got, tc.want) {
				t.Errorf("encodeKey(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
