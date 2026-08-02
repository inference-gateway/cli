package config

import (
	"fmt"
	"strings"
	"time"
)

// VisionConfig contains the vision annotation pipeline settings: a pluggable
// image annotator (image -> scene summary + numbered element list) and named
// frame sources the GetLatestFrame tool can read from. It is unrelated to
// gateway.vision_enabled, which only toggles vision support on the gateway
// container itself.
type VisionConfig struct {
	Annotator VisionAnnotatorConfig         `yaml:"annotator" mapstructure:"annotator"`
	Sources   map[string]VisionSourceConfig `yaml:"sources" mapstructure:"sources"` // named frame sources ("screen" is implicit)
}

// VisionAnnotatorConfig configures the image annotator: a side-call to a
// vision model through the inference gateway. The gateway also serves local
// models (e.g. Ollama), so offline annotation is just a gateway provider.
type VisionAnnotatorConfig struct {
	Enabled   bool   `yaml:"enabled" mapstructure:"enabled"`
	Model     string `yaml:"model" mapstructure:"model"` // "provider/model" vision model reference
	MaxTokens int    `yaml:"max_tokens" mapstructure:"max_tokens"`
	Timeout   int    `yaml:"timeout" mapstructure:"timeout"` // annotation timeout (seconds)
}

// VisionSourceConfig configures a named frame source.
type VisionSourceConfig struct {
	Type      string                `yaml:"type" mapstructure:"type"` // "directory" (the "screen" source is registered implicitly)
	Path      string                `yaml:"path" mapstructure:"path"`
	Prompt    string                `yaml:"prompt" mapstructure:"prompt"` // optional per-source annotation instruction override
	Retention VisionRetentionConfig `yaml:"retention" mapstructure:"retention"`
}

// VisionRetentionConfig is an optional sweep of a directory source; omit it if
// the producing daemon manages cleanup itself.
type VisionRetentionConfig struct {
	MaxFiles int    `yaml:"max_files" mapstructure:"max_files"` // keep newest N files (0 = unlimited)
	MaxAge   string `yaml:"max_age" mapstructure:"max_age"`     // e.g. "24h" (time.ParseDuration; "" = unlimited)
}

// AnnotatorReady reports whether the annotator is enabled and usable.
func (v VisionConfig) AnnotatorReady() bool {
	return v.Annotator.Enabled && v.Annotator.Model != ""
}

// Validate checks the vision section invariants so a typo fails at load.
func (v VisionConfig) Validate() error {
	if v.Annotator.Enabled && !strings.Contains(v.Annotator.Model, "/") {
		return fmt.Errorf("invalid vision.annotator.model %q: expected a \"provider/model\" vision model reference (e.g. \"ollama/qwen3-vl:2b\")", v.Annotator.Model)
	}
	for name, src := range v.Sources {
		if src.Type != "directory" {
			return fmt.Errorf("invalid vision.sources.%s.type %q: only \"directory\" is supported", name, src.Type)
		}
		if strings.TrimSpace(src.Path) == "" {
			return fmt.Errorf("vision.sources.%s.path must be set", name)
		}
		if src.Retention.MaxFiles < 0 {
			return fmt.Errorf("invalid vision.sources.%s.retention.max_files %d: must be >= 0", name, src.Retention.MaxFiles)
		}
		if src.Retention.MaxAge != "" {
			if _, err := time.ParseDuration(src.Retention.MaxAge); err != nil {
				return fmt.Errorf("invalid vision.sources.%s.retention.max_age %q: %w", name, src.Retention.MaxAge, err)
			}
		}
	}
	return nil
}
