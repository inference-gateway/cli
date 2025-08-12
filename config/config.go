package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/inference-gateway/cli/internal/logger"
	"gopkg.in/yaml.v3"
)

// Config represents the CLI configuration
type Config struct {
	Gateway GatewayConfig `yaml:"gateway"`
	Output  OutputConfig  `yaml:"output"`
	Tools   ToolsConfig   `yaml:"tools"`
	Compact CompactConfig `yaml:"compact"`
	Chat    ChatConfig    `yaml:"chat"`
}

// GatewayConfig contains gateway connection settings
type GatewayConfig struct {
	URL     string `yaml:"url"`
	APIKey  string `yaml:"api_key"`
	Timeout int    `yaml:"timeout"`
}

// OutputConfig contains output formatting settings
type OutputConfig struct {
	Format string `yaml:"format"`
	Quiet  bool   `yaml:"quiet"`
}

// ToolsConfig contains tool execution settings
type ToolsConfig struct {
	Enabled   bool                `yaml:"enabled"`
	Whitelist ToolWhitelistConfig `yaml:"whitelist"`
	Safety    SafetyConfig        `yaml:"safety"`
}

// ToolWhitelistConfig contains whitelisted commands and patterns
type ToolWhitelistConfig struct {
	Commands []string `yaml:"commands"`
	Patterns []string `yaml:"patterns"`
}

// SafetyConfig contains safety approval settings
type SafetyConfig struct {
	RequireApproval bool `yaml:"require_approval"`
}

// CompactConfig contains settings for compact command
type CompactConfig struct {
	OutputDir string `yaml:"output_dir"`
}

// ChatConfig contains chat-related settings
type ChatConfig struct {
	DefaultModel string `yaml:"default_model"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:8080",
			APIKey:  "",
			Timeout: 30,
		},
		Output: OutputConfig{
			Format: "text",
			Quiet:  false,
		},
		Tools: ToolsConfig{
			Enabled: false,
			Whitelist: ToolWhitelistConfig{
				Commands: []string{
					"ls", "pwd", "echo", "cat", "head", "tail",
					"grep", "find", "wc", "sort", "uniq",
				},
				Patterns: []string{
					"^git status$",
					"^git log --oneline -n [0-9]+$",
					"^docker ps$",
					"^kubectl get pods$",
				},
			},
			Safety: SafetyConfig{
				RequireApproval: true,
			},
		},
		Compact: CompactConfig{
			OutputDir: ".infer",
		},
		Chat: ChatConfig{
			DefaultModel: "",
		},
	}
}

// LoadConfig loads configuration from file
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = getDefaultConfigPath()
		logger.Debug("Using default config path", "path", configPath)
	} else {
		logger.Debug("Using custom config path", "path", configPath)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Debug("Config file not found, using default configuration", "path", configPath)
		return DefaultConfig(), nil
	}

	logger.Debug("Loading config file", "path", configPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		logger.Error("Failed to read config file", "path", configPath, "error", err)
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		logger.Error("Failed to parse config file", "path", configPath, "error", err)
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	logger.Debug("Successfully loaded config", "path", configPath, "gateway_url", config.Gateway.URL)
	return &config, nil
}

// SaveConfig saves configuration to file
func (c *Config) SaveConfig(configPath string) error {
	if configPath == "" {
		configPath = getDefaultConfigPath()
		logger.Debug("Using default config path for save", "path", configPath)
	} else {
		logger.Debug("Using custom config path for save", "path", configPath)
	}

	dir := filepath.Dir(configPath)
	logger.Debug("Creating config directory", "dir", dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Error("Failed to create config directory", "dir", dir, "error", err)
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		logger.Error("Failed to marshal config", "error", err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	logger.Debug("Writing config file", "path", configPath, "size", len(data))
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		logger.Error("Failed to write config file", "path", configPath, "error", err)
		return fmt.Errorf("failed to write config file: %w", err)
	}

	logger.Debug("Successfully saved config", "path", configPath)
	return nil
}

func getDefaultConfigPath() string {
	wd, err := os.Getwd()
	if err != nil {
		return ".infer/config.yaml"
	}
	return filepath.Join(wd, ".infer/config.yaml")
}
