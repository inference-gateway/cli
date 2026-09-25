package computer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	audio "github.com/inference-gateway/cli/internal/audio"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	capture "github.com/inference-gateway/cli/internal/computer/infrastructure/capture"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

const (
	// recordStartupWait is how long RecordStart watches ffmpeg for an early
	// exit (missing encoder, capture device, or permission) before reporting
	// success.
	recordStartupWait = 1500 * time.Millisecond
	// recordStopTimeout bounds how long finalizing may take before ffmpeg is
	// killed. It stays well under the 20s CLI shutdown budget.
	recordStopTimeout = 10 * time.Second
)

// RecordingStatus is the result data of RecordStart and RecordStop.
type RecordingStatus struct {
	Path            string                `json:"path"`
	Region          computerdomain.Region `json:"region"` // frame space
	FrameWidth      int                   `json:"frame_width"`
	FrameHeight     int                   `json:"frame_height"`
	DurationSeconds float64               `json:"duration_seconds,omitempty"`
	SizeBytes       int64                 `json:"size_bytes,omitempty"`
	Capped          bool                  `json:"capped,omitempty"`
	Message         string                `json:"message"`
}

// recordRequest is what RecordStart asks to capture.
type recordRequest struct {
	Mode   string // screen, window, or region
	Window string
	Region *computerdomain.Region // frame space
}

// recording is one ffmpeg process. done is closed once Wait returns; err and
// ended are written before that, so readers wait on done first.
type recording struct {
	status  RecordingStatus
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stderr  strings.Builder
	started time.Time
	done    chan struct{}
	ended   time.Time
	err     error
}

// ScreenRecorder runs at most one ffmpeg screen recording at a time. The
// process outlives the RecordStart tool call; RecordStop, the max_duration
// cap (ffmpeg -t), or Close on CLI shutdown ends it.
type ScreenRecorder struct {
	cfg         *config.Config
	notifier    agentdomain.UINotifier
	startupWait time.Duration

	mu     sync.Mutex
	cur    *recording
	closed bool
}

// NewScreenRecorder creates the recorder shared by RecordStart and RecordStop.
func NewScreenRecorder(cfg *config.Config, notifier agentdomain.UINotifier) *ScreenRecorder {
	if notifier == nil {
		notifier = agentdomain.NoopUINotifier{}
	}
	return &ScreenRecorder{cfg: cfg, notifier: notifier, startupWait: recordStartupWait}
}

// Start begins a recording. Resolving the capture area and ffmpeg (which may
// download it) happens before taking the lock, so Close is never held up.
func (r *ScreenRecorder) Start(ctx context.Context, req recordRequest) (RecordingStatus, error) {
	r.mu.Lock()
	err := r.idleLocked()
	r.mu.Unlock()
	if err != nil {
		return RecordingStatus{}, err
	}

	ffmpeg, args, rec, err := r.prepare(ctx, req)
	if err != nil {
		return RecordingStatus{}, err
	}
	if err := r.launch(ffmpeg, args, rec); err != nil {
		return RecordingStatus{}, err
	}
	return rec.status, nil
}

func (r *ScreenRecorder) idleLocked() error {
	if r.closed {
		return errors.New("screen recorder is shut down")
	}
	if r.cur != nil {
		select {
		case <-r.cur.done:
		default:
			return fmt.Errorf("a recording is already running (%s); call RecordStop first", r.cur.status.Path)
		}
	}
	return nil
}

