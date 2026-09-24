package runtime

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// Sidecar env values keep the old hand-rolled semantics: whitespace is trimmed
// and unparseable bools/ints are ignored instead of cast to false/0.
func TestApplySidecarEnv_StrictScalars(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantApproval bool
		wantWorkers  int
	}{
		{"padded values parse", map[string]string{"INFER_CHANNELS_REQUIRE_APPROVAL": " false ", "INFER_CHANNELS_MAX_WORKERS": " 9 "}, false, 9},
		{"garbage is ignored", map[string]string{"INFER_CHANNELS_REQUIRE_APPROVAL": "maybe", "INFER_CHANNELS_MAX_WORKERS": "30s"}, true, 4},
		{"empty is ignored", map[string]string{"INFER_CHANNELS_REQUIRE_APPROVAL": "", "INFER_CHANNELS_MAX_WORKERS": ""}, true, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfg := &config.Config{Channels: config.ChannelsConfig{RequireApproval: true, MaxWorkers: 4}}
			applySidecarEnv(&cfg.Channels, "channels")
			if cfg.Channels.RequireApproval != tt.wantApproval {
				t.Errorf("RequireApproval = %v, want %v", cfg.Channels.RequireApproval, tt.wantApproval)
			}
			if cfg.Channels.MaxWorkers != tt.wantWorkers {
				t.Errorf("MaxWorkers = %d, want %d", cfg.Channels.MaxWorkers, tt.wantWorkers)
			}
		})
	}
}

// TestInitConfigMCPEnvOverrides pins issue #1295: INFER_MCP_* env vars must
// override the mcp.yaml sidecar (or its defaults when the file is missing)
// instead of being dropped when the sidecar load replaces cfg.MCP wholesale.
func TestInitConfigMCPEnvOverrides(t *testing.T) {
	splitHomeProjectEnv(t)
	t.Setenv("INFER_MCP_ENABLED", "true")
	t.Setenv("INFER_MCP_CONNECTION_TIMEOUT", "45")
	t.Setenv("INFER_MCP_DISCOVERY_TIMEOUT", "60")

	initConfig()

	if !Cfg.MCP.Enabled {
		t.Error("INFER_MCP_ENABLED=true must enable MCP even when mcp.yaml is missing")
	}
	if Cfg.MCP.ConnectionTimeout != 45 {
		t.Errorf("ConnectionTimeout = %d, want 45", Cfg.MCP.ConnectionTimeout)
	}
	if Cfg.MCP.DiscoveryTimeout != 60 {
		t.Errorf("DiscoveryTimeout = %d, want 60", Cfg.MCP.DiscoveryTimeout)
	}
}
