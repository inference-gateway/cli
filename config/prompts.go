package config

import (
	yaml "gopkg.in/yaml.v3"

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
// Any field left empty in a partial prompts.yaml is backfilled from
// DefaultPromptsConfig() so callers always get a fully populated config.
// CustomInstructions is intentionally excluded from backfill - empty is
// a meaningful user choice there.
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
	// agent.mode_adjustment_plan/_auto are deliberately NOT backfilled: they
	// are optional per-mode overrides and their built-in defaults live in the
	// mode-change reminder guidance (config/reminders.go).
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
	mergeToolDefaults(&loaded.Tools, &defaults.Tools)
}

// mergeToolDefaults backfills any tool description left empty in the
// loaded prompts.yaml from the in-code defaults. A user can therefore
// override a single tool description without losing every other one.
func mergeToolDefaults(loaded, defaults *PromptsToolsConfig) {
	mergeToolDescription(&loaded.Bash, &defaults.Bash)
	mergeToolDescription(&loaded.BashOutput, &defaults.BashOutput)
	mergeToolDescription(&loaded.KillShell, &defaults.KillShell)
	mergeToolDescription(&loaded.ListShells, &defaults.ListShells)
	mergeToolDescription(&loaded.Read, &defaults.Read)
	mergeToolDescription(&loaded.Write, &defaults.Write)
	mergeToolDescription(&loaded.Edit, &defaults.Edit)
	mergeToolDescription(&loaded.MultiEdit, &defaults.MultiEdit)
	mergeToolDescription(&loaded.Delete, &defaults.Delete)
	mergeToolDescription(&loaded.Grep, &defaults.Grep)
	mergeToolDescription(&loaded.Tree, &defaults.Tree)
	mergeToolDescription(&loaded.TodoWrite, &defaults.TodoWrite)
	mergeToolDescription(&loaded.RequestPlanApproval, &defaults.RequestPlanApproval)
	mergeToolDescription(&loaded.AskUserQuestion, &defaults.AskUserQuestion)
	mergeToolDescription(&loaded.WebFetch, &defaults.WebFetch)
	mergeToolDescription(&loaded.WebSearch, &defaults.WebSearch)
	mergeToolDescription(&loaded.Schedule, &defaults.Schedule)
	mergeToolDescription(&loaded.Agent, &defaults.Agent)
	mergeToolDescription(&loaded.ListSubagents, &defaults.ListSubagents)
	mergeToolDescription(&loaded.GetSubagentResult, &defaults.GetSubagentResult)
	mergeToolDescription(&loaded.CloseSubagent, &defaults.CloseSubagent)
	mergeToolDescription(&loaded.ReadSubagentScreen, &defaults.ReadSubagentScreen)
	mergeToolDescription(&loaded.SendSubagentInput, &defaults.SendSubagentInput)
	mergeToolDescription(&loaded.ApproveSubagent, &defaults.ApproveSubagent)
	mergeToolDescription(&loaded.A2AQueryAgent, &defaults.A2AQueryAgent)
	mergeToolDescription(&loaded.A2AQueryTask, &defaults.A2AQueryTask)
	mergeToolDescription(&loaded.A2ASubmitTask, &defaults.A2ASubmitTask)
	mergeToolDescription(&loaded.Computer, &defaults.Computer)
	mergeToolDescription(&loaded.BrowserNavigate, &defaults.BrowserNavigate)
	mergeToolDescription(&loaded.BrowserClick, &defaults.BrowserClick)
	mergeToolDescription(&loaded.BrowserType, &defaults.BrowserType)
	mergeToolDescription(&loaded.BrowserRead, &defaults.BrowserRead)
	mergeToolDescription(&loaded.BrowserScreenshot, &defaults.BrowserScreenshot)
	mergeToolDescription(&loaded.BrowserTabs, &defaults.BrowserTabs)
	mergeToolDescription(&loaded.GetLatestFrame, &defaults.GetLatestFrame)
	mergeToolDescription(&loaded.ImageDecode, &defaults.ImageDecode)
	mergeToolDescription(&loaded.Memory, &defaults.Memory)
	mergeToolDescription(&loaded.Wait, &defaults.Wait)
	mergeToolDescription(&loaded.ImageGeneration, &defaults.ImageGeneration)
	mergeToolDescription(&loaded.ImageEdit, &defaults.ImageEdit)
	mergeToolDescription(&loaded.ImageVariation, &defaults.ImageVariation)
	mergeToolDescription(&loaded.TextToSpeech, &defaults.TextToSpeech)
}

