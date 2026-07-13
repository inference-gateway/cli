package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

const computerUseValidYAML = `---
enabled: true
floating_window:
  enabled: false
  position: bottom-left
  always_on_top: false
  respawn_on_close: false
screenshot:
  enabled: true
  max_width: 800
  max_height: 600
  target_width: 640
  target_height: 480
  format: png
  quality: 100
  streaming_enabled: false
  capture_interval: 5
  buffer_size: 2
  temp_dir: /tmp/cu
  log_captures: true
  show_overlay: false
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
	if !cfg.FloatingWindow.Enabled {
		t.Error("Expected FloatingWindow.Enabled to be true by default")
	}
	if cfg.FloatingWindow.Position != "top-right" {
		t.Errorf("Expected FloatingWindow.Position 'top-right', got %q", cfg.FloatingWindow.Position)
	}
	if cfg.Screenshot.MaxWidth != 1920 {
		t.Errorf("Expected Screenshot.MaxWidth=1920, got %d", cfg.Screenshot.MaxWidth)
	}
	if cfg.Screenshot.MaxHeight != 1080 {
		t.Errorf("Expected Screenshot.MaxHeight=1080, got %d", cfg.Screenshot.MaxHeight)
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
	if cfg.Tools.KeyboardType.MaxTextLength != 1000 {
		t.Errorf("Expected Tools.KeyboardType.MaxTextLength=1000, got %d", cfg.Tools.KeyboardType.MaxTextLength)
	}
	if cfg.Tools.KeyboardType.TypingDelayMs != 100 {
		t.Errorf("Expected Tools.KeyboardType.TypingDelayMs=100, got %d", cfg.Tools.KeyboardType.TypingDelayMs)
	}
	if !cfg.Tools.MouseMove.Enabled {
		t.Error("Expected Tools.MouseMove.Enabled true")
	}
	if !cfg.Tools.GetFocusedApp.Enabled {
		t.Error("Expected Tools.GetFocusedApp.Enabled true")
	}
}

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
				if cfg.Enabled != defaults.Enabled || cfg.Screenshot.MaxWidth != defaults.Screenshot.MaxWidth {
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
				if cfg.FloatingWindow.Position != "bottom-left" {
					t.Errorf("Expected FloatingWindow.Position 'bottom-left', got %q", cfg.FloatingWindow.Position)
				}
				if cfg.Screenshot.MaxWidth != 800 {
					t.Errorf("Expected Screenshot.MaxWidth=800, got %d", cfg.Screenshot.MaxWidth)
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
				if cfg.Tools.MouseMove.Enabled {
					t.Error("Expected Tools.MouseMove.Enabled false")
				}
				if cfg.Tools.KeyboardType.MaxTextLength != 500 {
					t.Errorf("Expected Tools.KeyboardType.MaxTextLength=500, got %d", cfg.Tools.KeyboardType.MaxTextLength)
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

func TestSaveComputerUse(t *testing.T) {
	roundTrip := &config.ComputerUseConfig{
		Enabled: true,
		FloatingWindow: config.FloatingWindowConfig{
			Enabled:        false,
			RespawnOnClose: false,
			Position:       "top-left",
			AlwaysOnTop:    false,
		},
		Screenshot: config.ScreenshotToolConfig{
			Enabled:          true,
			MaxWidth:         1024,
			MaxHeight:        768,
			TargetWidth:      512,
			TargetHeight:     384,
			Format:           "png",
			Quality:          90,
			StreamingEnabled: false,
			CaptureInterval:  10,
			BufferSize:       3,
			TempDir:          "/tmp/cu",
			LogCaptures:      true,
			ShowOverlay:      false,
		},
		RateLimit: config.RateLimitConfig{
			Enabled:             false,
			MaxActionsPerMinute: 90,
			WindowSeconds:       45,
		},
		Tools: config.ComputerUseToolsConfig{
			MouseMove:   config.MouseMoveToolConfig{Enabled: false},
			MouseClick:  config.MouseClickToolConfig{Enabled: true},
			MouseScroll: config.MouseScrollToolConfig{Enabled: false},
			KeyboardType: config.KeyboardTypeToolConfig{
				Enabled:       true,
				MaxTextLength: 250,
				TypingDelayMs: 75,
			},
			GetFocusedApp: config.GetFocusedAppToolConfig{Enabled: true},
			ActivateApp:   config.ActivateAppToolConfig{Enabled: false},
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
					loaded.FloatingWindow.Position != roundTrip.FloatingWindow.Position ||
					loaded.Screenshot.MaxWidth != roundTrip.Screenshot.MaxWidth ||
					loaded.Screenshot.Format != roundTrip.Screenshot.Format ||
					loaded.RateLimit.MaxActionsPerMinute != roundTrip.RateLimit.MaxActionsPerMinute {
					t.Errorf("Round-trip mismatch: got %+v", loaded)
				}
				if loaded.Tools.KeyboardType.MaxTextLength != roundTrip.Tools.KeyboardType.MaxTextLength ||
					loaded.Tools.KeyboardType.TypingDelayMs != roundTrip.Tools.KeyboardType.TypingDelayMs {
					t.Errorf("Tools.KeyboardType mismatch: got %+v", loaded.Tools.KeyboardType)
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
