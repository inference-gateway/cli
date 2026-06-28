package channels

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVoiceRetentionSaveWritesFile(t *testing.T) {
	dir := t.TempDir()
	r := &VoiceRetention{Dir: dir, Keep: 3}

	name, err := r.save("voice/file_42.oga", []byte("audio-bytes"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if name == "" {
		t.Fatal("expected a path, got empty string")
	}
	if got := filepath.Dir(name); got != dir {
		t.Errorf("file written to %q, want dir %q", got, dir)
	}
	if ext := filepath.Ext(name); ext != ".oga" {
		t.Errorf("extension = %q, want .oga", ext)
	}
	if base := filepath.Base(name); !strings.HasPrefix(base, recordingFilePrefix) {
		t.Errorf("file %q missing prefix %q", base, recordingFilePrefix)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "audio-bytes" {
		t.Errorf("content = %q, want audio-bytes", data)
	}
}

func TestVoiceRetentionSaveDisabled(t *testing.T) {
	dir := t.TempDir()
	r := &VoiceRetention{Dir: dir, Keep: 0}

	name, err := r.save("x.oga", []byte("x"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if name != "" {
		t.Errorf("expected no file path with Keep=0, got %q", name)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected nothing written with Keep=0, got %d entries", len(entries))
	}
}

func TestVoiceRetentionSaveNilReceiver(t *testing.T) {
	var r *VoiceRetention
	name, err := r.save("x.oga", []byte("x"))
	if err != nil {
		t.Fatalf("save on nil receiver: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty path on nil receiver, got %q", name)
	}
}

func TestVoiceRetentionSaveEnforcesCap(t *testing.T) {
	dir := t.TempDir()
	r := &VoiceRetention{Dir: dir, Keep: 2}

	for i := range 4 {
		if _, err := r.save("x.oga", []byte{byte(i)}); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected cap of 2 retained files, got %d", len(entries))
	}
}

func TestPruneRecordingsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)

	// names[i] gets mod time base+i*minute, so names[4] is the newest.
	var names []string
	for i := range 5 {
		name := filepath.Join(dir, fmt.Sprintf("%s%d.oga", recordingFilePrefix, i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mod := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(name, mod, mod); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}

	// A non-recording file must never be pruned.
	other := filepath.Join(dir, "keep-me.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pruneRecordings(dir, 2)

	for i, name := range names {
		_, statErr := os.Stat(name)
		removed := os.IsNotExist(statErr)
		wantRemoved := i < 3 // oldest three pruned, newest two kept
		if removed != wantRemoved {
			t.Errorf("file %d: removed=%v, want removed=%v (stat err=%v)", i, removed, wantRemoved, statErr)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("non-recording file should be kept: %v", err)
	}
}

func TestPruneRecordingsNoOpUnderCap(t *testing.T) {
	dir := t.TempDir()
	for i := range 2 {
		name := filepath.Join(dir, fmt.Sprintf("%s%d.oga", recordingFilePrefix, i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pruneRecordings(dir, 5)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected all 2 files kept under cap, got %d", len(entries))
	}
}
