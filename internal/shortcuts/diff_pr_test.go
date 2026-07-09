package shortcuts

import (
	"context"
	"testing"
)

func TestDiffPRShortcut(t *testing.T) {
	s := NewDiffPRShortcut()

	if s.GetName() != "diff-pr" {
		t.Errorf("GetName() = %q, want %q", s.GetName(), "diff-pr")
	}
	if s.GetUsage() != "/diff-pr [<pr-number>]" {
		t.Errorf("GetUsage() = %q, want %q", s.GetUsage(), "/diff-pr [<pr-number>]")
	}
	if s.GetDescription() == "" {
		t.Error("GetDescription() is empty")
	}

	if !s.CanExecute(nil) {
		t.Error("CanExecute(nil) = false, want true")
	}
	if !s.CanExecute([]string{"792"}) {
		t.Error("CanExecute([792]) = false, want true")
	}
	if s.CanExecute([]string{"792", "extra"}) {
		t.Error("CanExecute([792, extra]) = true, want false")
	}

	// Execute with no args - will fail because gh is not available in test env,
	// but should return a non-nil result with Success=false and an error message.
	res, err := s.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Error("Execute result should not be Success (gh not available in test)")
	}
	if res.Output == "" {
		t.Error("Execute result Output is empty, expected error message")
	}
}
