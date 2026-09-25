package display

import "testing"

func TestUseX11(t *testing.T) {
	tests := []struct {
		name    string
		wayland string
		display string
		want    bool
	}{
		{name: "x11 session", display: ":0", want: true},
		{name: "wayland session", wayland: "wayland-0", want: false},
		{name: "xwayland session", wayland: "wayland-0", display: ":0", want: false},
		{name: "no session", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WAYLAND_DISPLAY", tt.wayland)
			t.Setenv("DISPLAY", tt.display)
			if got := useX11(); got != tt.want {
				t.Errorf("useX11() = %v, want %v", got, tt.want)
			}
		})
	}
}
