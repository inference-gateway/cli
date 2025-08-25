package services

import (
	"fmt"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/utils"
	"github.com/spf13/viper"
)

// ConfigService handles configuration management and reloading
type ConfigService struct {
	viper  *viper.Viper
	config *config.Config
}

// NewConfigService creates a new config service
func NewConfigService(v *viper.Viper, cfg *config.Config) *ConfigService {
	return &ConfigService{
		viper:  v,
		config: cfg,
	}
}

// Reload reloads configuration from disk
func (cs *ConfigService) Reload() (*config.Config, error) {
	if err := cs.viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to re-read config file: %w", err)
	}

	newConfig := &config.Config{}
	if err := cs.viper.Unmarshal(newConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal reloaded config: %w", err)
	}

	cs.config = newConfig

	return newConfig, nil
}

// GetConfig returns the current config
func (cs *ConfigService) GetConfig() *config.Config {
	return cs.config
}

// SetValue sets a configuration value using dot notation and saves it to disk
func (cs *ConfigService) SetValue(key, value string) error {
	cs.viper.Set(key, value)

	if err := utils.WriteViperConfigWithIndent(cs.viper, 2); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	newConfig, err := cs.Reload()
	if err != nil {
		return fmt.Errorf("failed to reload config after setting: %w", err)
	}

	cs.config = newConfig

	return nil
}
