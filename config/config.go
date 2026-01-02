package config

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
	ContainerRuntime ContainerRuntimeConfig `yaml:"container_runtime" mapstructure:"container_runtime"`
	Gateway          GatewayConfig          `yaml:"gateway" mapstructure:"gateway"`
	Client           ClientConfig           `yaml:"client" mapstructure:"client"`
	Logging          LoggingConfig          `yaml:"logging" mapstructure:"logging"`
	Tools            ToolsConfig            `yaml:"tools" mapstructure:"tools"`
	Image            ImageConfig            `yaml:"image" mapstructure:"image"`
	Export           ExportConfig           `yaml:"export" mapstructure:"export"`
	Agent            AgentConfig            `yaml:"agent" mapstructure:"agent"`
	Git              GitConfig              `yaml:"git" mapstructure:"git"`
	SCM              SCMConfig              `yaml:"scm" mapstructure:"scm"`
	Storage          StorageConfig          `yaml:"storage" mapstructure:"storage"`
	Conversation     ConversationConfig     `yaml:"conversation" mapstructure:"conversation"`
	Chat             ChatConfig             `yaml:"chat" mapstructure:"chat"`
	A2A              A2AConfig              `yaml:"a2a" mapstructure:"a2a"`
	MCP              MCPConfig              `yaml:"mcp" mapstructure:"mcp"`
	Pricing          PricingConfig          `yaml:"pricing" mapstructure:"pricing"`
	Init             InitConfig             `yaml:"init" mapstructure:"init"`
	Compact          CompactConfig          `yaml:"compact" mapstructure:"compact"`
	Web              WebConfig              `yaml:"web" mapstructure:"web"`
	ComputerUse      ComputerUseConfig      `yaml:"computer_use" mapstructure:"computer_use"`
	configDir        string
}

// ContainerRuntimeConfig contains container runtime settings
type ContainerRuntimeConfig struct {
	Type string `yaml:"type" mapstructure:"type"` // "docker", "podman", or "" for auto-detect
}

