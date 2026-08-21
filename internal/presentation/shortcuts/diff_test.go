package shortcuts

import (
	"context"
	"testing"
)

func TestDiffShortcut(t *testing.T) {
	s := NewDiffShortcut()

	if s.GetName() != "diff" {
		t.Errorf("GetName() = %q, want %q", s.GetName(), "diff")
	}
	if s.GetUsage() != "/diff" {
		t.Errorf("GetUsage() = %q, want %q", s.GetUsage(), "/diff")
	}
	if s.GetDescription() == "" {
		t.Error("GetDescription() is empty")
	}

	if !s.CanExecute(nil) {
		t.Error("CanExecute(nil) = false, want true")
	}
	if s.CanExecute([]string{"extra"}) {
		t.Error("CanExecute([extra]) = true, want false")
	}

	res, err := s.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Error("Execute result not Success")
	}
	if res.SideEffect != SideEffectShowDiffViewer {
		t.Errorf("SideEffect = %v, want SideEffectShowDiffViewer", res.SideEffect)
	}
}
