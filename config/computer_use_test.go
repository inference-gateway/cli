package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// floating_window was removed in favor of the desktop app; it stays in the
// fixture to prove unknown/removed keys don't break config load.
const computerUseValidYAML = `---
enabled: true
floating_window:
  enabled: false
  position: bottom-left
  always_on_top: false
  respawn_on_close: false
screenshot:
  enabled: true
  target_width: 640
  target_height: 480
  format: png
  quality: 100
  streaming_enabled: false
  capture_interval: 5
  buffer_size: 2
  temp_dir: /tmp/cu
rate_limit:
  enabled: false
  max_actions_per_minute: 30
  window_seconds: 30
tools:
  mouse_move:
    enabled: false
  mouse_click:
    enabled: false
  mouse_scroll:
    enabled: false
  keyboard_type:
    enabled: true
    max_text_length: 500
    typing_delay_ms: 50
  get_focused_app:
    enabled: false
  activate_app:
    enabled: false
`

func TestComputerUseConstants(t *testing.T) {
	if config.ComputerUseFileName != "computer_use.yaml" {
		t.Errorf("Expected ComputerUseFileName 'computer_use.yaml', got %q", config.ComputerUseFileName)
	}
	expectedPath := config.ConfigDirName + "/" + config.ComputerUseFileName
	if config.DefaultComputerUsePath != expectedPath {
		t.Errorf("Expected DefaultComputerUsePath %q, got %q", expectedPath, config.DefaultComputerUsePath)
	}
}

func TestDefaultComputerUseConfig(t *testing.T) {
	cfg := config.DefaultComputerUseConfig()
	if cfg == nil {
		t.Fatal("DefaultComputerUseConfig() returned nil")
	}
	if cfg.Enabled {
		t.Error("Expected Enabled to be false by default")
	}
	if cfg.Screenshot.TargetWidth != 1024 {
		t.Errorf("Expected Screenshot.TargetWidth=1024, got %d", cfg.Screenshot.TargetWidth)
	}
	if cfg.Screenshot.TargetHeight != 768 {
		t.Errorf("Expected Screenshot.TargetHeight=768, got %d", cfg.Screenshot.TargetHeight)
	}
	if cfg.Screenshot.Format != "jpeg" {
		t.Errorf("Expected Screenshot.Format 'jpeg', got %q", cfg.Screenshot.Format)
	}
	if cfg.Screenshot.Quality != 85 {
		t.Errorf("Expected Screenshot.Quality=85, got %d", cfg.Screenshot.Quality)
	}
	if !cfg.Screenshot.StreamingEnabled {
		t.Error("Expected Screenshot.StreamingEnabled true")
	}
	if cfg.RateLimit.MaxActionsPerMinute != 60 {
		t.Errorf("Expected RateLimit.MaxActionsPerMinute=60, got %d", cfg.RateLimit.MaxActionsPerMinute)
	}
	if cfg.RateLimit.WindowSeconds != 60 {
		t.Errorf("Expected RateLimit.WindowSeconds=60, got %d", cfg.RateLimit.WindowSeconds)
	}
}

