package domain

import (
	"testing"
)

func TestScreenInputBuildsTheActionsItDrives(t *testing.T) {
	tests := []struct {
		name    string
		input   ScreenInput
		want    Action
		wantErr string
	}{
		{
			name:  "click carries its frame target",
			input: ScreenInput{Kind: ActionClick, X: 64, Y: 32},
			want:  Action{Kind: ActionClick, Target: &Target{X: 64, Y: 32}},
		},
		{
			name:  "double click keeps its frame target",
			input: ScreenInput{Kind: ActionDoubleClick, X: 2, Y: 3, Button: "left"},
			want:  Action{Kind: ActionDoubleClick, Target: &Target{X: 2, Y: 3}, Button: "left"},
		},
		{
			name:  "scroll keeps a reversed amount",
			input: ScreenInput{Kind: ActionScroll, Amount: -2, Direction: "horizontal"},
			want:  Action{Kind: ActionScroll, Direction: "horizontal", Amount: -2},
		},
		{
			name:  "a move carries its target",
			input: ScreenInput{Kind: ActionMove, X: 5, Y: 6},
			want:  Action{Kind: ActionMove, Target: &Target{X: 5, Y: 6}},
		},
		{
			name:  "type carries its text",
			input: ScreenInput{Kind: ActionType, Text: "hello"},
			want:  Action{Kind: ActionType, Text: "hello"},
		},
		{
			name:    "what the person never drives is refused",
			input:   ScreenInput{Kind: ActionScreenshot},
			wantErr: `unsupported screen input "screenshot"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.input.Action()
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("Action() = (%+v, %v), want the error %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Action(): %v", err)
			}
			if got.Kind != tt.want.Kind || got.Text != tt.want.Text || got.Button != tt.want.Button || got.Direction != tt.want.Direction || got.Amount != tt.want.Amount {
				t.Fatalf("Action() = %+v, want %+v", got, tt.want)
			}
			if (got.Target == nil) != (tt.want.Target == nil) {
				t.Fatalf("Action() target = %+v, want %+v", got.Target, tt.want.Target)
			}
			if got.Target != nil && (*got.Target != *tt.want.Target) {
				t.Fatalf("Action() target = %+v, want %+v", *got.Target, *tt.want.Target)
			}
		})
	}
}

func TestIsInputActionTellsTheAgentWaitFromTheObservation(t *testing.T) {
	inputs := []ActionKind{ActionMove, ActionClick, ActionDoubleClick, ActionTripleClick, ActionScroll, ActionType, ActionKey, ActionPress}
	for _, kind := range inputs {
		if !IsInputAction(kind) {
			t.Errorf("IsInputAction(%s) = false, want true", kind)
		}
	}
	observations := []ActionKind{ActionScreenshot, ActionCursor, ActionAccessibility}
	for _, kind := range observations {
		if IsInputAction(kind) {
			t.Errorf("IsInputAction(%s) = true, want false", kind)
		}
	}
}
