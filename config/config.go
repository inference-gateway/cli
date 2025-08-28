package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	logger "github.com/inference-gateway/cli/internal/logger"
)

const (
	ConfigDirName     = ".infer"
	ConfigFileName    = "config.yaml"
	GitignoreFileName = ".gitignore"
	LogsDirName       = "logs"

	DefaultConfigPath = ConfigDirName + "/" + ConfigFileName
	DefaultLogsPath   = ConfigDirName + "/" + LogsDirName
)

// Config represents the CLI configuration
type Config struct {
	Gateway      GatewayConfig      `yaml:"gateway" mapstructure:"gateway"`
	Client       ClientConfig       `yaml:"client" mapstructure:"client"`
	Logging      LoggingConfig      `yaml:"logging" mapstructure:"logging"`
	Tools        ToolsConfig        `yaml:"tools" mapstructure:"tools"`
	Compact      CompactConfig      `yaml:"compact" mapstructure:"compact"`
	Agent        AgentConfig        `yaml:"agent" mapstructure:"agent"`
	Git          GitConfig          `yaml:"git" mapstructure:"git"`
	Storage      StorageConfig      `yaml:"storage" mapstructure:"storage"`
	Conversation ConversationConfig `yaml:"conversation" mapstructure:"conversation"`
	Chat         ChatConfig         `yaml:"chat" mapstructure:"chat"`
}

// GatewayConfig contains gateway connection settings
type GatewayConfig struct {
	URL     string `yaml:"url" mapstructure:"url"`
	APIKey  string `yaml:"api_key" mapstructure:"api_key"`
	Timeout int    `yaml:"timeout" mapstructure:"timeout"`
}

// ClientConfig contains HTTP client settings
type ClientConfig struct {
	Timeout int         `yaml:"timeout" mapstructure:"timeout"`
	Retry   RetryConfig `yaml:"retry" mapstructure:"retry"`
}

