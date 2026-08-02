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

// VisionAnnotatorConfig configures the image annotator. The default "local"
// engine shells out to llama.cpp's llama-mtmd-cli with an auto-downloaded GGUF
// vision model (mirroring speech_to_text / whisper.cpp); the "gateway" engine
// side-calls a provider/model through the inference gateway instead.
type VisionAnnotatorConfig struct {
	Enabled      bool   `yaml:"enabled" mapstructure:"enabled"`
	Engine       string `yaml:"engine" mapstructure:"engine"`               // "local" (llama.cpp) or "gateway"
	Model        string `yaml:"model" mapstructure:"model"`                 // local: short name (e.g. "qwen3-vl-2b"); gateway: "provider/model"
	BinaryPath   string `yaml:"binary_path" mapstructure:"binary_path"`     // local engine; "" -> resolve llama-mtmd-cli on PATH
	ModelsDir    string `yaml:"models_dir" mapstructure:"models_dir"`       // "" -> ~/.infer/models/vlm
	AutoDownload bool   `yaml:"auto_download" mapstructure:"auto_download"` // download GGUF model files on first use if missing
	MaxTokens    int    `yaml:"max_tokens" mapstructure:"max_tokens"`
	Timeout      int    `yaml:"timeout" mapstructure:"timeout"` // annotation timeout (seconds)
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

// AnnotatorEngine values.
const (
	VisionEngineLocal   = "local"
	VisionEngineGateway = "gateway"
)

// AnnotatorReady reports whether the annotator is enabled and usable.
func (v VisionConfig) AnnotatorReady() bool {
	a := v.Annotator
	if !a.Enabled || a.Model == "" {
		return false
	}
	if a.Engine == VisionEngineGateway {
		return strings.Contains(a.Model, "/")
	}
	return true
}

// Validate checks the vision section invariants so a typo fails at load.
func (v VisionConfig) Validate() error {
	if v.Annotator.Enabled {
		switch v.Annotator.Engine {
		case "", VisionEngineLocal, VisionEngineGateway:
		default:
			return fmt.Errorf("invalid vision.annotator.engine %q: must be %q or %q",
				v.Annotator.Engine, VisionEngineLocal, VisionEngineGateway)
		}
		if v.Annotator.Model == "" {
			return fmt.Errorf("vision.annotator.model must be set when the annotator is enabled")
		}
		if v.Annotator.Engine == VisionEngineGateway && !strings.Contains(v.Annotator.Model, "/") {
			return fmt.Errorf("invalid vision.annotator.model %q for the gateway engine: expected \"provider/model\"", v.Annotator.Model)
		}
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
