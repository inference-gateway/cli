package config

import (
	"fmt"
	"net"
	"os"
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
	ClaudeCode       ClaudeCodeConfig       `yaml:"claude_code" mapstructure:"claude_code"`
	SpeechToText     SpeechToTextConfig     `yaml:"speech_to_text" mapstructure:"speech_to_text"`
	Client           ClientConfig           `yaml:"client" mapstructure:"client"`
	Logging          LoggingConfig          `yaml:"logging" mapstructure:"logging"`
	Tools            ToolsConfig            `yaml:"tools" mapstructure:"tools"`
	Image            ImageConfig            `yaml:"image" mapstructure:"image"`
	Export           ExportConfig           `yaml:"export" mapstructure:"export"`
	Agent            AgentConfig            `yaml:"agent" mapstructure:"agent"`
	Git              GitConfig              `yaml:"git" mapstructure:"git"`
	Storage          StorageConfig          `yaml:"storage" mapstructure:"storage"`
	Conversation     ConversationConfig     `yaml:"conversation" mapstructure:"conversation"`
	Chat             ChatConfig             `yaml:"chat" mapstructure:"chat"`
	A2A              A2AConfig              `yaml:"a2a" mapstructure:"a2a"`
	MCP              MCPConfig              `yaml:"mcp" mapstructure:"mcp"`
	Pricing          PricingConfig          `yaml:"pricing" mapstructure:"pricing"`
	Compact          CompactConfig          `yaml:"compact" mapstructure:"compact"`
	Web              WebConfig              `yaml:"web" mapstructure:"web"`
	ComputerUse      ComputerUseConfig      `yaml:"-" mapstructure:"-"`
	Channels         ChannelsConfig         `yaml:"-" mapstructure:"-"`
	Heartbeat        HeartbeatConfig        `yaml:"-" mapstructure:"-"`
	Prompts          PromptsConfig          `yaml:"-" mapstructure:"-"`
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

// ClaudeCodeConfig contains Claude Code CLI integration settings
type ClaudeCodeConfig struct {
	Enabled         bool   `yaml:"enabled" mapstructure:"enabled"`
	CLIPath         string `yaml:"cli_path" mapstructure:"cli_path"`
	Timeout         int    `yaml:"timeout" mapstructure:"timeout"`
	MaxOutputTokens int    `yaml:"max_output_tokens" mapstructure:"max_output_tokens"`
	ThinkingBudget  int    `yaml:"thinking_budget" mapstructure:"thinking_budget"`
	MaxTurns        int    `yaml:"max_turns" mapstructure:"max_turns"`
}

// SpeechToTextConfig contains speech-to-text (Whisper) integration settings.
// It is an opt-in feature flag: when Enabled, the /voice chat shortcut and
// inbound Telegram voice-message transcription become available. Transcription
// shells out to a local whisper.cpp binary (whisper-cli/whisper-cpp) and uses
// ffmpeg for audio capture and OGG->WAV conversion; the GGML model is
// downloaded on first use. None of this adds CGO to the Go binary.
type SpeechToTextConfig struct {
	Enabled             bool   `yaml:"enabled" mapstructure:"enabled"`
	Engine              string `yaml:"engine" mapstructure:"engine"`                               // "whisper.cpp" (only engine for now)
	BinaryPath          string `yaml:"binary_path" mapstructure:"binary_path"`                     // "" -> resolve whisper-cli/whisper-cpp on PATH
	Model               string `yaml:"model" mapstructure:"model"`                                 // "tiny" (->ggml-tiny.bin); base/small/medium/large/*.en accepted
	ModelsDir           string `yaml:"models_dir" mapstructure:"models_dir"`                       // "" -> ~/.infer/models/whisper
	Language            string `yaml:"language" mapstructure:"language"`                           // "" -> auto-detect
	AutoDownload        bool   `yaml:"auto_download" mapstructure:"auto_download"`                 // download model on first use if missing
	Timeout             int    `yaml:"timeout" mapstructure:"timeout"`                             // transcription timeout (seconds)
	MaxRecordingSeconds int    `yaml:"max_recording_seconds" mapstructure:"max_recording_seconds"` // chat /voice recording cap
	SilenceTimeout      int    `yaml:"silence_timeout" mapstructure:"silence_timeout"`             // stop /voice after N s of silence (0 = record full cap)
	FFmpegPath          string `yaml:"ffmpeg_path" mapstructure:"ffmpeg_path"`                     // "" -> resolve ffmpeg on PATH
	InputDevice         string `yaml:"input_device" mapstructure:"input_device"`                   // "" -> platform default mic
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
	Debug  bool   `yaml:"debug" mapstructure:"debug"`
	Dir    string `yaml:"dir" mapstructure:"dir"`
	Stdout bool   `yaml:"stdout" mapstructure:"stdout"`
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
	TodoWrite TodoWriteToolConfig `yaml:"todo_write" mapstructure:"todo_write"`
	Schedule  ScheduleToolConfig  `yaml:"schedule" mapstructure:"schedule"`

	Safety SafetyConfig `yaml:"safety" mapstructure:"safety"`
}

// BashToolConfig contains bash-specific tool settings
type BashToolConfig struct {
	Enabled          bool                   `yaml:"enabled" mapstructure:"enabled"`
	Timeout          int                    `yaml:"timeout" mapstructure:"timeout"`
	Mode             BashModesConfig        `yaml:"mode" mapstructure:"mode"`
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
	Enabled          bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval  *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
	StrictWhitespace bool  `yaml:"strict_whitespace" mapstructure:"strict_whitespace"`
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
	Enabled         bool              `yaml:"enabled" mapstructure:"enabled"`
	AllowedDomains  []string          `yaml:"allowed_domains" mapstructure:"allowed_domains"`
	Safety          FetchSafetyConfig `yaml:"safety" mapstructure:"safety"`
	Cache           FetchCacheConfig  `yaml:"cache" mapstructure:"cache"`
	RequireApproval *bool             `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
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

// ScheduleToolConfig contains schedule-specific tool settings.
// When enabled, the tool lets the LLM create recurring jobs that fire on a
// cron schedule and deliver their output through a configured channel
// (e.g. Telegram). Jobs are persisted as YAML files under StorageDir and
// hot-reloaded by the channels-manager daemon.
type ScheduleToolConfig struct {
	Enabled         bool   `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool  `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
	StorageDir      string `yaml:"storage_dir,omitempty" mapstructure:"storage_dir,omitempty"`
	MaxJobs         int    `yaml:"max_jobs,omitempty" mapstructure:"max_jobs,omitempty"`
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

// BashModeAllowConfig is the per-mode bash allow-list. Each entry is a Go regex
// matched against the WHOLE command (anchored as \A(?:entry)\z), so a bare token
// like "gh" matches only "gh" and never "gh issue list" - write "gh issue.*" to
// allow arguments. The single sentinel ".*" (or "^.*$"/".+") means "allow any
// single command" and additionally skips the clean-command guard, i.e. full
// autonomy (used by mode.auto so headless `infer agent` can act unattended).
type BashModeAllowConfig struct {
	Allow []string `yaml:"allow" mapstructure:"allow"`
}

// BashModesConfig holds the bash allow-list for each agent mode. The effective
// allow-list for a mode is mode.all.allow unioned with that mode's own list, so
// "all" is the baseline shared by every mode (standard, plan, auto). A command
// not matched by the effective list is default-denied: it falls through to
// approval in chat mode, or is rejected with an actionable reason in headless
// agent mode. There is no separate deny list - anything not allowed is denied.
type BashModesConfig struct {
	All      BashModeAllowConfig `yaml:"all" mapstructure:"all"`
	Plan     BashModeAllowConfig `yaml:"plan" mapstructure:"plan"`
	Standard BashModeAllowConfig `yaml:"standard" mapstructure:"standard"`
	Auto     BashModeAllowConfig `yaml:"auto" mapstructure:"auto"`
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

// ExportConfig contains settings for export command
type ExportConfig struct {
	OutputDir    string `yaml:"output_dir" mapstructure:"output_dir"`
	SummaryModel string `yaml:"summary_model" mapstructure:"summary_model"`
}

// CompactConfig contains conversation compaction settings
type CompactConfig struct {
	Enabled               bool `yaml:"enabled" mapstructure:"enabled"`
	AutoAt                int  `yaml:"auto_at" mapstructure:"auto_at"`
	KeepFirstMessages     int  `yaml:"keep_first_messages" mapstructure:"keep_first_messages"`
	RolloverOnIdleMinutes int  `yaml:"rollover_on_idle_minutes" mapstructure:"rollover_on_idle_minutes"`
	SummaryMaxTokens      int  `yaml:"summary_max_tokens" mapstructure:"summary_max_tokens"`
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
	InstallDir     string `yaml:"install_dir" mapstructure:"install_dir"`
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

// AgentContextConfig contains settings for agent context enrichment
type AgentContextConfig struct {
	GitContextEnabled      bool `yaml:"git_context_enabled" mapstructure:"git_context_enabled"`
	WorkingDirEnabled      bool `yaml:"working_dir_enabled" mapstructure:"working_dir_enabled"`
	GitContextRefreshTurns int  `yaml:"git_context_refresh_turns" mapstructure:"git_context_refresh_turns"`
}

// AgentSkillsConfig controls Agent Skills loading. Skills follow the
// SKILL.md / YAML-frontmatter contract shared by the official spec, so existing skill folders drop
// into .infer/skills/ unchanged. Disabled by default - when off, no
// scan runs and nothing is injected into the system prompt.
type AgentSkillsConfig struct {
	Enabled        bool     `yaml:"enabled" mapstructure:"enabled"`
	DisabledSkills []string `yaml:"disabled_skills,omitempty" mapstructure:"disabled_skills"`
}

// AgentConfig contains agent command-specific settings.
// All system prompts, custom instructions, and system reminder settings
// live in prompts.yaml and are read from cfg.Prompts.Agent.* at runtime.
type AgentConfig struct {
	Model                    string             `yaml:"model" mapstructure:"model"`
	SystemPromptWithDefaults bool               `yaml:"system_prompt_with_defaults" mapstructure:"system_prompt_with_defaults"`
	Context                  AgentContextConfig `yaml:"context" mapstructure:"context"`
	Skills                   AgentSkillsConfig  `yaml:"skills" mapstructure:"skills"`
	VerboseTools             bool               `yaml:"verbose_tools" mapstructure:"verbose_tools"`
	MaxTurns                 int                `yaml:"max_turns" mapstructure:"max_turns"`
	MaxTokens                int                `yaml:"max_tokens" mapstructure:"max_tokens"`
	MaxConcurrentTools       int                `yaml:"max_concurrent_tools" mapstructure:"max_concurrent_tools"`
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

// GitCommitMessageConfig contains settings for AI-generated commit
// messages. The system prompt lives in prompts.yaml under
// git.commit_message.system_prompt.
type GitCommitMessageConfig struct {
	Model string `yaml:"model" mapstructure:"model"`
}

// ConversationTitleConfig contains settings for AI-generated conversation
// titles. The system prompt lives in prompts.yaml under
// conversation.title_generation.system_prompt.
type ConversationTitleConfig struct {
	Enabled   bool   `yaml:"enabled" mapstructure:"enabled"`
	Model     string `yaml:"model" mapstructure:"model"`
	BatchSize int    `yaml:"batch_size" mapstructure:"batch_size"`
	Interval  int    `yaml:"interval" mapstructure:"interval"`
}

// ChatConfig contains chat interface settings
type ChatConfig struct {
	Theme       string            `yaml:"theme" mapstructure:"theme"`
	Keybindings KeybindingsConfig `yaml:"-" mapstructure:"-"`
	StatusBar   StatusBarConfig   `yaml:"status_bar" mapstructure:"status_bar"`
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
	StatusPollSeconds       int     `yaml:"status_poll_seconds" mapstructure:"status_poll_seconds"`
	PollingStrategy         string  `yaml:"polling_strategy" mapstructure:"polling_strategy"`
	InitialPollIntervalSec  int     `yaml:"initial_poll_interval_sec" mapstructure:"initial_poll_interval_sec"`
	MaxPollIntervalSec      int     `yaml:"max_poll_interval_sec" mapstructure:"max_poll_interval_sec"`
	BackoffMultiplier       float64 `yaml:"backoff_multiplier" mapstructure:"backoff_multiplier"`
	BackgroundMonitoring    bool    `yaml:"background_monitoring" mapstructure:"background_monitoring"`
	CompletedTaskRetention  int     `yaml:"completed_task_retention" mapstructure:"completed_task_retention"`
	AgentModeMaxWaitSeconds int     `yaml:"agent_mode_max_wait_seconds" mapstructure:"agent_mode_max_wait_seconds"`
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
		ClaudeCode: ClaudeCodeConfig{
			Enabled:         false,
			CLIPath:         "claude",
			Timeout:         600,
			MaxOutputTokens: 32000,
			ThinkingBudget:  10000,
			MaxTurns:        10,
		},
		SpeechToText: SpeechToTextConfig{
			Enabled:             false,
			Engine:              "whisper.cpp",
			BinaryPath:          "",
			Model:               "tiny",
			ModelsDir:           "",
			Language:            "",
			AutoDownload:        true,
			Timeout:             120,
			MaxRecordingSeconds: 30,
			SilenceTimeout:      2,
			FFmpegPath:          "",
			InputDevice:         "",
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
			Debug:  false,
			Dir:    "",
			Stdout: false,
		},
		Tools: ToolsConfig{
			Enabled: true,
			Sandbox: SandboxConfig{
				Directories: []string{".", "/tmp", ConfigDirName + "/tmp"},
				ProtectedPaths: []string{
					ConfigDirName + "/",
					".git/",
					"*.env",
				},
			},
			Bash: BashToolConfig{
				Enabled: true,
				Timeout: 120,
				Mode: BashModesConfig{
					// Baseline for EVERY mode: read-only / non-mutating commands.
					// Each entry is matched against the whole command, so " .*" lets
					// a command carry arguments. The clean-command guard still blocks
					// substitution, pipes/chains, redirects, dangerous find actions,
					// and printing an expanded $VAR (secret leak).
					All: BashModeAllowConfig{Allow: []string{
						`echo( .*)?`, `ls( .*)?`, `pwd( .*)?`, `tree( .*)?`,
						`wc( .*)?`, `sort( .*)?`, `uniq( .*)?`, `head( .*)?`, `tail( .*)?`,
						`task( .*)?`, `make( .*)?`, `find( .*)?`,
						`git status( .*)?`,
						`git branch( --show-current)?( -[alrvd])?`,
						`git log( .*)?`, `git diff( .*)?`, `git remote( -v)?`, `git show( .*)?`,
						`gh (issue|pr|repo|release|run|workflow) (list|view|status|diff|checks)( .*)?`,
						`gh auth status( .*)?`,
						`gh search (issues|code|prs|repos|commits)( .*)?`,
						`gh api [^ -][^ ]*( --paginate| --jq (?:'[^']*'|"[^"]*"|[^ ]+)| -q (?:'[^']*'|"[^"]*"|[^ ]+))*`,
					}},
					// Plan mode is read-only; it adds nothing beyond the baseline
					// (and the Bash tool is filtered out of plan mode entirely).
					Plan: BashModeAllowConfig{Allow: []string{}},
					// Standard mode is the interactive default: baseline + GitHub
					// writes. These publish, so the leak guard still applies.
					Standard: BashModeAllowConfig{Allow: []string{
						`gh issue (create|edit|comment)( .*)?`,
						`gh pr create( .*)?`,
						`gh project (item-add|item-edit|item-list|field-list|view|list)( .*)?`,
					}},
					// Auto mode is full autonomy: ".*" allows any single command and
					// skips the clean-command guard. This is what headless `infer
					// agent` runs under; tighten it to a curated list for CI secrets.
					Auto: BashModeAllowConfig{Allow: []string{`.*`}},
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
				Enabled:          true,
				RequireApproval:  &[]bool{true}[0],
				StrictWhitespace: false,
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
				Enabled:        true,
				AllowedDomains: []string{"golang.org", "localhost"},
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
			TodoWrite: TodoWriteToolConfig{
				Enabled:         true,
				RequireApproval: &[]bool{false}[0],
			},
			Schedule: ScheduleToolConfig{
				Enabled:         false,
				RequireApproval: &[]bool{true}[0],
				StorageDir:      "",
				MaxJobs:         100,
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
			Context: AgentContextConfig{
				GitContextEnabled:      true,
				WorkingDirEnabled:      true,
				GitContextRefreshTurns: 10,
			},
			Skills: AgentSkillsConfig{
				Enabled:        false,
				DisabledSkills: nil,
			},
			SystemPromptWithDefaults: true,
			VerboseTools:             false,
			MaxTurns:                 50,
			MaxTokens:                8192,
			MaxConcurrentTools:       5,
		},
		Git: GitConfig{
			CommitMessage: GitCommitMessageConfig{
				Model: "",
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
			},
		},
		Chat: ChatConfig{
			Theme: "tokyo-night",
			Keybindings: KeybindingsConfig{
				Enabled:  true,
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
				StatusPollSeconds:       5,
				PollingStrategy:         "exponential",
				InitialPollIntervalSec:  2,
				MaxPollIntervalSec:      60,
				BackoffMultiplier:       2.0,
				BackgroundMonitoring:    true,
				CompletedTaskRetention:  5,
				AgentModeMaxWaitSeconds: 300,
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
		Compact: CompactConfig{
			Enabled:               true,
			AutoAt:                80,
			KeepFirstMessages:     2,
			RolloverOnIdleMinutes: 30,
			SummaryMaxTokens:      1024,
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
				InstallDir:     "~/.local/bin",
			},
			Servers: []SSHServerConfig{},
		},
	}
}

