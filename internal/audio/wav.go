package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// WAVDurationSeconds returns the duration of a RIFF/WAVE file in seconds by
// reading its fmt and data chunk headers. It lets callers report the duration
// of synthesized speech without external tools.
func WAVDurationSeconds(path string) (float64, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return 0, fmt.Errorf("opening wav: %w", err)
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, 12)
	if _, err := io.ReadFull(f, header); err != nil {
		return 0, fmt.Errorf("reading wav header: %w", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a WAV file: %s", path)
	}

	var (
		byteRate uint32
		dataSize uint64
	)
	chunk := make([]byte, 8)
	for {
		if _, err := io.ReadFull(f, chunk); err != nil {
			return 0, fmt.Errorf("reading wav chunks: %w", err)
		}
		size := binary.LittleEndian.Uint32(chunk[4:8])
		switch string(chunk[0:4]) {
		case "fmt ":
			if size < 16 || size > 4096 {
				return 0, fmt.Errorf("invalid wav fmt chunk size %d: %s", size, path)
			}
			fmtChunk := make([]byte, size)
			if _, err := io.ReadFull(f, fmtChunk); err != nil {
				return 0, fmt.Errorf("reading wav fmt chunk: %w", err)
			}
			byteRate = binary.LittleEndian.Uint32(fmtChunk[8:12])
		case "data":
			dataSize = uint64(size)
			if byteRate == 0 || dataSize == 0 {
				return 0, fmt.Errorf("wav file lacks sample data: %s", path)
			}
			return float64(dataSize) / float64(byteRate), nil
		default:
			if _, err := io.CopyN(io.Discard, f, int64(size)); err != nil {
				return 0, fmt.Errorf("skipping wav chunk: %w", err)
			}
		}
	}
}
