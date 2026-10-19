package runtime

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// The A2A server block rides the same INFER_* walkthrough as every other
// main-config block: struct-tag keys, INFER prefix, dot-to-underscore replacer.
func TestA2AServerEnvOverrides(t *testing.T) {
	t.Run("the defaults survive without env overrides", func(t *testing.T) {
		cfg := config.DefaultConfig()
		resolveViperEnvironmentVariables(newEnvViper(), cfg, "")

		want := config.A2AServerConfig{Enabled: false, Host: "127.0.0.1", Port: 8090, Token: "", Mode: "standard"}
		if cfg.A2A.Server != want {
			t.Errorf("a2a.server default = %+v, want %+v", cfg.A2A.Server, want)
		}
	})

	t.Run("INFER_A2A_SERVER_* overrides win", func(t *testing.T) {
		t.Setenv("INFER_A2A_SERVER_ENABLED", "true")
		t.Setenv("INFER_A2A_SERVER_HOST", "0.0.0.0")
		t.Setenv("INFER_A2A_SERVER_PORT", "9443")
		t.Setenv("INFER_A2A_SERVER_TOKEN", "peer-secret")
		t.Setenv("INFER_A2A_SERVER_MODE", "plan")

		cfg := config.DefaultConfig()
		resolveViperEnvironmentVariables(newEnvViper(), cfg, "")

		want := config.A2AServerConfig{Enabled: true, Host: "0.0.0.0", Port: 9443, Token: "peer-secret", Mode: "plan"}
		if cfg.A2A.Server != want {
			t.Errorf("a2a.server = %+v, want %+v", cfg.A2A.Server, want)
		}
	})
}
