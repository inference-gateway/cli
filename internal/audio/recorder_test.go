package audio

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestRecordCandidatesDarwin(t *testing.T) {
	got := recordCandidates("darwin", "ffmpeg", "", "out.wav", 10, 0)
	if len(got) != 1 {
		t.Fatalf("darwin candidates = %d, want 1", len(got))
	}
	args := strings.Join(got[0].args, " ")
	for _, want := range []string{"-f avfoundation", "-i :default", "-t 10", "-ar 16000", "-ac 1", "out.wav"} {
		if !strings.Contains(args, want) {
			t.Errorf("darwin args %q missing %q", args, want)
		}
	}
}

func TestRecordCandidatesDarwinDevice(t *testing.T) {
	got := recordCandidates("darwin", "ffmpeg", "1", "out.wav", 5, 0)
	if args := strings.Join(got[0].args, " "); !strings.Contains(args, "-i :1") {
		t.Errorf("expected device :1 in %q", args)
	}
}

func TestRecordCandidatesLinux(t *testing.T) {
	got := recordCandidates("linux", "ffmpeg", "", "out.wav", 10, 0)
	names := make([]string, len(got))
	for i, c := range got {
		names[i] = c.name
	}
	want := []string{"ffmpeg", "ffmpeg", "arecord", "sox"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("linux candidate names = %v, want %v", names, want)
	}
	if a := strings.Join(got[0].args, " "); !strings.Contains(a, "-f alsa") {
		t.Errorf("first linux candidate should use alsa, got %q", a)
	}
	if a := strings.Join(got[1].args, " "); !strings.Contains(a, "-f pulse") {
		t.Errorf("second linux candidate should use pulse, got %q", a)
	}
}

func TestRecordCandidatesWindows(t *testing.T) {
	got := recordCandidates("windows", "ffmpeg", "", "out.wav", 10, 0)
	if len(got) != 1 {
		t.Fatalf("windows candidates = %d, want 1", len(got))
	}
	if a := strings.Join(got[0].args, " "); !strings.Contains(a, "-f dshow") || !strings.Contains(a, "audio=default") {
		t.Errorf("windows args missing dshow/default: %q", a)
	}
}

func TestRecordCandidatesSilence(t *testing.T) {
	got := recordCandidates("darwin", "ffmpeg", "", "out.wav", 30, 2)
	if len(got) != 1 || !got[0].silence {
		t.Fatalf("expected one silence-enabled candidate, got %+v", got)
	}
	args := strings.Join(got[0].args, " ")
	if !strings.Contains(args, "silencedetect=noise=-30dB:d=2") {
		t.Errorf("expected silencedetect filter in args, got %q", args)
	}
	if !strings.Contains(args, "-loglevel info") {
		t.Errorf("silencedetect needs info log level, got %q", args)
	}
}

func TestRecordCandidatesNoSilenceWhenDisabled(t *testing.T) {
	got := recordCandidates("darwin", "ffmpeg", "", "out.wav", 30, 0)
	if got[0].silence {
		t.Error("expected silence disabled when silenceTimeout is 0")
	}
	if strings.Contains(strings.Join(got[0].args, " "), "silencedetect") {
		t.Error("expected no silencedetect filter when disabled")
	}
}

func TestParseSilenceStart(t *testing.T) {
	cases := []struct {
		line string
		want float64
		ok   bool
	}{
		{"[silencedetect @ 0x] silence_start: 3.214", 3.214, true},
		{"[silencedetect @ 0x] silence_start: 0", 0, true},
		{"[silencedetect @ 0x] silence_end: 5.1 | silence_duration: 1.9", 0, false},
		{"size=  100kB time=00:00:05", 0, false},
	}
	for _, c := range cases {
		got, ok := parseSilenceStart(c.line)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseSilenceStart(%q) = (%v, %v), want (%v, %v)", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestRecordUsesSilenceRunner(t *testing.T) {
	r := NewRecorder(config.SpeechToTextConfig{SilenceTimeout: 2})
	r.lookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	r.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		t.Fatal("non-silence runner must not be used when silence is enabled")
		return nil, nil
	}
	var gotArgs []string
	r.runSilence = func(ctx context.Context, name string, args []string) error {
		gotArgs = args
		return nil
	}

	out, err := r.Record(context.Background(), 30)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	defer func() { _ = os.Remove(out) }()
	if !strings.Contains(strings.Join(gotArgs, " "), "silencedetect") {
		t.Errorf("expected silence runner to receive silencedetect args, got %v", gotArgs)
	}
}

func TestRecordSuccess(t *testing.T) {
	r := NewRecorder(config.SpeechToTextConfig{})
	r.lookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	called := false
	r.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		called = true
		return nil, nil
	}

	out, err := r.Record(context.Background(), 5)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	defer func() { _ = os.Remove(out) }()
	if !called {
		t.Error("expected run to be called")
	}
	if !strings.HasSuffix(out, ".wav") {
		t.Errorf("Record returned %q, want a .wav path", out)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("expected output file to exist: %v", err)
	}
}

func TestRecorderEnsureAvailable(t *testing.T) {
	r := NewRecorder(config.SpeechToTextConfig{})
	r.lookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	if err := r.EnsureAvailable(); err != nil {
		t.Errorf("expected available, got %v", err)
	}
	r.lookPath = func(string) (string, error) { return "", errors.New("nope") }
	if err := r.EnsureAvailable(); err == nil {
		t.Error("expected error when no recorder tool is installed")
	}
}

func TestRecordTimeoutReturnsActionableError(t *testing.T) {
	r := NewRecorder(config.SpeechToTextConfig{})
	r.lookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	r.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("signal: killed")
	}

	// A cancelled parent context simulates the wall-clock guard firing (a hung
	// ffmpeg that never produced audio).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Record(ctx, 5)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error mentioning microphone access, got %v", err)
	}
}

func TestRecordNoRecorder(t *testing.T) {
	r := NewRecorder(config.SpeechToTextConfig{})
	r.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	r.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		t.Fatal("run should not be called when no recorder is available")
		return nil, nil
	}

	if _, err := r.Record(context.Background(), 5); err == nil {
		t.Fatal("expected error when no recorder is available")
	}
}
