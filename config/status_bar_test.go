package config

import (
	"strings"
	"testing"
)

func TestConfigValidate_SubagentLingerSeconds(t *testing.T) {
	tests := []struct {
		name    string
		linger  int
		wantErr bool
	}{
		{"default five lingers", 5, false},
		{"zero drops rows immediately", 0, false},
		{"large windows are allowed", 600, false},
		{"negative values are rejected", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Chat.StatusBar.SubagentLingerSeconds = tt.linger
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() with linger %d returned err %v, wantErr %v", tt.linger, err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "subagent_linger_seconds") {
				t.Errorf("Validate() error %q should name subagent_linger_seconds", err.Error())
			}
		})
	}
}

func TestGetDefaultStatusBarConfig_SubagentDefaults(t *testing.T) {
	cfg := GetDefaultStatusBarConfig()
	if cfg.SubagentLingerSeconds != 5 {
		t.Errorf("SubagentLingerSeconds = %d, want 5", cfg.SubagentLingerSeconds)
	}
	if !cfg.Indicators.Subagents {
		t.Error("Indicators.Subagents should be enabled by default")
	}
}
