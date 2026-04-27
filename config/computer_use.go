package config

import (
	utils "github.com/inference-gateway/cli/config/utils"
)

const (
	ComputerUseFileName    = "computer_use.yaml"
	DefaultComputerUsePath = ConfigDirName + "/" + ComputerUseFileName
)

// ComputerUseConfig contains computer use tool settings
type ComputerUseConfig struct {
	Enabled        bool                   `yaml:"enabled" mapstructure:"enabled"`
	FloatingWindow FloatingWindowConfig   `yaml:"floating_window" mapstructure:"floating_window"`
	Screenshot     ScreenshotToolConfig   `yaml:"screenshot" mapstructure:"screenshot"`
	RateLimit      RateLimitConfig        `yaml:"rate_limit" mapstructure:"rate_limit"`
	Tools          ComputerUseToolsConfig `yaml:"tools" mapstructure:"tools"`
}

// ComputerUseToolsConfig contains individual computer use tool settings
type ComputerUseToolsConfig struct {
	MouseMove     MouseMoveToolConfig     `yaml:"mouse_move" mapstructure:"mouse_move"`
	MouseClick    MouseClickToolConfig    `yaml:"mouse_click" mapstructure:"mouse_click"`
	MouseScroll   MouseScrollToolConfig   `yaml:"mouse_scroll" mapstructure:"mouse_scroll"`
	KeyboardType  KeyboardTypeToolConfig  `yaml:"keyboard_type" mapstructure:"keyboard_type"`
	GetFocusedApp GetFocusedAppToolConfig `yaml:"get_focused_app" mapstructure:"get_focused_app"`
	ActivateApp   ActivateAppToolConfig   `yaml:"activate_app" mapstructure:"activate_app"`
}

// ScreenshotToolConfig contains screenshot-specific tool settings
type ScreenshotToolConfig struct {
	Enabled          bool   `yaml:"enabled" mapstructure:"enabled"`
	MaxWidth         int    `yaml:"max_width" mapstructure:"max_width"`
	MaxHeight        int    `yaml:"max_height" mapstructure:"max_height"`
	TargetWidth      int    `yaml:"target_width" mapstructure:"target_width"`
	TargetHeight     int    `yaml:"target_height" mapstructure:"target_height"`
	Format           string `yaml:"format" mapstructure:"format"`
	Quality          int    `yaml:"quality" mapstructure:"quality"`
	StreamingEnabled bool   `yaml:"streaming_enabled" mapstructure:"streaming_enabled"`
	CaptureInterval  int    `yaml:"capture_interval" mapstructure:"capture_interval"`
	BufferSize       int    `yaml:"buffer_size" mapstructure:"buffer_size"`
	TempDir          string `yaml:"temp_dir" mapstructure:"temp_dir"`
	LogCaptures      bool   `yaml:"log_captures" mapstructure:"log_captures"`
	ShowOverlay      bool   `yaml:"show_overlay" mapstructure:"show_overlay"`
}

// FloatingWindowConfig contains floating progress window settings
type FloatingWindowConfig struct {
	Enabled        bool   `yaml:"enabled" mapstructure:"enabled"`
	RespawnOnClose bool   `yaml:"respawn_on_close" mapstructure:"respawn_on_close"`
	Position       string `yaml:"position" mapstructure:"position"`
	AlwaysOnTop    bool   `yaml:"always_on_top" mapstructure:"always_on_top"`
}

// MouseMoveToolConfig contains mouse move-specific tool settings
type MouseMoveToolConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
}

// MouseClickToolConfig contains mouse click-specific tool settings
type MouseClickToolConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
}

// MouseScrollToolConfig contains mouse scroll-specific tool settings
type MouseScrollToolConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
}

// KeyboardTypeToolConfig contains keyboard type-specific tool settings
type KeyboardTypeToolConfig struct {
	Enabled       bool `yaml:"enabled" mapstructure:"enabled"`
	MaxTextLength int  `yaml:"max_text_length" mapstructure:"max_text_length"`
	TypingDelayMs int  `yaml:"typing_delay_ms" mapstructure:"typing_delay_ms"`
}

// GetFocusedAppToolConfig contains get focused app-specific tool settings
type GetFocusedAppToolConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
}

// ActivateAppToolConfig contains activate app-specific tool settings
type ActivateAppToolConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
}

// RateLimitConfig contains rate limiting settings
type RateLimitConfig struct {
	Enabled             bool `yaml:"enabled" mapstructure:"enabled"`
	MaxActionsPerMinute int  `yaml:"max_actions_per_minute" mapstructure:"max_actions_per_minute"`
	WindowSeconds       int  `yaml:"window_seconds" mapstructure:"window_seconds"`
}

// DefaultComputerUseConfig returns the in-code default computer_use
// configuration used when no computer_use.yaml file exists. `infer init`
// seeds the file from this and the runtime falls back to it when the file
// is absent.
func DefaultComputerUseConfig() *ComputerUseConfig {
	return &ComputerUseConfig{
		Enabled: false,
		FloatingWindow: FloatingWindowConfig{
			Enabled:        true,
			RespawnOnClose: true,
			Position:       "top-right",
			AlwaysOnTop:    true,
		},
		Screenshot: ScreenshotToolConfig{
			Enabled:          true,
			MaxWidth:         1920,
			MaxHeight:        1080,
			TargetWidth:      1024,
			TargetHeight:     768,
			Format:           "jpeg",
			Quality:          85,
			StreamingEnabled: true,
			CaptureInterval:  3,
			BufferSize:       5,
			TempDir:          "",
			LogCaptures:      false,
			ShowOverlay:      true,
		},
		RateLimit: RateLimitConfig{
			Enabled:             true,
			MaxActionsPerMinute: 60,
			WindowSeconds:       60,
		},
		Tools: ComputerUseToolsConfig{
			MouseMove:   MouseMoveToolConfig{Enabled: true},
			MouseClick:  MouseClickToolConfig{Enabled: true},
			MouseScroll: MouseScrollToolConfig{Enabled: true},
			KeyboardType: KeyboardTypeToolConfig{
				Enabled:       true,
				MaxTextLength: 1000,
				TypingDelayMs: 100,
			},
			GetFocusedApp: GetFocusedAppToolConfig{Enabled: true},
			ActivateApp:   ActivateAppToolConfig{Enabled: true},
		},
	}
}

// LoadComputerUse reads computer_use.yaml from disk. When the file is
// missing it returns the in-code defaults so callers can treat absence as
// "use defaults" without special-casing. The file body is run through
// os.ExpandEnv so `${VAR}`-style references resolve from the environment.
func LoadComputerUse(path string) (*ComputerUseConfig, error) {
	return utils.LoadYAML(path, "computer_use", DefaultComputerUseConfig)
}

// SaveComputerUse writes the computer_use configuration to disk, creating
// any missing parent directories.
func SaveComputerUse(path string, cfg *ComputerUseConfig) error {
	return utils.SaveYAML(path, "computer_use", cfg)
}
