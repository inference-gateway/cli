package shortcuts

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	config "github.com/inference-gateway/cli/config"
)

// AudioRecorder records microphone audio to a WAV file. It is defined here (not
// in internal/domain) so the VoiceShortcut can be unit-tested with a hand-written
// fake, mirroring ClipboardWriter in copy.go.
type AudioRecorder interface {
	// EnsureAvailable returns an actionable error if no capture tool is installed.
	EnsureAvailable() error
	Record(ctx context.Context, maxSeconds int) (wavPath string, err error)
}

// Transcriber transcribes a WAV file into text.
type Transcriber interface {
	// EnsureAvailable returns an actionable error if the whisper binary is missing.
	EnsureAvailable() error
	Transcribe(ctx context.Context, wavPath string) (string, error)
}

// VoiceShortcut records the microphone, transcribes it with Whisper, and places
// the resulting text into the chat input field.
type VoiceShortcut struct {
	cfg         config.SpeechToTextConfig
	recorder    AudioRecorder
	transcriber Transcriber
}

// NewVoiceShortcut creates a new VoiceShortcut.
func NewVoiceShortcut(cfg config.SpeechToTextConfig, recorder AudioRecorder, transcriber Transcriber) *VoiceShortcut {
	return &VoiceShortcut{
		cfg:         cfg,
		recorder:    recorder,
		transcriber: transcriber,
	}
}

func (v *VoiceShortcut) GetName() string { return "voice" }
func (v *VoiceShortcut) GetDescription() string {
	return "Record from the microphone and transcribe to the input field using Whisper"
}
func (v *VoiceShortcut) GetUsage() string { return "/voice [seconds]" }

func (v *VoiceShortcut) CanExecute(args []string) bool {
	if len(args) > 1 {
		return false
	}
	if len(args) == 1 {
		if _, err := strconv.Atoi(strings.TrimSpace(args[0])); err != nil {
			return false
		}
	}
	return true
}

func (v *VoiceShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	seconds := v.maxRecordingSeconds()
	if len(args) == 1 {
		n, err := strconv.Atoi(strings.TrimSpace(args[0]))
		if err != nil || n <= 0 {
			return ShortcutResult{Output: fmt.Sprintf("Invalid duration %q (expected a positive number of seconds)", args[0]), Success: false}, nil
		}
		seconds = n
	}

	// Fail fast before recording if a required external tool is missing, so the
	// user sees an actionable error instead of being prompted to speak in vain.
	if err := v.transcriber.EnsureAvailable(); err != nil {
		return ShortcutResult{Output: fmt.Sprintf("%v", err), Success: false}, nil
	}
	if err := v.recorder.EnsureAvailable(); err != nil {
		return ShortcutResult{Output: fmt.Sprintf("🎙 %v", err), Success: false}, nil
	}

	wavPath, err := v.recorder.Record(ctx, seconds)
	if err != nil {
		return ShortcutResult{Output: fmt.Sprintf("🎙 Recording failed: %v", err), Success: false}, nil
	}
	defer func() { _ = os.Remove(wavPath) }()

	text, err := v.transcriber.Transcribe(ctx, wavPath)
	if err != nil {
		return ShortcutResult{Output: fmt.Sprintf("Transcription failed: %v", err), Success: false}, nil
	}

	if strings.TrimSpace(text) == "" {
		return ShortcutResult{Output: "No speech detected", Success: true}, nil
	}

	return ShortcutResult{
		Success:    true,
		SideEffect: SideEffectSetInput,
		Data:       text,
	}, nil
}

// maxRecordingSeconds returns the configured recording cap, defaulting to 30.
func (v *VoiceShortcut) maxRecordingSeconds() int {
	if v.cfg.MaxRecordingSeconds > 0 {
		return v.cfg.MaxRecordingSeconds
	}
	return 30
}
