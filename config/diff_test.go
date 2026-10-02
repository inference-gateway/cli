package config

import (
	"slices"
	"testing"
)

func TestChangedKeys(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
		want   []string
	}{
		{"no change", func(*Config) {}, nil},
		{"config.yaml leaf", func(c *Config) { c.Gateway.Timeout = 300 }, []string{"gateway.timeout"}},
		{"sidecar leaf", func(c *Config) { c.Tools.Safety.ApprovalBehaviour = ApprovalBehaviourBlock }, []string{"tools.safety.approval_behaviour"}},
		{"nested sidecar", func(c *Config) { c.ComputerUse.Enabled = !c.ComputerUse.Enabled }, []string{"computer_use.enabled"}},
		{"slice", func(c *Config) { c.Gateway.IncludeModels = []string{"openai/gpt-5"} }, []string{"gateway.include_models"}},
		{"unexported field", func(c *Config) { c.SetConfigDir("/elsewhere") }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after := DefaultConfig(), DefaultConfig()
			tt.change(after)
			if got := ChangedKeys(before, after); !slices.Equal(got, tt.want) {
				t.Fatalf("ChangedKeys() = %v, want %v", got, tt.want)
			}
		})
	}
}
