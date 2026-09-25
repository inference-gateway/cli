package computer

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	capture "github.com/inference-gateway/cli/internal/computer/infrastructure/capture"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// TestMain lets the test binary stand in for ffmpeg: with INFER_FAKE_FFMPEG
// set it writes the output file (the last argument), then exits on "q" from
// stdin or once the -t duration elapses, like ffmpeg does.
func TestMain(m *testing.M) {
	if os.Getenv("INFER_FAKE_FFMPEG") == "1" {
		fakeFFmpeg(os.Args[1:])
	}
	os.Exit(m.Run())
}

func fakeFFmpeg(args []string) {
	if os.Getenv("INFER_FAKE_FFMPEG_FAIL") == "1" {
		_, _ = os.Stderr.WriteString("[vost#0:0] Unknown encoder 'libx264'\n")
		os.Exit(1)
	}
	limit := 30.0
	if i := slices.Index(args, "-t"); i >= 0 {
		limit, _ = strconv.ParseFloat(args[i+1], 64)
	}
	_ = os.WriteFile(args[len(args)-1], []byte("fake mp4"), 0o644)

	quit := make(chan struct{})
	go func() {
		b := make([]byte, 1)
		for {
			if _, err := os.Stdin.Read(b); err != nil || b[0] == 'q' {
				close(quit)
				return
			}
		}
	}()
	select {
	case <-quit:
	case <-time.After(time.Duration(limit * float64(time.Second))):
	}
	os.Exit(0)
}

type chanNotifier chan any

func (c chanNotifier) Notify(event any) { c <- event }

func newFakeRecorder(t *testing.T) (*ScreenRecorder, chanNotifier) {
	t.Helper()
	t.Setenv("INFER_FAKE_FFMPEG", "1")
	events := make(chanNotifier, 16)
	r := NewScreenRecorder(&config.Config{}, events)
	r.startupWait = 50 * time.Millisecond
	return r, events
}

func launchFake(r *ScreenRecorder, out, seconds string) error {
	return r.launch(os.Args[0], []string{"-t", seconds, out}, &recording{status: RecordingStatus{Path: out}})
}

func TestScreenRecorderStartStop(t *testing.T) {
	r, events := newFakeRecorder(t)
	out := filepath.Join(t.TempDir(), "a.mp4")

	if _, err := r.Stop(); err == nil || !strings.Contains(err.Error(), "no recording is running") {
		t.Fatalf("Stop() with nothing running: err = %v", err)
	}
	if err := launchFake(r, out, "30"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	err := launchFake(r, filepath.Join(t.TempDir(), "b.mp4"), "30")
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second launch: err = %v, want already running", err)
	}

	status, err := r.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if status.Path != out || status.SizeBytes == 0 || status.Capped {
		t.Fatalf("Stop() = %+v, want path %s, non-zero size, not capped", status, out)
	}
	for _, want := range []bool{true, false} {
		if got := (<-events).(agentdomain.ScreenRecordingStatusEvent); got.Active != want {
			t.Fatalf("event Active = %v, want %v", got.Active, want)
		}
	}
	if _, err := r.Stop(); err == nil {
		t.Fatal("second Stop() should fail")
	}
}

