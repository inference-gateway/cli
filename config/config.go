package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	ConfigDirName       = ".infer"
	AgentsDirName       = ".agents"
	ConfigFileName      = "config.yaml"
	GitignoreFileName   = ".gitignore"
	LogsDirName         = "logs"
	MemoryDirName       = "memory"
	MemoryIndexFileName = "MEMORY.md"

	DefaultConfigPath           = ConfigDirName + "/" + ConfigFileName
	DefaultLogsPath             = ConfigDirName + "/" + LogsDirName
	DefaultMemoryMaxChars       = 2000
	DefaultMemoryMaxEntryChars  = 2000
	DefaultSkillsMaxChars       = 4000
	DefaultInstructionsMaxChars = 8000
	DefaultInstructionsMaxLines = 399
)

// Config represents the CLI configuration
type Config struct {
	ContainerRuntime ContainerRuntimeConfig `yaml:"container_runtime" mapstructure:"container_runtime"`
	Gateway          GatewayConfig          `yaml:"gateway" mapstructure:"gateway"`
	SpeechToText     SpeechToTextConfig     `yaml:"speech_to_text" mapstructure:"speech_to_text"`
	Client           ClientConfig           `yaml:"client" mapstructure:"client"`
	Logging          LoggingConfig          `yaml:"logging" mapstructure:"logging"`
	Tools            ToolsConfig            `yaml:"tools" mapstructure:"tools"`
	Image            ImageConfig            `yaml:"image" mapstructure:"image"`
	Export           ExportConfig           `yaml:"export" mapstructure:"export"`
	Agent            AgentConfig            `yaml:"agent" mapstructure:"agent"`
	Git              GitConfig              `yaml:"git" mapstructure:"git"`
	Storage          StorageConfig          `yaml:"storage" mapstructure:"storage"`
	Telemetry        TelemetryConfig        `yaml:"telemetry" mapstructure:"telemetry"`
	Conversation     ConversationConfig     `yaml:"conversation" mapstructure:"conversation"`
	Chat             ChatConfig             `yaml:"chat" mapstructure:"chat"`
	A2A              A2AConfig              `yaml:"a2a" mapstructure:"a2a"`
	MCP              MCPConfig              `yaml:"mcp" mapstructure:"mcp"`
	Pricing          PricingConfig          `yaml:"pricing" mapstructure:"pricing"`
	ContextWindows   map[string]int         `yaml:"context_windows" mapstructure:"context_windows"`
	Compact          CompactConfig          `yaml:"compact" mapstructure:"compact"`
	Web              WebConfig              `yaml:"web" mapstructure:"web"`
	ComputerUse      ComputerUseConfig      `yaml:"-" mapstructure:"-"`
	Channels         ChannelsConfig         `yaml:"-" mapstructure:"-"`
	Heartbeat        HeartbeatConfig        `yaml:"-" mapstructure:"-"`
	Prompts          PromptsConfig          `yaml:"-" mapstructure:"-"`
	Reminders        RemindersConfig        `yaml:"-" mapstructure:"-"`
	Memory           MemoryConfig           `yaml:"-" mapstructure:"-"`
	Hooks            HooksConfig            `yaml:"-" mapstructure:"-"`
	Plugins          PluginsConfig          `yaml:"-" mapstructure:"-"`
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
	Mock             bool     `yaml:"mock,omitempty" mapstructure:"mock,omitempty"`
	StandaloneBinary bool     `yaml:"standalone_binary" mapstructure:"standalone_binary"`
	Debug            bool     `yaml:"debug,omitempty" mapstructure:"debug,omitempty"`
	IncludeModels    []string `yaml:"include_models,omitempty" mapstructure:"include_models,omitempty"`
	ExcludeModels    []string `yaml:"exclude_models,omitempty" mapstructure:"exclude_models,omitempty"`
	VisionEnabled    bool     `yaml:"vision_enabled" mapstructure:"vision_enabled"`
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
	RetainRecordings    int    `yaml:"retain_recordings" mapstructure:"retain_recordings"`         // keep last N inbound voice/audio files (0 = keep none)
	RecordingsDir       string `yaml:"recordings_dir" mapstructure:"recordings_dir"`               // "" -> ~/.infer/voice
}