// IsApprovalRequired checks if approval is required for a specific tool
// It returns true if tool-specific approval is set to true, or if global approval is true and tool-specific is not set to false
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
	case "TodoWrite":
		if c.Tools.TodoWrite.RequireApproval != nil {
			return *c.Tools.TodoWrite.RequireApproval
		}
	case "Schedule":
		if c.Tools.Schedule.RequireApproval != nil {
			return *c.Tools.Schedule.RequireApproval
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
	case "Screenshot", "MouseMove", "MouseClick", "MouseScroll", "KeyboardType", "GetFocusedApp", "ActivateApp", "GetLatestScreenshot":
		return false
	}

	return globalApproval
}

// IsA2AToolsEnabled checks if A2A tools should be enabled
// A2A tools are enabled when a2a.enabled is true, regardless of tools.enabled
func (c *Config) IsA2AToolsEnabled() bool {
	return c.A2A.Enabled
}

// IsClaudeCodeMode checks if Claude Code CLI mode is enabled
// When enabled, the CLI will use Claude Max/Pro subscription instead of gateway
func (c *Config) IsClaudeCodeMode() bool {
	return c.ClaudeCode.Enabled
}

// IsSpeechToTextEnabled checks if speech-to-text (Whisper) is enabled.
// When enabled, the /voice chat shortcut and Telegram voice-message
// transcription become available.
func (c *Config) IsSpeechToTextEnabled() bool {
	return c.SpeechToText.Enabled
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

func (c *Config) GetDefaultModel() string {
	return c.Agent.Model
}

func (c *Config) GetSandboxDirectories() []string {
	return c.Tools.Sandbox.Directories
}

// MergeSandboxDirectories appends directories from extra that are not already
// present, preserving order. Used to union the userspace (~/.infer) sandbox
// allowlist into the active config so global directories are not lost when a
// project config.yaml shadows the global one. Skills directories are reachable
// via the isWithinSkillsDir carve-out when agent.skills.enabled is set; this is
// for any other directory a user keeps allowed globally.
func (c *Config) MergeSandboxDirectories(extra []string) {
	seen := make(map[string]struct{}, len(c.Tools.Sandbox.Directories))
	for _, dir := range c.Tools.Sandbox.Directories {
		seen[dir] = struct{}{}
	}
	for _, dir := range extra {
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		c.Tools.Sandbox.Directories = append(c.Tools.Sandbox.Directories, dir)
	}
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

// ResolveConfigDir searches the standard project then userspace locations
// for an existing config.yaml and returns its directory. Falls back to the
// default project directory name when nothing is found on disk.
func ResolveConfigDir() string {
	candidates := []string{DefaultConfigPath}
	if homeDir, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(homeDir, ConfigDirName, ConfigFileName))
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return filepath.Dir(path)
		}
	}
	return ConfigDirName
}

