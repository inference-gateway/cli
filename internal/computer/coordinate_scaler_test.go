package computer

import "testing"

func TestScaleAPIToScreen(t *testing.T) {
	tests := []struct {
		name             string
		apiX, apiY       int
		screenX, screenY int
		wantErr          bool
	}{
		{"origin", 0, 0, 0, 0, false},
		{"center", 512, 384, 756, 491, false},
		{"max corner", 1024, 768, 1512, 982, false},
		{"x out of bounds", 1025, 100, 0, 0, true},
		{"y out of bounds", 100, 769, 0, 0, true},
		{"negative", -1, 0, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, err := ScaleAPIToScreen(tt.apiX, tt.apiY, 1024, 768, 1512, 982)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (x != tt.screenX || y != tt.screenY) {
				t.Fatalf("got (%d,%d), want (%d,%d)", x, y, tt.screenX, tt.screenY)
			}
		})
	}
}