// prepare resolves the capture rectangle, ffmpeg and the output file.
func (r *ScreenRecorder) prepare(ctx context.Context, req recordRequest) (string, []string, *recording, error) {
	if err := capture.Preflight(); err != nil {
		return "", nil, nil, err
	}
	screen, err := capture.PrimaryScreen(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	frameW, frameH := r.cfg.ComputerUse.Screenshot.FitDims(screen.Width, screen.Height)

	var rect display.Region
	switch req.Mode {
	case "region":
		rect, err = frameRegionToScreen(req.Region, frameW, frameH, screen.Width, screen.Height)
	case "window":
		var bounds display.Region
		if bounds, err = capture.WindowBounds(ctx, req.Window); err == nil {
			rect, err = clampToScreen(bounds, screen.Width, screen.Height, req.Window)
		}
	default:
		rect = display.Region{Width: screen.Width, Height: screen.Height}
	}
	if err != nil {
		return "", nil, nil, err
	}

	ffmpeg, err := resolveFFmpeg(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	rc := r.cfg.ComputerUse.Recording
	dir, err := rc.ResolveOutputDir()
	if err != nil {
		return "", nil, nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, nil, fmt.Errorf("create recordings directory: %w", err)
	}
	out := filepath.Join(dir, time.Now().Format("20060102-150405.000")+".mp4")
	args, err := ffmpegArgs(runtime.GOOS, os.Getenv("DISPLAY"), rect, screen, rc.Framerate, rc.MaxDuration, out)
	if err != nil {
		return "", nil, nil, err
	}

	region := computerdomain.Region{
		X:      rect.X * frameW / screen.Width,
		Y:      rect.Y * frameH / screen.Height,
		Width:  rect.Width * frameW / screen.Width,
		Height: rect.Height * frameH / screen.Height,
	}
	if req.Mode == "region" {
		region = *req.Region
	}
	rec := &recording{status: RecordingStatus{
		Path:        out,
		Region:      region,
		FrameWidth:  frameW,
		FrameHeight: frameH,
		Message: fmt.Sprintf("recording %s [x=%d y=%d w=%d h=%d] of the %dx%d frame space to %s; it stops automatically after %ds, call RecordStop to finish",
			cmp.Or(req.Mode, "screen"), region.X, region.Y, region.Width, region.Height, frameW, frameH, out, rc.MaxDuration),
	}}
	return ffmpeg, args, rec, nil
}

// launch starts ffmpeg and waits briefly for an early failure. The badge is
// switched on before Start so the waiter's "off" can never overtake it.
func (r *ScreenRecorder) launch(ffmpeg string, args []string, rec *recording) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.idleLocked(); err != nil {
		return err
	}

	// ponytail: ffmpeg shares the CLI's process group, so closing the terminal
	// (SIGHUP) can cut it off before the MP4 is finalized; give it its own
	// group (Setpgid / CREATE_NEW_PROCESS_GROUP) if that matters.
	cmd := exec.Command(ffmpeg, args...) // not CommandContext: it must outlive the tool call
	stdin, err := cmd.StdinPipe()        // ffmpeg quits on "q"; a TTY stdin would also garble the TUI
	if err != nil {
		return fmt.Errorf("ffmpeg stdin: %w", err)
	}
	cmd.Stderr = &rec.stderr
	rec.cmd, rec.stdin, rec.done = cmd, stdin, make(chan struct{})

	r.notifier.Notify(agentdomain.ScreenRecordingStatusEvent{Active: true})
	if err := cmd.Start(); err != nil {
		r.notifier.Notify(agentdomain.ScreenRecordingStatusEvent{Active: false})
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	rec.started = time.Now()
	go func() {
		rec.err = cmd.Wait()
		rec.ended = time.Now()
		r.notifier.Notify(agentdomain.ScreenRecordingStatusEvent{Active: false})
		close(rec.done)
	}()

	select {
	case <-rec.done:
		_ = os.Remove(rec.status.Path)
		return fmt.Errorf("ffmpeg (%s) stopped right after starting: %s; it needs libx264 and screen capture support, so install a full ffmpeg build (e.g. `brew install ffmpeg`, `apt install ffmpeg`), or delete it if it is an outdated download under ~/.infer/bin",
			ffmpeg, stderrTail(rec))
	case <-time.After(r.startupWait):
	}
	r.cur = rec
	return nil
}

// Stop finalizes the active recording, or the one the time cap already
// finished, and reports the file.
func (r *ScreenRecorder) Stop() (RecordingStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.cur
	if rec == nil {
		return RecordingStatus{}, errors.New("no recording is running; call RecordStart first")
	}
	r.cur = nil
	return rec.finish()
}

// Close finalizes any active recording and refuses new ones. The container
// calls it on shutdown (normal exit, SIGINT, SIGTERM).
func (r *ScreenRecorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if rec := r.cur; rec != nil {
		r.cur = nil
		if _, err := rec.finish(); err != nil {
			logger.Warn("failed to finalize screen recording", "error", err)
		}
	}
}

// finish asks ffmpeg to quit (writing the MP4 index), waits, and reports.
// Exit status 255 is ffmpeg finalizing after a signal (e.g. a terminal
// Ctrl+C reaching the whole process group), which still yields a valid file.
func (rec *recording) finish() (RecordingStatus, error) {
	stopped := false
	select {
	case <-rec.done: // hit the time cap, or was signalled
	default:
		stopped = true
		_, _ = io.WriteString(rec.stdin, "q") // an error means ffmpeg is already exiting
		select {
		case <-rec.done:
		case <-time.After(recordStopTimeout):
			_ = rec.cmd.Process.Kill()
			<-rec.done
			return RecordingStatus{}, fmt.Errorf("ffmpeg did not finalize %s within %s and was killed; the file may not play", rec.status.Path, recordStopTimeout)
		}
	}

	var exitErr *exec.ExitError
	if rec.err != nil && (!errors.As(rec.err, &exitErr) || exitErr.ExitCode() != 255) {
		return RecordingStatus{}, fmt.Errorf("ffmpeg failed while recording %s: %v: %s", rec.status.Path, rec.err, stderrTail(rec))
	}
	fi, err := os.Stat(rec.status.Path)
	if err != nil || fi.Size() == 0 {
		return RecordingStatus{}, fmt.Errorf("recording %s is missing or empty: %s", rec.status.Path, stderrTail(rec))
	}

	s := rec.status
	s.DurationSeconds = rec.ended.Sub(rec.started).Round(100 * time.Millisecond).Seconds()
	s.SizeBytes = fi.Size()
	s.Capped = !stopped && rec.err == nil
	s.Message = fmt.Sprintf("saved %s (%.1fs, %.1f MB)", s.Path, s.DurationSeconds, float64(s.SizeBytes)/(1<<20))
	if s.Capped {
		s.Message += "; it stopped automatically at the max_duration limit"
	}
	return s, nil
}

// stderrTail returns ffmpeg's last error line. Only call it after done.
func stderrTail(rec *recording) string {
	lines := strings.Split(strings.TrimSpace(rec.stderr.String()), "\n")
	if tail := strings.TrimSpace(lines[len(lines)-1]); tail != "" {
		return tail
	}
	if rec.err != nil {
		return rec.err.Error()
	}
	return "no error output"
}

// resolveFFmpeg prefers ffmpeg on PATH and otherwise downloads the prebuilt
// binary into ~/.infer/bin.
// ponytail: no capability probe or re-download of an outdated ~/.infer/bin
// build; it is shared with the gateway and speech tools, and the startup
// check reports a missing encoder with the fix.
func resolveFFmpeg(ctx context.Context) (string, error) {
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, nil
	}
	path, err := audio.NewBinaryStore(config.SpeechToTextConfig{AutoDownload: true}).EnsureBinary(ctx, "ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg is not on PATH and downloading it failed: %w; install ffmpeg (e.g. `brew install ffmpeg`, `apt install ffmpeg`)", err)
	}
	return path, nil
}