// ResolveRecordingsDir returns the directory where retained inbound voice/audio
// recordings are stored, defaulting to ~/.infer/voice when RecordingsDir is unset.
func (c SpeechToTextConfig) ResolveRecordingsDir() (string, error) {
	if strings.TrimSpace(c.RecordingsDir) != "" {
		return c.RecordingsDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ConfigDirName, "voice"), nil
}

// ClientConfig contains HTTP client settings
type ClientConfig struct {
	Timeout           int         `yaml:"timeout" mapstructure:"timeout"`
	StallThresholdSec int         `yaml:"stall_threshold_sec" mapstructure:"stall_threshold_sec"`
	Retry             RetryConfig `yaml:"retry" mapstructure:"retry"`
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
	Debug   bool          `yaml:"debug" mapstructure:"debug"`
	Dir     string        `yaml:"dir" mapstructure:"dir"`
	Stdout  bool          `yaml:"stdout" mapstructure:"stdout"`
	Archive ArchiveConfig `yaml:"archive" mapstructure:"archive"`
}

// ArchiveConfig contains log archiving/rotation settings.
// When enabled, log files exceeding MaxSizeMB are automatically archived
// (compressed and renamed) to prevent unbounded disk usage.
type ArchiveConfig struct {
	Enabled   bool `yaml:"enabled" mapstructure:"enabled"`
	MaxSizeMB int  `yaml:"max_size_mb" mapstructure:"max_size_mb"`
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
	Enabled         bool                      `yaml:"enabled" mapstructure:"enabled"`
	Sandbox         SandboxConfig             `yaml:"sandbox" mapstructure:"sandbox"`
	Bash            BashToolConfig            `yaml:"bash" mapstructure:"bash"`
	Read            ReadToolConfig            `yaml:"read" mapstructure:"read"`
	Write           WriteToolConfig           `yaml:"write" mapstructure:"write"`
	Edit            EditToolConfig            `yaml:"edit" mapstructure:"edit"`
	Delete          DeleteToolConfig          `yaml:"delete" mapstructure:"delete"`
	Grep            GrepToolConfig            `yaml:"grep" mapstructure:"grep"`
	Tree            TreeToolConfig            `yaml:"tree" mapstructure:"tree"`
	WebFetch        WebFetchToolConfig        `yaml:"web_fetch" mapstructure:"web_fetch"`
	WebSearch       WebSearchToolConfig       `yaml:"web_search" mapstructure:"web_search"`
	TodoWrite       TodoWriteToolConfig       `yaml:"todo_write" mapstructure:"todo_write"`
	Schedule        ScheduleToolConfig        `yaml:"schedule" mapstructure:"schedule"`
	Agent           AgentToolConfig           `yaml:"agent" mapstructure:"agent"`
	AskUserQuestion AskUserQuestionToolConfig `yaml:"ask_user_question" mapstructure:"ask_user_question"`
	Wait            WaitToolConfig            `yaml:"wait" mapstructure:"wait"`

	// MaxResultBytes caps the size of a single tool result fed back to the LLM.
	// Oversized results are middle-truncated (head + tail kept) so one
	// pathological output (a huge Read/WebFetch/bash dump) can't dominate the
	// context window in a single turn. 0 disables the cap.
	MaxResultBytes int `yaml:"max_result_bytes" mapstructure:"max_result_bytes"`

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
	Enabled            bool `yaml:"enabled" mapstructure:"enabled"`
	MaxConcurrent      int  `yaml:"max_concurrent" mapstructure:"max_concurrent"`
	MaxOutputBufferMB  int  `yaml:"max_output_buffer_mb" mapstructure:"max_output_buffer_mb"`
	RetentionMinutes   int  `yaml:"retention_minutes" mapstructure:"retention_minutes"`
	CompletedRetention int  `yaml:"completed_retention" mapstructure:"completed_retention"`
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

// AskUserQuestionToolConfig contains AskUserQuestion-specific tool settings.
// The tool is read-only and plan-mode only, so it carries no approval flag.
type AskUserQuestionToolConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
}

