package domain

import "testing"

func TestTokenCount(t *testing.T) {
	set := int64(42)
	tests := []struct {
		name  string
		count *int64
		want  int
	}{
		{"an unset count reads zero", nil, 0},
		{"a set count reads through", &set, 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TokenCount(tt.count); got != tt.want {
				t.Errorf("TokenCount(%v) = %d, want %d", tt.count, got, tt.want)
			}
		})
	}
}
