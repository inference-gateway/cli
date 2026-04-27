package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

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

func TestLoadComputerUse_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "non-existent.yaml")

	cfg, err := config.LoadComputerUse(path)
	if err != nil {
		t.Fatalf("LoadComputerUse() should not error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadComputerUse() returned nil")
	}
	defaults := config.DefaultComputerUseConfig()
	if cfg.Enabled != defaults.Enabled || cfg.Screenshot.MaxWidth != defaults.Screenshot.MaxWidth {
		t.Errorf("Expected defaults, got %+v", cfg)
	}
}

func TestLoadComputerUse_ValidYAML(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "computer_use.yaml")

	yamlContent := `---
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
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	cfg, err := config.LoadComputerUse(path)
	if err != nil {
		t.Fatalf("LoadComputerUse() failed: %v", err)
	}
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
}

func TestLoadComputerUse_EnvironmentVariableExpansion(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "computer_use.yaml")

	t.Setenv("TEST_CU_TEMP_DIR", "/var/tmp/expanded")

	yamlContent := `---
enabled: true
screenshot:
  temp_dir: "${TEST_CU_TEMP_DIR}"
`
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	cfg, err := config.LoadComputerUse(path)
	if err != nil {
		t.Fatalf("LoadComputerUse() failed: %v", err)
	}
	if cfg.Screenshot.TempDir != "/var/tmp/expanded" {
		t.Errorf("Expected expanded temp_dir '/var/tmp/expanded', got %q", cfg.Screenshot.TempDir)
	}
}

func TestLoadComputerUse_InvalidYAML(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "computer_use.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: ["), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	if _, err := config.LoadComputerUse(path); err == nil {
		t.Fatal("Expected error from invalid YAML, got nil")
	}
}

func TestSaveComputerUse_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "subdir", "computer_use.yaml")

	cfg := &config.ComputerUseConfig{
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

	if err := config.SaveComputerUse(path, cfg); err != nil {
		t.Fatalf("SaveComputerUse() failed: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("File was not created")
	}

	loaded, err := config.LoadComputerUse(path)
	if err != nil {
		t.Fatalf("LoadComputerUse() failed: %v", err)
	}
	if loaded.Enabled != cfg.Enabled ||
		loaded.FloatingWindow.Position != cfg.FloatingWindow.Position ||
		loaded.Screenshot.MaxWidth != cfg.Screenshot.MaxWidth ||
		loaded.Screenshot.Format != cfg.Screenshot.Format ||
		loaded.RateLimit.MaxActionsPerMinute != cfg.RateLimit.MaxActionsPerMinute {
		t.Errorf("Round-trip mismatch: got %+v", loaded)
	}
	if loaded.Tools.KeyboardType.MaxTextLength != cfg.Tools.KeyboardType.MaxTextLength ||
		loaded.Tools.KeyboardType.TypingDelayMs != cfg.Tools.KeyboardType.TypingDelayMs {
		t.Errorf("Tools.KeyboardType mismatch: got %+v", loaded.Tools.KeyboardType)
	}
}

func TestSaveComputerUse_CreatesParentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "deeply", "nested", "computer_use.yaml")
	if err := config.SaveComputerUse(path, config.DefaultComputerUseConfig()); err != nil {
		t.Fatalf("SaveComputerUse() failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("File not created at nested path: %v", err)
	}
}
