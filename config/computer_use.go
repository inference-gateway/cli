package config

import (
	"math"

	utils "github.com/inference-gateway/cli/config/utils"
)

const (
	ComputerUseFileName    = "computer_use.yaml"
	DefaultComputerUsePath = ConfigDirName + "/" + ComputerUseFileName
)

// Computer-use approval levels control when the host UI is prompted for
// confirmation before executing a computer-use tool.
const (
	ComputerUseApprovalNever       = "never"       // computer-use actions bypass approval (default)
	ComputerUseApprovalDestructive = "destructive" // input actions (click, type, key, move, scroll) require approval; observations bypass
	ComputerUseApprovalAlways      = "always"      // every computer-use action requires approval
)

// ComputerUseConfig contains computer use tool settings
type ComputerUseConfig struct {
	Enabled    bool                 `yaml:"enabled" mapstructure:"enabled"`
	Screenshot ScreenshotToolConfig `yaml:"screenshot" mapstructure:"screenshot"`
	RateLimit  RateLimitConfig      `yaml:"rate_limit" mapstructure:"rate_limit"`
	Approval   string               `yaml:"approval" mapstructure:"approval"`
}

// ScreenshotToolConfig contains screenshot-specific tool settings
type ScreenshotToolConfig struct {
	Enabled          bool   `yaml:"enabled" mapstructure:"enabled"`
	TargetWidth      int    `yaml:"target_width" mapstructure:"target_width"`
	TargetHeight     int    `yaml:"target_height" mapstructure:"target_height"`
	Format           string `yaml:"format" mapstructure:"format"`
	Quality          int    `yaml:"quality" mapstructure:"quality"`
	StreamingEnabled bool   `yaml:"streaming_enabled" mapstructure:"streaming_enabled"`
	CaptureInterval  int    `yaml:"capture_interval" mapstructure:"capture_interval"`
	BufferSize       int    `yaml:"buffer_size" mapstructure:"buffer_size"`
	TempDir          string `yaml:"temp_dir" mapstructure:"temp_dir"`
}

// FitDims returns the frame dimensions for a screen, scaling uniformly to fit
// inside the target box. Preserving the aspect ratio keeps one scale factor
// for both axes, so VLM coordinates map back to the screen without the
// grounding drift that stretching to a fixed box introduces. When no target
// is configured (or the screen already fits) the screen dimensions are
// returned unchanged.
func (s ScreenshotToolConfig) FitDims(screenW, screenH int) (int, int) {
	if s.TargetWidth <= 0 || s.TargetHeight <= 0 || screenW <= 0 || screenH <= 0 {
		return screenW, screenH
	}
	scale := math.Min(float64(s.TargetWidth)/float64(screenW), float64(s.TargetHeight)/float64(screenH))
	if scale >= 1 {
		return screenW, screenH
	}
	return int(math.Round(float64(screenW) * scale)), int(math.Round(float64(screenH) * scale))
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
		Enabled:  false,
		Approval: ComputerUseApprovalNever,
		Screenshot: ScreenshotToolConfig{
			Enabled:          true,
			TargetWidth:      1024,
			TargetHeight:     768,
			Format:           "jpeg",
			Quality:          85,
			StreamingEnabled: true,
			CaptureInterval:  3,
			BufferSize:       60,
			TempDir:          "",
		},
		RateLimit: RateLimitConfig{
			Enabled:             true,
			MaxActionsPerMinute: 60,
			WindowSeconds:       60,
		},
	}
}

// LoadComputerUse reads computer_use.yaml from disk. When the file is
// missing it returns the in-code defaults so callers can treat absence as
// "use defaults" without special-casing. The file is decoded over the
// defaults, so tool sections added after a user's file was seeded keep
// their default enablement instead of loading as disabled. The file body is
// run through os.ExpandEnv so `${VAR}`-style references resolve from the
// environment.
func LoadComputerUse(path string) (*ComputerUseConfig, error) {
	return utils.LoadYAMLMerged(path, "computer_use", DefaultComputerUseConfig)
}

// SaveComputerUse writes the computer_use configuration to disk, creating
// any missing parent directories.
func SaveComputerUse(path string, cfg *ComputerUseConfig) error {
	return utils.SaveYAML(path, "computer_use", cfg)
}
