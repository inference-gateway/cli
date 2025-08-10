package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the CLI configuration
type Config struct {
	Gateway GatewayConfig `yaml:"gateway"`
	Output  OutputConfig  `yaml:"output"`
	Tools   ToolsConfig   `yaml:"tools"`
	Compact CompactConfig `yaml:"compact"`
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
	}
}

// LoadConfig loads configuration from file
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = getDefaultConfigPath()
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// SaveConfig saves configuration to file
func (c *Config) SaveConfig(configPath string) error {
	if configPath == "" {
		configPath = getDefaultConfigPath()
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func getDefaultConfigPath() string {
	wd, err := os.Getwd()
	if err != nil {
		return ".infer.yaml"
	}
	return filepath.Join(wd, ".infer.yaml")
}
