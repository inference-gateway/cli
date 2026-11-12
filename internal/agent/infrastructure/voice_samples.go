package infrastructure

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxVoiceSamplesBytes caps the voice samples a checkpoint carries. A library
// beyond it fails the teleport with a clear message instead of a stalled upload.
const MaxVoiceSamplesBytes = 64 << 20

// voiceSampleExtension is the one kind of file a voice samples library holds.
const voiceSampleExtension = ".wav"

// PackVoiceSamples packs the WAV files at the top of a voice samples library
// as a gzipped tar. A library that is missing or holds none gives no payload.
func PackVoiceSamples(dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	var size int64
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !isVoiceSample(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		names = append(names, entry.Name())
		size += info.Size()
	}
	if size > MaxVoiceSamplesBytes {
		return nil, fmt.Errorf("the voice samples in %s are %d bytes, over the %d byte limit of a teleport", dir, size, MaxVoiceSamplesBytes)
	}
	return tarFiles(dir, names)
}

// UnpackVoiceSamples restores packed voice samples into a library, replacing
// a sample of the same name and keeping the others. It refuses anything that
// is not a WAV file at the top of the library.
func UnpackVoiceSamples(data []byte, dir string) error {
	if len(data) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return untar(data, dir, notAVoiceSample, MaxRepositoryBytes)
}

func isVoiceSample(name string) bool {
	return strings.EqualFold(filepath.Ext(name), voiceSampleExtension)
}

func notAVoiceSample(rel string) bool {
	return strings.Contains(rel, "/") || !isVoiceSample(rel)
}
