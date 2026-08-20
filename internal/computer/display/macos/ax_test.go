package macos

import "testing"

func TestNormalizeAXRole(t *testing.T) {
	tests := []struct{ in, want string }{
		{"AXButton", "button"},
		{"AXDockItem", "dock item"},
		{"AXMenuBarItem", "menu bar item"},
		{"AXUnknown", "unknown"},
		{"Weird", "weird"},
	}
	for _, tt := range tests {
		if got := normalizeAXRole(tt.in); got != tt.want {
			t.Errorf("normalizeAXRole(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