// GatewayConfig contains gateway connection settings
type GatewayConfig struct {
	URL              string   `yaml:"url" mapstructure:"url"`
	APIKey           string   `yaml:"api_key" mapstructure:"api_key"`
	Timeout          int      `yaml:"timeout" mapstructure:"timeout"`
	OCI              string   `yaml:"oci,omitempty" mapstructure:"oci,omitempty"`
	Run              bool     `yaml:"run" mapstructure:"run"`
	StandaloneBinary bool     `yaml:"standalone_binary" mapstructure:"standalone_binary"`
	Debug            bool     `yaml:"debug,omitempty" mapstructure:"debug,omitempty"`
	IncludeModels    []string `yaml:"include_models,omitempty" mapstructure:"include_models,omitempty"`
	ExcludeModels    []string `yaml:"exclude_models,omitempty" mapstructure:"exclude_models,omitempty"`
	VisionEnabled    bool     `yaml:"vision_enabled" mapstructure:"vision_enabled"`
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

// ImageConfig contains image service settings
type ImageConfig struct {
	MaxSize           int64                        `yaml:"max_size" mapstructure:"max_size"`
	Timeout           int                          `yaml:"timeout" mapstructure:"timeout"`
	ClipboardOptimize ClipboardImageOptimizeConfig `yaml:"clipboard_optimize" mapstructure:"clipboard_optimize"`
}

// ClipboardImageOptimizeConfig contains clipboard image optimization settings
type ClipboardImageOptimizeConfig struct {
	Enabled     bool `yaml:"enabled" mapstructure:"enabled"`
	MaxWidth    int  `yaml:"max_width" mapstructure:"max_width"`
	MaxHeight   int  `yaml:"max_height" mapstructure:"max_height"`
	Quality     int  `yaml:"quality" mapstructure:"quality"`
	ConvertJPEG bool `yaml:"convert_jpeg" mapstructure:"convert_jpeg"`
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

	Safety SafetyConfig `yaml:"safety" mapstructure:"safety"`
}

// BashToolConfig contains bash-specific tool settings
type BashToolConfig struct {
	Enabled          bool                   `yaml:"enabled" mapstructure:"enabled"`
	Timeout          int                    `yaml:"timeout" mapstructure:"timeout"`
	Whitelist        ToolWhitelistConfig    `yaml:"whitelist" mapstructure:"whitelist"`
	RequireApproval  *bool                  `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
	BackgroundShells BackgroundShellsConfig `yaml:"background_shells" mapstructure:"background_shells"`
}

// BackgroundShellsConfig contains background shell execution settings
type BackgroundShellsConfig struct {
	Enabled           bool `yaml:"enabled" mapstructure:"enabled"`
	MaxConcurrent     int  `yaml:"max_concurrent" mapstructure:"max_concurrent"`
	MaxOutputBufferMB int  `yaml:"max_output_buffer_mb" mapstructure:"max_output_buffer_mb"`
	RetentionMinutes  int  `yaml:"retention_minutes" mapstructure:"retention_minutes"`
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

// QueryAgentToolConfig contains Query-specific tool settings
type QueryAgentToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// SubmitTaskToolConfig contains SubmitTask-specific tool settings
type SubmitTaskToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// QueryTaskToolConfig contains QueryTask-specific tool settings
type QueryTaskToolConfig struct {
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

// ComputerUseConfig contains computer use tool settings
type ComputerUseConfig struct {
	Enabled      bool                   `yaml:"enabled" mapstructure:"enabled"`
	Display      string                 `yaml:"display" mapstructure:"display"`
	Screenshot   ScreenshotToolConfig   `yaml:"screenshot" mapstructure:"screenshot"`
	MouseMove    MouseMoveToolConfig    `yaml:"mouse_move" mapstructure:"mouse_move"`
	MouseClick   MouseClickToolConfig   `yaml:"mouse_click" mapstructure:"mouse_click"`
	KeyboardType KeyboardTypeToolConfig `yaml:"keyboard_type" mapstructure:"keyboard_type"`
	RateLimit    RateLimitConfig        `yaml:"rate_limit" mapstructure:"rate_limit"`
}

// ScreenshotToolConfig contains screenshot-specific tool settings
type ScreenshotToolConfig struct {
	Enabled          bool   `yaml:"enabled" mapstructure:"enabled"`
	MaxWidth         int    `yaml:"max_width" mapstructure:"max_width"`
	MaxHeight        int    `yaml:"max_height" mapstructure:"max_height"`
	Format           string `yaml:"format" mapstructure:"format"`
	Quality          int    `yaml:"quality" mapstructure:"quality"`
	RequireApproval  *bool  `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
	StreamingEnabled bool   `yaml:"streaming_enabled" mapstructure:"streaming_enabled"`
	CaptureInterval  int    `yaml:"capture_interval" mapstructure:"capture_interval"` // seconds
	BufferSize       int    `yaml:"buffer_size" mapstructure:"buffer_size"`           // number of screenshots
	TempDir          string `yaml:"temp_dir" mapstructure:"temp_dir"`                 // path for disk storage
}

// MouseMoveToolConfig contains mouse move-specific tool settings
type MouseMoveToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// MouseClickToolConfig contains mouse click-specific tool settings
type MouseClickToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// KeyboardTypeToolConfig contains keyboard type-specific tool settings
type KeyboardTypeToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	MaxTextLength   int   `yaml:"max_text_length" mapstructure:"max_text_length"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
}

// RateLimitConfig contains rate limiting settings
type RateLimitConfig struct {
	Enabled             bool `yaml:"enabled" mapstructure:"enabled"`
	MaxActionsPerMinute int  `yaml:"max_actions_per_minute" mapstructure:"max_actions_per_minute"`
	WindowSeconds       int  `yaml:"window_seconds" mapstructure:"window_seconds"`
}

// SafetyConfig contains safety approval settings
type SafetyConfig struct {
	RequireApproval bool `yaml:"require_approval" mapstructure:"require_approval"`
}

// ExportConfig contains settings for export command
type ExportConfig struct {
	OutputDir    string `yaml:"output_dir" mapstructure:"output_dir"`
	SummaryModel string `yaml:"summary_model" mapstructure:"summary_model"`
}

// CompactConfig contains conversation compaction settings
type CompactConfig struct {
	Enabled           bool `yaml:"enabled" mapstructure:"enabled"`
	AutoAt            int  `yaml:"auto_at" mapstructure:"auto_at"`
	KeepFirstMessages int  `yaml:"keep_first_messages" mapstructure:"keep_first_messages"`
}

// WebConfig contains web terminal settings
type WebConfig struct {
	Enabled               bool              `yaml:"enabled" mapstructure:"enabled"`
	Port                  int               `yaml:"port" mapstructure:"port"`
	Host                  string            `yaml:"host" mapstructure:"host"`
	SessionInactivityMins int               `yaml:"session_inactivity_mins" mapstructure:"session_inactivity_mins"`
	SSH                   WebSSHConfig      `yaml:"ssh" mapstructure:"ssh"`
	Servers               []SSHServerConfig `yaml:"servers" mapstructure:"servers"`
}

// WebSSHConfig contains SSH connection settings for remote servers
type WebSSHConfig struct {
	Enabled        bool   `yaml:"enabled" mapstructure:"enabled"`
	UseSSHConfig   bool   `yaml:"use_ssh_config" mapstructure:"use_ssh_config"`
	KnownHostsPath string `yaml:"known_hosts_path" mapstructure:"known_hosts_path"`
	AutoInstall    bool   `yaml:"auto_install" mapstructure:"auto_install"`
	InstallVersion string `yaml:"install_version" mapstructure:"install_version"`
}

// SSHServerConfig contains configuration for a single remote SSH server
type SSHServerConfig struct {
	Name        string   `yaml:"name" mapstructure:"name"`
	ID          string   `yaml:"id" mapstructure:"id"`
	RemoteHost  string   `yaml:"remote_host" mapstructure:"remote_host"`
	RemotePort  int      `yaml:"remote_port" mapstructure:"remote_port"`
	RemoteUser  string   `yaml:"remote_user" mapstructure:"remote_user"`
	CommandPath string   `yaml:"command_path" mapstructure:"command_path"`
	CommandArgs []string `yaml:"command_args" mapstructure:"command_args"`
	AutoInstall *bool    `yaml:"auto_install,omitempty" mapstructure:"auto_install"`
	InstallPath string   `yaml:"install_path" mapstructure:"install_path"`
	Description string   `yaml:"description" mapstructure:"description"`
	Tags        []string `yaml:"tags" mapstructure:"tags"`
}

// SystemRemindersConfig contains settings for dynamic system reminders
type SystemRemindersConfig struct {
	Enabled      bool   `yaml:"enabled" mapstructure:"enabled"`
	Interval     int    `yaml:"interval" mapstructure:"interval"`
	ReminderText string `yaml:"reminder_text" mapstructure:"reminder_text"`
}

// AgentConfig contains agent command-specific settings
type AgentConfig struct {
	Model              string                `yaml:"model" mapstructure:"model"`
	SystemPrompt       string                `yaml:"system_prompt" mapstructure:"system_prompt"`
	SystemPromptPlan   string                `yaml:"system_prompt_plan" mapstructure:"system_prompt_plan"`
	SystemPromptRemote string                `yaml:"system_prompt_remote" mapstructure:"system_prompt_remote"`
	SystemReminders    SystemRemindersConfig `yaml:"system_reminders" mapstructure:"system_reminders"`
	VerboseTools       bool                  `yaml:"verbose_tools" mapstructure:"verbose_tools"`
	MaxTurns           int                   `yaml:"max_turns" mapstructure:"max_turns"`
	MaxTokens          int                   `yaml:"max_tokens" mapstructure:"max_tokens"`
	MaxConcurrentTools int                   `yaml:"max_concurrent_tools" mapstructure:"max_concurrent_tools"`
}

// GitConfig contains git shortcut-specific settings
type GitConfig struct {
	CommitMessage GitCommitMessageConfig `yaml:"commit_message" mapstructure:"commit_message"`
}

// A2AConfig contains A2A agent configuration
type A2AConfig struct {
	Enabled bool           `yaml:"enabled" mapstructure:"enabled"`
	Agents  []string       `yaml:"agents,omitempty" mapstructure:"agents"`
	Cache   A2ACacheConfig `yaml:"cache" mapstructure:"cache"`
	Task    A2ATaskConfig  `yaml:"task" mapstructure:"task"`
	Tools   A2AToolsConfig `yaml:"tools" mapstructure:"tools"`
}

// A2AToolsConfig contains A2A-specific tool configurations
type A2AToolsConfig struct {
	QueryAgent QueryAgentToolConfig `yaml:"query_agent" mapstructure:"query_agent"`
	QueryTask  QueryTaskToolConfig  `yaml:"query_task" mapstructure:"query_task"`
	SubmitTask SubmitTaskToolConfig `yaml:"submit_task" mapstructure:"submit_task"`
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
	Theme       string            `yaml:"theme" mapstructure:"theme"`
	Keybindings KeybindingsConfig `yaml:"keybindings" mapstructure:"keybindings"`
	StatusBar   StatusBarConfig   `yaml:"status_bar" mapstructure:"status_bar"`
}

// KeybindingsConfig contains settings for customizing keybindings
type KeybindingsConfig struct {
	Enabled  bool                       `yaml:"enabled" mapstructure:"enabled"`
	Bindings map[string]KeyBindingEntry `yaml:"bindings,omitempty" mapstructure:"bindings,omitempty"`
}

// KeyBindingEntry defines a complete keybinding with its properties
type KeyBindingEntry struct {
	Keys        []string `yaml:"keys" mapstructure:"keys"`
	Description string   `yaml:"description,omitempty" mapstructure:"description,omitempty"`
	Category    string   `yaml:"category,omitempty" mapstructure:"category,omitempty"`
	Enabled     *bool    `yaml:"enabled,omitempty" mapstructure:"enabled,omitempty"`
}

// StatusBarConfig contains settings for the chat status bar
// The status bar displays model information and system status indicators
type StatusBarConfig struct {
	Enabled    bool                `yaml:"enabled" mapstructure:"enabled"`
	Indicators StatusBarIndicators `yaml:"indicators" mapstructure:"indicators"`
}

// StatusBarIndicators contains individual enable/disable toggles for each indicator
// All indicators are enabled by default to maintain current behavior
type StatusBarIndicators struct {
	Model            bool `yaml:"model" mapstructure:"model"`
	Theme            bool `yaml:"theme" mapstructure:"theme"`
	MaxOutput        bool `yaml:"max_output" mapstructure:"max_output"`
	A2AAgents        bool `yaml:"a2a_agents" mapstructure:"a2a_agents"`
	Tools            bool `yaml:"tools" mapstructure:"tools"`
	BackgroundShells bool `yaml:"background_shells" mapstructure:"background_shells"`
	A2ATasks         bool `yaml:"a2a_tasks" mapstructure:"a2a_tasks"`
	MCP              bool `yaml:"mcp" mapstructure:"mcp"`
	ContextUsage     bool `yaml:"context_usage" mapstructure:"context_usage"`
	SessionTokens    bool `yaml:"session_tokens" mapstructure:"session_tokens"`
	Cost             bool `yaml:"cost" mapstructure:"cost"`
	GitBranch        bool `yaml:"git_branch" mapstructure:"git_branch"`
}

// InitConfig contains settings for the /init shortcut
type InitConfig struct {
	Prompt string `yaml:"prompt" mapstructure:"prompt"`
}

// SCMConfig contains settings for source control management shortcuts
type SCMConfig struct {
	PRCreate SCMPRCreateConfig `yaml:"pr_create" mapstructure:"pr_create"`
	Cleanup  SCMCleanupConfig  `yaml:"cleanup" mapstructure:"cleanup"`
}

// SCMPRCreateConfig contains settings for the /scm pr create shortcut
type SCMPRCreateConfig struct {
	Prompt       string `yaml:"prompt" mapstructure:"prompt"`
	BaseBranch   string `yaml:"base_branch" mapstructure:"base_branch"`
	BranchPrefix string `yaml:"branch_prefix" mapstructure:"branch_prefix"`
	Model        string `yaml:"model" mapstructure:"model"`
}

// SCMCleanupConfig contains settings for cleanup after PR creation
type SCMCleanupConfig struct {
	ReturnToBase      bool `yaml:"return_to_base" mapstructure:"return_to_base"`
	DeleteLocalBranch bool `yaml:"delete_local_branch" mapstructure:"delete_local_branch"`
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
	StorageTypeJsonl    StorageType = "jsonl"
)

// StorageConfig contains storage backend configuration
type StorageConfig struct {
	Enabled  bool                  `yaml:"enabled" mapstructure:"enabled"`
	Type     StorageType           `yaml:"type" mapstructure:"type"`
	SQLite   SQLiteStorageConfig   `yaml:"sqlite,omitempty" mapstructure:"sqlite,omitempty"`
	Postgres PostgresStorageConfig `yaml:"postgres,omitempty" mapstructure:"postgres,omitempty"`
	Redis    RedisStorageConfig    `yaml:"redis,omitempty" mapstructure:"redis,omitempty"`
	Jsonl    JsonlStorageConfig    `yaml:"jsonl,omitempty" mapstructure:"jsonl,omitempty"`
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

// JsonlStorageConfig contains JSONL-specific configuration
type JsonlStorageConfig struct {
	Path string `yaml:"path" mapstructure:"path"`
}

// A2AAgentInfo contains information about an A2A agent connection
type A2AAgentInfo struct {
	Name        string            `yaml:"name" mapstructure:"name"`
	URL         string            `yaml:"url" mapstructure:"url"`
	APIKey      string            `yaml:"api_key" mapstructure:"api_key"`
	Description string            `yaml:"description,omitempty" mapstructure:"description,omitempty"`
	Timeout     int               `yaml:"timeout" mapstructure:"timeout"`
	Enabled     bool              `yaml:"enabled" mapstructure:"enabled"`
	Metadata    map[string]string `yaml:"metadata,omitempty" mapstructure:"metadata,omitempty"`
}

// A2ATaskConfig contains configuration for A2A task processing
type A2ATaskConfig struct {
	StatusPollSeconds      int     `yaml:"status_poll_seconds" mapstructure:"status_poll_seconds"`
	PollingStrategy        string  `yaml:"polling_strategy" mapstructure:"polling_strategy"`
	InitialPollIntervalSec int     `yaml:"initial_poll_interval_sec" mapstructure:"initial_poll_interval_sec"`
	MaxPollIntervalSec     int     `yaml:"max_poll_interval_sec" mapstructure:"max_poll_interval_sec"`
	BackoffMultiplier      float64 `yaml:"backoff_multiplier" mapstructure:"backoff_multiplier"`
	BackgroundMonitoring   bool    `yaml:"background_monitoring" mapstructure:"background_monitoring"`
	CompletedTaskRetention int     `yaml:"completed_task_retention" mapstructure:"completed_task_retention"`
}

// A2ACacheConfig contains settings for A2A agent card caching
type A2ACacheConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	TTL     int  `yaml:"ttl" mapstructure:"ttl"`
}

// GetDefaultStatusBarConfig returns the default status bar configuration
// All indicators are enabled by default except MaxOutput to maintain current behavior
func GetDefaultStatusBarConfig() StatusBarConfig {
	return StatusBarConfig{
		Enabled: true,
		Indicators: StatusBarIndicators{
			Model:            true,
			Theme:            true,
			MaxOutput:        false,
			A2AAgents:        true,
			Tools:            true,
			BackgroundShells: true,
			A2ATasks:         true,
			MCP:              true,
			ContextUsage:     true,
			SessionTokens:    true,
			Cost:             true,
			GitBranch:        true,
		},
	}
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config { //nolint:funlen
	return &Config{
		ContainerRuntime: ContainerRuntimeConfig{
			Type: "docker",
		},
		Gateway: GatewayConfig{
			URL:              "http://localhost:8080",
			APIKey:           "",
			Timeout:          200,
			OCI:              "ghcr.io/inference-gateway/inference-gateway:latest",
			Run:              true,
			StandaloneBinary: true,
			IncludeModels:    []string{},
			ExcludeModels: []string{
				"ollama_cloud/cogito-2.1:671b",
				"ollama_cloud/kimi-k2:1t",
				"ollama_cloud/kimi-k2-thinking",
				"ollama_cloud/deepseek-v3.1:671b",
				"groq/whisper-large-v3",
				"groq/whisper-large-v3-turbo",
				"groq/playai-tts",
				"groq/playai-tts-arabic",
			},
			VisionEnabled: true,
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
				Directories: []string{".", "/tmp", ConfigDirName + "/tmp"},
				ProtectedPaths: []string{
					ConfigDirName + "/config.yaml",
					ConfigDirName + "/*.db",
					ConfigDirName + "/shortcuts/",
					ConfigDirName + "/agents.yaml",
					ConfigDirName + "/mcp.yaml",
					".git/",
					"*.env",
				},
			},
			Bash: BashToolConfig{
				Enabled: true,
				Timeout: 120,
				Whitelist: ToolWhitelistConfig{
					Commands: []string{
						"ls", "pwd", "tree",
						"wc", "sort", "uniq", "head", "tail",
						"task", "make", "find",
					},
					Patterns: []string{
						"^git status$",
						"^git branch( --show-current)?( -[alrvd])?$",
						"^git log",
						"^git diff",
						"^git remote( -v)?$",
						"^git show",
					},
				},
				BackgroundShells: BackgroundShellsConfig{
					Enabled:           true,
					MaxConcurrent:     5,
					MaxOutputBufferMB: 10,
					RetentionMinutes:  60,
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
				WhitelistedDomains: []string{"golang.org", "localhost"},
				Safety: FetchSafetyConfig{
					MaxSize:       10485760, // 10MB
					Timeout:       30,       // 30 seconds
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
				Owner: DetectGithubOwner(),
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
		Image: ImageConfig{
			MaxSize: 5242880, // 5MB
			Timeout: 30,      // 30 seconds
			ClipboardOptimize: ClipboardImageOptimizeConfig{
				Enabled:     true,
				MaxWidth:    1920, // 1920px max width
				MaxHeight:   1080, // 1080px max height
				Quality:     75,   // 75% JPEG quality
				ConvertJPEG: true,
			},
		},
		Export: ExportConfig{
			OutputDir:    ConfigDirName + "/tmp",
			SummaryModel: "",
		},
		Agent: AgentConfig{
			Model: "",
			SystemPromptPlan: `You are an AI planning assistant in PLAN MODE. Your role is to analyze user requests and create ACTIONABLE, EXECUTABLE plans WITHOUT executing them.

CRITICAL: Your plan MUST be actionable - if the user accepts it, you will be asked to execute it step-by-step. Plans that are not actionable are NOT plans.

CAPABILITIES IN PLAN MODE:
- Read, Grep, and Tree tools for gathering information
- TodoWrite for tracking planning progress
- RequestPlanApproval tool to submit your plan for user approval
- Analyze code structure and dependencies
- Break down complex tasks into concrete, executable steps
- Identify exact files and code locations that need changes

RESTRICTIONS IN PLAN MODE:
- DO NOT execute Write, Edit, Delete, Bash, or modification tools
- DO NOT make any changes to files or system
- DO NOT attempt to implement the plan
- Focus solely on creating an executable plan

PLANNING WORKFLOW:
1. Use Read/Grep/Tree to understand the codebase thoroughly
2. Analyze the user's request and identify ALL requirements
3. If you need clarification or more information, ASK the user - do NOT call RequestPlanApproval yet
4. Break down into specific, numbered action steps
5. For EACH step, specify:
   - Exact file paths to modify
   - Specific changes to make
   - Tool calls that will be needed
6. Include testing and validation steps
7. When your plan is complete and actionable, call RequestPlanApproval tool

DECISION MAKING:
- Need more info? ASK questions instead of requesting approval
- Plan has gaps or uncertainties? ASK for clarification
- Plan is complete and specific? Call RequestPlanApproval tool

OUTPUT FORMAT - ACTIONABLE STEPS:
Structure your plan with concrete actions:
- Overview: What will be done and why
- Steps: Numbered steps with SPECIFIC actions
  Example: "Step 1: Edit /path/to/file.go - Add function X at line Y"
  Example: "Step 2: Run 'task test' to verify changes"
- Files: Exact list of files to be modified
- Testing: Specific commands to run and expected outcomes

REMEMBER:
- If accepted, YOU will execute this plan. Make it specific and actionable!
- Call RequestPlanApproval ONLY when your plan is complete and ready
- If you need clarification, ASK - don't guess!`,
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
- Tools: ALWAYS use parallel execution when possible - batch multiple tool calls in a single response to improve efficiency
- Tools: Prefer Grep for search, Read for specific files

PARALLEL TOOL EXECUTION:
- When you need to perform multiple operations, make ALL tool calls in a single response
- Examples: Read multiple files, search multiple patterns, execute multiple commands
- The system supports up to 5 concurrent tool executions by default
- This reduces back-and-forth communication and significantly improves performance

WORKFLOW:
When asked to implement features or fix issues:
1. Plan with TodoWrite
2. Search codebase to understand context
3. Implement solution
4. Run tests with: task test
5. Run lint/format with: task fmt and task lint
6. Commit changes (only if explicitly asked)
7. Create a pull request (only if explicitly asked)

A2A ARTIFACT DOWNLOADS:
When a delegated A2A task completes with artifacts:
1. Wait for the automatic completion notification
2. The completion message will show artifact details including Download URLs
3. Use WebFetch with download=true to automatically save artifacts to disk
   Example: WebFetch(url="http://agent/artifacts/123/file.png", download=true)
4. The file will be saved to <configDir>/tmp with filename extracted from URL
5. Check the tool result for the saved file path

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
			SystemPromptRemote: `Remote system administration agent. You are operating on a remote machine via SSH.

FOCUS: System operations, service management, monitoring, diagnostics, and infrastructure tasks.

CONTEXT: This is a shared system environment, not a project workspace. Users may be managing servers, containers, services, or general infrastructure.`,
			SystemReminders: SystemRemindersConfig{
				Enabled:  true,
				Interval: 4,
				ReminderText: `<system-reminder>
This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware. If you are working on tasks that would benefit from a todo list please use the TodoWrite tool to create one. If not, please feel free to ignore. Again do not mention this message to the user.
</system-reminder>`,
			},
			VerboseTools:       false,
			MaxTurns:           50,
			MaxTokens:          8192,
			MaxConcurrentTools: 5,
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
			Type:    "jsonl",
			Jsonl: JsonlStorageConfig{
				Path: ConfigDirName + "/conversations",
			},
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
			Keybindings: KeybindingsConfig{
				Enabled:  false,
				Bindings: GetDefaultKeybindings(),
			},
			StatusBar: GetDefaultStatusBarConfig(),
		},
		A2A: A2AConfig{
			Enabled: true,
			Cache: A2ACacheConfig{
				Enabled: true,
				TTL:     300,
			},
			Task: A2ATaskConfig{
				StatusPollSeconds:      5,
				PollingStrategy:        "exponential",
				InitialPollIntervalSec: 2,
				MaxPollIntervalSec:     60,
				BackoffMultiplier:      2.0,
				BackgroundMonitoring:   true,
				CompletedTaskRetention: 5,
			},
			Tools: A2AToolsConfig{
				QueryAgent: QueryAgentToolConfig{
					Enabled:         true,
					RequireApproval: &[]bool{false}[0],
				},
				QueryTask: QueryTaskToolConfig{
					Enabled:         true,
					RequireApproval: &[]bool{false}[0],
				},
				SubmitTask: SubmitTaskToolConfig{
					Enabled:         true,
					RequireApproval: &[]bool{true}[0],
				},
			},
		},
		MCP:     *DefaultMCPConfig(),
		Pricing: GetDefaultPricingConfig(),
		Init: InitConfig{
			Prompt: `Please analyze this project and generate a comprehensive AGENTS.md file. Start by using the Tree tool to understand the project structure.
Use your available tools to examine configuration files, documentation, build systems, and development workflow.
Focus on creating actionable documentation that will help other AI agents understand how to work effectively with this project.

The AGENTS.md file should include:
- Project overview and main technologies
- Architecture and structure
- Development environment setup
- Key commands (build, test, lint, run)
- Testing instructions
- Project conventions and coding standards
- Important files and configurations

Write the AGENTS.md file to the project root when you have gathered enough information.`,
		},
		SCM: SCMConfig{
			PRCreate: SCMPRCreateConfig{
				Prompt:       "",
				BaseBranch:   "main",
				BranchPrefix: "",
			},
			Cleanup: SCMCleanupConfig{
				ReturnToBase:      true,
				DeleteLocalBranch: false,
			},
		},
		Compact: CompactConfig{
			Enabled:           true,
			AutoAt:            80,
			KeepFirstMessages: 2,
		},
		Web: WebConfig{
			Enabled:               false,
			Port:                  3000,
			Host:                  "localhost",
			SessionInactivityMins: 5,
			SSH: WebSSHConfig{
				Enabled:        false,
				UseSSHConfig:   true,
				KnownHostsPath: "~/.ssh/known_hosts",
				AutoInstall:    true,
				InstallVersion: "latest",
			},
			Servers: []SSHServerConfig{},
		},
		ComputerUse: ComputerUseConfig{
			Enabled: false, // Security: disabled by default
			Display: ":0",
			Screenshot: ScreenshotToolConfig{
				Enabled:          true,
				MaxWidth:         1920,
				MaxHeight:        1080,
				Format:           "jpeg",
				Quality:          80,
				RequireApproval:  &[]bool{false}[0],
				StreamingEnabled: false,
				CaptureInterval:  3,
				BufferSize:       30,
				TempDir:          "/tmp/infer-screenshots",
			},
			MouseMove: MouseMoveToolConfig{
				Enabled:         true,
				RequireApproval: &[]bool{true}[0],
			},
			MouseClick: MouseClickToolConfig{
				Enabled:         true,
				RequireApproval: &[]bool{true}[0],
			},
			KeyboardType: KeyboardTypeToolConfig{
				Enabled:         true,
				MaxTextLength:   1000,
				RequireApproval: &[]bool{true}[0],
			},
			RateLimit: RateLimitConfig{
				Enabled:             true,
				MaxActionsPerMinute: 60,
				WindowSeconds:       60,
			},
		},
	}
}

