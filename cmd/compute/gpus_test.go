package compute

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestRunPodAPIKeyPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		authFile string
		legacy   string
		want     string
		wantWarn bool
	}{
		{name: "the environment wins", env: "rp-env", authFile: "RUNPOD_API_KEY: rp-auth\n", legacy: "rp-legacy", want: "rp-env"},
		{name: "then auth.yaml", authFile: "RUNPOD_API_KEY: rp-auth\n", legacy: "rp-legacy", want: "rp-auth"},
		{name: "then the legacy config.yaml key, with a warning", legacy: "rp-legacy", want: "rp-legacy", wantWarn: true},
		{name: "none leaves it to the prompt", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv(config.RunPodAPIKeyName, tt.env)
			if tt.authFile != "" {
				dir := filepath.Join(home, config.ConfigDirName)
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, config.AuthFileName), []byte(tt.authFile), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.Config{Provisioner: config.ProvisionerConfig{RunPod: config.ProvisionerRunPodConfig{APIKey: tt.legacy}}}
			var warn bytes.Buffer

			got, err := runPodAPIKey(cfg, &warn)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("runPodAPIKey() = %q, want %q", got, tt.want)
			}
			if warned := strings.Contains(warn.String(), "deprecated"); warned != tt.wantWarn {
				t.Fatalf("warning = %q, want a deprecation warning %v", warn.String(), tt.wantWarn)
			}
		})
	}
}
