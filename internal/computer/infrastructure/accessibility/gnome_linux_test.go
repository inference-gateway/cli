package accessibility

import (
	"errors"
	"testing"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

func TestPickWindow(t *testing.T) {
	windows := []gnomeWindow{
		{PID: 10, App: "org.gnome.Nautilus", X: 20, Y: 40},
		{PID: 11, App: "org.gnome.TextEditor", X: 300, Y: 150, Focused: true},
		{PID: 10, App: "org.gnome.Nautilus", X: 60, Y: 80},
	}
	tests := []struct {
		target  string
		want    gnomeWindow
		wantErr error
	}{
		{target: "", want: windows[1]},
		{target: "frontmost", want: windows[1]},
		{target: "app:nautilus", want: windows[2]},
		{target: "TextEditor", want: windows[1]},
		{target: "pid:10", want: windows[2]},
		{target: "pid:99", want: gnomeWindow{PID: 99}},
		{target: "pid:zero", wantErr: ErrUnavailable},
		{target: "app:gimp", wantErr: ErrUnavailable},
		{target: "dock", wantErr: ErrUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got, err := pickWindow(windows, tt.target)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Errorf("pickWindow(%q) = %+v, %v, want %+v, %v", tt.target, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestPlaceOnScreenMovesBoxesToTheWindow(t *testing.T) {
	elements := []computerdomain.UIElement{{Label: "Save", BBox: [4]int{10, 20, 110, 60}}}
	placed := placeOnScreen(elements, gnomeWindow{X: 300, Y: 150})
	if want := [4]int{310, 170, 410, 210}; placed[0].BBox != want {
		t.Errorf("bbox = %v, want %v", placed[0].BBox, want)
	}
}