// RetryConfig contains retry logic settings
type RetryConfig struct {
	Enabled              bool  `yaml:"enabled" mapstructure:"enabled"`
	MaxAttempts          int   `yaml:"max_attempts" mapstructure:"max_attempts"`
	InitialBackoffSec    int   `yaml:"initial_backoff_sec" mapstructure:"initial_backoff_sec"`
	MaxBackoffSec        int   `yaml:"max_backoff_sec" mapstructure:"max_backoff_sec"`
	BackoffMultiplier    int   `yaml:"backoff_multiplier" mapstructure:"backoff_multiplier"`
	RetryableStatusCodes []int `yaml:"retryable_status_codes" mapstructure:"retryable_status_codes"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Debug bool   `yaml:"debug" mapstructure:"debug"`
	Dir   string `yaml:"dir" mapstructure:"dir"`
}

// ToolsConfig contains tool execution settings
type ToolsConfig struct {
	Enabled   bool                `yaml:"enabled" mapstructure:"enabled"`
	Sandbox   SandboxConfig       `yaml:"sandbox" mapstructure:"sandbox"`
	Bash      BashToolConfig      `yaml:"bash" mapstructure:"bash"`
	Read      ReadToolConfig      `yaml:"read" mapstructure:"read"`
	Write     WriteToolConfig     `yaml:"write" mapstructure:"write"`
	Edit      EditToolConfig      `yaml:"edit" mapstructure:"edit"`
	Delete    DeleteToolConfig    `yaml:"delete" mapstructure:"delete"`
	Grep      GrepToolConfig      `yaml:"grep" mapstructure:"grep"`
	Tree      TreeToolConfig      `yaml:"tree" mapstructure:"tree"`
	WebFetch  WebFetchToolConfig  `yaml:"web_fetch" mapstructure:"web_fetch"`
	WebSearch WebSearchToolConfig `yaml:"web_search" mapstructure:"web_search"`
	Github    GithubToolConfig    `yaml:"github" mapstructure:"github"`
	TodoWrite TodoWriteToolConfig `yaml:"todo_write" mapstructure:"todo_write"`
	Safety    SafetyConfig        `yaml:"safety" mapstructure:"safety"`
}

// BashToolConfig contains bash-specific tool settings
type BashToolConfig struct {
	Enabled         bool                `yaml:"enabled" mapstructure:"enabled"`
	Whitelist       ToolWhitelistConfig `yaml:"whitelist" mapstructure:"whitelist"`
	RequireApproval *bool               `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// ReadToolConfig contains read-specific tool settings
type ReadToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// WriteToolConfig contains write-specific tool settings
type WriteToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// EditToolConfig contains edit-specific tool settings
type EditToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// DeleteToolConfig contains delete-specific tool settings
type DeleteToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// GrepToolConfig contains grep-specific tool settings
type GrepToolConfig struct {
	Enabled         bool   `yaml:"enabled" mapstructure:"enabled"`
	Backend         string `yaml:"backend" mapstructure:"backend"`
	RequireApproval *bool  `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// TreeToolConfig contains tree-specific tool settings
type TreeToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// WebFetchToolConfig contains fetch-specific tool settings
type WebFetchToolConfig struct {
	Enabled            bool              `yaml:"enabled" mapstructure:"enabled"`
	WhitelistedDomains []string          `yaml:"whitelisted_domains" mapstructure:"whitelisted_domains"`
	Safety             FetchSafetyConfig `yaml:"safety" mapstructure:"safety"`
	Cache              FetchCacheConfig  `yaml:"cache" mapstructure:"cache"`
	RequireApproval    *bool             `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// WebSearchToolConfig contains web search-specific tool settings
type WebSearchToolConfig struct {
	Enabled         bool     `yaml:"enabled" mapstructure:"enabled"`
	DefaultEngine   string   `yaml:"default_engine" mapstructure:"default_engine"`
	MaxResults      int      `yaml:"max_results" mapstructure:"max_results"`
	Engines         []string `yaml:"engines" mapstructure:"engines"`
	Timeout         int      `yaml:"timeout" mapstructure:"timeout"`
	RequireApproval *bool    `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// TodoWriteToolConfig contains TodoWrite-specific tool settings
type TodoWriteToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// GithubToolConfig contains GitHub fetch-specific tool settings
type GithubToolConfig struct {
	Enabled         bool               `yaml:"enabled" mapstructure:"enabled"`
	Token           string             `yaml:"token" mapstructure:"token"`
	BaseURL         string             `yaml:"base_url" mapstructure:"base_url"`
	Owner           string             `yaml:"owner" mapstructure:"owner"`
	Repo            string             `yaml:"repo,omitempty" mapstructure:"repo,omitempty"`
	Safety          GithubSafetyConfig `yaml:"safety" mapstructure:"safety"`
	RequireApproval *bool              `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// GithubSafetyConfig contains safety settings for GitHub fetch operations
type GithubSafetyConfig struct {
	MaxSize int64 `yaml:"max_size" mapstructure:"max_size"`
	Timeout int   `yaml:"timeout" mapstructure:"timeout"`
}

// ToolWhitelistConfig contains whitelisted commands and patterns
type ToolWhitelistConfig struct {
	Commands []string `yaml:"commands" mapstructure:"commands"`
	Patterns []string `yaml:"patterns" mapstructure:"patterns"`
}

// SandboxConfig contains sandbox directory settings
type SandboxConfig struct {
	Directories    []string `yaml:"directories" mapstructure:"directories"`
	ProtectedPaths []string `yaml:"protected_paths" mapstructure:"protected_paths"`
}

// SafetyConfig contains safety approval settings
type SafetyConfig struct {
	RequireApproval bool `yaml:"require_approval" mapstructure:"require_approval"`
}

// CompactConfig contains settings for compact command
type CompactConfig struct {
	OutputDir    string `yaml:"output_dir" mapstructure:"output_dir"`
	SummaryModel string `yaml:"summary_model" mapstructure:"summary_model"`
}

// OptimizationConfig contains token optimization settings
type OptimizationConfig struct {
	Enabled                    bool `yaml:"enabled" mapstructure:"enabled"`
	MaxHistory                 int  `yaml:"max_history" mapstructure:"max_history"`
	CompactThreshold           int  `yaml:"compact_threshold" mapstructure:"compact_threshold"`
	TruncateLargeOutputs       bool `yaml:"truncate_large_outputs" mapstructure:"truncate_large_outputs"`
	SkipRedundantConfirmations bool `yaml:"skip_redundant_confirmations" mapstructure:"skip_redundant_confirmations"`
}

// SystemRemindersConfig contains settings for dynamic system reminders
type SystemRemindersConfig struct {
	Enabled      bool   `yaml:"enabled" mapstructure:"enabled"`
	Interval     int    `yaml:"interval" mapstructure:"interval"`
	ReminderText string `yaml:"reminder_text" mapstructure:"reminder_text"`
}

// AgentConfig contains agent command-specific settings
type AgentConfig struct {
	Model           string                `yaml:"model" mapstructure:"model"`
	SystemPrompt    string                `yaml:"system_prompt" mapstructure:"system_prompt"`
	SystemReminders SystemRemindersConfig `yaml:"system_reminders" mapstructure:"system_reminders"`
	VerboseTools    bool                  `yaml:"verbose_tools" mapstructure:"verbose_tools"`
	MaxTurns        int                   `yaml:"max_turns" mapstructure:"max_turns"`
	MaxTokens       int                   `yaml:"max_tokens" mapstructure:"max_tokens"`
	Optimization    OptimizationConfig    `yaml:"optimization" mapstructure:"optimization"`
}

// GitConfig contains git shortcut-specific settings
type GitConfig struct {
	CommitMessage GitCommitMessageConfig `yaml:"commit_message" mapstructure:"commit_message"`
}

// ConversationConfig contains conversation-specific settings
type ConversationConfig struct {
	TitleGeneration ConversationTitleConfig `yaml:"title_generation" mapstructure:"title_generation"`
}

// GitCommitMessageConfig contains settings for AI-generated commit messages
type GitCommitMessageConfig struct {
	Model        string `yaml:"model" mapstructure:"model"`
	SystemPrompt string `yaml:"system_prompt" mapstructure:"system_prompt"`
}

// ConversationTitleConfig contains settings for AI-generated conversation titles
type ConversationTitleConfig struct {
	Enabled      bool   `yaml:"enabled" mapstructure:"enabled"`
	Model        string `yaml:"model" mapstructure:"model"`
	SystemPrompt string `yaml:"system_prompt" mapstructure:"system_prompt"`
	BatchSize    int    `yaml:"batch_size" mapstructure:"batch_size"`
	Interval     int    `yaml:"interval" mapstructure:"interval"`
}

// ChatConfig contains chat interface settings
type ChatConfig struct {
	Theme string `yaml:"theme" mapstructure:"theme"`
}

// FetchSafetyConfig contains safety settings for fetch operations
type FetchSafetyConfig struct {
	MaxSize       int64 `yaml:"max_size" mapstructure:"max_size"`
	Timeout       int   `yaml:"timeout" mapstructure:"timeout"`
	AllowRedirect bool  `yaml:"allow_redirect" mapstructure:"allow_redirect"`
}

// FetchCacheConfig contains cache settings for fetch operations
type FetchCacheConfig struct {
	Enabled bool  `yaml:"enabled" mapstructure:"enabled"`
	TTL     int   `yaml:"ttl" mapstructure:"ttl"`
	MaxSize int64 `yaml:"max_size" mapstructure:"max_size"`
}

// StorageType represents the type of storage backend
type StorageType string

const (
	StorageTypeMemory   StorageType = "memory"
	StorageTypeSQLite   StorageType = "sqlite"
	StorageTypePostgres StorageType = "postgres"
	StorageTypeRedis    StorageType = "redis"
)

// StorageConfig contains storage backend configuration
type StorageConfig struct {
	Enabled  bool                  `yaml:"enabled" mapstructure:"enabled"`
	Type     StorageType           `yaml:"type" mapstructure:"type"`
	SQLite   SQLiteStorageConfig   `yaml:"sqlite,omitempty" mapstructure:"sqlite,omitempty"`
	Postgres PostgresStorageConfig `yaml:"postgres,omitempty" mapstructure:"postgres,omitempty"`
	Redis    RedisStorageConfig    `yaml:"redis,omitempty" mapstructure:"redis,omitempty"`
}

// SQLiteStorageConfig contains SQLite-specific configuration
type SQLiteStorageConfig struct {
	Path string `yaml:"path" mapstructure:"path"`
}

// PostgresStorageConfig contains Postgres-specific configuration
type PostgresStorageConfig struct {
	Host     string `yaml:"host" mapstructure:"host"`
	Port     int    `yaml:"port" mapstructure:"port"`
	Database string `yaml:"database" mapstructure:"database"`
	Username string `yaml:"username" mapstructure:"username"`
	Password string `yaml:"password" mapstructure:"password"`
	SSLMode  string `yaml:"ssl_mode" mapstructure:"ssl_mode"`
}

// RedisStorageConfig contains Redis-specific configuration
type RedisStorageConfig struct {
	Host     string `yaml:"host" mapstructure:"host"`
	Port     int    `yaml:"port" mapstructure:"port"`
	Password string `yaml:"password" mapstructure:"password"`
	DB       int    `yaml:"db" mapstructure:"db"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config { //nolint:funlen
	return &Config{
		Gateway: GatewayConfig{
			URL:     "http://localhost:8080",
			APIKey:  "",
			Timeout: 200,
		},
		Client: ClientConfig{
			Timeout: 200,
			Retry: RetryConfig{
				Enabled:              true,
				MaxAttempts:          3,
				InitialBackoffSec:    5,
				MaxBackoffSec:        60,
				BackoffMultiplier:    2,
				RetryableStatusCodes: []int{400, 408, 429, 500, 502, 503, 504},
			},
		},
		Logging: LoggingConfig{
			Debug: false,
			Dir:   "",
		},
		Tools: ToolsConfig{
			Enabled: true,
			Sandbox: SandboxConfig{
				Directories: []string{".", "/tmp"},
				ProtectedPaths: []string{
					ConfigDirName + "/",
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
			OutputDir:    ConfigDirName,
			SummaryModel: "",
		},
		Agent: AgentConfig{
			Model: "",
			SystemPrompt: `Autonomous software engineering agent. Execute tasks iteratively until completion.

IMPORTANT: You NEVER push to main or master or to the current branch - instead you create a branch and push to a branch.
IMPORTANT: You ALWAYS prefer to search for specific matches in a file rather than reading it all - prefer to use Grep tool over Read tool for efficiency.
IMPORTANT: You ALWAYS prefer to see AGENTS.md before README.md files.
IMPORTANT: When reading project documentation, prefer AGENTS.md if available, otherwise fallback to README.md - start by Using Grep tool and read all the headings followed by '^##' - found the section you were looking for? great - use Read tool. You didn't find anything? continue to see '^###'

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
			SystemReminders: SystemRemindersConfig{
				Enabled:  true,
				Interval: 4,
				ReminderText: `<system-reminder>
This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware. If you are working on tasks that would benefit from a todo list please use the TodoWrite tool to create one. If not, please feel free to ignore. Again do not mention this message to the user.
</system-reminder>`,
			},
			VerboseTools: false,
			MaxTurns:     50,
			MaxTokens:    4096,
			Optimization: OptimizationConfig{
				Enabled:                    false,
				MaxHistory:                 10,
				CompactThreshold:           20,
				TruncateLargeOutputs:       true,
				SkipRedundantConfirmations: true,
			},
		},
		Git: GitConfig{
			CommitMessage: GitCommitMessageConfig{
				Model: "",
				SystemPrompt: `Generate a concise git commit message following conventional commit format.

REQUIREMENTS:
- MUST use format: "type: Brief description"
- MUST be under 50 characters total
- MUST use imperative mood (e.g., "Add", "Fix", "Update")
- Types: feat, fix, docs, style, refactor, test, chore

EXAMPLES:
- "feat: Add git shortcut with AI commits"
- "fix: Resolve build error in container"
- "docs: Update README installation guide"
- "refactor: Simplify error handling"

Respond with ONLY the commit message, no quotes or explanation.`,
			},
		},
		Storage: StorageConfig{
			Enabled: true,
			Type:    "sqlite",
			SQLite: SQLiteStorageConfig{
				Path: ConfigDirName + "/conversations.db",
			},
			Postgres: PostgresStorageConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "infer_conversations",
				Username: "",
				Password: "",
				SSLMode:  "prefer",
			},
			Redis: RedisStorageConfig{
				Host:     "localhost",
				Port:     6379,
				Password: "",
				DB:       0,
			},
		},
		Conversation: ConversationConfig{
			TitleGeneration: ConversationTitleConfig{
				Enabled:   true,
				Model:     "",
				BatchSize: 10,
				SystemPrompt: `Generate a concise conversation title based on the messages provided.

REQUIREMENTS:
- MUST be under 50 characters total
- MUST be descriptive and capture the main topic
- MUST use title case
- NO quotes, colons, or special characters
- Focus on the primary subject or task discussed

EXAMPLES:
- "React Component Testing"
- "Database Migration Setup"
- "API Error Handling"
- "Docker Configuration"

Respond with ONLY the title, no quotes or explanation.`,
			},
		},
		Chat: ChatConfig{
			Theme: "tokyo-night",
		},
	}
}

