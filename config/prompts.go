package config

import (
	configutils "github.com/inference-gateway/cli/config/utils"
)

const (
	PromptsFileName    = "prompts.yaml"
	DefaultPromptsPath = ConfigDirName + "/" + PromptsFileName
)

// LoadPrompts reads prompts.yaml from disk. When the file is missing it
// returns the in-code defaults so callers can treat absence as "use
// defaults" without special-casing. The file body is run through
// os.ExpandEnv - any literal `${…}` token in a customised prompt must be
// escaped as `$$…`.
//
// Any prompt left empty in a partial prompts.yaml is backfilled from
// DefaultPromptsConfig(). CustomInstructions is intentionally excluded from
// backfill - empty is a meaningful user choice there.
func LoadPrompts(path string) (*PromptsConfig, error) {
	cfg, err := configutils.LoadYAML(path, "prompts", DefaultPromptsConfig)
	if err != nil {
		return nil, err
	}
	mergePromptDefaults(cfg, DefaultPromptsConfig())
	return cfg, nil
}

func mergePromptDefaults(loaded, defaults *PromptsConfig) {
	if loaded.Agent.SystemPrompt == "" {
		loaded.Agent.SystemPrompt = defaults.Agent.SystemPrompt
	}
	if loaded.Agent.SystemPromptRemote == "" {
		loaded.Agent.SystemPromptRemote = defaults.Agent.SystemPromptRemote
	}
	if loaded.Agent.SystemPromptHeartbeat == "" {
		loaded.Agent.SystemPromptHeartbeat = defaults.Agent.SystemPromptHeartbeat
	}
	if loaded.Git.CommitMessage.SystemPrompt == "" {
		loaded.Git.CommitMessage.SystemPrompt = defaults.Git.CommitMessage.SystemPrompt
	}
	if loaded.Conversation.TitleGeneration.SystemPrompt == "" {
		loaded.Conversation.TitleGeneration.SystemPrompt = defaults.Conversation.TitleGeneration.SystemPrompt
	}
	if loaded.Init.Prompt == "" {
		loaded.Init.Prompt = defaults.Init.Prompt
	}
	if loaded.Vision.Annotator.ScreenSystemPrompt == "" {
		loaded.Vision.Annotator.ScreenSystemPrompt = defaults.Vision.Annotator.ScreenSystemPrompt
	}
	if loaded.Vision.Annotator.SceneSystemPrompt == "" {
		loaded.Vision.Annotator.SceneSystemPrompt = defaults.Vision.Annotator.SceneSystemPrompt
	}
}

// SavePrompts writes the prompts configuration to disk, creating any
// missing parent directories.
func SavePrompts(path string, cfg *PromptsConfig) error {
	return configutils.SaveYAML(path, "prompts", cfg)
}

// PromptsConfig holds every customisable LLM prompt the CLI ships with.
// It mirrors the nested key structure those prompts had when they lived
// under .infer/config.yaml so users can move existing values verbatim.
type PromptsConfig struct {
	Agent        PromptsAgentConfig        `yaml:"agent" mapstructure:"agent"`
	Git          PromptsGitConfig          `yaml:"git" mapstructure:"git"`
	Conversation PromptsConversationConfig `yaml:"conversation" mapstructure:"conversation"`
	Init         PromptsInitConfig         `yaml:"init" mapstructure:"init"`
	Vision       PromptsVisionConfig       `yaml:"vision" mapstructure:"vision"`
}

// PromptsVisionConfig holds the image annotator task prompts.
type PromptsVisionConfig struct {
	Annotator PromptsVisionAnnotatorConfig `yaml:"annotator" mapstructure:"annotator"`
}

// PromptsVisionAnnotatorConfig carries the two built-in annotation task
// prompts: UI-element detection for the screen source, general scene
// description for everything else. Directory sources can override per-source
// via vision.sources.<name>.prompt in config.yaml.
type PromptsVisionAnnotatorConfig struct {
	ScreenSystemPrompt string `yaml:"screen_system_prompt" mapstructure:"screen_system_prompt"`
	SceneSystemPrompt  string `yaml:"scene_system_prompt" mapstructure:"scene_system_prompt"`
}

type PromptsAgentConfig struct {
	SystemPrompt          string `yaml:"system_prompt" mapstructure:"system_prompt"`
	SystemPromptRemote    string `yaml:"system_prompt_remote" mapstructure:"system_prompt_remote"`
	SystemPromptHeartbeat string `yaml:"system_prompt_heartbeat" mapstructure:"system_prompt_heartbeat"`
	CustomInstructions    string `yaml:"custom_instructions" mapstructure:"custom_instructions"`
	ModeAdjustmentPlan    string `yaml:"mode_adjustment_plan,omitempty" mapstructure:"mode_adjustment_plan"`
	ModeAdjustmentAuto    string `yaml:"mode_adjustment_auto,omitempty" mapstructure:"mode_adjustment_auto"`
}

type PromptsGitConfig struct {
	CommitMessage PromptsGitCommitMessageConfig `yaml:"commit_message" mapstructure:"commit_message"`
}

type PromptsGitCommitMessageConfig struct {
	SystemPrompt string `yaml:"system_prompt" mapstructure:"system_prompt"`
}

type PromptsConversationConfig struct {
	TitleGeneration PromptsConversationTitleConfig `yaml:"title_generation" mapstructure:"title_generation"`
}