// ScheduleToolConfig contains schedule-specific tool settings.
// When enabled, the tool lets the LLM create recurring jobs that fire on a
// cron schedule and deliver their output through a configured channel
// (e.g. Telegram). Jobs are persisted through the configured storage backend
// and hot-reloaded by the channels-manager daemon.
type ScheduleToolConfig struct {
	Enabled         bool  `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval *bool `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
	MaxJobs         int   `yaml:"max_jobs,omitempty" mapstructure:"max_jobs,omitempty"`
}

// WaitToolConfig contains settings for the Wait tool, which blocks inside a
// single tool execution until a condition is met (shells exit, file event,
// or check command succeeds), then returns once with the outcome.
type WaitToolConfig struct {
	Enabled               bool `yaml:"enabled" mapstructure:"enabled"`
	MaxTimeoutSeconds     int  `yaml:"max_timeout_seconds" mapstructure:"max_timeout_seconds"`
	CommandPollIntervalMs int  `yaml:"command_poll_interval_ms" mapstructure:"command_poll_interval_ms"`
}

// AgentToolConfig contains settings for the Agent tool, which spawns local
// subagents (each an `infer agent` subprocess) in parallel and folds their
// results back into the main context. Unlike the A2A tools it needs no agent
// server. Subagents run either headless (background) or interactive (in a tmux
// pane the user can watch).
type AgentToolConfig struct {
	Enabled            bool                   `yaml:"enabled" mapstructure:"enabled"`
	RequireApproval    *bool                  `yaml:"require_approval,omitempty" mapstructure:"require_approval,omitempty"`
	Mode               string                 `yaml:"mode" mapstructure:"mode"`                 // headless | interactive
	Wait               bool                   `yaml:"wait" mapstructure:"wait"`                 // false => async (fire-and-forget + notify)
	MaxParallel        int                    `yaml:"max_parallel" mapstructure:"max_parallel"` // cap on concurrent subagents per call
	MaxDepth           int                    `yaml:"max_depth" mapstructure:"max_depth"`       // recursion guard (a subagent is itself an `infer agent`)
	Model              string                 `yaml:"model,omitempty" mapstructure:"model,omitempty"`
	InheritMock        bool                   `yaml:"inherit_mock" mapstructure:"inherit_mock"` // propagate gateway.mock to spawned subagents
	Interactive        AgentInteractiveConfig `yaml:"interactive" mapstructure:"interactive"`
	CompletedRetention int                    `yaml:"completed_retention" mapstructure:"completed_retention"`
}

