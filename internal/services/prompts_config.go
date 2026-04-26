package services

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
	yaml "gopkg.in/yaml.v3"
)

// PromptsConfigService manages the prompts.yaml configuration where every
// LLM-facing prompt the CLI uses lives. It mirrors the keybindings and MCP
// service contracts so callers can treat all sibling YAML configs the same
// way.
type PromptsConfigService struct {
	configPath string
}

func NewPromptsConfigService(configPath string) *PromptsConfigService {
	return &PromptsConfigService{configPath: configPath}
}

// Load reads prompts.yaml from disk. When the file is missing it returns
// the in-code defaults so callers can treat absence as "use defaults"
// without special-casing.
func (s *PromptsConfigService) Load() (*config.PromptsConfig, error) {
	if _, err := os.Stat(s.configPath); os.IsNotExist(err) {
		logger.Info("Prompts config file does not exist, returning defaults", "path", s.configPath)
		return config.DefaultPromptsConfig(), nil
	}

	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read prompts config: %w", err)
	}

	var cfg config.PromptsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse prompts config: %w", err)
	}

	return &cfg, nil
}

// Save writes the prompts configuration to disk, creating any missing
// parent directories.
func (s *PromptsConfigService) Save(cfg *config.PromptsConfig) error {
	var buf bytes.Buffer

	buf.WriteString("---\n")

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal prompts config: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close encoder: %w", err)
	}

	configDir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(s.configPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write prompts config: %w", err)
	}

	logger.Info("Prompts config saved", "path", s.configPath)
	return nil
}

// DefaultPromptsConfig exposes the default prompts config used when
// no file exists. Callers (init, reset) use it to seed a fresh file.
func DefaultPromptsConfig() *config.PromptsConfig {
	return config.DefaultPromptsConfig()
}