type PromptsConversationTitleConfig struct {
	SystemPrompt string `yaml:"system_prompt" mapstructure:"system_prompt"`
}

type PromptsInitConfig struct {
	Prompt string `yaml:"prompt" mapstructure:"prompt"`
}

// DefaultPromptsConfig returns the in-code default prompts. This is the
// single source of truth - `infer init` seeds prompts.yaml from this and
// the runtime overlay falls back to it when fields are missing.
func DefaultPromptsConfig() *PromptsConfig { //nolint:funlen
	return &PromptsConfig{
		Agent: PromptsAgentConfig{
			SystemPrompt: `Autonomous software engineering agent. Execute tasks iteratively until completion. For GitHub operations (issues, pull requests, releases, the API), use the gh CLI via the Bash tool - there is no built-in GitHub tool. When the user types "#N" in chat (e.g. "#123"), the CLI pre-fetches that issue and inlines its title, body, and recent comments before sending; do NOT re-fetch those issues via gh - use the inlined content directly unless the user explicitly asks for fresher data.`,
			SystemPromptRemote: `Remote-control assistant. You are responding through a messaging channel (e.g. Telegram).

STYLE:
- Reply concisely. Match the user's tone and length.
- For casual messages ("hi", "thanks"), respond in one short line.
- Skip preamble, recaps, and tool-availability lists.

CAPABILITIES:
- You have full agent tools (Bash, Read, Write, Edit, etc.) plus any configured MCP/A2A tools.
- Use them only when the user asks for work that requires them.
- For greetings or open-ended questions, just chat - do not run tools.

CONSTRAINTS:
- Each message starts a fresh session; do not assume prior context unless it appears in the conversation history.
- Tool approval may be enforced by the channel manager - long approval chains are noisy in a chat UI, so prefer single, well-scoped tool calls.`,
			SystemPromptHeartbeat: `You are an autonomous agent that has just been woken up by a periodic heartbeat tick.

PURPOSE: Self-driven progress checks. The user did not just send a message - you were woken up on a schedule to inspect persistent state and take any action that has become possible or overdue since the last tick.

WHAT TO CHECK (in order):
1. Pending todos in your conversation history (TodoWrite items not yet completed).
2. Background tasks you previously started (long-running shells, scheduled jobs, A2A tasks).
3. External signals you have explicit instructions to monitor (issues, PRs, queues - only if user-configured).

DECISION RULE:
- If nothing actionable is pending, respond briefly with "no action needed" and stop. Do NOT invent work.
- If exactly one thing is pending, take the next concrete step using your tools.
- If multiple things are pending, pick the highest-priority single item and do that - leave the rest for the next tick.

CONSTRAINTS:
- You run autonomously without human approval. Be conservative: prefer read-only inspection over irreversible changes unless the action was already authorised.
- Never spam channels or open noisy artifacts (PRs, issues) on a heartbeat unless the user has set up explicit instructions for that behaviour.
- Each tick is a fresh session - you have no memory of previous ticks beyond what is persisted (todos, scheduled jobs, conversation history).`,
			CustomInstructions: ``,
		},
		Git: PromptsGitConfig{
			CommitMessage: PromptsGitCommitMessageConfig{
				SystemPrompt: `Generate a concise git commit message following conventional commit format.

REQUIREMENTS:
- MUST use format: "type(scope): brief description"
- Scope MUST name the domain being worked on (the package, module, or feature area inferred from the diff)
- MUST be under 50 characters total
- MUST use imperative mood (e.g., "add", "fix", "update", "refactor")
- Types: feat, fix, docs, style, refactor, test, chore

EXAMPLES:
- "feat(shortcuts): add git shortcut with AI commits"
- "fix(container): resolve build error in container"
- "docs(readme): update installation guide"
- "refactor(examples): simplify error handling"

Respond with ONLY the commit message, no quotes or explanation.`,
			},
		},
		Vision: PromptsVisionConfig{
			Annotator: PromptsVisionAnnotatorConfig{
				ScreenSystemPrompt: `You are a UI screen annotator. Describe the screenshot for an agent that cannot see it: what application/screen is shown and which interactive elements exist (buttons, links, text fields, menus, checkboxes, tabs), including OS chrome: the macOS Dock and its app icons (name each app you recognize by its icon), the menu bar, and desktop icons. Read visible text exactly. Be precise about element positions.`,
				SceneSystemPrompt:  `You are a scene annotator. Describe the image for an agent that cannot see it: what the scene shows and the notable objects, people, and text in it. Be factual and concise.`,
			},
		},
		Conversation: PromptsConversationConfig{
			TitleGeneration: PromptsConversationTitleConfig{
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
		Init: PromptsInitConfig{
			Prompt: `Generate an AGENTS.md at the project root following the open standard at https://agents.md.

AGENTS.md is a README for coding agents - a predictable place for the context and instructions a new contributor would need. It complements (not duplicates) README.md.

Guidelines:
- Keep it concise - aim for ~400 words. Prefer signal over completeness.
- Use standard Markdown with whatever headings fit the project; there is no required structure.
- Cover what actually matters for an agent to be productive: build/test/lint commands, code style, testing, security gotchas, and any non-obvious conventions. Skip anything obvious from the file tree.
- Be specific: real commands, real file paths, real constraints. No filler.

Briefly inspect the project (build system, config files, existing docs) to ground the content, then write the file.`,
		},
	}
}