func TestScreenRecorderMaxDurationCap(t *testing.T) {
	r, _ := newFakeRecorder(t)
	out := filepath.Join(t.TempDir(), "capped.mp4")
	if err := launchFake(r, out, "0.2"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	<-r.cur.done

	status, err := r.Stop()
	if err != nil {
		t.Fatalf("Stop after cap: %v", err)
	}
	if !status.Capped || status.Path != out {
		t.Fatalf("Stop() = %+v, want capped recording at %s", status, out)
	}
}

func TestScreenRecorderCloseFinalizesAndRefusesNew(t *testing.T) {
	r, _ := newFakeRecorder(t)
	out := filepath.Join(t.TempDir(), "shutdown.mp4")
	if err := launchFake(r, out, "30"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	rec := r.cur

	r.Close()
	select {
	case <-rec.done:
	default:
		t.Fatal("Close() returned while ffmpeg was still running")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("recording missing after Close: %v", err)
	}
	if err := launchFake(r, filepath.Join(t.TempDir(), "late.mp4"), "30"); err == nil {
		t.Fatal("launch after Close should fail")
	}
	r.Close()
}

func TestScreenRecorderStartupFailure(t *testing.T) {
	r, _ := newFakeRecorder(t)
	r.startupWait = 10 * time.Second
	t.Setenv("INFER_FAKE_FFMPEG_FAIL", "1")
	out := filepath.Join(t.TempDir(), "fail.mp4")

	err := launchFake(r, out, "30")
	if err == nil || !strings.Contains(err.Error(), "Unknown encoder 'libx264'") {
		t.Fatalf("launch: err = %v, want ffmpeg stderr", err)
	}
	if r.cur != nil {
		t.Fatal("a failed start must not become the active recording")
	}
}

func TestFFmpegArgs(t *testing.T) {
	out := "/rec/a.mp4"
	tail := []string{"-t", "120", "-r", "15", "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-y", out}
	head := []string{"-hide_banner", "-loglevel", "error", "-nostats"}
	logical := capture.Screen{Width: 1440, Height: 900, NativeWidth: 1440, NativeHeight: 900}
	scaled := capture.Screen{Width: 1280, Height: 720, NativeWidth: 1920, NativeHeight: 1080}

	tests := []struct {
		name   string
		goos   string
		rect   display.Region
		screen capture.Screen
		want   []string
	}{
		{
			name: "darwin screen crops relative to the Retina input", goos: "darwin",
			rect: display.Region{Width: 1440, Height: 900}, screen: logical,
			want: []string{"-f", "avfoundation", "-capture_cursor", "1", "-framerate", "15", "-i", "Capture screen 0:none",
				"-vf", "crop=w=trunc(iw*1440/1440/2)*2:h=trunc(ih*900/900/2)*2:x=trunc(iw*0/1440):y=trunc(ih*0/900)"},
		},
		{
			name: "darwin region", goos: "darwin",
			rect: display.Region{X: 100, Y: 80, Width: 641, Height: 361}, screen: logical,
			want: []string{"-f", "avfoundation", "-capture_cursor", "1", "-framerate", "15", "-i", "Capture screen 0:none",
				"-vf", "crop=w=trunc(iw*641/1440/2)*2:h=trunc(ih*361/900/2)*2:x=trunc(iw*100/1440):y=trunc(ih*80/900)"},
		},
		{
			name: "linux window rounds to even", goos: "linux",
			rect: display.Region{X: 10, Y: 20, Width: 801, Height: 601}, screen: logical,
			want: []string{"-f", "x11grab", "-draw_mouse", "1", "-framerate", "15", "-video_size", "800x600", "-i", ":0+10,20"},
		},
		{
			name: "windows region scales to physical pixels", goos: "windows",
			rect: display.Region{X: 100, Y: 50, Width: 641, Height: 361}, screen: scaled,
			want: []string{"-f", "gdigrab", "-draw_mouse", "1", "-framerate", "15", "-offset_x", "150", "-offset_y", "75",
				"-video_size", "960x540", "-i", "desktop"},
		},
		{
			name: "windows screen", goos: "windows",
			rect: display.Region{Width: 1280, Height: 720}, screen: scaled,
			want: []string{"-f", "gdigrab", "-draw_mouse", "1", "-framerate", "15", "-offset_x", "0", "-offset_y", "0",
				"-video_size", "1920x1080", "-i", "desktop"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ffmpegArgs(tt.goos, ":0", tt.rect, tt.screen, 15, 120, out)
			if err != nil {
				t.Fatalf("ffmpegArgs: %v", err)
			}
			want := slices.Concat(head, tt.want, tail)
			if !slices.Equal(got, want) {
				t.Fatalf("ffmpegArgs()\n got %q\nwant %q", got, want)
			}
		})
	}

	if _, err := ffmpegArgs("plan9", "", display.Region{Width: 10, Height: 10}, logical, 15, 120, out); err == nil {
		t.Error("unsupported OS should fail")
	}
	if _, err := ffmpegArgs("linux", ":0", display.Region{Width: 1, Height: 10}, logical, 15, 120, out); err == nil {
		t.Error("a 1px wide area should fail")
	}
}

func TestNativeRectClampsAndRoundsToEven(t *testing.T) {
	s := capture.Screen{Width: 1366, Height: 768, NativeWidth: 2049, NativeHeight: 1152}
	got := nativeRect(display.Region{Width: 1366, Height: 768}, s)
	if want := (display.Region{Width: 2048, Height: 1152}); got != want {
		t.Fatalf("nativeRect() = %+v, want %+v", got, want)
	}
}

func TestClampToScreen(t *testing.T) {
	got, err := clampToScreen(display.Region{X: -20, Y: 700, Width: 400, Height: 300}, 1440, 900, "app:Safari")
	if err != nil || got != (display.Region{X: 0, Y: 700, Width: 380, Height: 200}) {
		t.Fatalf("clampToScreen() = %+v, %v", got, err)
	}
	if _, err := clampToScreen(display.Region{X: 1500, Y: 0, Width: 400, Height: 300}, 1440, 900, "app:Safari"); err == nil {
		t.Fatal("a window on another display should fail")
	}
}

func TestFrameRegionToScreen(t *testing.T) {
	got, err := frameRegionToScreen(&computerdomain.Region{X: 10, Y: 20, Width: 100, Height: 50}, 1024, 640, 1440, 900)
	if err != nil || got != (display.Region{X: 14, Y: 28, Width: 141, Height: 71}) {
		t.Fatalf("frameRegionToScreen() = %+v, %v", got, err)
	}
	_, err = frameRegionToScreen(&computerdomain.Region{X: 1000, Y: 0, Width: 100, Height: 50}, 1024, 640, 1440, 900)
	if want := "region [x=1000 y=0 w=100 h=50] is outside the 1024x640 frame space"; err == nil || err.Error() != want {
		t.Fatalf("out-of-bounds error = %v, want %q", err, want)
	}
}

func TestParseRecordRequest(t *testing.T) {
	region := map[string]any{"x": 0.0, "y": 80.0, "width": 1280.0, "height": 720.0}
	tests := []struct {
		name    string
		args    map[string]any
		want    recordRequest
		wantErr bool
	}{
		{name: "defaults to screen", args: map[string]any{}, want: recordRequest{Mode: "screen"}},
		{name: "window", args: map[string]any{"mode": "window", "window": "app:Safari"}, want: recordRequest{Mode: "window", Window: "app:Safari"}},
		{name: "region", args: map[string]any{"mode": "region", "region": region},
			want: recordRequest{Mode: "region", Region: &computerdomain.Region{Y: 80, Width: 1280, Height: 720}}},
		{name: "unknown mode", args: map[string]any{"mode": "tab"}, wantErr: true},
		{name: "region without mode", args: map[string]any{"region": region}, wantErr: true},
		{name: "region mode without region", args: map[string]any{"mode": "region"}, wantErr: true},
		{name: "window with screen mode", args: map[string]any{"window": "frontmost"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRecordRequest(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseRecordRequest() err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Mode != tt.want.Mode || got.Window != tt.want.Window || (got.Region == nil) != (tt.want.Region == nil) ||
				(got.Region != nil && *got.Region != *tt.want.Region) {
				t.Fatalf("parseRecordRequest() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