// IsApprovalRequired checks if approval is required for a specific tool
// It returns true if tool-specific approval is set to true, or if global approval is true and tool-specific is not set to false
// ConfigService interface implementation
func (c *Config) IsApprovalRequired(toolName string) bool { // nolint:gocyclo,cyclop
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
	case "RequestPlanApproval":
		return false
	case "A2A_QueryAgent":
		if c.A2A.Tools.QueryAgent.RequireApproval != nil {
			return *c.A2A.Tools.QueryAgent.RequireApproval
		}
	case "A2A_QueryTask":
		if c.A2A.Tools.QueryTask.RequireApproval != nil {
			return *c.A2A.Tools.QueryTask.RequireApproval
		}
	case "A2A_SubmitTask":
		if c.A2A.Tools.SubmitTask.RequireApproval != nil {
			return *c.A2A.Tools.SubmitTask.RequireApproval
		}
	case "Screenshot":
		if c.ComputerUse.Screenshot.RequireApproval != nil {
			return *c.ComputerUse.Screenshot.RequireApproval
		}
	case "MouseMove":
		if c.ComputerUse.MouseMove.RequireApproval != nil {
			return *c.ComputerUse.MouseMove.RequireApproval
		}
	case "MouseClick":
		if c.ComputerUse.MouseClick.RequireApproval != nil {
			return *c.ComputerUse.MouseClick.RequireApproval
		}
	case "KeyboardType":
		if c.ComputerUse.KeyboardType.RequireApproval != nil {
			return *c.ComputerUse.KeyboardType.RequireApproval
		}
	}

	return globalApproval
}