// AgentInteractiveConfig configures the tmux-backed interactive surface for
// subagents (used when mode is "interactive").
type AgentInteractiveConfig struct {
	Multiplexer string `yaml:"multiplexer" mapstructure:"multiplexer"` // tmux (only supported value)
	Layout      string `yaml:"layout" mapstructure:"layout"`           // vertical | horizontal | window
	Fallback    string `yaml:"fallback" mapstructure:"fallback"`       // headless | error (when not inside tmux)
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

// Approval-behaviour values for SafetyConfig.ApprovalBehaviour - they select HOW a
// tool that needs approval is delivered (see SafetyConfig for full semantics).
const (
	ApprovalBehaviourPrompt = "prompt"
	ApprovalBehaviourIPC    = "ipc"
	ApprovalBehaviourBlock  = "block"
)

// SafetyConfig contains safety approval settings
type SafetyConfig struct {
	RequireApproval bool `yaml:"require_approval" mapstructure:"require_approval"`
	// ApprovalBehaviour selects HOW a tool that needs approval is handled:
	//   "prompt" (default): ask an interactive approver via whatever channel is
	//       attached - a TUI prompt in chat, IPC under the channel-manager; if none
	//       is reachable (CI/heartbeat) the action is blocked with a reason.
	//   "ipc":   force stdin/stdout IPC approval; blocked if no IPC broker.
	//   "block": reject immediately with a reason, never ask.
	// It governs delivery only - whether a tool needs approval at all is decided by
	// RequireApproval / the per-tool require_approval override / the per-mode bash
	// allow-list. Resolve via ApprovalBehaviourFor; validated by Config.Validate.
	ApprovalBehaviour string `yaml:"approval_behaviour" mapstructure:"approval_behaviour"`
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
	Tmux                  bool              `yaml:"tmux" mapstructure:"tmux"`
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
	TreeEnabled            bool `yaml:"tree_enabled" mapstructure:"tree_enabled"`
}

// AgentSkillsConfig controls Agent Skills loading. Skills follow the
// SKILL.md / YAML-frontmatter contract shared by the official spec, so existing skill folders drop
// into .infer/skills/ unchanged. Enabled by default - disable via
// agent.skills.enabled=false in config. When off, no scan runs and
// nothing is injected into the system prompt.
type AgentSkillsConfig struct {
	Enabled        bool     `yaml:"enabled" mapstructure:"enabled"`
	DisabledSkills []string `yaml:"disabled_skills,omitempty" mapstructure:"disabled_skills"`
	MaxChars       int      `yaml:"max_chars" mapstructure:"max_chars"`
}

// AgentsMDConfig controls native injection of the project-root AGENTS.md
// into the system prompt.
type AgentsMDConfig struct {
	Enabled  bool `yaml:"enabled" mapstructure:"enabled"`
	MaxChars int  `yaml:"max_chars" mapstructure:"max_chars"`
	MaxLines int  `yaml:"max_lines" mapstructure:"max_lines"`
}

// AgentConfig contains agent command-specific settings.
// All system prompts, custom instructions, and system reminder settings
// live in prompts.yaml and are read from cfg.Prompts.Agent.* at runtime.
type AgentConfig struct {
	Model                    string             `yaml:"model" mapstructure:"model"`
	SystemPromptWithDefaults bool               `yaml:"system_prompt_with_defaults" mapstructure:"system_prompt_with_defaults"`
	Context                  AgentContextConfig `yaml:"context" mapstructure:"context"`
	Skills                   AgentSkillsConfig  `yaml:"skills" mapstructure:"skills"`
	AgentsMD                 AgentsMDConfig     `yaml:"agents_md" mapstructure:"agents_md"`
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
	Enabled               bool           `yaml:"enabled" mapstructure:"enabled"`
	Agents                []string       `yaml:"agents,omitempty" mapstructure:"agents"`
	LivenessProbeEnabled  bool           `yaml:"liveness_probe_enabled,omitempty" mapstructure:"liveness_probe_enabled,omitempty"`
	LivenessProbeInterval int            `yaml:"liveness_probe_interval,omitempty" mapstructure:"liveness_probe_interval,omitempty"`
	Cache                 A2ACacheConfig `yaml:"cache" mapstructure:"cache"`
	Task                  A2ATaskConfig  `yaml:"task" mapstructure:"task"`
	Tools                 A2AToolsConfig `yaml:"tools" mapstructure:"tools"`
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
	Theme         string            `yaml:"theme" mapstructure:"theme"`
	Keybindings   KeybindingsConfig `yaml:"-" mapstructure:"-"`
	StatusBar     StatusBarConfig   `yaml:"status_bar" mapstructure:"status_bar"`
	InputMaxLines int               `yaml:"input_max_lines" mapstructure:"input_max_lines"`
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
	GitPR            bool `yaml:"git_pr" mapstructure:"git_pr"`
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
	StorageTypeD1       StorageType = "d1"
)

// TelemetryConfig controls the OpenTelemetry metrics the CLI records - tool
// outcomes, token usage, sessions. The recorded data is written as OTLP/semconv
// JSON under <config-dir>/telemetry (always, private - no prompt/response
// content) and, opt-in, exported to an OpenTelemetry collector. OTLP export
// activates only when otlp.endpoint (or OTEL_EXPORTER_OTLP_ENDPOINT) is set.
// Named "telemetry" (not "metrics") to leave room for traces/logs later.
type TelemetryConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// RetentionDays is how long a session's telemetry file stays active before
	// `infer stats` archives it. 0 disables archiving.
	RetentionDays   int        `yaml:"retention_days" mapstructure:"retention_days"`
	OTLP            OTLPConfig `yaml:"otlp" mapstructure:"otlp"`
	ReceiverAddress string     `yaml:"receiver_address" mapstructure:"receiver_address"`
	// AttrSessionIDKey / AttrToolCallIDKey are the baggage member names injected
	// into subprocess env (BAGGAGE) and outgoing HTTP requests. The defaults are
	// the OTel semconv attributes and must match what the consumer (e.g. the ADK)
	// reads; the byte-for-byte cross-repo contract is these member names, not the
	// config/env names.
	AttrSessionIDKey  string `yaml:"attr_session_id_key" mapstructure:"attr_session_id_key"`
	AttrToolCallIDKey string `yaml:"attr_tool_call_id_key" mapstructure:"attr_tool_call_id_key"`
}

