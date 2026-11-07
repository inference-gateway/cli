package utils

import "testing"

func TestShortID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"a long id trims to eight", "0123456789abcdef", "01234567"},
		{"an eight id stays itself", "01234567", "01234567"},
		{"a short id stays itself", "abc", "abc"},
		{"an empty id stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShortID(tt.id); got != tt.want {
				t.Errorf("ShortID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}
