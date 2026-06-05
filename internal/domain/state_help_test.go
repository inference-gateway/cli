package domain

import "testing"

func TestViewStateHelp_String(t *testing.T) {
	if got := ViewStateHelp.String(); got != "Help" {
		t.Errorf("expected ViewStateHelp.String() == %q, got %q", "Help", got)
	}
}

func TestTransition_ChatToHelpAndBack(t *testing.T) {
	s := NewApplicationState()

	if err := s.TransitionToView(ViewStateChat); err != nil {
		t.Fatalf("transition to chat failed: %v", err)
	}

	if err := s.TransitionToView(ViewStateHelp); err != nil {
		t.Fatalf("expected chat -> help to be valid, got: %v", err)
	}
	if s.GetCurrentView() != ViewStateHelp {
		t.Errorf("expected current view Help, got %s", s.GetCurrentView())
	}

	if err := s.TransitionToView(ViewStateChat); err != nil {
		t.Fatalf("expected help -> chat to be valid, got: %v", err)
	}
	if s.GetCurrentView() != ViewStateChat {
		t.Errorf("expected current view Chat, got %s", s.GetCurrentView())
	}
}

func TestTransition_HelpFromNonChatIsInvalid(t *testing.T) {
	s := NewApplicationState()

	// Model selection only allows transitioning into chat, not directly to help.
	if err := s.TransitionToView(ViewStateHelp); err == nil {
		t.Error("expected model-selection -> help to be rejected")
	}
}
