package domain

import (
	"context"
	"fmt"
)

// ScreenInputKind lists the input kinds a person drives a held screen with:
// the pointer, wheel and keyboard, in frame coordinate space. The observation
// kinds stay out; so does the accessibility press, which the person's own
// click replaces.
func ScreenInputKinds() []ActionKind {
	return []ActionKind{ActionMove, ActionClick, ActionDoubleClick, ActionScroll, ActionType, ActionKey}
}

// IsInputAction reports whether the agent executes the kind by driving the
// pointer, keyboard or clipboard, so it waits while the person holds the
// screen. Observations capture or read and never wait.
func IsInputAction(kind ActionKind) bool {
	switch kind {
	case ActionMove, ActionClick, ActionDoubleClick, ActionTripleClick, ActionScroll, ActionType, ActionKey, ActionPress:
		return true
	}
	return false
}

// ScreenInput is one person-driven input on the session's screen, in frame
// coordinate space (the space the Screen view's image and its X-Frame-Width
// and X-Frame-Height headers use): what to do and, per kind, on what.
type ScreenInput struct {
	Kind      ActionKind `json:"kind"` // move, click, double_click, scroll, type, key
	X         int        `json:"x,omitempty"`
	Y         int        `json:"y,omitempty"`
	Text      string     `json:"text,omitempty"`      // type: the text to type
	Combo     string     `json:"combo,omitempty"`     // key: a combo such as "ctrl+shift+t" or a key name
	Button    string     `json:"button,omitempty"`    // click kinds: left (default), right, middle
	Direction string     `json:"direction,omitempty"` // scroll: vertical (default) or horizontal
	Amount    int        `json:"amount,omitempty"`    // scroll: wheel clicks; negative reverses
}

// Action builds the action this input drives the screen with, so the display
// dispatch stays one code path with the Computer tool's. A person sends only
// what they hold the screen for.
func (s ScreenInput) Action() (Action, error) {
	kindOK := false
	for _, kind := range ScreenInputKinds() {
		if kind == s.Kind {
			kindOK = true
			break
		}
	}
	if !kindOK {
		return Action{}, fmt.Errorf("unsupported screen input %q", s.Kind)
	}
	a := Action{
		Kind:      s.Kind,
		Text:      s.Text,
		Combo:     s.Combo,
		Button:    s.Button,
		Direction: s.Direction,
		Amount:    s.Amount,
	}
	if a.Button == "" {
		a.Button = "left"
	}
	if a.Direction == "" {
		a.Direction = "vertical"
	}
	if a.Amount == 0 {
		a.Amount = 3
	}
	if a.Target == nil && (a.Kind == ActionMove || a.Kind == ActionClick || a.Kind == ActionDoubleClick) {
		a.Target = &Target{X: s.X, Y: s.Y}
	}
	switch {
	case a.Kind == ActionType && a.Text == "":
		return a, fmt.Errorf("screen input type requires text")
	case a.Kind == ActionKey && a.Combo == "":
		return a, fmt.Errorf("screen input key requires combo")
	}
	return a, nil
}

// ScreenControl owns the take-over invariant: exactly one actor, the person at
// the keyboard or the agent, drives a screen at a time. TakeOver claims it for
// the person, HandBack hands it to the agent and WaitRelease blocks one agent
// input action until the person lets go.
type ScreenControl interface {
	// TakeOver claims the screen for the person. It fails while the person
	// already holds it, from another tab of the same session or another one.
	TakeOver() error
	// HandBack releases the screen to the agent. Releasing an unheld screen
	// is a no-op.
	HandBack()
	// Held reports whether the person currently drives the screen.
	Held() bool
	// WaitRelease blocks while the person drives the screen and returns when
	// the agent may act again, or the context's error when its turn ends.
	WaitRelease(ctx context.Context) error
	// Drive performs one person input on the screen in frame space.
	Drive(ctx context.Context, input ScreenInput) error
}
