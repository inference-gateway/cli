package headless

import (
	"errors"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func screenSource(frame *agentdomain.Frame, err error) func(string) (agentdomain.FrameSource, bool) {
	source := &agentdomainmocks.FakeFrameSource{}
	source.GetLatestFrameReturns(frame, err)
	return func(name string) (agentdomain.FrameSource, bool) { return source, name == screenFrameSource }
}

func TestPanelReadsTheLatestScreenFrame(t *testing.T) {
	deps := panelDeps()
	deps.Frames = screenSource(&agentdomain.Frame{ID: "f1", Data: "anVwZw==", Format: "jpeg", Width: 4, Height: 2}, nil)
	p, sink := startPanel(deps)

	if !handle(t, p, map[string]any{"type": "read_frame", "id": "s1"}) {
		t.Fatal("read_frame must be consumed")
	}
	frame := readFrame(t, sink, "frame")
	got := frame["frame"].(map[string]any)
	if frame["id"] != "s1" || got["data"] != "anVwZw==" || got["format"] != "jpeg" || got["width"] != float64(4) {
		t.Fatalf("unexpected reply %v", frame)
	}
}

func TestPanelAnswersReadFrameErrors(t *testing.T) {
	tests := []struct {
		name   string
		frames func(string) (agentdomain.FrameSource, bool)
		want   string
	}{
		{"no source", func(string) (agentdomain.FrameSource, bool) { return nil, false }, "streams no screen"},
		{"empty buffer", screenSource(nil, errors.New("buffer is empty")), "buffer is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := panelDeps()
			deps.Frames = tt.frames
			p, sink := startPanel(deps)
			handle(t, p, map[string]any{"type": "read_frame", "id": "s1"})
			frame := readFrame(t, sink, "frame")
			if err, _ := frame["error"].(string); !strings.Contains(err, tt.want) || frame["frame"] != nil {
				t.Fatalf("unexpected reply %v", frame)
			}
		})
	}
}
