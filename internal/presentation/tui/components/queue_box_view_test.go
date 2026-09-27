package components

import (
	"strings"
	"testing"

	ansi "github.com/charmbracelet/x/ansi"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func newQueueBoxView() *QueueBoxView {
	view := NewQueueBoxView(agentStartupProvider())
	view.SetWidth(80)
	return view
}

func queuedMessage(source convdomain.QueuedMessageSource, content string) convdomain.QueuedMessage {
	return convdomain.QueuedMessage{
		Message:   sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(content)},
		RequestID: "req-test",
		Source:    source,
	}
}

func TestQueueBoxView_SourceMarkers(t *testing.T) {
	tests := []struct {
		name   string
		source convdomain.QueuedMessageSource
		marker string
	}{
		{"composer", convdomain.QueueSourceComposer, "[You]"},
		{"stdin", convdomain.QueueSourceStdin, "[Stdin]"},
		{"a2a task", convdomain.QueueSourceA2A, "[A2A]"},
		{"background shell", convdomain.QueueSourceShell, "[Shell]"},
		{"subagent", convdomain.QueueSourceSubagent, "[Subagent]"},
		{"unmapped job kind", convdomain.QueueSourceJob, "[Job]"},
		{"legacy empty source", "", "[Job]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := newQueueBoxView()
			out := ansi.Strip(view.Render([]convdomain.QueuedMessage{
				queuedMessage(tt.source, "hello world"),
			}))

			want := "   " + tt.marker + " hello world"
			if !strings.Contains(out, want) {
				t.Errorf("rendered %q, want a line containing %q", out, want)
			}
		})
	}
}

// TestQueueBoxView_MarkersInFIFOOrder pins the oldest-first rendering: the
// first entry in the queue is the first line in the box.
func TestQueueBoxView_MarkersInFIFOOrder(t *testing.T) {
	view := newQueueBoxView()
	messages := []convdomain.QueuedMessage{
		queuedMessage(convdomain.QueueSourceComposer, "first typed"),
		queuedMessage(convdomain.QueueSourceA2A, "second from a2a"),
		queuedMessage(convdomain.QueueSourceShell, "third from shell"),
	}

	lines := strings.Split(ansi.Strip(view.Render(messages)), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 queued lines, got %d: %q", len(lines), lines)
	}
	for i, want := range []string{"[You] first typed", "[A2A] second from a2a", "[Shell] third from shell"} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want it to contain %q", i, lines[i], want)
		}
	}
}

// TestQueueBoxView_JobResultHeaderCollapsed pins every job kind's finished
// result collapsing to its one-line header in the preview.
func TestQueueBoxView_JobResultHeaderCollapsed(t *testing.T) {
	tests := []struct {
		name    string
		source  convdomain.QueuedMessageSource
		content string
	}{
		{"a2a task", convdomain.QueueSourceA2A, "[A2A Task Completed: research]\n\nTask result body"},
		{"background shell", convdomain.QueueSourceShell, "[Background Shell Completed: build]\n\nexit 0"},
		{"subagent", convdomain.QueueSourceSubagent, "[Subagent Failed: explore]\n\nagent died"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := newQueueBoxView()
			out := ansi.Strip(view.Render([]convdomain.QueuedMessage{queuedMessage(tt.source, tt.content)}))

			header, _, _ := strings.Cut(tt.content, "\n")
			if !strings.Contains(out, header) {
				t.Errorf("rendered %q, want the collapsed header %q", out, header)
			}
			if _, body, found := strings.Cut(tt.content, "\n\n"); found && strings.Contains(out, body) {
				t.Errorf("rendered %q, want the body %q dropped from the preview", out, body)
			}
		})
	}
}

// TestQueueBoxView_JobNoteNotCollapsed keeps a supervisor progress note (no
// completion header) whole; only formatted job results collapse.
func TestQueueBoxView_JobNoteNotCollapsed(t *testing.T) {
	view := newQueueBoxView()
	note := "compiling (42%)\nstill running"
	out := ansi.Strip(view.Render([]convdomain.QueuedMessage{
		queuedMessage(convdomain.QueueSourceShell, note),
	}))

	if !strings.Contains(out, "still running") {
		t.Errorf("rendered %q, want the note body kept", out)
	}
}

// TestQueueBoxView_ComposerHeaderLikeTextNotCollapsed: a typed message that
// happens to start like a job header keeps its full preview.
func TestQueueBoxView_ComposerHeaderLikeTextNotCollapsed(t *testing.T) {
	view := newQueueBoxView()
	content := "[Something Completed: phase one]\nfull body"
	out := ansi.Strip(view.Render([]convdomain.QueuedMessage{
		queuedMessage(convdomain.QueueSourceComposer, content),
	}))

	if !strings.Contains(out, "full body") {
		t.Errorf("rendered %q, want the composer text kept whole", out)
	}
}
