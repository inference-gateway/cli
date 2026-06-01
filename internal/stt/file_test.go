package stt

import (
	"context"
	"errors"
	"os"
	"testing"
)

type fakeConverter struct {
	wav    string
	err    error
	gotSrc string
}

func (f *fakeConverter) ToWhisperWAV(ctx context.Context, srcPath string) (string, error) {
	f.gotSrc = srcPath
	return f.wav, f.err
}

type fakeWavTranscriber struct {
	text   string
	err    error
	gotWAV string
}

func (f *fakeWavTranscriber) Transcribe(ctx context.Context, wavPath string) (string, error) {
	f.gotWAV = wavPath
	return f.text, f.err
}

func TestFileTranscriberSuccess(t *testing.T) {
	wav, err := os.CreateTemp(t.TempDir(), "*.wav")
	if err != nil {
		t.Fatal(err)
	}
	wavPath := wav.Name()
	_ = wav.Close()

	conv := &fakeConverter{wav: wavPath}
	tr := &fakeWavTranscriber{text: "decoded text"}
	f := &FileTranscriber{converter: conv, transcriber: tr}

	got, err := f.TranscribeFile(context.Background(), "/tmp/voice.ogg")
	if err != nil {
		t.Fatalf("TranscribeFile: %v", err)
	}
	if got != "decoded text" {
		t.Errorf("got %q, want 'decoded text'", got)
	}
	if conv.gotSrc != "/tmp/voice.ogg" {
		t.Errorf("converter got src %q", conv.gotSrc)
	}
	if tr.gotWAV != wavPath {
		t.Errorf("transcriber got wav %q, want %q", tr.gotWAV, wavPath)
	}
	// The intermediate WAV must be cleaned up.
	if _, err := os.Stat(wavPath); !os.IsNotExist(err) {
		t.Errorf("expected intermediate wav removed, stat err = %v", err)
	}
}

func TestFileTranscriberConvertError(t *testing.T) {
	f := &FileTranscriber{
		converter:   &fakeConverter{err: errors.New("ffmpeg missing")},
		transcriber: &fakeWavTranscriber{text: "should not run"},
	}
	if _, err := f.TranscribeFile(context.Background(), "/tmp/voice.ogg"); err == nil {
		t.Fatal("expected error when conversion fails")
	}
}
