package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/inference-gateway/cli/internal/logger"
	"gopkg.in/yaml.v3"
)

// Config represents the CLI configuration
type Config struct {
	Gateway   GatewayConfig   `yaml:"gateway"`
	Output    OutputConfig    `yaml:"output"`
	Tools     ToolsConfig     `yaml:"tools"`
	Compact   CompactConfig   `yaml:"compact"`
	Chat      ChatConfig      `yaml:"chat"`
	Fetch     FetchConfig     `yaml:"fetch"`
	WebSearch WebSearchConfig `yaml:"web_search"`
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
	Enabled      bool                `yaml:"enabled"`
	Whitelist    ToolWhitelistConfig `yaml:"whitelist"`
	Safety       SafetyConfig        `yaml:"safety"`
	ExcludePaths []string            `yaml:"exclude_paths"`
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
	SystemPrompt string `yaml:"system_prompt"`
}

// FetchConfig contains settings for content fetching
type FetchConfig struct {
	Enabled            bool              `yaml:"enabled"`
	WhitelistedDomains []string          `yaml:"whitelisted_domains"`
	GitHub             GitHubFetchConfig `yaml:"github"`
	Safety             FetchSafetyConfig `yaml:"safety"`
	Cache              FetchCacheConfig  `yaml:"cache"`
}

// GitHubFetchConfig contains GitHub-specific fetch settings
type GitHubFetchConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
	BaseURL string `yaml:"base_url"`
}

// FetchSafetyConfig contains safety settings for fetch operations
type FetchSafetyConfig struct {
	MaxSize       int64 `yaml:"max_size"`
	Timeout       int   `yaml:"timeout"`
	AllowRedirect bool  `yaml:"allow_redirect"`
}

// FetchCacheConfig contains cache settings for fetch operations
type FetchCacheConfig struct {
	Enabled bool  `yaml:"enabled"`
	TTL     int   `yaml:"ttl"`
	MaxSize int64 `yaml:"max_size"`
}

// WebSearchConfig contains settings for web search functionality
type WebSearchConfig struct {
	Enabled       bool     `yaml:"enabled"`
	DefaultEngine string   `yaml:"default_engine"`
	MaxResults    int      `yaml:"max_results"`
	Engines       []string `yaml:"engines"`
	Timeout       int      `yaml:"timeout"`
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
			Enabled: true,
			Whitelist: ToolWhitelistConfig{
				Commands: []string{
					"ls", "pwd", "echo",
					"grep", "wc", "sort", "uniq",
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
			ExcludePaths: []string{
				".infer/",
				".infer/*",
			},
		},
		Compact: CompactConfig{
			OutputDir: ".infer",
		},
		Chat: ChatConfig{
			DefaultModel: "",
			SystemPrompt: "",
		},
		Fetch: FetchConfig{
			Enabled:            false,
			WhitelistedDomains: []string{"github.com"},
			GitHub: GitHubFetchConfig{
				Enabled: false,
				Token:   "",
				BaseURL: "https://api.github.com",
			},
			Safety: FetchSafetyConfig{
				MaxSize:       8192, // 8KB
				Timeout:       30,   // 30 seconds
				AllowRedirect: true,
			},
			Cache: FetchCacheConfig{
				Enabled: true,
				TTL:     3600,     // 1 hour
				MaxSize: 52428800, // 50MB
			},
		},
		WebSearch: WebSearchConfig{
			Enabled:       true,
			DefaultEngine: "duckduckgo",
			MaxResults:    10,
			Engines:       []string{"duckduckgo", "google"},
			Timeout:       10,
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

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	defer func() {
		if err := encoder.Close(); err != nil {
			logger.Error("Failed to close YAML encoder", "error", err)
		}
	}()

	if err := encoder.Encode(c); err != nil {
		logger.Error("Failed to marshal config", "error", err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	data := buf.Bytes()

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
