package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/inference-gateway/cli/internal/logger"
	"gopkg.in/yaml.v3"
)

// Config represents the CLI configuration
type Config struct {
	Gateway GatewayConfig `yaml:"gateway"`
	Logging LoggingConfig `yaml:"logging"`
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

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Debug bool `yaml:"debug"`
}

// ToolsConfig contains tool execution settings
type ToolsConfig struct {
	Enabled   bool                `yaml:"enabled"`
	Sandbox   SandboxConfig       `yaml:"sandbox"`
	Bash      BashToolConfig      `yaml:"bash"`
	Read      ReadToolConfig      `yaml:"read"`
	Write     WriteToolConfig     `yaml:"write"`
	Edit      EditToolConfig      `yaml:"edit"`
	Delete    DeleteToolConfig    `yaml:"delete"`
	Grep      GrepToolConfig      `yaml:"grep"`
	Tree      TreeToolConfig      `yaml:"tree"`
	WebFetch  WebFetchToolConfig  `yaml:"web_fetch"`
	WebSearch WebSearchToolConfig `yaml:"web_search"`
	Github    GithubToolConfig    `yaml:"github"`
	TodoWrite TodoWriteToolConfig `yaml:"todo_write"`
	Safety    SafetyConfig        `yaml:"safety"`
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
	Enabled         bool  `yaml:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty"`
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

// WebFetchToolConfig contains fetch-specific tool settings
type WebFetchToolConfig struct {
	Enabled            bool              `yaml:"enabled"`
	WhitelistedDomains []string          `yaml:"whitelisted_domains"`
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

// GithubToolConfig contains GitHub fetch-specific tool settings
type GithubToolConfig struct {
	Enabled         bool               `yaml:"enabled"`
	Token           string             `yaml:"token"`
	BaseURL         string             `yaml:"base_url"`
	Owner           string             `yaml:"owner"`
	Repo            string             `yaml:"repo,omitempty"`
	Safety          GithubSafetyConfig `yaml:"safety"`
	RequireApproval *bool              `yaml:"require_approval,omitempty"`
}

// GithubSafetyConfig contains safety settings for GitHub fetch operations
type GithubSafetyConfig struct {
	MaxSize int64 `yaml:"max_size"`
	Timeout int   `yaml:"timeout"`
}

// ToolWhitelistConfig contains whitelisted commands and patterns
type ToolWhitelistConfig struct {
	Commands []string `yaml:"commands"`
	Patterns []string `yaml:"patterns"`
}

// SandboxConfig contains sandbox directory settings
type SandboxConfig struct {
	Directories    []string `yaml:"directories"`
	ProtectedPaths []string `yaml:"protected_paths"`
}

// SafetyConfig contains safety approval settings
type SafetyConfig struct {
	RequireApproval bool `yaml:"require_approval"`
}

// CompactConfig contains settings for compact command
type CompactConfig struct {
	OutputDir    string `yaml:"output_dir"`
	SummaryModel string `yaml:"summary_model"`
}

// OptimizationConfig contains token optimization settings
type OptimizationConfig struct {
	Enabled                    bool `yaml:"enabled"`
	MaxHistory                 int  `yaml:"max_history"`
	CompactThreshold           int  `yaml:"compact_threshold"`
	TruncateLargeOutputs       bool `yaml:"truncate_large_outputs"`
	SkipRedundantConfirmations bool `yaml:"skip_redundant_confirmations"`
}

// ChatConfig contains chat-related settings
type ChatConfig struct {
	DefaultModel string             `yaml:"default_model"`
	SystemPrompt string             `yaml:"system_prompt"`
	Optimization OptimizationConfig `yaml:"optimization"`
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
func DefaultConfig() *Config { //nolint:funlen
	return &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:8080",
			APIKey:  "",
			Timeout: 200,
		},
		Logging: LoggingConfig{
			Debug: false,
		},
		Tools: ToolsConfig{
			Enabled: true,
			Sandbox: SandboxConfig{
				Directories: []string{".", "/tmp"},
				ProtectedPaths: []string{
					".infer/",
					".git/",
					"*.env",
				},
			},
			Bash: BashToolConfig{
				Enabled: true,
				Whitelist: ToolWhitelistConfig{
					Commands: []string{
						"ls", "pwd", "echo",
						"wc", "sort", "uniq",
						"task",
					},
					Patterns: []string{
						"^git branch( --show-current)?$",
						"^git checkout -b [a-zA-Z0-9/_-]+( [a-zA-Z0-9/_-]+)?$",
						"^git checkout [a-zA-Z0-9/_-]+",
						"^git add [a-zA-Z0-9/_.-]+",
						"^git diff+",
						"^git remote -v$",
						"^git status$",
						"^git log --oneline -n [0-9]+$",
						"^git commit -m \".+\"$",
						"^git push( --set-upstream)?( origin)? (feature|fix|bugfix|hotfix|chore|docs|test|refactor|build|ci|perf|style)/[a-zA-Z0-9/_.-]+$",
						"^git push( --set-upstream)?( origin)? develop$",
						"^git push( --set-upstream)?( origin)? staging$",
						"^git push( --set-upstream)?( origin)? release/[a-zA-Z0-9._-]+$",
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
				Enabled:         true,
				RequireApproval: &[]bool{true}[0],
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
			WebFetch: WebFetchToolConfig{
				Enabled:            true,
				WhitelistedDomains: []string{"golang.org"},
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
			Github: GithubToolConfig{
				Enabled: true,
				Token:   "%GITHUB_TOKEN%",
				BaseURL: "https://api.github.com",
				Safety: GithubSafetyConfig{
					MaxSize: 1048576, // 1MB
					Timeout: 30,      // 30 seconds
				},
				Owner: "",
				Repo:  "",
			},
			TodoWrite: TodoWriteToolConfig{
				Enabled:         true,
				RequireApproval: &[]bool{false}[0],
			},
			Safety: SafetyConfig{
				RequireApproval: true,
			},
		},
		Compact: CompactConfig{
			OutputDir:    ".infer",
			SummaryModel: "",
		},
		Chat: ChatConfig{
			DefaultModel: "",
			SystemPrompt: `Software engineering assistant. Concise (<4 lines), direct answers only.

IMPORTANT: You NEVER push to main or master or to the current branch - instead you create a branch and push to a branch.
IMPORTANT: You NEVER read all the README.md - start by reading 300 lines

RULES:
- Security: Defensive only (analysis, detection, docs)
- Style: no emojis/comments unless asked, use conventional commits
- Code: Follow existing patterns, check deps, no secrets
- Tasks: Use TodoWrite, mark progress immediately
- Chat exports: Read only "## Summary" to "---" section
- Tools: Batch calls, prefer Grep for search

WORKFLOW:
When asked to implement features or fix issues:
1. Plan with TodoWrite
2. Search codebase to understand context
3. Implement solution
4. Run tests with: task test
5. Run lint/format with: task fmt and task lint
6. Commit changes (only if explicitly asked)
7. Create a pull request (only if explicitly asked)

EXAMPLE:
<user>Can you create a pull request with the changes?</user>
<assistant>I will checkout to a new branch</assistant>
<tool>Bash(git checkout -b feat/my-new-feature)</tool>
<assistant>Now I will modify the files</assistant>
<tool>Read|Edit|Grep etc</tool>
<tool>Bash(git add <files>)</tool>
<tool>Bash(git commit -m <message>)</tool>
<assistant>Now I will push the changes</assistant>
<tool>Bash(git push origin <branch>)</tool>
<assistant>Now I'll create a pull request</assistant>
<tool>Github(...)</tool>
`,
			Optimization: OptimizationConfig{
				Enabled:                    false,
				MaxHistory:                 10,
				CompactThreshold:           20,
				TruncateLargeOutputs:       true,
				SkipRedundantConfirmations: true,
			},
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
// ConfigService interface implementation
func (c *Config) IsApprovalRequired(toolName string) bool {
	globalApproval := c.Tools.Safety.RequireApproval

	switch toolName {
	case "Bash":
		if c.Tools.Bash.RequireApproval != nil {
			logger.Debug("Tool approval check", "tool", toolName, "specific", *c.Tools.Bash.RequireApproval, "global", globalApproval)
			return *c.Tools.Bash.RequireApproval
		}
	case "Read":
		if c.Tools.Read.RequireApproval != nil {
			logger.Debug("Tool approval check", "tool", toolName, "specific", *c.Tools.Read.RequireApproval, "global", globalApproval)
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
	case "WebFetch":
		if c.Tools.WebFetch.RequireApproval != nil {
			return *c.Tools.WebFetch.RequireApproval
		}
	case "WebSearch":
		if c.Tools.WebSearch.RequireApproval != nil {
			return *c.Tools.WebSearch.RequireApproval
		}
	case "Github":
		if c.Tools.Github.RequireApproval != nil {
			return *c.Tools.Github.RequireApproval
		}
	case "TodoWrite":
		if c.Tools.TodoWrite.RequireApproval != nil {
			return *c.Tools.TodoWrite.RequireApproval
		}
	}

	return globalApproval
}

// Additional ConfigService methods
func (c *Config) IsDebugMode() bool {
	return c.Logging.Debug
}

func (c *Config) GetOutputDirectory() string {
	return c.Compact.OutputDir
}

func (c *Config) GetGatewayURL() string {
	return c.Gateway.URL
}

func (c *Config) GetAPIKey() string {
	return c.Gateway.APIKey
}

func (c *Config) GetTimeout() int {
	return c.Gateway.Timeout
}

func (c *Config) GetSystemPrompt() string {
	return c.Chat.SystemPrompt
}

func (c *Config) GetDefaultModel() string {
	return c.Chat.DefaultModel
}

// ValidatePathInSandbox checks if a path is within the configured sandbox directories
func (c *Config) ValidatePathInSandbox(path string) error {
	if len(c.Tools.Sandbox.Directories) == 0 {
		return fmt.Errorf("no sandbox directories configured")
	}

	if err := c.checkProtectedPaths(path); err != nil {
		return err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	for _, sandboxDir := range c.Tools.Sandbox.Directories {
		absSandboxDir, err := filepath.Abs(sandboxDir)
		if err != nil {
			continue
		}

		relPath, err := filepath.Rel(absSandboxDir, absPath)
		if err != nil {
			continue
		}

		if !strings.HasPrefix(relPath, "..") {
			return nil
		}
	}

	return fmt.Errorf("path '%s' is outside configured sandbox directories", path)
}

// checkProtectedPaths checks if a path matches any protected path patterns
func (c *Config) checkProtectedPaths(path string) error {
	normalizedPath := filepath.ToSlash(filepath.Clean(path))

	for _, protectedPath := range c.Tools.Sandbox.ProtectedPaths {
		if normalizedPath == strings.TrimSuffix(protectedPath, "/") {
			return fmt.Errorf("access to path '%s' is excluded for security", path)
		}

		if strings.HasSuffix(protectedPath, "/") {
			dirPattern := strings.TrimSuffix(protectedPath, "/")
			if strings.HasPrefix(normalizedPath, dirPattern+"/") || normalizedPath == dirPattern {
				return fmt.Errorf("access to path '%s' is excluded for security", path)
			}
		}

		if strings.HasSuffix(protectedPath, "/*") {
			dirPattern := strings.TrimSuffix(protectedPath, "/*")
			if strings.HasPrefix(normalizedPath, dirPattern+"/") || normalizedPath == dirPattern {
				return fmt.Errorf("access to path '%s' is excluded for security", path)
			}
		}

		if strings.Contains(protectedPath, "*") {
			matched, err := filepath.Match(protectedPath, filepath.Base(normalizedPath))
			if err == nil && matched {
				return fmt.Errorf("access to path '%s' is excluded for security", path)
			}
		}
	}

	return nil
}

// ResolveEnvironmentVariables resolves environment variable references in the format %VAR_NAME%
func ResolveEnvironmentVariables(value string) string {
	if value == "" {
		return value
	}

	envVarPattern := regexp.MustCompile(`%([A-Z_][A-Z0-9_]*)%`)

	result := envVarPattern.ReplaceAllStringFunc(value, func(match string) string {
		varName := match[1 : len(match)-1]

		if envValue := os.Getenv(varName); envValue != "" {
			logger.Debug("Resolved environment variable", "var", varName, "value", "[redacted]")
			return envValue
		}

		logger.Debug("Environment variable not set", "var", varName)
		return ""
	})

	return result
}
