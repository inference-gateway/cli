//go:build linux

package capture

import "testing"

func TestCheckX11(t *testing.T) {
	tests := []struct {
		session, display string
		wantErr          bool
	}{
		{"x11", ":0", false},
		{"", ":1", false},
		{"wayland", ":0", true},
		{"x11", "", true},
	}
	for _, tt := range tests {
		if err := checkX11(tt.session, tt.display); (err != nil) != tt.wantErr {
			t.Errorf("checkX11(%q, %q) err = %v, wantErr %v", tt.session, tt.display, err, tt.wantErr)
		}
	}
}