// OTLPConfig configures the optional OTLP export. When Endpoint is empty (and
// OTEL_EXPORTER_OTLP_ENDPOINT is unset) no exporter is initialized and nothing
// leaves the machine.
type OTLPConfig struct {
	// Endpoint is the OTLP/HTTP collector base URL (e.g. http://localhost:4318).
	// Empty disables export. Falls back to OTEL_EXPORTER_OTLP_ENDPOINT.
	Endpoint string `yaml:"endpoint" mapstructure:"endpoint"`
	// Headers are sent on every export request (e.g. auth tokens).
	Headers map[string]string `yaml:"headers,omitempty" mapstructure:"headers,omitempty"`
	// Interval is the periodic export interval in seconds (default 60).
	Interval int `yaml:"interval" mapstructure:"interval"`
}

// StorageConfig contains storage backend configuration
type StorageConfig struct {
	Enabled  bool                  `yaml:"enabled" mapstructure:"enabled"`
	Type     StorageType           `yaml:"type" mapstructure:"type"`
	SQLite   SQLiteStorageConfig   `yaml:"sqlite,omitempty" mapstructure:"sqlite,omitempty"`
	Postgres PostgresStorageConfig `yaml:"postgres,omitempty" mapstructure:"postgres,omitempty"`
	Redis    RedisStorageConfig    `yaml:"redis,omitempty" mapstructure:"redis,omitempty"`
	Jsonl    JsonlStorageConfig    `yaml:"jsonl,omitempty" mapstructure:"jsonl,omitempty"`
	D1       D1StorageConfig       `yaml:"d1,omitempty" mapstructure:"d1,omitempty"`
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

// D1StorageConfig contains Cloudflare D1-specific configuration. D1 is SQLite
// over an HTTP query API; the api_token is a secret and is normally injected
// via INFER_STORAGE_D1_API_TOKEN rather than written to the config file.
type D1StorageConfig struct {
	AccountID  string `yaml:"account_id" mapstructure:"account_id"`
	DatabaseID string `yaml:"database_id" mapstructure:"database_id"`
	APIToken   string `yaml:"api_token" mapstructure:"api_token"`
	BaseURL    string `yaml:"base_url,omitempty" mapstructure:"base_url,omitempty"`
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
			GitPR:            true,
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
			RetainRecordings:    0,
			RecordingsDir:       "",
		},
		Client: ClientConfig{
			Timeout:           200,
			StallThresholdSec: 30,
			Retry: RetryConfig{
				Enabled:              true,
				MaxAttempts:          5,
				InitialBackoffSec:    5,
				MaxBackoffSec:        60,
				BackoffMultiplier:    2,
				RetryableStatusCodes: []int{408, 429, 500, 502, 503, 504},
			},
		},
		Logging: LoggingConfig{
			Debug:  false,
			Dir:    "",
			Stdout: false,
			Archive: ArchiveConfig{
				Enabled:   true,
				MaxSizeMB: 1024,
			},
		},
		Tools: ToolsConfig{
			Enabled:        true,
			MaxResultBytes: 250000,
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
					All: BashModeAllowConfig{Allow: []string{
						`echo( .*)?`, `ls( .*)?`, `pwd( .*)?`, `tree( .*)?`,
						`wc( .*)?`, `sort( .*)?`, `uniq( .*)?`, `head( .*)?`, `tail( .*)?`,
						`task( .*)?`, `make( .*)?`, `find( .*)?`, `sleep( .*)?`,
						`git status( .*)?`,
						`git branch( --show-current)?( -[alrvd])?`,
						`git log( .*)?`, `git diff( .*)?`, `git remote( -v)?`, `git show( .*)?`,
						`gh (issue|pr|repo|release|run|workflow) (list|view|status|diff|checks)( .*)?`,
						`gh auth status( .*)?`,
						`gh search (issues|code|prs|repos|commits)( .*)?`,
						`gh project (list|view|item-list|field-list)( .*)?`,
					}},
					Plan:     BashModeAllowConfig{Allow: []string{}},
					Standard: BashModeAllowConfig{Allow: []string{}},
					Auto:     BashModeAllowConfig{Allow: []string{`.*`}},
				},
				BackgroundShells: BackgroundShellsConfig{
					Enabled:            true,
					MaxConcurrent:      5,
					MaxOutputBufferMB:  10,
					RetentionMinutes:   60,
					CompletedRetention: 5,
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
				MaxJobs:         100,
			},
			AskUserQuestion: AskUserQuestionToolConfig{
				Enabled: true,
			},
			Wait: WaitToolConfig{
				Enabled:               true,
				MaxTimeoutSeconds:     600,
				CommandPollIntervalMs: 2000,
			},
			Agent: AgentToolConfig{
				Enabled:            true,
				RequireApproval:    &[]bool{true}[0],
				Mode:               "interactive",
				Wait:               true,
				MaxParallel:        4,
				MaxDepth:           1,
				InheritMock:        true,
				CompletedRetention: 5,
				Interactive: AgentInteractiveConfig{
					Multiplexer: "tmux",
					Layout:      "vertical",
					Fallback:    "headless",
				},
			},
			Safety: SafetyConfig{
				RequireApproval:   true,
				ApprovalBehaviour: ApprovalBehaviourPrompt,
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
				TreeEnabled:            true,
			},
			Skills: AgentSkillsConfig{
				Enabled:        true,
				DisabledSkills: nil,
				MaxChars:       DefaultSkillsMaxChars,
			},
			AgentsMD: AgentsMDConfig{
				Enabled:  true,
				MaxChars: DefaultInstructionsMaxChars,
				MaxLines: DefaultInstructionsMaxLines,
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
			D1: D1StorageConfig{
				AccountID:  "",
				DatabaseID: "",
				APIToken:   "",
				BaseURL:    "https://api.cloudflare.com/client/v4",
			},
		},
		Telemetry: TelemetryConfig{
			Enabled:       true,
			RetentionDays: 7,
			OTLP: OTLPConfig{
				Endpoint: "",
				Interval: 60,
			},
			AttrSessionIDKey:  "session.id",
			AttrToolCallIDKey: "gen_ai.tool.call.id",
		},
		Conversation: ConversationConfig{
			TitleGeneration: ConversationTitleConfig{
				Enabled:   true,
				Model:     "",
				BatchSize: 10,
			},
		},
		Chat: ChatConfig{
			Theme: "",
			Keybindings: KeybindingsConfig{
				Enabled:  true,
				Bindings: GetDefaultKeybindings(),
			},
			StatusBar:     GetDefaultStatusBarConfig(),
			InputMaxLines: 20,
		},
		A2A: A2AConfig{
			Enabled:               true,
			LivenessProbeEnabled:  true,
			LivenessProbeInterval: 30,
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
			Tmux:                  true,
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
		return false
	case "Schedule":
		if c.Tools.Schedule.RequireApproval != nil {
			return *c.Tools.Schedule.RequireApproval
		}
	case "Agent":
		if c.Tools.Agent.RequireApproval != nil {
			return *c.Tools.Agent.RequireApproval
		}
	case "ListSubagents", "GetSubagentResult", "ReadSubagentScreen":
		return false
	case "ApproveSubagent":
		return true
	case "RequestPlanApproval":
		return false
	case "AskUserQuestion":
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
	case "Wait":
		return false
	case "Memory":
		return false
	case "Screenshot", "MouseMove", "MouseClick", "MouseScroll", "KeyboardType", "GetFocusedApp", "ActivateApp", "GetLatestScreenshot":
		return false
	}

	return globalApproval
}

