package shortcuts

import (
	"context"
	"errors"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

type fakeRecorder struct {
	wavPath  string
	err      error
	availErr error
	gotSecs  int
	recorded bool
}

func (f *fakeRecorder) EnsureAvailable() error { return f.availErr }

func (f *fakeRecorder) Record(ctx context.Context, maxSeconds int) (string, error) {
	f.recorded = true
	f.gotSecs = maxSeconds
	return f.wavPath, f.err
}

type fakeTranscriber struct {
	text     string
	err      error
	availErr error
	gotWAV   string
}

func (f *fakeTranscriber) EnsureAvailable() error { return f.availErr }

func (f *fakeTranscriber) Transcribe(ctx context.Context, wavPath string) (string, error) {
	f.gotWAV = wavPath
	return f.text, f.err
}

func TestVoiceShortcutMetadata(t *testing.T) {
	v := NewVoiceShortcut(config.SpeechToTextConfig{}, &fakeRecorder{}, &fakeTranscriber{})
	if v.GetName() != "voice" {
		t.Errorf("GetName = %q", v.GetName())
	}
	if v.GetUsage() == "" || v.GetDescription() == "" {
		t.Error("usage/description should be non-empty")
	}
}

func TestVoiceShortcutCanExecute(t *testing.T) {
	v := NewVoiceShortcut(config.SpeechToTextConfig{}, &fakeRecorder{}, &fakeTranscriber{})
	cases := []struct {
		args []string
		want bool
	}{
		{nil, true},
		{[]string{"10"}, true},
		{[]string{"abc"}, false},
		{[]string{"10", "20"}, false},
	}
	for _, c := range cases {
		if got := v.CanExecute(c.args); got != c.want {
			t.Errorf("CanExecute(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestVoiceShortcutSuccess(t *testing.T) {
	rec := &fakeRecorder{wavPath: "/tmp/x.wav"}
	tr := &fakeTranscriber{text: "hello there"}
	v := NewVoiceShortcut(config.SpeechToTextConfig{MaxRecordingSeconds: 15}, rec, tr)

	res, err := v.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success || res.SideEffect != SideEffectSetInput {
		t.Fatalf("expected success + SideEffectSetInput, got %+v", res)
	}
	if res.Data.(string) != "hello there" {
		t.Errorf("Data = %v, want 'hello there'", res.Data)
	}
	if rec.gotSecs != 15 {
		t.Errorf("recorder got %d seconds, want default 15", rec.gotSecs)
	}
	if tr.gotWAV != "/tmp/x.wav" {
		t.Errorf("transcriber got wav %q", tr.gotWAV)
	}
}

func TestVoiceShortcutDurationArg(t *testing.T) {
	rec := &fakeRecorder{wavPath: "/tmp/x.wav"}
	v := NewVoiceShortcut(config.SpeechToTextConfig{MaxRecordingSeconds: 30}, rec, &fakeTranscriber{text: "hi"})
	if _, err := v.Execute(context.Background(), []string{"8"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.gotSecs != 8 {
		t.Errorf("recorder got %d seconds, want 8", rec.gotSecs)
	}
}

func TestVoiceShortcutWhisperUnavailable(t *testing.T) {
	rec := &fakeRecorder{wavPath: "/tmp/x.wav"}
	tr := &fakeTranscriber{availErr: errors.New("whisper binary not found")}
	v := NewVoiceShortcut(config.SpeechToTextConfig{}, rec, tr)

	res, err := v.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Success {
		t.Error("expected failure when whisper binary is unavailable")
	}
	if rec.recorded {
		t.Error("must not record audio when whisper is unavailable")
	}
}

func TestVoiceShortcutRecorderUnavailable(t *testing.T) {
	rec := &fakeRecorder{availErr: errors.New("no microphone recorder found")}
	tr := &fakeTranscriber{text: "hi"}
	v := NewVoiceShortcut(config.SpeechToTextConfig{}, rec, tr)

	res, _ := v.Execute(context.Background(), nil)
	if res.Success {
		t.Error("expected failure when no recorder is available")
	}
	if rec.recorded {
		t.Error("must not record when recorder is unavailable")
	}
}

func TestVoiceShortcutRecordError(t *testing.T) {
	v := NewVoiceShortcut(config.SpeechToTextConfig{}, &fakeRecorder{err: errors.New("no mic")}, &fakeTranscriber{})
	res, err := v.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Success || res.SideEffect != SideEffectNone {
		t.Errorf("expected failure with no side effect, got %+v", res)
	}
}

func TestVoiceShortcutTranscribeError(t *testing.T) {
	v := NewVoiceShortcut(config.SpeechToTextConfig{},
		&fakeRecorder{wavPath: "/tmp/x.wav"},
		&fakeTranscriber{err: errors.New("whisper missing")})
	res, _ := v.Execute(context.Background(), nil)
	if res.Success {
		t.Errorf("expected failure on transcribe error, got %+v", res)
	}
}

func TestVoiceShortcutEmptyTranscript(t *testing.T) {
	v := NewVoiceShortcut(config.SpeechToTextConfig{},
		&fakeRecorder{wavPath: "/tmp/x.wav"},
		&fakeTranscriber{text: "   "})
	res, _ := v.Execute(context.Background(), nil)
	if !res.Success || res.SideEffect != SideEffectNone {
		t.Errorf("expected success with no side effect on empty transcript, got %+v", res)
	}
}
