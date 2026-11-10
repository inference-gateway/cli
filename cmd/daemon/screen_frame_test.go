package daemon

import (
	"errors"
	"testing"

	web "github.com/inference-gateway/cli/internal/presentation/web"
)

func TestFrameOfReadsTheWorkersFrameReply(t *testing.T) {
	frame, err := frameOf([]byte(`{"type":"frame","id":"s1","frame":{"id":"f1","data":"anBlZw==","format":"jpeg"}}`))
	if err != nil || frame == nil || frame.ID != "f1" {
		t.Fatalf("frameOf(a frame) = (%+v, %v), want the frame f1", frame, err)
	}

	if _, err := frameOf([]byte(`{"type":"frame","id":"s1","unchanged":true}`)); !errors.Is(err, web.ErrFrameUnchanged) {
		t.Fatalf("frameOf(unchanged) = %v, want ErrFrameUnchanged", err)
	}

	if _, err := frameOf([]byte(`{"type":"frame","id":"s1","error":"buffer is empty"}`)); err == nil || err.Error() != "buffer is empty" {
		t.Fatalf("frameOf(an error) = %v, want the worker's error", err)
	}
}