func mergeToolDescription(loaded, defaults *PromptsToolDescription) {
	if loaded.Description == "" {
		loaded.Description = defaults.Description
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
	Tools        PromptsToolsConfig        `yaml:"tools" mapstructure:"tools"`
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
	// ModeAdjustmentPlan / ModeAdjustmentAuto carry per-mode adjustment
	// instructions - NOT system prompts. The system prompt at message[0] stays
	// byte-stable across mode switches (Shift+Tab) to keep the prompt/KV cache
	// warm; these texts are injected instead as the {guidance} of the
	// mode-change <system-reminder> user message when the agent switches into
	// plan / auto-accept mode. They have no in-code default: the built-ins
	// live in the mode-change-reminder guidance map in reminders.go, so an
	// empty value here means "use the reminders defaults". The deprecated
	// system_prompt_plan / system_prompt_auto keys (and
	// INFER_PROMPTS_AGENT_SYSTEM_PROMPT_PLAN / _AUTO) still load into these
	// fields - see UnmarshalYAML and the INFER_PROMPTS_* bindings in
	// cmd/runtime/config.go.
	ModeAdjustmentPlan string `yaml:"mode_adjustment_plan,omitempty" mapstructure:"mode_adjustment_plan"`
	ModeAdjustmentAuto string `yaml:"mode_adjustment_auto,omitempty" mapstructure:"mode_adjustment_auto"`
}

// UnmarshalYAML keeps the deprecated `agent.system_prompt_plan` /
// `agent.system_prompt_auto` keys loading after the mode-adjustment rename
// (issue #1134): they decode into ModeAdjustmentPlan/ModeAdjustmentAuto
// unless the new keys are set. A deprecation/migration note lives in
// docs/configuration-reference.md.
func (c *PromptsAgentConfig) UnmarshalYAML(value *yaml.Node) error {
	type promptsAgentPlain PromptsAgentConfig
	plain := promptsAgentPlain{}
	if err := value.Decode(&plain); err != nil {
		return err
	}
	*c = PromptsAgentConfig(plain)

	legacy := struct {
		SystemPromptPlan string `yaml:"system_prompt_plan"`
		SystemPromptAuto string `yaml:"system_prompt_auto"`
	}{}
	if err := value.Decode(&legacy); err != nil {
		return err
	}
	if c.ModeAdjustmentPlan == "" {
		c.ModeAdjustmentPlan = legacy.SystemPromptPlan
	}
	if c.ModeAdjustmentAuto == "" {
		c.ModeAdjustmentAuto = legacy.SystemPromptAuto
	}
	return nil
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

// PromptsToolDescription holds a single tool's LLM-visible description.
// It is wrapped in a struct (rather than being a bare string) so future
// fields - e.g. per-parameter description overrides - can be added
// without breaking existing prompts.yaml files.
type PromptsToolDescription struct {
	Description string `yaml:"description" mapstructure:"description"`
}

// PromptsToolsConfig groups every tool whose description is exposed to
// the LLM. MCP tools are intentionally excluded - their descriptions
// come from the MCP server at runtime and overriding them here would
// drift from whatever the server reports. Tools that ship with the
// binary but only register conditionally (e.g. background-shell tools,
// computer-use tools) still get a default so the override slot exists
// regardless of whether the tool is enabled in this run.
type PromptsToolsConfig struct {
	Bash                PromptsToolDescription `yaml:"Bash" mapstructure:"Bash"`
	BashOutput          PromptsToolDescription `yaml:"BashOutput" mapstructure:"BashOutput"`
	KillShell           PromptsToolDescription `yaml:"KillShell" mapstructure:"KillShell"`
	ListShells          PromptsToolDescription `yaml:"ListShells" mapstructure:"ListShells"`
	Read                PromptsToolDescription `yaml:"Read" mapstructure:"Read"`
	Write               PromptsToolDescription `yaml:"Write" mapstructure:"Write"`
	Edit                PromptsToolDescription `yaml:"Edit" mapstructure:"Edit"`
	MultiEdit           PromptsToolDescription `yaml:"MultiEdit" mapstructure:"MultiEdit"`
	Delete              PromptsToolDescription `yaml:"Delete" mapstructure:"Delete"`
	Grep                PromptsToolDescription `yaml:"Grep" mapstructure:"Grep"`
	Tree                PromptsToolDescription `yaml:"Tree" mapstructure:"Tree"`
	TodoWrite           PromptsToolDescription `yaml:"TodoWrite" mapstructure:"TodoWrite"`
	RequestPlanApproval PromptsToolDescription `yaml:"RequestPlanApproval" mapstructure:"RequestPlanApproval"`
	AskUserQuestion     PromptsToolDescription `yaml:"AskUserQuestion" mapstructure:"AskUserQuestion"`
	WebFetch            PromptsToolDescription `yaml:"WebFetch" mapstructure:"WebFetch"`
	WebSearch           PromptsToolDescription `yaml:"WebSearch" mapstructure:"WebSearch"`
	Schedule            PromptsToolDescription `yaml:"Schedule" mapstructure:"Schedule"`
	Agent               PromptsToolDescription `yaml:"Agent" mapstructure:"Agent"`
	ListSubagents       PromptsToolDescription `yaml:"ListSubagents" mapstructure:"ListSubagents"`
	GetSubagentResult   PromptsToolDescription `yaml:"GetSubagentResult" mapstructure:"GetSubagentResult"`
	CloseSubagent       PromptsToolDescription `yaml:"CloseSubagent" mapstructure:"CloseSubagent"`
	ReadSubagentScreen  PromptsToolDescription `yaml:"ReadSubagentScreen" mapstructure:"ReadSubagentScreen"`
	SendSubagentInput   PromptsToolDescription `yaml:"SendSubagentInput" mapstructure:"SendSubagentInput"`
	ApproveSubagent     PromptsToolDescription `yaml:"ApproveSubagent" mapstructure:"ApproveSubagent"`
	A2AQueryAgent       PromptsToolDescription `yaml:"A2A_QueryAgent" mapstructure:"A2A_QueryAgent"`
	A2AQueryTask        PromptsToolDescription `yaml:"A2A_QueryTask" mapstructure:"A2A_QueryTask"`
	A2ASubmitTask       PromptsToolDescription `yaml:"A2A_SubmitTask" mapstructure:"A2A_SubmitTask"`
	Computer            PromptsToolDescription `yaml:"Computer" mapstructure:"Computer"`
	BrowserNavigate     PromptsToolDescription `yaml:"BrowserNavigate" mapstructure:"BrowserNavigate"`
	BrowserClick        PromptsToolDescription `yaml:"BrowserClick" mapstructure:"BrowserClick"`
	BrowserType         PromptsToolDescription `yaml:"BrowserType" mapstructure:"BrowserType"`
	BrowserRead         PromptsToolDescription `yaml:"BrowserRead" mapstructure:"BrowserRead"`
	BrowserScreenshot   PromptsToolDescription `yaml:"BrowserScreenshot" mapstructure:"BrowserScreenshot"`
	BrowserTabs         PromptsToolDescription `yaml:"BrowserTabs" mapstructure:"BrowserTabs"`
	GetLatestFrame      PromptsToolDescription `yaml:"GetLatestFrame" mapstructure:"GetLatestFrame"`
	ImageDecode         PromptsToolDescription `yaml:"ImageDecode" mapstructure:"ImageDecode"`
	Memory              PromptsToolDescription `yaml:"Memory" mapstructure:"Memory"`
	Wait                PromptsToolDescription `yaml:"Wait" mapstructure:"Wait"`
	ImageGeneration     PromptsToolDescription `yaml:"ImageGeneration" mapstructure:"ImageGeneration"`
	ImageEdit           PromptsToolDescription `yaml:"ImageEdit" mapstructure:"ImageEdit"`
	ImageVariation      PromptsToolDescription `yaml:"ImageVariation" mapstructure:"ImageVariation"`
	TextToSpeech        PromptsToolDescription `yaml:"TextToSpeech" mapstructure:"TextToSpeech"`
}

// DefaultPromptsConfig returns the in-code default prompts. This is the
// single source of truth - `infer init` seeds prompts.yaml from this and
// the runtime overlay falls back to it when fields are missing.
func DefaultPromptsConfig() *PromptsConfig { //nolint:funlen
	return &PromptsConfig{
		Agent: PromptsAgentConfig{
			SystemPrompt: `Autonomous software engineering agent. Execute tasks iteratively until completion. For GitHub operations (issues, pull requests, releases, the API), use the gh CLI via the Bash tool - there is no built-in GitHub tool. When the user types "#N" in chat (e.g. "#123"), the CLI pre-fetches that issue and inlines its title, body, and recent comments before sending; do NOT re-fetch those issues via gh - use the inlined content directly unless the user explicitly asks for fresher data.`,
			// Per-mode adjustment instructions (plan / auto-accept) have their
			// built-in texts in the mode-change-reminder guidance map
			// (config/reminders.go); leave these empty unless overriding.
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
- MUST be under 50 characters total
- MUST use imperative mood (e.g., "add", "fix", "update", "refactor")
- Types: feat, fix, docs, style, refactor, test, chore

EXAMPLES:
- "feat: add git shortcut with AI commits"
- "fix: resolve build error in container"
- "docs: update README installation guide"
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
		Tools: defaultPromptsToolsConfig(),
	}
}

// defaultPromptsToolsConfig returns the in-code tool descriptions. Each
// string is the verbatim description previously hardcoded in the
// corresponding tool's Definition() method - moving it here is an
// override hook, not a content change.
func defaultPromptsToolsConfig() PromptsToolsConfig { //nolint:funlen
	return PromptsToolsConfig{
		Bash: PromptsToolDescription{
			Description: `Execute allowed bash commands securely. Only pre-approved commands from the allowed list can be executed. Each segment of a pipe or &&/||/; chain must itself be allowed, and file-write redirections (>, >>) and command substitution ($(...), backticks) are blocked unless an anchored allowed list pattern (^...$) explicitly allows them; benign redirects like 2>&1 or >/dev/null are fine.`,
		},
		BashOutput: PromptsToolDescription{
			Description: `Retrieves output from a running or completed background bash shell. Returns only new output since the last read. Use this to monitor long-running commands that were moved to the background.`,
		},
		KillShell: PromptsToolDescription{
			Description: `Kills a running background bash shell by its ID. Sends SIGTERM first, then SIGKILL if needed after 5 seconds.`,
		},
		ListShells: PromptsToolDescription{
			Description: `Lists all background shell processes currently running or recently completed. Shows shell ID, command, state, elapsed time, and output size for each shell. Use this to monitor background processes started with the Bash tool.`,
		},
		Read: PromptsToolDescription{
			Description: `Reads a file from the local filesystem. You can access any file directly by using this tool.
Assume this tool is able to read all files on the machine. Only read paths you have seen - in the PROJECT STRUCTURE context, Tree/Grep output, a previous tool result, or the user's message. Never guess or invent file paths; if unsure, run the Tree tool on the directory first.

Usage:
- The file_path parameter can be either an absolute path or a relative path (relative paths will be resolved to absolute paths)
- By default, it reads up to 2000 lines starting from the beginning of the file
- You can optionally specify a line offset and limit (especially handy for long files), but it's recommended to read the whole file by not providing these parameters
- Any lines longer than 2000 characters will be truncated
- Results are returned using cat -n format, with line numbers starting at 1
- This tool can read PDF files (.pdf). PDFs are processed page by page, extracting both text and visual content for analysis.
- This tool cannot read image files. If the user wants to share an image, they should use the @ file reference syntax to attach it directly to their message.
- You have the capability to call multiple tools in a single response. It is always better to speculatively read multiple files as a batch that are potentially useful.
- If you read a file that exists but has empty contents you will receive a system reminder warning in place of file contents.`,
		},
		Write: PromptsToolDescription{
			Description: `Writes a file to the local filesystem.
Usage:
- This tool will overwrite the existing file if there is one at the provided path.
- If this is an existing file, you MUST use the Read tool first to read the file's contents. This tool will fail if you did not read the file first.
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.
- Only use emojis if the user explicitly requests it. Avoid writing emojis to files unless asked.`,
		},
		Edit: PromptsToolDescription{
			Description: `Performs exact string replacements in files.

Usage:
- You must use your Read tool at least once in the conversation before editing. This tool will error if you attempt an edit without reading the file.
- When editing text from Read tool output, ensure you preserve the exact indentation (tabs/spaces) as it appears AFTER the line number prefix. The line number prefix format is: spaces + line number + tab. Everything after that tab is the actual file content to match. Never include any part of the line number prefix in the old_string or new_string.
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- Only use emojis if the user explicitly requests it. Avoid adding emojis to files unless asked.
- The edit will FAIL if old_string is not unique in the file. Either provide a larger string with more surrounding context to make it unique or use replace_all to change every instance of old_string.
- Use replace_all for replacing and renaming strings across the file. This parameter is useful if you want to rename a variable for instance.`,
		},
		MultiEdit: PromptsToolDescription{
			Description: `This is a tool for making multiple edits to a single file in one operation. It is built on top of the Edit tool and allows you to perform multiple find-and-replace operations efficiently. Prefer this tool over the Edit tool when you need to make multiple edits to the same file.

Before using this tool:

1. Use the Read tool to understand the file's contents and context
2. Verify the directory path is correct

To make multiple file edits, provide the following:
1. file_path: The absolute path to the file to modify (must be absolute, not relative)
2. edits: An array of edit operations to perform, where each edit contains:
   - old_string: The text to replace (must match the file contents exactly, including all whitespace and indentation)
   - new_string: The edited text to replace the old_string
   - replace_all: Replace all occurrences of old_string. This parameter is optional and defaults to false.

IMPORTANT:
- All edits are applied in sequence, in the order they are provided
- Each edit operates on the result of the previous edit
- All edits must be valid for the operation to succeed - if any edit fails, none will be applied
- This tool is ideal when you need to make several changes to different parts of the same file
- For Jupyter notebooks (.ipynb files), use the NotebookEdit instead

CRITICAL REQUIREMENTS:
1. All edits follow the same requirements as the single Edit tool
2. The edits are atomic - either all succeed or none are applied
3. Plan your edits carefully to avoid conflicts between sequential operations

WARNING:
- The tool will fail if edits.old_string doesn't match the file contents exactly (including whitespace)
- The tool will fail if edits.old_string and edits.new_string are the same
- Since edits are applied in sequence, ensure that earlier edits don't affect the text that later edits are trying to find

When making edits:
- Ensure all edits result in idiomatic, correct code
- Do not leave the code in a broken state
- Always use absolute file paths (starting with /)
- Only use emojis if the user explicitly requests it. Avoid adding emojis to files unless asked.
- Use replace_all for replacing and renaming strings across the file. This parameter is useful if you want to rename a variable for instance.

If you want to create a new file, use:
- A new file path, including dir name if needed
- First edit: empty old_string and the new file's contents as new_string
- Subsequent edits: normal edit operations on the created content`,
		},
		Delete: PromptsToolDescription{
			Description: `Delete files or directories from the filesystem. Supports wildcard patterns for batch operations. Restricted to current working directory for security.`,
		},
		Grep: PromptsToolDescription{
			Description: "A powerful search tool with configurable backend (ripgrep or Go implementation)\n\n Usage:\n - ALWAYS use Grep for search tasks. NEVER invoke `grep` or `rg` as a Bash command. The Grep tool has been optimized for correct permissions and access.\n - Supports full regex syntax (e.g., \"log.*Error\", \"function\\s+\\w+\")\n - Filter files with glob parameter (e.g., \"*.js\", \"**/*.tsx\") or type parameter (e.g., \"js\", \"py\", \"rust\")\n - Output modes: \"content\" shows matching lines, \"files_with_matches\" shows only file paths (default), \"count\" shows match counts\n - Use the Agent tool for open-ended searches requiring multiple rounds\n - Pattern syntax: When using ripgrep backend - literal braces need escaping (use `interface\\{\\}` to find `any` in Go code)\n - Multiline matching: By default patterns match within single lines only. For cross-line patterns like `struct \\{[\\s\\S]*?field`, use `multiline: true`\n",
		},
		Tree: PromptsToolDescription{
			Description: `Display directory structure in a tree format, similar to the Unix tree command. Use format "compact" for a token-efficient one-directory-per-line listing (root-first, git-tracked non-ignored files only) when you just need to see where files live.`,
		},
		TodoWrite: PromptsToolDescription{
			Description: `Use this tool to create and manage a structured task list for your current coding session. This helps you track progress, organize complex tasks, and demonstrate thoroughness to the user.
It also helps the user understand the progress of the task and overall progress of their requests.

## When to Use This Tool
Use this tool proactively in these scenarios:

1. Complex multi-step tasks - When a task requires 3 or more distinct steps or actions
2. Non-trivial and complex tasks - Tasks that require careful planning or multiple operations
3. User explicitly requests todo list - When the user directly asks you to use the todo list
4. User provides multiple tasks - When users provide a list of things to be done (numbered or comma-separated)
5. After receiving new instructions - Immediately capture user requirements as todos
6. When you start working on a task - Mark it as in_progress BEFORE beginning work. Ideally you should only have one todo as in_progress at a time
7. After completing a task - Mark it as completed and add any new follow-up tasks discovered during implementation

## When NOT to Use This Tool

Skip using this tool when:
1. There is only a single, straightforward task
2. The task is trivial and tracking it provides no organizational benefit
3. The task can be completed in less than 3 trivial steps
4. The task is purely conversational or informational

NOTE that you should not use this tool if there is only one trivial task to do. In this case you are better off just doing the task directly.

## Task States and Management

1. **Task States**: Use these states to track progress:
   - pending: Task not yet started
   - in_progress: Currently working on (limit to ONE task at a time)
   - completed: Task finished successfully

2. **Task Management**:
   - Update task status in real-time as you work
   - Mark tasks complete IMMEDIATELY after finishing (don't batch completions)
   - Only have ONE task in_progress at any time
   - Complete current tasks before starting new ones
   - Remove tasks that are no longer relevant from the list entirely

3. **Task Completion Requirements**:
   - ONLY mark a task as completed when you have FULLY accomplished it
   - If you encounter errors, blockers, or cannot finish, keep the task as in_progress
   - When blocked, create a new task describing what needs to be resolved
   - Never mark a task as completed if:
     - Tests are failing
     - Implementation is partial
     - You encountered unresolved errors
     - You couldn't find necessary files or dependencies

4. **Task Breakdown**:
   - Create specific, actionable items
   - Break complex tasks into smaller, manageable steps
   - Use clear, descriptive task names

When in doubt, use this tool. Being proactive with task management demonstrates attentiveness and ensures you complete all requirements successfully.`,
		},
		RequestPlanApproval: PromptsToolDescription{
			Description: `Submit your completed plan for user approval and persist it to storage.

What happens:
- The plan is saved to the configured storage backend with an infer://plans/<id> URI (on the default jsonl backend it also lands as <configDir>/plans/<id>.md); retrieve it any time with 'infer plans show <id>'
- The plan is displayed to the user with Accept / Reject / Approve Each Step options
- Accept switches to auto-approve mode; Approve Each Step keeps standard mode (per-action approval)
- If approved, you'll switch to execution mode with full tool access
- If rejected, the stored plan remains as an audit trail and the user provides feedback

Required parameters:
- title: A short human-readable phrase (≤ 60 chars, no slashes). Becomes the H1 heading and the filename slug.
- plan: The full plan as Markdown using H2 sections in this order - ## Context, ## Files to Modify, ## Current Code, ## Changes, ## Performance Impact, ## Critical Files, ## Edge Cases, ## Verification. Omit any section that is not applicable.

Only call this tool when the plan is final. If you need clarification, ask the user in a normal assistant turn first.`,
		},
		AskUserQuestion: PromptsToolDescription{
			Description: `Ask the user 1-4 multiple-choice clarifying questions as an interactive form (plan mode only).

Use this when the plan hinges on a discrete decision the user should make - format, scope, approach, naming, trade-off - instead of guessing or asking in prose. Call it BEFORE RequestPlanApproval; fold the answers into the plan, then submit the plan.

Each question has:
- header: a short chip/tag (<= 12 chars), e.g. "Scope", "Format"
- question: the full question text
- options: 2-4 choices, each with a concise label (returned as the answer) and a description of the trade-off
- multiSelect (optional): set true to let the user pick more than one option

The UI always adds an "Other" free-text choice, so you do not need an "Other" option yourself. The user answers with the keyboard and the selected labels (and any free text) are returned to you.

Notes:
- Ask only what you genuinely need - prefer one focused round of questions over many.
- If no interactive user is available (headless run), you will be told to proceed with stated assumptions instead.`,
		},
		WebFetch: PromptsToolDescription{
			Description: `Fetch content from allowed URLs. Set download=true to save the file to disk automatically. Useful for downloading A2A task artifacts or other files.`,
		},
		WebSearch: PromptsToolDescription{
			Description: `Search the web using Google or DuckDuckGo search engines`,
		},
		Schedule: PromptsToolDescription{
			Description: `Schedule a task that fires on a cron schedule. Each fire runs an agent and records the run to storage; when the current session was triggered by a messaging channel (e.g. Telegram), the output is also delivered back through that channel.

IMPORTANT - clarify intent before creating: ALWAYS confirm with the user whether they want the task to run **once** (e.g. "remind me at 6pm today to call mum") or **recurring** (e.g. "send me a quote every morning"). If their request is ambiguous, ASK them - do not guess. Set run_once=true for one-off tasks; the scheduler will delete the job automatically after it fires once. Set run_once=false (or omit) for recurring tasks.

Each fire creates a brand-new agent session - no context is carried between runs. Choose narrow, specific prompts to avoid wasted compute.

Operations:
- create: Add a new scheduled job. Required: cron_expression, prompt. Optional: run_once, name, description, model.
- list: List all scheduled jobs.
- get: Fetch one job. Required: job_id.
- update: Modify an existing job. Required: job_id. Any of cron_expression, prompt, run_once, name, description, model can be updated.
- delete: Remove a job. Required: job_id.

Delivery routing (channel + recipient) is derived automatically from the current session - you never pass it. From a channel-driven session (e.g. responding to a Telegram message) the job delivers its output back to that channel; from any other session the job is record-only and its results are read from storage.

Cron expression format: standard 5-field crontab syntax (minute hour day-of-month month day-of-week). The "@every <duration>" descriptor is also supported. Examples:
- "0 8 * * *"       - every day at 08:00 (recurring)
- "*/15 * * * *"    - every 15 minutes (recurring)
- "0 9 * * 1-5"     - weekdays at 09:00 (recurring)
- "@every 1h"       - every hour (recurring)
- "0 18 26 4 *"     - April 26 at 18:00 (use with run_once=true for "today at 6pm")

For one-off jobs, build a cron expression that pinpoints the exact moment (use the current date's day/month) and set run_once=true. The job will fire once at that time and then be deleted automatically.

The scheduler runs inside the 'infer daemon' process. Jobs only fire while that daemon is running.`,
		},
		Agent: PromptsToolDescription{
			Description: `Spawn local subagents - each an autonomous "infer headless" subprocess with its own isolated session - to run work in parallel and fold their results back into this conversation. Use this to fan out independent tasks (research, edits across separate areas, parallel investigations) without standing up an A2A agent server.

Provide either 'tasks' (an array of {description, label?, model?, system_prompt?, type?} objects) to run several subagents at once, or 'description' for a single subagent. Give a subagent a specialized role/persona by setting its system_prompt (each subagent can have its own).

Choose each subagent's capability with 'type': ReadOnly (DEFAULT) is Explore-like - read/search tools only, never needs approval - use it for investigation, research, and reading code. ReadWrite can modify files and run commands; its mutations require approval. Prefer ReadOnly unless the task must change something.

The subagent surface (headless background vs. an interactive tmux pane you can watch) is set by the operator via config (tools.agent.mode) - you do NOT choose it and there is no mode parameter; just describe the task.

In EVERY mode you are AUTOMATICALLY NOTIFIED when each subagent finishes (its result is folded into this conversation). After dispatching, END YOUR TURN and wait: do NOT poll with ListSubagents/GetSubagentResult and do NOT CloseSubagent to fetch a result (closing only stops one early). The '[Subagent Completed: ...]' message arrives on its own - act on it then.
- Headless subagents honor tools.agent.wait: blocking (default) waits until all finish and returns their aggregated results (fan-out / fan-in); async returns immediately and notifies you as each completes.
- Interactive subagents are fire-and-watch: the call returns once their tmux panes launch; you watch them live and are notified when each finishes. Use CloseSubagent only to stop one early.

Each subagent is independent and cannot itself spawn further subagents. Prefer narrow, self-contained task descriptions.`,
		},
		ListSubagents: PromptsToolDescription{
			Description: `Snapshot the local subagents spawned by the Agent tool (id, label, mode, status). Running subagents notify you automatically when they finish - this is a one-off status check, NOT a way to poll for completion. Use a returned subagent_id with GetSubagentResult or CloseSubagent.`,
		},
		GetSubagentResult: PromptsToolDescription{
			Description: `Re-read a completed interactive subagent's last assistant message on demand. Do NOT use this to poll for completion - you are notified automatically when a subagent finishes; a running subagent refuses this call. If you just dispatched a subagent, END YOUR TURN and wait for the notification instead of calling this. Pass the subagent_id from ListSubagents.`,
		},
		CloseSubagent: PromptsToolDescription{
			Description: `Stop a subagent early or tidy a finished interactive pane. You do NOT need to close a subagent to receive its result - results arrive automatically when it finishes. Never CloseSubagent just to fetch a result or because its output looks empty; wait for the automatic notification. For an interactive subagent this folds in its last assistant message and kills the pane; for a headless one it cancels the subprocess. Pass the subagent_id from ListSubagents.`,
		},
		ReadSubagentScreen: PromptsToolDescription{
			Description: `Capture the RAW terminal screen of an interactive subagent's tmux pane - the rendered TUI exactly as drawn (input box, status bar, menus and all). Use this to inspect or test a subagent's live TUI; unlike GetSubagentResult it works while the subagent is running and is NOT a way to poll for completion (you are notified automatically when a subagent finishes). Optional 'lines' bounds the output to the last N lines. Interactive subagents only. Pass the subagent_id from ListSubagents. To drive an interactive terminal program that is NOT a subagent (a TUI, REPL, or another CLI), use the ` + "`tmux`" + ` skill (raw tmux via Bash) instead of this tool.`,
		},
		SendSubagentInput: PromptsToolDescription{
			Description: `Type into an interactive subagent's TUI - to re-prompt it or drive its interface. Provide 'text' (typed literally) and/or 'keys' (named keys: Enter, Escape, Up, Down, Left, Right, Tab, BSpace, Space, Home, End, PageUp, PageDown). With submit=true (default) it presses Enter to send a new prompt and you are AUTOMATICALLY NOTIFIED when the subagent finishes the resulting turn - do not poll. With submit=false it only sends the keys (for TUI navigation/testing); observe the effect with ReadSubagentScreen. Interactive subagents only. Pass the subagent_id from ListSubagents. To drive an interactive terminal program that is NOT a subagent, use the ` + "`tmux`" + ` skill (raw tmux via Bash) instead of this tool.`,
		},
		ApproveSubagent: PromptsToolDescription{
			Description: `Respond to an interactive subagent that is waiting for tool approval (you are notified with a '[Subagent ... awaiting approval]' message showing what it wants to run). decision='approve' lets it proceed, 'reject' declines. This action itself requires YOUR confirmation in this chat before it is relayed. Only Approve/Reject - do not auto-approve. Pass the subagent_id from ListSubagents.`,
		},
		A2AQueryAgent: PromptsToolDescription{
			Description: `Retrieve an A2A agent's metadata card showing its capabilities and configuration. Use ONLY for discovering what an agent can do. For asking questions or requesting work from an agent, use the Agent tool instead.`,
		},
		A2AQueryTask: PromptsToolDescription{
			Description: `Query the status and result of a specific A2A task. Returns the complete task object including status, artifacts, and message data. IMPORTANT: When you submit a task via A2A_SubmitTask, it automatically monitors the task in the background and emits an event when complete - you will be notified automatically. DO NOT manually query recently submitted tasks during background monitoring. Only use this tool to: 1) Check tasks from previous conversations, 2) Check tasks submitted outside this session, or 3) Get detailed results AFTER you receive a completion notification.`,
		},
		A2ASubmitTask: PromptsToolDescription{
			Description: `Submit work to an A2A agent server and delegate it to run in the background. IMPORTANT: This tool returns IMMEDIATELY after submission. DO NOT poll, query, or download artifacts right after submission. The system automatically monitors the task in the background and you will be AUTOMATICALLY NOTIFIED when it completes - the result will appear in the conversation. After submission, you MUST wait for the automatic notification before taking any follow-up actions. You can tell the user the task is running and you're waiting for it to complete. Use this for ANY interaction where you need an agent to respond with answers or complete work. The A2A_QueryTask tool is ONLY for retrieving metadata/capabilities or checking status of previously submitted tasks, NOT for polling just-submitted tasks.`,
		},
		Computer: PromptsToolDescription{
			Description: `Drives the computer's accessibility tree, mouse, keyboard, and screen through one action-based interface. PREFER "accessibility" as the first observation: it returns compact role/label/state/bbox text in the same coordinate space as screenshots, costs no vision tokens, and works for text-only models. Use "press" with an exact returned label to activate a standard control without moving the cursor or taking a screenshot. If accessibility reports empty, unavailable, unsupported, or insufficient content, fall back to "screenshot" (pass "region" to re-capture a frame-space rectangle at native resolution for small UI). Other actions: "cursor", "move"/"click"/"double_click"/"triple_click" (pointer actions at frame-space x/y), "scroll", "type" (types text INTO GUI APPLICATIONS at the cursor - DO NOT use it to run shell commands, use Bash instead), and "key" (a combo such as "enter" or "cmd+a"). To open applications on macOS, use Bash with 'open -a AppName'. Reach for GetLatestFrame only when you cannot see images yourself (its annotated text mode) or for non-screen frame sources.`,
		},
		BrowserNavigate: PromptsToolDescription{
			Description: `Opens a URL in the automated browser session. Launches the browser on first use (or attaches to a running one when a CDP endpoint is configured) and keeps the session open across browser tool calls. Returns the final URL and page title after navigation.`,
		},
		BrowserClick: PromptsToolDescription{
			Description: `Clicks in the current browser page, either at an element (CSS selector or text= / role-based Playwright selector) or at viewport x/y coordinates (CSS pixels) taken from a BrowserScreenshot. Provide 'selector' OR both 'x' and 'y'. Use BrowserRead or BrowserScreenshot first to locate the target. Coordinate clicks are not available on the extension backend. Requires an active page (call BrowserNavigate first).`,
		},
		BrowserType: PromptsToolDescription{
			Description: `Types text into an input element in the current browser page identified by a selector, replacing its existing value. Set press_enter to true to submit afterwards (e.g. search boxes, forms). Requires an active page (call BrowserNavigate first).`,
		},
		BrowserRead: PromptsToolDescription{
			Description: `Reads the current browser page: URL, title, and visible text (of the whole page or of the element matched by an optional selector). Sensitive input values (passwords, tokens, one-time codes) are redacted. Also returns recent browser-initiated events (console messages, dialogs, and window.inferNotify(...) calls from page scripts) so pages can signal back to you. Read-only.`,
		},
		BrowserScreenshot: PromptsToolDescription{
			Description: `Captures a screenshot of the current browser page/active tab and attaches it as an image (models with vision see it inline). Use it to see what the user is looking at, then act with BrowserClick (by selector or x/y coordinates). Password fields render masked by the browser. Read-only.`,
		},
		BrowserTabs: PromptsToolDescription{
			Description: `Lists the browser's open tabs (index, URL, title) and marks the active one, so you know which page the user is currently on. Read-only.`,
		},
		GetLatestFrame: PromptsToolDescription{
			Description: `Retrieves the latest frame from a named frame source. This is a read-only operation that does NOT require approval. For the desktop, first use Computer's "accessibility" action; use Computer "screenshot" or this tool's annotated text only when the accessibility tree is empty, unavailable, or insufficient. Use this tool when you cannot see images yourself ("annotated" calls the configured vision annotator) or for non-screen frame sources such as cameras. Sources: "screen" (the screenshot ring buffer, captured every few seconds when streaming is enabled) and any configured directory sources. When format is omitted it is chosen automatically based on your vision capability. Pass "region" to zoom: the given frame-space rectangle is re-captured at native resolution, the reliable way to read small UI.`,
		},
		ImageDecode: PromptsToolDescription{
			Description: `Describes a local image file or http(s) URL as text: a scene summary plus a numbered list of detected elements (label, text, bounding box), produced by the configured vision annotator. Use it to inspect any image on disk or from a URL - e.g. outputs of ImageGeneration/ImageEdit under ~/.infer/projects/<project-slug>/artifacts, @-referenced image paths, downloaded files, or remote images - especially when you cannot see images yourself. Pass an optional prompt to ask a specific question about the image; the summary then answers it. Read-only, does not require approval.`,
		},
		Memory: PromptsToolDescription{
			Description: `Persistent, cross-session memory stored as fact-files in a global memory directory, organized by project. Global facts live at the root (<name>.md); project facts live in a per-project subdirectory (<project>/<name>.md, e.g. inference-gateway-cli/build-commands.md). Each fact is one Markdown file with YAML frontmatter (name, description, metadata.type, metadata.project, metadata.session - the session that last wrote it); MEMORY.md is the index (one line per fact, linking to its path). At session start you are given the index entries for the current project and for global facts; other projects are listed by name only - read the full index (read with no name) to see them.

Operations:
- read: With no name, return the full MEMORY.md index (all projects). With a name, return that fact-file's full content. Use the exact name shown in the index: "build-commands" for a global fact, "inference-gateway-cli/build-commands" for a project fact.
- write: Create or update a fact. Required: name (a short slug), description (a one-line summary shown in the index), type (one of user, feedback, project, reference), and content (the fact body in Markdown; writes over the per-entry size cap are rejected, so keep it tight). Optional: project - where the fact belongs. Defaults: "user" facts are global (they describe the user, not a project); feedback/project/reference facts go under the current project (detected from the git remote). Pass project "global" to force a global fact, or an org/repo name to file it under another project.
- delete: Remove a fact and its index entry. Required: name (the exact name from the index, including the project prefix for project facts).

Guidelines: record one fact per memory; keep description to a single line; before writing, check the index for an existing entry and update it rather than creating a duplicate; delete facts that turn out to be wrong. Never edit MEMORY.md by hand - the tool maintains it.`,
		},
		Wait: PromptsToolDescription{
			Description: `Block until a condition is met, then return once with the outcome. Use this instead of sleep-and-poll loops (Bash("sleep N") + BashOutput) to avoid wasting LLM round-trips.

Conditions:
- shells: block until the given background shell ID(s) exit (or all pending background tasks when omitted). Returns exit codes and tail output for each shell.
- file: block until a file path is created, modified, or removed (uses fsnotify).
- command: re-run a check command server-side at a fixed interval until it exits 0 (e.g. "curl -sf localhost:8080/health"). The check command goes through the same bash allow-list as the Bash tool. Optional pending_exit_codes lists exit codes that mean "still pending, keep polling"; include 0 for commands like 'gh run view --exit-status' that exit 0 for both 'still running' and 'completed successfully'. Any exit code not in this list ends the wait immediately with reason check_failed and the command's output, so checks that distinguish pending from failed return the moment the outcome is known.

Waiting for CI after a push: Wait(condition=command, command="gh pr checks", pending_exit_codes=[8], timeout_seconds=600). gh pr checks exits 0 when all checks pass, 8 while checks are pending, and non-zero otherwise, so the wait returns as soon as CI concludes either way. On check_failed the last output lists the failing checks (dig deeper with "gh run view --log-failed"). If the wait times out while checks are still pending, call Wait again - waiting costs zero completions.

Every mode requires timeout_seconds (bounded by the config ceiling). Returns a structured result: the outcome (condition_met, check_failed, or timeout), elapsed time, and the condition details (exit codes, last output) - included on failure too.

Cancellation: Esc in chat or session cancel interrupts the wait immediately.`,
		},
		ImageGeneration: PromptsToolDescription{
			Description: `Generate an image from a text prompt and save it as a PNG under ~/.infer/projects/<project-slug>/artifacts/. Returns the saved file path.

The image is generated by the image model configured for this tool (not the chat model), so do not ask which model to use. Write the prompt yourself from the user's intent and the conversation context; it is sent to the image model verbatim.

Cost control: quality defaults to "low" and size to "1024x1024". Keep those defaults unless the user explicitly asks for higher quality or a different size - higher tiers cost significantly more and take longer. "make it nicer" is not an explicit request for high quality; "high quality", "hd", "1536x1024" are.`,
		},
		TextToSpeech: PromptsToolDescription{
			Description: `Synthesize speech from text with a local TTS engine and save it as a WAV file. Returns the saved file path and the audio duration.

Pass only text for a stock voice. Pass voice_sample (a path to a short WAV recording of the target speaker, ideally 10-30 seconds of clean single-speaker speech) to clone that voice and speak the given text in it. Pass output_path to choose where the WAV is written; it defaults to a timestamped file under the configured output directory.

The synthesis runs fully offline and may be slow on the first call (model download). Do not claim the audio was played aloud - it is written to disk for the user to open.`,
		},
		ImageEdit: PromptsToolDescription{
			Description: `Edit an existing image and save the result as a PNG under ~/.infer/projects/<project-slug>/artifacts/. Returns the saved file path.

Provide the local file path of the image to edit and a plain prompt describing the change. The request goes to /v1/images/edits using the image model configured for this tool (not the chat model), so do not ask which model to use.

Optionally pass mask: a local file path to a PNG whose fully transparent areas (alpha = 0) mark the region to edit; all other pixels are preserved exactly. The mask must be a PNG with the same dimensions as the input image. Omit mask to let the model localize the change from the prompt alone.

Cost control: quality defaults to "auto" and size to "1024x1024". Keep those defaults unless the user explicitly asks for a different tier or size - higher tiers cost significantly more and take longer.`,
		},
		ImageVariation: PromptsToolDescription{
			Description: `Create a variation of an existing image and save the result as a PNG under ~/.infer/projects/<project-slug>/artifacts/. Returns the saved file path.

Provide the local file path of the image to base the variation on. The request goes to /v1/images/variations using the image model configured for this tool (not the chat model), so do not ask which model to use.

Cost control: size defaults to "1024x1024". Keep that default unless the user explicitly asks for a different size - larger sizes cost significantly more and take longer.`,
		},
	}
}
