package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	config "github.com/inference-gateway/cli/config"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// TestPIDRegistry verifies the last-one-out reference-counting logic:
//   - register N PIDs, deregister N-1 does NOT kill the gateway
//   - deregistering the last PID kills the gateway
//   - stale PID entries (dead processes) are pruned by signal-0 probe
func TestPIDRegistry(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Start a fake gateway process (long-running sleep) and write its PID.
	gwCmd := exec.Command("sleep", "30")
	if err := gwCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gwCmd.Process.Kill() })

	pidsDir := filepath.Join(tempHome, ".infer", "run", "pids")
	gwPIDPath := filepath.Join(tempHome, ".infer", "run", "gateway.pid")
	_ = os.MkdirAll(pidsDir, 0755)
	_ = os.WriteFile(gwPIDPath, []byte(strconv.Itoa(gwCmd.Process.Pid)), 0644)

	con1 := startSleep(t)
	con2 := startSleep(t)
	t.Cleanup(func() { _ = con1.Process.Kill() })
	t.Cleanup(func() { _ = con2.Process.Kill() })

	_ = os.WriteFile(filepath.Join(pidsDir, strconv.Itoa(con1.Process.Pid)), nil, 0644)
	_ = os.WriteFile(filepath.Join(pidsDir, strconv.Itoa(con2.Process.Pid)), nil, 0644)

	gm := &Manager{}
	_ = os.WriteFile(filepath.Join(pidsDir, strconv.Itoa(os.Getpid())), nil, 0644)

	if !gm.pruneAndCheckLive() {
		t.Fatal("expected live registrations after registering PIDs")
	}

	gm.deregisterPID()

	if !gm.pruneAndCheckLive() {
		t.Fatal("expected live registrations after deregistering self, 2 still alive")
	}

	if !utils.ProcessAlive(gwCmd.Process.Pid) {
		t.Fatal("gateway was killed while other consumers were still registered")
	}

	_ = con1.Process.Kill()
	_ = con1.Wait()

	_ = con2.Process.Kill()
	_ = con2.Wait()

	// pruneAndCheckLive prunes dead entries and reports whether any live remain.
	if gm.pruneAndCheckLive() {
		t.Fatal("expected no live registrations after all consumers exited")
	}

	if _, err := os.Stat(filepath.Join(pidsDir, strconv.Itoa(con1.Process.Pid))); !os.IsNotExist(err) {
		t.Error("stale PID file was not pruned")
	}
}

func startSleep(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

// TestNeedsAudioRestart verifies an already-running gateway is only restarted
// when the gateway TTS engine is configured and the Audio API answers 404.
func TestNeedsAudioRestart(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		engine      string
		audioStatus int
		want        bool
	}{
		{"tts disabled", false, "gateway", http.StatusNotFound, false},
		{"local engine", true, "qwen3-tts", http.StatusNotFound, false},
		{"audio missing", true, "gateway", http.StatusNotFound, true},
		{"default engine, audio missing", true, "", http.StatusNotFound, true},
		{"audio enabled (bad request)", true, "gateway", http.StatusBadRequest, false},
		{"audio warming up (503)", true, "gateway", http.StatusServiceUnavailable, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/audio/speech" {
					w.WriteHeader(tt.audioStatus)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			cfg := config.DefaultConfig()
			cfg.Gateway.URL = srv.URL
			cfg.TextToSpeech.Enabled = tt.enabled
			cfg.TextToSpeech.Engine = tt.engine
			gm := NewManager("test-session", cfg, nil)

			if got := gm.needsAudioRestart(); got != tt.want {
				t.Errorf("needsAudioRestart() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAudioAPIEnabledUnreachable pins that an unreachable gateway never
// triggers a restart - that failure belongs to the normal start flow.
func TestAudioAPIEnabledUnreachable(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gateway.URL = "http://127.0.0.1:1"
	gm := NewManager("test-session", cfg, nil)
	if !gm.audioAPIEnabled() {
		t.Error("audioAPIEnabled() = false for an unreachable gateway, want true")
	}
}

// TestImageVersion verifies the container image tag parses into a version and
// floating references (latest, digest, untagged) yield "" (unknown).
func TestImageVersion(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{"pinned tag", "ghcr.io/inference-gateway/inference-gateway:v0.50.0", "0.50.0"},
		{"plain tag", "inference-gateway:0.50.0", "0.50.0"},
		{"registry with port", "localhost:5000/gateway:1.2.3", "1.2.3"},
		{"latest", "ghcr.io/inference-gateway/inference-gateway:latest", ""},
		{"digest", "ghcr.io/inference-gateway/inference-gateway@sha256:abcdef", ""},
		{"untagged", "ghcr.io/inference-gateway/inference-gateway", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imageVersion(tt.image); got != tt.want {
				t.Errorf("imageVersion(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

// TestProbeVersionEndpoint verifies the /version probe accepts JSON and plain
// text bodies and rejects anything else (404 on older gateways, HTML, garbage).
func TestProbeVersionEndpoint(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"json", http.StatusOK, `{"version":"0.50.0"}`, "0.50.0"},
		{"json v-prefixed", http.StatusOK, `{"version":"v1.2.3"}`, "1.2.3"},
		{"plain text", http.StatusOK, "0.50.0\n", "0.50.0"},
		{"missing endpoint", http.StatusNotFound, "not found", ""},
		{"html", http.StatusOK, "<html><body>gateway</body></html>", ""},
		{"garbage", http.StatusOK, "not a version!", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			if got := probeVersionEndpoint(context.Background(), srv.URL); got != tt.want {
				t.Errorf("probeVersionEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestVersionFallbacks verifies the detection chain: the /version endpoint
// wins, managed modes fall back to the image tag or the cached binary, and
// externally managed gateways have no local fallback.
func TestVersionFallbacks(t *testing.T) {
	tests := []struct {
		name        string
		run         bool
		oci         string
		standalone  bool
		probeStatus int
		probeBody   string
		want        string
	}{
		{"external without endpoint", false, "", false, http.StatusNotFound, "", ""},
		{"external with endpoint", false, "", false, http.StatusOK, `{"version":"9.9.9"}`, "9.9.9"},
		{"container tag", true, "ghcr.io/inference-gateway/inference-gateway:v0.50.0", false, http.StatusNotFound, "", "0.50.0"},
		{"container latest", true, "ghcr.io/inference-gateway/inference-gateway:latest", false, http.StatusNotFound, "", ""},
		{"container prefers endpoint", true, "ghcr.io/inference-gateway/inference-gateway:v0.2.0", false, http.StatusOK, `{"version":"0.1.0"}`, "0.1.0"},
		{"standalone binary, none cached", true, "", true, http.StatusNotFound, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir()) // no managed binary cached

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/version" {
					w.WriteHeader(tt.probeStatus)
					_, _ = w.Write([]byte(tt.probeBody))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer srv.Close()

			cfg := config.DefaultConfig()
			cfg.Gateway.URL = srv.URL
			cfg.Gateway.Run = tt.run
			cfg.Gateway.OCI = tt.oci
			cfg.Gateway.StandaloneBinary = tt.standalone
			gm := NewManager("test-session", cfg, nil)

			if got := gm.Version(context.Background()); got != tt.want {
				t.Errorf("Version() = %q, want %q", got, tt.want)
			}
		})
	}
}