// clampToScreen crops window bounds to the primary screen.
func clampToScreen(b display.Region, screenW, screenH int, window string) (display.Region, error) {
	x0, y0 := max(b.X, 0), max(b.Y, 0)
	x1, y1 := min(b.X+b.Width, screenW), min(b.Y+b.Height, screenH)
	if x1 <= x0 || y1 <= y0 {
		return display.Region{}, fmt.Errorf("window %q is not on the primary display", window)
	}
	return display.Region{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, nil
}

// nativeRect scales a logical rectangle to the pixels the grabber captures,
// flooring the origin, clamping to the screen and rounding the size down to
// even numbers (libx264 with yuv420p needs even dimensions).
func nativeRect(rect display.Region, s capture.Screen) display.Region {
	sx := float64(s.NativeWidth) / float64(s.Width)
	sy := float64(s.NativeHeight) / float64(s.Height)
	x, y := int(float64(rect.X)*sx), int(float64(rect.Y)*sy)
	return display.Region{
		X:      x,
		Y:      y,
		Width:  min(int(float64(rect.Width)*sx), s.NativeWidth-x) &^ 1,
		Height: min(int(float64(rect.Height)*sy), s.NativeHeight-y) &^ 1,
	}
}

// ffmpegArgs builds the capture command for a logical screen rectangle.
// ponytail: records at native resolution; 5K+ displays exceed the 4096px
// width many H.264 decoders accept, add a scale filter if that bites.
// macOS crops relative to the captured input size, so the Retina backing
// scale never needs to be known; X11 and GDI take the area as input options.
func ffmpegArgs(goos, displayName string, rect display.Region, s capture.Screen, fps, maxSeconds int, out string) ([]string, error) {
	if rect.Width < 2 || rect.Height < 2 {
		return nil, fmt.Errorf("capture area %dx%d is too small to record", rect.Width, rect.Height)
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostats"}
	rate := strconv.Itoa(fps)
	switch goos {
	case "darwin":
		crop := fmt.Sprintf("crop=w=trunc(iw*%d/%d/2)*2:h=trunc(ih*%d/%d/2)*2:x=trunc(iw*%d/%d):y=trunc(ih*%d/%d)",
			rect.Width, s.Width, rect.Height, s.Height, rect.X, s.Width, rect.Y, s.Height)
		args = append(args, "-f", "avfoundation", "-capture_cursor", "1", "-framerate", rate,
			"-i", "Capture screen 0:none", "-vf", crop)
	case "linux":
		n := nativeRect(rect, s)
		args = append(args, "-f", "x11grab", "-draw_mouse", "1", "-framerate", rate,
			"-video_size", fmt.Sprintf("%dx%d", n.Width, n.Height), "-i", fmt.Sprintf("%s+%d,%d", displayName, n.X, n.Y))
	case "windows":
		n := nativeRect(rect, s)
		args = append(args, "-f", "gdigrab", "-draw_mouse", "1", "-framerate", rate,
			"-offset_x", strconv.Itoa(n.X), "-offset_y", strconv.Itoa(n.Y),
			"-video_size", fmt.Sprintf("%dx%d", n.Width, n.Height), "-i", "desktop")
	default:
		return nil, fmt.Errorf("screen recording is not supported on %s", goos)
	}
	// -r pins the output rate: without it ffmpeg guesses one from the first
	// grabbed frames (x11grab drifts to 23-26fps) and pads with duplicates.
	return append(args, "-t", strconv.Itoa(maxSeconds), "-r", rate,
		"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-y", out), nil
}
