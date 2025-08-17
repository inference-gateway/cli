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
	Debug  bool   `yaml:"debug"`
}

// ToolsConfig contains tool execution settings
type ToolsConfig struct {
	Enabled      bool                `yaml:"enabled"`
	Bash         BashToolConfig      `yaml:"bash"`
	Read         ReadToolConfig      `yaml:"read"`
	Write        WriteToolConfig     `yaml:"write"`
	Edit         EditToolConfig      `yaml:"edit"`
	Delete       DeleteToolConfig    `yaml:"delete"`
	Grep         GrepToolConfig      `yaml:"grep"`
	Tree         TreeToolConfig      `yaml:"tree"`
	Fetch        FetchToolConfig     `yaml:"fetch"`
	WebSearch    WebSearchToolConfig `yaml:"web_search"`
	TodoWrite    TodoWriteToolConfig `yaml:"todo_write"`
	Safety       SafetyConfig        `yaml:"safety"`
	ExcludePaths []string            `yaml:"exclude_paths"`
}

// BashToolConfig contains bash-specific tool settings
type BashToolConfig struct {
	Enabled         bool                `yaml:"enabled"`
	Whitelist       ToolWhitelistConfig `yaml:"whitelist"`
	RequireApproval *bool               `yaml:"require_approval,omitempty"`
}

// ReadToolConfig contains read-specific tool settings
type ReadToolConfig struct {
	Enabled         bool  `yaml:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty"`
}

// WriteToolConfig contains write-specific tool settings
type WriteToolConfig struct {
	Enabled         bool  `yaml:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty"`
}

// EditToolConfig contains edit-specific tool settings
type EditToolConfig struct {
	Enabled         bool  `yaml:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty"`
}

// DeleteToolConfig contains delete-specific tool settings
type DeleteToolConfig struct {
	Enabled           bool     `yaml:"enabled"`
	RequireApproval   *bool    `yaml:"require_approval,omitempty"`
	ProtectedPaths    []string `yaml:"protected_paths"`
	AllowWildcards    bool     `yaml:"allow_wildcards"`
	RestrictToWorkDir bool     `yaml:"restrict_to_workdir"`
}

// GrepToolConfig contains grep-specific tool settings
type GrepToolConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Backend         string `yaml:"backend"`
	RequireApproval *bool  `yaml:"require_approval,omitempty"`
}

// TreeToolConfig contains tree-specific tool settings
type TreeToolConfig struct {
	Enabled         bool  `yaml:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty"`
}

// FetchToolConfig contains fetch-specific tool settings
type FetchToolConfig struct {
	Enabled            bool              `yaml:"enabled"`
	WhitelistedDomains []string          `yaml:"whitelisted_domains"`
	GitHub             GitHubFetchConfig `yaml:"github"`
	Safety             FetchSafetyConfig `yaml:"safety"`
	Cache              FetchCacheConfig  `yaml:"cache"`
	RequireApproval    *bool             `yaml:"require_approval,omitempty"`
}

// WebSearchToolConfig contains web search-specific tool settings
type WebSearchToolConfig struct {
	Enabled         bool     `yaml:"enabled"`
	DefaultEngine   string   `yaml:"default_engine"`
	MaxResults      int      `yaml:"max_results"`
	Engines         []string `yaml:"engines"`
	Timeout         int      `yaml:"timeout"`
	RequireApproval *bool    `yaml:"require_approval,omitempty"`
}