//nolint:gocognit // table-driven with per-case check closures
func TestLoadComputerUse(t *testing.T) {
	defaults := config.DefaultComputerUseConfig()

	tests := []struct {
		name    string
		yaml    string
		env     map[string]string
		wantErr bool
		check   func(t *testing.T, cfg *config.ComputerUseConfig)
	}{
		{
			name: "non-existent file returns defaults",
			check: func(t *testing.T, cfg *config.ComputerUseConfig) {
				if cfg.Enabled != defaults.Enabled || cfg.Screenshot.TargetWidth != defaults.Screenshot.TargetWidth {
					t.Errorf("Expected defaults, got %+v", cfg)
				}
			},
		},
		{
			name: "valid yaml",
			yaml: computerUseValidYAML,
			check: func(t *testing.T, cfg *config.ComputerUseConfig) {
				if !cfg.Enabled {
					t.Error("Expected Enabled true")
				}
				if cfg.Screenshot.TargetWidth != 640 {
					t.Errorf("Expected Screenshot.TargetWidth=640, got %d", cfg.Screenshot.TargetWidth)
				}
				if cfg.Screenshot.Format != "png" {
					t.Errorf("Expected Screenshot.Format 'png', got %q", cfg.Screenshot.Format)
				}
				if cfg.RateLimit.Enabled {
					t.Error("Expected RateLimit.Enabled false")
				}
				if cfg.RateLimit.MaxActionsPerMinute != 30 {
					t.Errorf("Expected RateLimit.MaxActionsPerMinute=30, got %d", cfg.RateLimit.MaxActionsPerMinute)
				}
			},
		},
		{
			name: "environment variable expansion",
			env:  map[string]string{"TEST_CU_TEMP_DIR": "/var/tmp/expanded"},
			yaml: `---
enabled: true
screenshot:
  temp_dir: "${TEST_CU_TEMP_DIR}"
`,
			check: func(t *testing.T, cfg *config.ComputerUseConfig) {
				if cfg.Screenshot.TempDir != "/var/tmp/expanded" {
					t.Errorf("Expected expanded temp_dir '/var/tmp/expanded', got %q", cfg.Screenshot.TempDir)
				}
			},
		},
		{
			name:    "invalid yaml returns error",
			yaml:    "not: valid: yaml: [",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "computer_use.yaml")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.yaml != "" {
				if err := os.WriteFile(path, []byte(tt.yaml), 0644); err != nil {
					t.Fatalf("Failed to write yaml: %v", err)
				}
			}

			cfg, err := config.LoadComputerUse(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error from invalid YAML, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadComputerUse() failed: %v", err)
			}
			if cfg == nil {
				t.Fatal("LoadComputerUse() returned nil")
			}
			tt.check(t, cfg)
		})
	}
}

func TestFitDims(t *testing.T) {
	tests := []struct {
		name             string
		targetW, targetH int
		screenW, screenH int
		wantW, wantH     int
	}{
		{"16:10 retina is width-limited", 1024, 768, 1512, 982, 1024, 665},
		{"4:3 screen fills the box", 1024, 768, 2048, 1536, 1024, 768},
		{"tall screen is height-limited", 1024, 768, 900, 1600, 432, 768},
		{"small screen is untouched", 1024, 768, 800, 600, 800, 600},
		{"no target returns screen dims", 0, 0, 1512, 982, 1512, 982},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ScreenshotToolConfig{TargetWidth: tt.targetW, TargetHeight: tt.targetH}
			w, h := cfg.FitDims(tt.screenW, tt.screenH)
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("FitDims(%d,%d) = (%d,%d), want (%d,%d)", tt.screenW, tt.screenH, w, h, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestSaveComputerUse(t *testing.T) {
	roundTrip := &config.ComputerUseConfig{
		Enabled: true,
		Screenshot: config.ScreenshotToolConfig{
			Enabled:          true,
			TargetWidth:      512,
			TargetHeight:     384,
			Format:           "png",
			Quality:          90,
			StreamingEnabled: false,
			CaptureInterval:  10,
			BufferSize:       3,
			TempDir:          "/tmp/cu",
		},
		RateLimit: config.RateLimitConfig{
			Enabled:             false,
			MaxActionsPerMinute: 90,
			WindowSeconds:       45,
		},
	}

	tests := []struct {
		name  string
		path  []string
		cfg   *config.ComputerUseConfig
		check func(t *testing.T, path string)
	}{
		{
			name: "round trip preserves fields",
			path: []string{"subdir", "computer_use.yaml"},
			cfg:  roundTrip,
			check: func(t *testing.T, path string) {
				loaded, err := config.LoadComputerUse(path)
				if err != nil {
					t.Fatalf("LoadComputerUse() failed: %v", err)
				}
				if loaded.Enabled != roundTrip.Enabled ||
					loaded.Screenshot.TargetWidth != roundTrip.Screenshot.TargetWidth ||
					loaded.Screenshot.Format != roundTrip.Screenshot.Format ||
					loaded.RateLimit.MaxActionsPerMinute != roundTrip.RateLimit.MaxActionsPerMinute {
					t.Errorf("Round-trip mismatch: got %+v", loaded)
				}
			},
		},
		{
			name: "creates parent directory",
			path: []string{"deeply", "nested", "computer_use.yaml"},
			cfg:  config.DefaultComputerUseConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(append([]string{t.TempDir()}, tt.path...)...)
			if err := config.SaveComputerUse(path, tt.cfg); err != nil {
				t.Fatalf("SaveComputerUse() failed: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("File not created at %q: %v", path, err)
			}
			if tt.check != nil {
				tt.check(t, path)
			}
		})
	}
}