// ApprovalBehaviourFor returns how an approval-requiring action should be
// delivered for toolName: one of ApprovalBehaviourPrompt (default),
// ApprovalBehaviourIPC, or ApprovalBehaviourBlock. It reads the global
// tools.safety.approval_behaviour; toolName is reserved for a future per-tool
// override. An empty or unrecognised value resolves to the safe default
// (prompt). This decides HOW to handle an action that needs approval;
// IsApprovalRequired decides WHETHER it does.
func (c *Config) ApprovalBehaviourFor(toolName string) string {
	switch c.Tools.Safety.ApprovalBehaviour {
	case ApprovalBehaviourBlock, ApprovalBehaviourIPC, ApprovalBehaviourPrompt:
		return c.Tools.Safety.ApprovalBehaviour
	default:
		return ApprovalBehaviourPrompt
	}
}

// Validate checks cross-cutting config invariants after load so a typo fails fast
// instead of silently falling back. It currently validates
// tools.safety.approval_behaviour; extend it as new validated settings are added.
func (c *Config) Validate() error {
	switch c.Tools.Safety.ApprovalBehaviour {
	case "", ApprovalBehaviourPrompt, ApprovalBehaviourIPC, ApprovalBehaviourBlock:
	default:
		return fmt.Errorf(
			"invalid tools.safety.approval_behaviour %q: must be one of %q, %q, or %q",
			c.Tools.Safety.ApprovalBehaviour,
			ApprovalBehaviourPrompt, ApprovalBehaviourIPC, ApprovalBehaviourBlock,
		)
	}

	if c.SpeechToText.RetainRecordings < 0 {
		return fmt.Errorf(
			"invalid speech_to_text.retain_recordings %d: must be >= 0",
			c.SpeechToText.RetainRecordings,
		)
	}

	if err := c.Memory.Validate(); err != nil {
		return err
	}

	if err := c.Reminders.Validate(); err != nil {
		return fmt.Errorf("invalid reminders: %w", err)
	}
	if err := c.Hooks.Validate(); err != nil {
		return fmt.Errorf("invalid hooks: %w", err)
	}
	return nil
}

