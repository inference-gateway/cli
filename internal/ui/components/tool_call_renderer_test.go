package components

import (
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestToolCallRenderer_BashOutputStreamLineCounting verifies that a single
// coalesced BashOutputChunkEvent carrying several newline-joined lines is split
// so TotalOutputLines (which drives the "+N more lines" indicator) and the
// rolling 7-line preview stay accurate. This is the renderer side of the
// streamed-bash-output coalescing fix.
func TestToolCallRenderer_BashOutputStreamLineCounting(t *testing.T) {
	const toolCallID = "tc-1"

	r := NewToolCallRenderer(nil)
	r.tools[toolCallID] = &ToolRenderState{
		CallID:   toolCallID,
		ToolName: "Bash",
		Status:   "running",
	}

	r.handleBashOutputStream(domain.BashOutputChunkEvent{
		ToolCallID: toolCallID,
		Output:     "a\nb\nc\nd\ne\nf\ng\nh",
	})

	state := r.tools[toolCallID]
	if state.TotalOutputLines != 8 {
		t.Errorf("expected TotalOutputLines=8, got %d", state.TotalOutputLines)
	}
	if len(state.OutputBuffer) != 7 {
		t.Fatalf("expected OutputBuffer to keep the last 7 lines, got %d", len(state.OutputBuffer))
	}
	if state.OutputBuffer[0] != "b" || state.OutputBuffer[6] != "h" {
		t.Errorf("expected last 7 lines b..h, got %v", state.OutputBuffer)
	}

	r.handleBashOutputStream(domain.BashOutputChunkEvent{
		ToolCallID: toolCallID,
		Output:     "i\nj",
	})

	state = r.tools[toolCallID]
	if state.TotalOutputLines != 10 {
		t.Errorf("expected TotalOutputLines=10 after second chunk, got %d", state.TotalOutputLines)
	}
	if len(state.OutputBuffer) != 7 {
		t.Errorf("expected OutputBuffer to stay capped at 7, got %d", len(state.OutputBuffer))
	}
	if got := state.OutputBuffer[len(state.OutputBuffer)-1]; got != "j" {
		t.Errorf("expected last preview line to be j, got %q", got)
	}
}
