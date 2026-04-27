package config

import (
	utils "github.com/inference-gateway/cli/config/utils"
)

const (
	ComputerUseFileName    = "computer_use.yaml"
	DefaultComputerUsePath = ConfigDirName + "/" + ComputerUseFileName
)

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