// IsA2AToolsEnabled checks if A2A tools should be enabled
// A2A tools are enabled when a2a.enabled is true, regardless of tools.enabled
func (c *Config) IsA2AToolsEnabled() bool {
	return c.A2A.Enabled
}

// IsAgentToolEnabled reports whether the Agent tool (local subagents) is on.
func (c *Config) IsAgentToolEnabled() bool {
	return c.Tools.Enabled && c.Tools.Agent.Enabled
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

// ResolveMemoryDir resolves the directory that holds the memory index
// (MEMORY.md) and the fact-files. It honors an explicit Memory.Dir override
// and otherwise defaults to the global userspace ~/.infer/memory. The store is
// shared across all projects: global facts live at the root and project facts
// under a per-project subdirectory (<project-slug>/<slug>.md).
func (c *Config) ResolveMemoryDir() (string, error) {
	if strings.TrimSpace(c.Memory.Dir) != "" {
		return c.Memory.Dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ConfigDirName, MemoryDirName), nil
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

// TelemetryDir is the userspace telemetry store (~/.infer/telemetry). Telemetry
// is user-global: recorded and read here regardless of the working directory or
// any project-local .infer, so `infer stats` sees every session in one place.
// Falls back to a relative path only when $HOME is unknown.
func TelemetryDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(ConfigDirName, "telemetry")
	}
	return filepath.Join(home, ConfigDirName, "telemetry")
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

	carveOut := (c.Agent.Skills.Enabled && isWithinSkillsDir(absPath)) ||
		(c.Plugins.Enabled && c.isWithinPluginsDir(absPath)) ||
		c.isWithinConfigSubdir(absPath, "tmp", "plans") ||
		isWithinMemoryDir(absPath, c.Memory)

	if err := c.checkProtectedPaths(path, carveOut); err != nil {
		return err
	}

	if carveOut {
		return nil
	}

	if len(c.Tools.Sandbox.Directories) == 0 {
		return nil
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

// isWithinSkillsDir reports whether absPath lives inside one of the skills
// directories: the project (./.infer/skills), the open-standard
// (./.agents/skills), or the user-global (~/.infer/skills) location. Feeds
// the carveOut path in ValidatePathInSandbox - gated there on
// agent.skills.enabled - so reads of SKILL.md and references/*.md succeed even
// though the broader .infer/ directory is in ProtectedPaths. File-level
// protections like *.env still apply.
func isWithinSkillsDir(absPath string) bool {
	dirs := make([]string, 0, 3)
	if projectDir, err := filepath.Abs(filepath.Join(ConfigDirName, "skills")); err == nil {
		dirs = append(dirs, projectDir)
	}
	if agentsDir, err := filepath.Abs(filepath.Join(AgentsDirName, "skills")); err == nil {
		dirs = append(dirs, agentsDir)
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
// subdirectories of the config dir. It checks both the project-relative
// ConfigDirName (./.infer/<name>) and the resolved config dir
// (GetConfigDir()/<name>) so that operational areas - tmp scratch, persisted
// plans - stay reachable even when the config was loaded from the userspace
// location (~/.infer). This keeps the rest of .infer/ protected as a whole.
func (c *Config) isWithinConfigSubdir(absPath string, names ...string) bool {
	configDirs := []string{ConfigDirName}
	if resolved := c.GetConfigDir(); resolved != "" && resolved != ConfigDirName {
		configDirs = append(configDirs, resolved)
	}

	for _, name := range names {
		for _, base := range configDirs {
			dir, err := filepath.Abs(filepath.Join(base, name))
			if err != nil {
				continue
			}
			if absPath == dir || strings.HasPrefix(absPath, dir+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// isWithinPluginsDir reports whether absPath lives inside the plugins
// storage root, so plugin SKILL.md bodies stay readable even though the
// broader .infer/ directory is protected.
func (c *Config) isWithinPluginsDir(absPath string) bool {
	dir, err := c.Plugins.ResolveDir()
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return absPath == absDir || strings.HasPrefix(absPath, absDir+string(filepath.Separator))
}

// isWithinMemoryDir reports whether absPath lives inside the global memory
// directory (~/.infer/memory or the configured Memory.Dir override), so reads of
// memory fact-files succeed even though .infer/ is otherwise protected. Gated on
// Memory.Enabled. The Memory tool itself writes via its own atomic writer rather
// than the sandboxed file writer, so this carve-out mainly governs manual reads.
func isWithinMemoryDir(absPath string, m MemoryConfig) bool {
	if !m.Enabled {
		return false
	}
	dir := m.Dir
	if strings.TrimSpace(dir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		dir = filepath.Join(home, ConfigDirName, MemoryDirName)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return absPath == absDir || strings.HasPrefix(absPath, absDir+string(filepath.Separator))
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
