package audio

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	config "github.com/inference-gateway/cli/config"
)

// recordGraceSeconds is how long past the requested duration a capture tool is
// allowed to run before it is killed. It guards against backends (notably
// ffmpeg's macOS avfoundation) that block indefinitely when the microphone is
// unavailable instead of honoring their duration flag.
const recordGraceSeconds = 5

// minSilenceStartSeconds ignores silence reported at the very start of a
// recording (before the speaker has said anything), so initial quiet does not
// stop the capture immediately.
const minSilenceStartSeconds = 0.5

// silenceStartRe matches ffmpeg silencedetect "silence_start" log lines.
var silenceStartRe = regexp.MustCompile(`silence_start:\s*(-?[0-9.]+)`)

// silenceRunner runs an ffmpeg capture and stops it once the speaker goes quiet.
// It is a field so tests can substitute a fake.
type silenceRunner func(ctx context.Context, name string, args []string) error

// Recorder captures microphone audio into a 16kHz mono WAV file by shelling out
// to ffmpeg (with arecord/sox fallbacks on Linux), mirroring the candidate-list
// pattern used by the clipboard text writer. It adds no CGO.
type Recorder struct {
	cfg      config.SpeechToTextConfig
	binaries *BinaryStore

	// run, runSilence and lookPath are overridable in tests.
	run        commandRunner
	runSilence silenceRunner
	lookPath   func(string) (string, error)
}

// NewRecorder creates a Recorder from the speech-to-text config.
func NewRecorder(cfg config.SpeechToTextConfig) *Recorder {
	return &Recorder{
		cfg:        cfg,
		binaries:   NewBinaryStore(cfg),
		run:        execRun,
		runSilence: runFFmpegWithSilenceStop,
		lookPath:   exec.LookPath,
	}
}

// ffmpegBin returns the ffmpeg binary to invoke: an explicit configured path or
// a PATH lookup, then a prebuilt binary in ~/.infer/bin (downloaded on first use
// when auto_download is enabled), mirroring the transcriber and converter. It
// falls back to the plain name so callers still report the usual install hint.
func (r *Recorder) ffmpegBin(ctx context.Context) string {
	if bin, err := resolveFFmpeg(r.cfg.FFmpegPath, r.lookPath); err == nil {
		return bin
	}
	if r.cfg.AutoDownload && r.binaries != nil {
		if path, err := r.binaries.EnsureBinary(ctx, "ffmpeg"); err == nil {
			return path
		}
	}
	return ffmpegName(r.cfg.FFmpegPath)
}

