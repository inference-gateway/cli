package audio

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestWAVDurationSeconds(t *testing.T) {
	t.Run("valid wav", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a.wav")
		if err := writeTestWAV(path, 24000, 2.0); err != nil {
			t.Fatal(err)
		}
		got, err := WAVDurationSeconds(path)
		if err != nil {
			t.Fatalf("WAVDurationSeconds: %v", err)
		}
		if got < 1.99 || got > 2.01 {
			t.Errorf("duration = %v, want ~2.0", got)
		}
	})

	t.Run("not a wav", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "b.wav")
		if err := os.WriteFile(path, []byte("not a wave"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := WAVDurationSeconds(path); err == nil {
			t.Error("expected error for non-WAV content")
		}
	})
}

// writeTestWAV writes a minimal valid 16-bit mono PCM WAV.
func writeTestWAV(path string, sampleRate int, seconds float64) error {
	n := int(float64(sampleRate) * seconds * 2)
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+n))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(n))
	buf.Write(make([]byte, n))
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
