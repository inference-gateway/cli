package capture

import "testing"

func TestParseTarget(t *testing.T) {
	tests := []struct {
		in      string
		want    target
		wantErr bool
	}{
		{in: "", want: target{frontmost: true}},
		{in: "frontmost", want: target{frontmost: true}},
		{in: "pid:4242", want: target{pid: 4242}},
		{in: "app:Google Chrome", want: target{name: "Google Chrome"}},
		{in: " Safari ", want: target{name: "Safari"}},
		{in: "pid:abc", wantErr: true},
		{in: "pid:-1", wantErr: true},
		{in: "app: ", wantErr: true},
		{in: "dock", wantErr: true},
		{in: "menubar", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseTarget(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseTarget(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("parseTarget(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}