// IsBashCommandAllowed (and the per-mode allow-list resolution) lives in
// bash_allowedlist.go, alongside the shell-aware clean-command guard (redirection
// stripping, compound-command splitting, command-substitution rejection) it
// relies on.

// ValidatePathInSandbox checks if a path is within the configured sandbox directories
func (c *Config) ValidatePathInSandbox(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// The skills carve-out is gated on agent.skills.enabled: when skills are off
	// (the default) the directory is not allowed and falls through to the
	// .infer/ protected-path check below. The tmp/plans carve-out stays unconditional.
	carveOut := (c.Agent.Skills.Enabled && isWithinSkillsDir(absPath)) ||
		isWithinConfigSubdir(absPath, "tmp", "plans")

	if err := c.checkProtectedPaths(path, carveOut); err != nil {
		return err
	}

	if carveOut {
		return nil
	}

	if len(c.Tools.Sandbox.Directories) == 0 {
		return nil
	}

	if c.ClaudeCode.Enabled {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			claudeDir := filepath.Join(homeDir, ".claude")
			if strings.HasPrefix(absPath, claudeDir) {
				return nil
			}
		}
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

// isWithinSkillsDir reports whether absPath lives inside either the project
// (./.infer/skills) or user-global (~/.infer/skills) skills directory. Feeds
// the carveOut path in ValidatePathInSandbox - gated there on
// agent.skills.enabled - so reads of SKILL.md and references/*.md succeed even
// though the broader .infer/ directory is in ProtectedPaths. File-level
// protections like *.env still apply.
func isWithinSkillsDir(absPath string) bool {
	dirs := make([]string, 0, 2)
	if projectDir, err := filepath.Abs(filepath.Join(ConfigDirName, "skills")); err == nil {
		dirs = append(dirs, projectDir)
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(homeDir, ConfigDirName, "skills"))
	}

	for _, dir := range dirs {
		if absPath == dir || strings.HasPrefix(absPath, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// isWithinConfigSubdir reports whether absPath lives inside one of the named
// subdirectories of the project config dir (./.infer/<name>). These are
// operational areas - tmp scratch, persisted plans - that stay reachable even
// though the rest of .infer/ is protected as a whole.
func isWithinConfigSubdir(absPath string, names ...string) bool {
	for _, name := range names {
		dir, err := filepath.Abs(filepath.Join(ConfigDirName, name))
		if err != nil {
			continue
		}
		if absPath == dir || strings.HasPrefix(absPath, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// checkProtectedPaths checks if a path matches any protected path patterns. When
// carveOut is set the path is an operational carve-out under the config dir
// (skills/tmp/plans), so the config-dir directory match is skipped while
// file-level protections (e.g. *.env, .git/) are still enforced.
func (c *Config) checkProtectedPaths(path string, carveOut bool) error {
	normalizedPath := filepath.ToSlash(filepath.Clean(path))

	for _, protectedPath := range c.Tools.Sandbox.ProtectedPaths {
		if carveOut && strings.TrimSuffix(protectedPath, "/") == ConfigDirName {
			continue
		}

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
	NamespaceDiffViewer   KeyNamespace = "diff_viewer"
	NamespaceExplorer     KeyNamespace = "explorer"
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
