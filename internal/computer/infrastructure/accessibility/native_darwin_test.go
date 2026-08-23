//go:build darwin

package accessibility

import "testing"

func TestApplicationNamesMatch(t *testing.T) {
	tests := []struct {
		name      string
		owner     string
		requested string
		want      bool
	}{
		{name: "exact", owner: "CapCut", requested: "CapCut", want: true},
		{name: "case insensitive", owner: "iTerm2", requested: "iterm2", want: true},
		{name: "bundle and display names", owner: "inference-gateway-desktop", requested: "Inference Gateway Desktop", want: true},
		{name: "different applications", owner: "Inference Gateway", requested: "Inference Gateway Desktop", want: false},
		{name: "punctuation only", owner: "-", requested: ".", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := applicationNamesMatch(test.owner, test.requested); got != test.want {
				t.Fatalf("applicationNamesMatch(%q, %q) = %v, want %v", test.owner, test.requested, got, test.want)
			}
		})
	}
}