// TodoWriteToolConfig contains TodoWrite-specific tool settings
type TodoWriteToolConfig struct {
	Enabled         bool  `yaml:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty"`
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

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:8080",
			APIKey:  "",
			Timeout: 200,
		},
		Output: OutputConfig{
			Format: "text",
			Quiet:  false,
			Debug:  false,
		},
		Tools: ToolsConfig{
			Enabled: true,
			Bash: BashToolConfig{
				Enabled: true,
				Whitelist: ToolWhitelistConfig{
					Commands: []string{
						"ls", "pwd", "echo",
						"wc", "sort", "uniq",
						"gh", "task",
					},
					Patterns: []string{
						"^git status$",
						"^git log --oneline -n [0-9]+$",
						"^docker ps$",
						"^kubectl get pods$",
					},
				},
			},
			Read: ReadToolConfig{
				Enabled:         true,
				RequireApproval: &[]bool{false}[0],
			},
			Write: WriteToolConfig{
				Enabled:         true,
				RequireApproval: &[]bool{true}[0],
			},
			Edit: EditToolConfig{
				Enabled:         true,
				RequireApproval: &[]bool{true}[0],
			},
			Delete: DeleteToolConfig{
				Enabled:           true,
				RequireApproval:   &[]bool{true}[0],
				ProtectedPaths:    []string{".infer/", ".infer/*", ".git/", ".git/*"},
				AllowWildcards:    true,
				RestrictToWorkDir: true,
			},
			Grep: GrepToolConfig{
				Enabled:         true,
				Backend:         "auto",
				RequireApproval: &[]bool{false}[0],
			},
			Tree: TreeToolConfig{
				Enabled:         true,
				RequireApproval: &[]bool{false}[0],
			},
			Fetch: FetchToolConfig{
				Enabled:            true,
				WhitelistedDomains: []string{"github.com", "golang.org"},
				GitHub: GitHubFetchConfig{
					Enabled: true,
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
			WebSearch: WebSearchToolConfig{
				Enabled:       true,
				DefaultEngine: "duckduckgo",
				MaxResults:    10,
				Engines:       []string{"duckduckgo", "google"},
				Timeout:       10,
			},
			TodoWrite: TodoWriteToolConfig{
				Enabled:         true,
				RequireApproval: &[]bool{false}[0],
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
			SystemPrompt: `
You are an assistant for software engineering tasks.

## Security

* Defensive security only. No offensive/malicious code.
* Allowed: analysis, detection rules, defensive tools, docs.

## URLs

* Never guess/generate. Use only user-provided or local.

## Style

* Concise (<4 lines).
* No pre/postamble. Answer directly.
* Prefer one-word/short answers.
* Explain bash only if non-trivial.
* No emojis unless asked.
* No code comments unless asked.

## Proactiveness

* Act only when asked. Don't surprise user.

## Code Conventions

* Follow existing style, libs, idioms.
* Never assume deps. Check imports/config.
* No secrets in code/logs.

## Tasks

* Always plan with **TodoWrite**.
* Mark todos in_progress/completed immediately.
* Don't batch completions.

## Workflow

1. Plan with TodoWrite.
2. Explore code via search.
3. Implement.
4. Verify with tests.
5. Run lint/typecheck (ask if unknown). Suggest documenting.
6. Commit only if asked.

## Tools

* Prefer Task tool for search.
* Use agents when relevant.
* Handle redirects.
* Batch tool calls for efficiency.`,
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

// IsApprovalRequired checks if approval is required for a specific tool
// It returns true if tool-specific approval is set to true, or if global approval is true and tool-specific is not set to false
func (c *Config) IsApprovalRequired(toolName string) bool {
	globalApproval := c.Tools.Safety.RequireApproval

	switch toolName {
	case "Bash":
		if c.Tools.Bash.RequireApproval != nil {
			return *c.Tools.Bash.RequireApproval
		}
	case "Read":
		if c.Tools.Read.RequireApproval != nil {
			return *c.Tools.Read.RequireApproval
		}
	case "Write":
		if c.Tools.Write.RequireApproval != nil {
			return *c.Tools.Write.RequireApproval
		}
	case "Edit":
		if c.Tools.Edit.RequireApproval != nil {
			return *c.Tools.Edit.RequireApproval
		}
	case "Delete":
		if c.Tools.Delete.RequireApproval != nil {
			return *c.Tools.Delete.RequireApproval
		}
	case "Grep":
		if c.Tools.Grep.RequireApproval != nil {
			return *c.Tools.Grep.RequireApproval
		}
	case "Tree":
		if c.Tools.Tree.RequireApproval != nil {
			return *c.Tools.Tree.RequireApproval
		}
	case "Fetch":
		if c.Tools.Fetch.RequireApproval != nil {
			return *c.Tools.Fetch.RequireApproval
		}
	case "WebSearch":
		if c.Tools.WebSearch.RequireApproval != nil {
			return *c.Tools.WebSearch.RequireApproval
		}
	case "TodoWrite":
		if c.Tools.TodoWrite.RequireApproval != nil {
			return *c.Tools.TodoWrite.RequireApproval
		}
	}

	return globalApproval
}