// IsA2AToolsEnabled checks if A2A tools should be enabled
// A2A tools are enabled when a2a.enabled is true, regardless of tools.enabled
func (c *Config) IsA2AToolsEnabled() bool {
	return c.A2A.Enabled
}

func (c *Config) GetAgentConfig() *AgentConfig {
	return &c.Agent
}

func (c *Config) GetOutputDirectory() string {
	return c.Export.OutputDir
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
	if os.Getenv("INFER_REMOTE_MANAGED") == "true" && c.Agent.SystemPromptRemote != "" {
		return c.Agent.SystemPromptRemote
	}
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

func (c *Config) GetIncludeModels() []string {
	return c.Gateway.IncludeModels
}

func (c *Config) GetExcludeModels() []string {
	return c.Gateway.ExcludeModels
}

// SetConfigDir sets the configuration directory path
func (c *Config) SetConfigDir(dir string) {
	c.configDir = dir
}

// GetConfigDir returns the configuration directory path
func (c *Config) GetConfigDir() string {
	if c.configDir == "" {
		return ConfigDirName
	}
	return c.configDir
}

// IsBashCommandWhitelisted checks if a specific bash command is whitelisted
func (c *Config) IsBashCommandWhitelisted(command string) bool {
	command = strings.TrimSpace(command)

	for _, allowed := range c.Tools.Bash.Whitelist.Commands {
		if command == allowed || strings.HasPrefix(command, allowed+" ") {
			return true
		}
	}

	for _, pattern := range c.Tools.Bash.Whitelist.Patterns {
		matched, err := regexp.MatchString(pattern, command)
		if err == nil && matched {
			return true
		}
	}

	return false
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
			return envValue
		}

		return ""
	})

	return result
}