// EnsureAvailable reports whether a microphone capture tool can be resolved
// (possibly by downloading ffmpeg), without recording. It lets callers fail fast
// (with an actionable error) before prompting the user to speak.
func (r *Recorder) EnsureAvailable() error {
	candidates := recordCandidates(runtime.GOOS, r.ffmpegBin(context.Background()), r.cfg.InputDevice, "", 0, 0)
	if len(candidates) == 0 {
		return fmt.Errorf("microphone recording is not supported on %s", runtime.GOOS)
	}
	for _, c := range candidates {
		if _, err := r.lookPath(c.name); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no microphone recorder found: install ffmpeg (e.g. `brew install ffmpeg` or `apt install ffmpeg`)")
}

// Record captures up to maxSeconds of microphone audio and returns the path to a
// 16kHz mono WAV file. When speech_to_text.silence_timeout is set, ffmpeg
// recordings stop shortly after the speaker goes quiet instead of always running
// for the full cap. The caller owns the returned file and should remove it.
func (r *Recorder) Record(ctx context.Context, maxSeconds int) (string, error) {
	if maxSeconds <= 0 {
		maxSeconds = 30
	}

	out, err := tempWAV()
	if err != nil {
		return "", err
	}

	candidates := recordCandidates(runtime.GOOS, r.ffmpegBin(ctx), r.cfg.InputDevice, out, maxSeconds, r.cfg.SilenceTimeout)
	if len(candidates) == 0 {
		_ = os.Remove(out)
		return "", fmt.Errorf("microphone recording is not supported on %s", runtime.GOOS)
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(maxSeconds+recordGraceSeconds)*time.Second)
	defer cancel()

	var lastErr error
	for _, c := range candidates {
		if _, err := r.lookPath(c.name); err != nil {
			lastErr = err
			continue
		}

		var runErr error
		if c.silence {
			runErr = r.runSilence(runCtx, c.name, c.args)
		} else {
			_, runErr = r.run(runCtx, c.name, c.args...)
		}
		if runErr != nil {
			if runCtx.Err() != nil {
				_ = os.Remove(out)
				return "", fmt.Errorf("microphone recording timed out after %ds with no audio captured "+
					"- grant microphone access to your terminal (macOS: System Settings > Privacy & Security > "+
					"Microphone) and check speech_to_text.input_device", maxSeconds+recordGraceSeconds)
			}
			lastErr = runErr
			continue
		}
		return out, nil
	}

	_ = os.Remove(out)
	return "", fmt.Errorf("no working microphone recorder found (install ffmpeg, or arecord/sox on Linux): %w", lastErr)
}

// recordCandidates builds the ordered recorder invocations for a platform. It is
// a pure function so it can be unit-tested across GOOS values. When silenceTimeout
// is > 0, ffmpeg invocations enable silencedetect (and run at info log level so
// the events reach stderr) and are flagged for the silence-aware runner.
func recordCandidates(goos, ffmpeg, device, out string, seconds, silenceTimeout int) []candidate {
	dur := strconv.Itoa(seconds)

	ffmpegCapture := func(format, input string) candidate {
		loglevel := "error"
		var filter []string
		silence := false
		if silenceTimeout > 0 {
			loglevel = "info"
			filter = []string{"-af", fmt.Sprintf("silencedetect=noise=-30dB:d=%d", silenceTimeout)}
			silence = true
		}
		args := []string{"-hide_banner", "-loglevel", loglevel, "-f", format, "-i", input, "-t", dur}
		args = append(args, filter...)
		args = append(args, "-ar", "16000", "-ac", "1", "-y", out)
		return candidate{name: ffmpeg, args: args, silence: silence}
	}

	switch goos {
	case "darwin":
		return []candidate{ffmpegCapture("avfoundation", ":"+deviceOr(device, "default"))}
	case "windows":
		return []candidate{ffmpegCapture("dshow", "audio="+deviceOr(device, "default"))}
	default:
		dev := deviceOr(device, "default")
		return []candidate{
			ffmpegCapture("alsa", dev),
			ffmpegCapture("pulse", dev),
			{name: "arecord", args: []string{"-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-d", dur, out}},
			{name: "sox", args: []string{"-d", "-r", "16000", "-c", "1", "-b", "16", out, "trim", "0", dur}},
		}
	}
}

// deviceOr returns the configured device or a platform default when empty.
func deviceOr(device, fallback string) string {
	if d := strings.TrimSpace(device); d != "" {
		return d
	}
	return fallback
}

// parseSilenceStart extracts the timestamp from an ffmpeg "silence_start" line.
func parseSilenceStart(line string) (float64, bool) {
	m := silenceStartRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// runFFmpegWithSilenceStop runs ffmpeg and gracefully stops it once the speaker
// goes quiet (a silencedetect "silence_start" event past the initial-silence
// window), so the recording ends when the user finishes talking rather than at
// the full cap. ffmpeg finalizes the WAV on SIGINT, so a stop is reported as a
// successful recording.
func runFFmpegWithSilenceStop(ctx context.Context, name string, args []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var stopped atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if stopped.Load() {
				continue
			}
			if t, ok := parseSilenceStart(sc.Text()); ok && t >= minSilenceStartSeconds {
				stopped.Store(true)
				_ = cmd.Process.Signal(os.Interrupt)
			}
		}
	}()

	waitErr := cmd.Wait()
	<-done
	if stopped.Load() {
		return nil
	}
	return waitErr
}
