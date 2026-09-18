package shortcuts

import (
	"testing"
)

func TestCustomShortcutConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  CustomShortcutConfig
		wantErr bool
	}{
		{"command only", CustomShortcutConfig{Name: "git", Command: "git diff"}, false},
		{"tool only", CustomShortcutConfig{Name: "read", Tool: "Read"}, false},
		{"no name", CustomShortcutConfig{Command: "git diff"}, true},
		{"neither command nor tool", CustomShortcutConfig{Name: "git"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.config.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