// DetectGithubOwner attempts to detect the GitHub owner from the git remote URL
// Returns empty string if not a git repository or not a GitHub remote
func DetectGithubOwner() string {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return ""
	}

	cmd = exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	remoteURL := strings.TrimSpace(string(output))
	return parseGithubOwnerFromURL(remoteURL)
}

// parseGithubOwnerFromURL extracts the GitHub owner from a git remote URL
// Supports both HTTPS and SSH formats:
// - https://github.com/owner/repo.git
// - git@github.com:owner/repo.git
func parseGithubOwnerFromURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}

	httpsPattern := regexp.MustCompile(`^https?://github\.com/([^/]+)/`)
	if matches := httpsPattern.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1]
	}

	sshPattern := regexp.MustCompile(`^git@github\.com:([^/]+)/`)
	if matches := sshPattern.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// KeyNamespace represents the namespace for key binding actions
type KeyNamespace string

// Namespace constants for organizing key binding actions
const (
	NamespaceGlobal       KeyNamespace = "global"
	NamespaceChat         KeyNamespace = "chat"
	NamespaceClipboard    KeyNamespace = "clipboard"
	NamespaceDisplay      KeyNamespace = "display"
	NamespaceHelp         KeyNamespace = "help"
	NamespaceMode         KeyNamespace = "mode"
	NamespaceNavigation   KeyNamespace = "navigation"
	NamespacePlanApproval KeyNamespace = "plan_approval"
	NamespaceSelection    KeyNamespace = "selection"
	NamespaceTextEditing  KeyNamespace = "text_editing"
	NamespaceTools        KeyNamespace = "tools"
)

// ActionID constructs a namespaced action ID from namespace and action name
// Format: "namespace_action" (e.g., "global_quit", "chat_enter_key_handler")
func ActionID(namespace KeyNamespace, action string) string {
	return string(namespace) + "_" + action
}

// Global port registry to prevent race conditions when allocating ports
var (
	allocatedPorts = make(map[int]bool)
	portMutex      sync.Mutex
)

// FindAvailablePort finds the next available port starting from basePort
// It checks up to 100 ports after the base port
// Binds to all interfaces (0.0.0.0) to match Docker's behavior
// Thread-safe: uses global port registry to prevent race conditions
func FindAvailablePort(basePort int) int {
	portMutex.Lock()
	defer portMutex.Unlock()

	for port := basePort; port < basePort+100; port++ {
		if allocatedPorts[port] {
			continue
		}

		address := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			continue
		}
		_ = listener.Close()

		allocatedPorts[port] = true
		return port
	}
	return basePort
}

// ReleasePort releases a previously allocated port
// Should be called when containers are stopped
func ReleasePort(port int) {
	portMutex.Lock()
	defer portMutex.Unlock()
	delete(allocatedPorts, port)
}
