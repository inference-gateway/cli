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

// KeybindingsConfigService manages the keybindings.yaml configuration
type KeybindingsConfigService struct {
	configPath string
}

// NewKeybindingsConfigService creates a new keybindings config service
func NewKeybindingsConfigService(configPath string) *KeybindingsConfigService {
	return &KeybindingsConfigService{
		configPath: configPath,
	}
}

// Load reads the keybindings configuration from disk. When the file is
// missing it returns the in-code defaults so callers can treat absence as
// "use defaults" without special-casing.
func (s *KeybindingsConfigService) Load() (*config.KeybindingsConfig, error) {
	if _, err := os.Stat(s.configPath); os.IsNotExist(err) {
		logger.Info("Keybindings config file does not exist, returning defaults", "path", s.configPath)
		return defaultKeybindingsConfig(), nil
	}

	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read keybindings config: %w", err)
	}

	var cfg config.KeybindingsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse keybindings config: %w", err)
	}

	return &cfg, nil
}

// Save writes the keybindings configuration to disk, creating any missing
// parent directories.
func (s *KeybindingsConfigService) Save(cfg *config.KeybindingsConfig) error {
	var buf bytes.Buffer

	buf.WriteString("---\n")

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal keybindings config: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close encoder: %w", err)
	}

	configDir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(s.configPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write keybindings config: %w", err)
	}

	logger.Info("Keybindings config saved", "path", s.configPath)
	return nil
}

func defaultKeybindingsConfig() *config.KeybindingsConfig {
	return &config.KeybindingsConfig{
		Enabled:  true,
		Bindings: config.GetDefaultKeybindings(),
	}
}

// DefaultKeybindingsConfig exposes the default keybindings config used when
// no file exists. Callers (init, reset) use it to seed a fresh file.
func DefaultKeybindingsConfig() *config.KeybindingsConfig {
	return defaultKeybindingsConfig()
}