// IsApprovalRequired checks if approval is required for a specific tool
// It returns true if tool-specific approval is set to true, or if global approval is true and tool-specific is not set to false
// ConfigService interface implementation
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
	return c.Agent.SystemPrompt
}

func (c *Config) GetDefaultModel() string {
	return c.Agent.Model
}

func (c *Config) GetSandboxDirectories() []string {
	return c.Tools.Sandbox.Directories
}

func (c *Config) GetProtectedPaths() []string {
	return c.Tools.Sandbox.ProtectedPaths
}

func (c *Config) GetTheme() string {
	return c.Chat.Theme
}

// ValidatePathInSandbox checks if a path is within the configured sandbox directories
func (c *Config) ValidatePathInSandbox(path string) error {
	if err := c.checkProtectedPaths(path); err != nil {
		return err
	}

	if len(c.Tools.Sandbox.Directories) == 0 {
		return nil
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
		if strings.HasSuffix(protectedPath, "/") {
			dirPattern := strings.TrimSuffix(protectedPath, "/")
			if strings.Contains(normalizedPath, "/"+dirPattern+"/") || strings.HasSuffix(normalizedPath, "/"+dirPattern) {
				return fmt.Errorf("access to path '%s' is excluded for security", path)
			}
			if strings.HasPrefix(normalizedPath, dirPattern+"/") || normalizedPath == dirPattern {
				return fmt.Errorf("access to path '%s' is excluded for security", path)
			}
		}

		if strings.Contains(protectedPath, "*") && !strings.HasSuffix(protectedPath, "/*") {
			matched, err := filepath.Match(protectedPath, filepath.Base(normalizedPath))
			if err == nil && matched {
				return fmt.Errorf("access to path '%s' is excluded for security", path)
			}
		}

		if strings.HasSuffix(protectedPath, "/*") {
			dirPattern := strings.TrimSuffix(protectedPath, "/*")
			if strings.Contains(normalizedPath, "/"+dirPattern+"/") || strings.HasSuffix(normalizedPath, "/"+dirPattern) {
				return fmt.Errorf("access to path '%s' is excluded for security", path)
			}
			if strings.HasPrefix(normalizedPath, dirPattern+"/") || normalizedPath == dirPattern {
				return fmt.Errorf("access to path '%s' is excluded for security", path)
			}
		}

		cleanProtectedPath := strings.TrimSuffix(protectedPath, "/")
		if normalizedPath == cleanProtectedPath || strings.HasSuffix(normalizedPath, "/"+cleanProtectedPath) {
			return fmt.Errorf("access to path '%s' is excluded for security", path)
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
