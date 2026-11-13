package config_test

import (
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestGatewayPinnedRelease(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		wantTag    string
		wantPinned bool
	}{
		{"the default follows the latest release", config.DefaultConfig().Gateway.Version, "", false},
		{"an unset version follows the latest release", "", "", false},
		{"latest follows the latest release", config.GatewayVersionLatest, "", false},
		{"a version pins its release", "0.65.0", "v0.65.0", true},
		{"a tag pins its release", "v0.65.0", "v0.65.0", true},
		{"surrounding space is ignored", " 0.65.0 ", "v0.65.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag, pinned := config.GatewayConfig{Version: tt.version}.PinnedRelease()
			if tag != tt.wantTag || pinned != tt.wantPinned {
				t.Errorf("PinnedRelease() = (%q, %v), want (%q, %v)", tag, pinned, tt.wantTag, tt.wantPinned)
			}
		})
	}
}

func TestValidateGatewayVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{"latest", config.GatewayVersionLatest, false},
		{"unset", "", false},
		{"a version", "0.65.0", false},
		{"a tag", "v0.65.0", false},
		{"a pre-release", "0.66.0-rc.1", false},
		{"a branch name", "main", true},
		{"a partial version", "0.65", true},
		{"a path", "0.65.0/../evil", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Gateway.Version = tt.version

			err := cfg.Validate()

			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "gateway.version") {
				t.Errorf("Validate() error = %q, want it to name gateway.version", err)
			}
		})
	}
}
