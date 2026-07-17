package stt

import (
	"context"
	"os"

	config "github.com/inference-gateway/cli/config"
	audio "github.com/inference-gateway/cli/internal/audio"
)

// audioConverter converts an arbitrary audio file into a Whisper-ready WAV.
type audioConverter interface {
	ToWhisperWAV(ctx context.Context, srcPath string) (string, error)
}

// wavTranscriber transcribes a WAV file into text.
type wavTranscriber interface {
	Transcribe(ctx context.Context, wavPath string) (string, error)
}

// FileTranscriber transcribes an arbitrary audio file by first converting it to
// 16kHz mono WAV with ffmpeg, then running Whisper. It is used for Telegram
// voice messages (OGG/Opus), which must be decoded before transcription.
type FileTranscriber struct {
	converter   audioConverter
	transcriber wavTranscriber
}

// NewFileTranscriber creates a FileTranscriber from the speech-to-text config.
func NewFileTranscriber(cfg config.SpeechToTextConfig) *FileTranscriber {
	converter := audio.NewConverter(cfg)
	if cfg.AutoDownload {
		converter.SetBinaryEnsurer(NewBinaryManager(cfg).EnsureBinary)
	}
	return &FileTranscriber{
		converter:   converter,
		transcriber: NewWhisperTranscriber(cfg),
	}
}

// TranscribeFile converts audioPath to WAV and returns its transcription. The
// intermediate WAV file is removed before returning.
func (f *FileTranscriber) TranscribeFile(ctx context.Context, audioPath string) (string, error) {
	wav, err := f.converter.ToWhisperWAV(ctx, audioPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(wav) }()
	return f.transcriber.Transcribe(ctx, wav)
}
