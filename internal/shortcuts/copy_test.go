package shortcuts

import (
	"context"
	"errors"
	"strings"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// fakeClipboard is a hand-written ClipboardWriter that records the copied text
// instead of touching the real system clipboard.
type fakeClipboard struct {
	copied string
	called int
	err    error
}

func (f *fakeClipboard) Copy(_ context.Context, text string) error {
	f.called++
	f.copied = text
	return f.err
}

func TestCopyShortcut_Metadata(t *testing.T) {
	sc := NewCopyShortcut(&domainmocks.FakeConversationRepository{}, &fakeClipboard{})

	if sc.GetName() != "copy" {
		t.Errorf("GetName = %q, want %q", sc.GetName(), "copy")
	}
	if sc.GetDescription() == "" {
		t.Error("GetDescription should not be empty")
	}
	if !strings.HasPrefix(sc.GetUsage(), "/copy") {
		t.Errorf("GetUsage = %q, want it to start with /copy", sc.GetUsage())
	}

	cases := []struct {
		args []string
		want bool
	}{
		{nil, true},
		{[]string{"markdown"}, true},
		{[]string{"a", "b"}, false},
	}
	for _, tc := range cases {
		if got := sc.CanExecute(tc.args); got != tc.want {
			t.Errorf("CanExecute(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestCopyShortcut_EmptyConversation(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetMessageCountReturns(0)
	clip := &fakeClipboard{}

	sc := NewCopyShortcut(repo, clip)
	res, err := sc.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !res.Success {
		t.Errorf("expected Success=true for empty conversation, got false")
	}
	if !strings.Contains(res.Output, "No conversation to copy") {
		t.Errorf("expected empty-conversation message, got: %s", res.Output)
	}
	if clip.called != 0 {
		t.Errorf("clipboard should not be called for empty conversation, called %d times", clip.called)
	}
	if repo.ExportCallCount() != 0 {
		t.Errorf("Export should not be called for empty conversation, called %d times", repo.ExportCallCount())
	}
}

func TestCopyShortcut_SuccessfulDefaultCopy(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetMessageCountReturns(2)
	// 12 bytes, 2 newlines -> reported as 3 lines.
	exported := []byte("line1\nline2\n")
	repo.ExportReturns(exported, nil)
	clip := &fakeClipboard{}

	sc := NewCopyShortcut(repo, clip)
	res, err := sc.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !res.Success {
		t.Errorf("expected Success=true, got false (output: %s)", res.Output)
	}
	if res.SideEffect != SideEffectNone {
		t.Errorf("expected SideEffectNone, got %v", res.SideEffect)
	}
	if repo.ExportCallCount() != 1 {
		t.Fatalf("expected Export called once, got %d", repo.ExportCallCount())
	}
	if got := repo.ExportArgsForCall(0); got != domain.ExportText {
		t.Errorf("expected default format %v, got %v", domain.ExportText, got)
	}
	if clip.called != 1 {
		t.Fatalf("expected clipboard called once, got %d", clip.called)
	}
	if clip.copied != string(exported) {
		t.Errorf("clipboard received %q, want %q", clip.copied, string(exported))
	}
	if !strings.Contains(res.Output, "Copied conversation to clipboard") {
		t.Errorf("expected success confirmation, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "3 lines") {
		t.Errorf("expected line count in output, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "12 bytes") {
		t.Errorf("expected byte count %d in output, got: %s", len(exported), res.Output)
	}
}

func TestCopyShortcut_FormatRouting(t *testing.T) {
	cases := []struct {
		name string
		arg  string
		want domain.ExportFormat
	}{
		{"text", "text", domain.ExportText},
		{"txt alias", "txt", domain.ExportText},
		{"markdown", "markdown", domain.ExportMarkdown},
		{"md alias", "md", domain.ExportMarkdown},
		{"json", "json", domain.ExportJSON},
		{"uppercase", "JSON", domain.ExportJSON},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &domainmocks.FakeConversationRepository{}
			repo.GetMessageCountReturns(1)
			repo.ExportReturns([]byte("data"), nil)
			clip := &fakeClipboard{}

			sc := NewCopyShortcut(repo, clip)
			res, err := sc.Execute(context.Background(), []string{tc.arg})
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if !res.Success {
				t.Fatalf("expected Success=true, got false (output: %s)", res.Output)
			}
			if got := repo.ExportArgsForCall(0); got != tc.want {
				t.Errorf("format %q routed to %v, want %v", tc.arg, got, tc.want)
			}
		})
	}
}

func TestCopyShortcut_UnknownFormat(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetMessageCountReturns(1)
	clip := &fakeClipboard{}

	sc := NewCopyShortcut(repo, clip)
	res, err := sc.Execute(context.Background(), []string{"xml"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if res.Success {
		t.Errorf("expected Success=false for unknown format")
	}
	if !strings.Contains(res.Output, "unknown format") {
		t.Errorf("expected unknown-format message, got: %s", res.Output)
	}
	if repo.ExportCallCount() != 0 {
		t.Errorf("Export should not be called for unknown format, called %d times", repo.ExportCallCount())
	}
	if clip.called != 0 {
		t.Errorf("clipboard should not be called for unknown format, called %d times", clip.called)
	}
}

func TestCopyShortcut_ExportError(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetMessageCountReturns(2)
	repo.ExportReturns(nil, errors.New("boom"))
	clip := &fakeClipboard{}

	sc := NewCopyShortcut(repo, clip)
	res, err := sc.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if res.Success {
		t.Errorf("expected Success=false when Export fails")
	}
	if !strings.Contains(res.Output, "Failed to export conversation") {
		t.Errorf("expected export-failure message, got: %s", res.Output)
	}
	if clip.called != 0 {
		t.Errorf("clipboard should not be called when Export fails, called %d times", clip.called)
	}
}

func TestCopyShortcut_ClipboardError(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetMessageCountReturns(2)
	repo.ExportReturns([]byte("data"), nil)
	clip := &fakeClipboard{err: errors.New("no util")}

	sc := NewCopyShortcut(repo, clip)
	res, err := sc.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if res.Success {
		t.Errorf("expected Success=false when clipboard write fails")
	}
	if !strings.Contains(res.Output, "Failed to copy to clipboard") {
		t.Errorf("expected clipboard-failure message, got: %s", res.Output)
	}
}
