package display

import (
	"errors"
	"strings"
	"testing"
)

func TestScrollMouseTurnsTheWheelOneNotchPerClick(t *testing.T) {
	tests := []struct {
		name          string
		clicks        int
		axis          string
		wantNotches   int
		wantDirection string
	}{
		{name: "down", clicks: 3, axis: "vertical", wantNotches: 3, wantDirection: "down"},
		{name: "up", clicks: -2, axis: "vertical", wantNotches: 2, wantDirection: "up"},
		{name: "right", clicks: 1, axis: "horizontal", wantNotches: 1, wantDirection: "right"},
		{name: "left", clicks: -4, axis: "horizontal", wantNotches: 4, wantDirection: "left"},
		{name: "axis defaults to vertical", clicks: 1, axis: "", wantNotches: 1, wantDirection: "down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNotches, gotDirection := scrollOnce(t, tt.clicks, tt.axis)
			if gotNotches != tt.wantNotches || gotDirection != tt.wantDirection {
				t.Fatalf("wheel turned %d %s, want %d %s", gotNotches, gotDirection, tt.wantNotches, tt.wantDirection)
			}
		})
	}
}

func TestScrollMouseByZeroLeavesTheWheelAlone(t *testing.T) {
	restore := robot.ScrollDir
	t.Cleanup(func() { robot.ScrollDir = restore })
	robot.ScrollDir = func(int, ...any) error {
		t.Fatal("the wheel turned for a scroll by zero")
		return nil
	}
	if err := (&robotController{}).ScrollMouse(t.Context(), 0, "vertical"); err != nil {
		t.Fatal(err)
	}
}

func TestSendKeyComboAcceptsThePageKeyAliases(t *testing.T) {
	tests := []struct {
		combo   string
		wantKey string
	}{
		{combo: "pagedown", wantKey: "pagedown"},
		{combo: "page_down", wantKey: "pagedown"},
		{combo: "PgDn", wantKey: "pagedown"},
		{combo: "page_up", wantKey: "pageup"},
		{combo: "pgup", wantKey: "pageup"},
		{combo: "shift+page_down", wantKey: "pagedown"},
	}
	for _, tt := range tests {
		t.Run(tt.combo, func(t *testing.T) {
			var tapped string
			tapKeysWith(t, func(key string, _ ...any) error {
				tapped = key
				return nil
			})
			if err := (&robotController{}).SendKeyCombo(t.Context(), tt.combo); err != nil {
				t.Fatal(err)
			}
			if tapped != tt.wantKey {
				t.Fatalf("tapped %q, want %q", tapped, tt.wantKey)
			}
		})
	}
}

func TestSendKeyComboNamesTheKeyItCouldNotSend(t *testing.T) {
	refused := errors.New("robotgo: window not found")
	tapKeysWith(t, func(string, ...any) error { return refused })

	err := (&robotController{}).SendKeyCombo(t.Context(), "ctrl+hyperspace")

	if !errors.Is(err, refused) || !strings.Contains(err.Error(), `"hyperspace"`) || !strings.Contains(err.Error(), "known key name") {
		t.Fatalf("err = %v, want it to name the key and point at the key name", err)
	}
}

func scrollOnce(t *testing.T, clicks int, axis string) (notches int, direction string) {
	t.Helper()
	restore := robot.ScrollDir
	t.Cleanup(func() { robot.ScrollDir = restore })
	robot.ScrollDir = func(x int, dir ...any) error {
		notches, direction = x, dir[0].(string)
		return nil
	}
	if err := (&robotController{}).ScrollMouse(t.Context(), clicks, axis); err != nil {
		t.Fatal(err)
	}
	return notches, direction
}

func tapKeysWith(t *testing.T, tap func(key string, args ...any) error) {
	t.Helper()
	restore := robot.KeyTap
	t.Cleanup(func() { robot.KeyTap = restore })
	robot.KeyTap = tap
}
