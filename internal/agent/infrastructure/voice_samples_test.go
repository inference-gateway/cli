package infrastructure

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func fileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestVoiceSamplesTravelAsTheWAVFilesAtTheTopOfTheLibrary(t *testing.T) {
	source := t.TempDir()
	writeFiles(t, source, map[string]string{
		"Confident.wav":   "confident",
		"LOUD.WAV":        "loud",
		"notes.txt":       "not a sample",
		"nested/deep.wav": "below the top",
	})
	target := filepath.Join(t.TempDir(), "voice", "samples")
	writeFiles(t, target, map[string]string{"Confident.wav": "an older take", "Mine.wav": "already here"})

	packed, err := PackVoiceSamples(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := UnpackVoiceSamples(packed, target); err != nil {
		t.Fatal(err)
	}

	if got, want := fileNames(t, target), []string{"Confident.wav", "LOUD.WAV", "Mine.wav"}; !slices.Equal(got, want) {
		t.Fatalf("library holds %v, want %v", got, want)
	}
	if got, _ := os.ReadFile(filepath.Join(target, "Confident.wav")); string(got) != "confident" {
		t.Fatalf("a sample of the same name was not replaced: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(target, "Mine.wav")); string(got) != "already here" {
		t.Fatalf("a sample the target already had was changed: %q", got)
	}
}

func TestPackVoiceSamplesGivesNoPayloadWithoutSamples(t *testing.T) {
	empty := t.TempDir()
	onlyNotes := t.TempDir()
	writeFiles(t, onlyNotes, map[string]string{"notes.txt": "not a sample"})
	for name, dir := range map[string]string{"missing library": filepath.Join(empty, "nowhere"), "empty library": empty, "no wav files": onlyNotes} {
		t.Run(name, func(t *testing.T) {
			packed, err := PackVoiceSamples(dir)
			if err != nil || packed != nil {
				t.Fatalf("PackVoiceSamples = %d bytes, %v, want no payload and no error", len(packed), err)
			}
		})
	}
}

func TestPackVoiceSamplesRefusesALibraryOverTheLimit(t *testing.T) {
	dir := t.TempDir()
	big, err := os.Create(filepath.Join(dir, "big.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err := big.Truncate(MaxVoiceSamplesBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = big.Close()
	if _, err := PackVoiceSamples(dir); err == nil {
		t.Fatal("a library over the limit was packed")
	}
}

func TestUnpackVoiceSamplesRefusesWhatIsNotASampleAtTheTop(t *testing.T) {
	for _, entry := range []string{"../escaped.wav", "/abs.wav", "nested/deep.wav", "script.sh", ".git/config"} {
		t.Run(entry, func(t *testing.T) {
			dir := t.TempDir()
			if err := UnpackVoiceSamples(tarOf(t, entry, "pwned\n"), dir); err == nil {
				t.Fatalf("entry %q was written", entry)
			}
			if names := fileNames(t, dir); len(names) != 0 {
				t.Fatalf("entry %q left %v in the library", entry, names)
			}
		})
	}
}

func TestUnpackVoiceSamplesIgnoresAnEmptyPayload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never-created")
	if err := UnpackVoiceSamples(nil, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("an empty payload created the library: %v", err)
	}
}
