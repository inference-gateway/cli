package config

import (
	utils "github.com/inference-gateway/cli/config/utils"
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
	cfg, err := utils.LoadYAML(path, "prompts", DefaultPromptsConfig)
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
	if loaded.Agent.SystemPromptPlan == "" {
		loaded.Agent.SystemPromptPlan = defaults.Agent.SystemPromptPlan
	}
	if loaded.Agent.SystemPromptRemote == "" {
		loaded.Agent.SystemPromptRemote = defaults.Agent.SystemPromptRemote
	}
	if loaded.Agent.SystemPromptHeartbeat == "" {
		loaded.Agent.SystemPromptHeartbeat = defaults.Agent.SystemPromptHeartbeat
	}
	if loaded.Agent.SystemReminders.ReminderText == "" {
		loaded.Agent.SystemReminders.ReminderText = defaults.Agent.SystemReminders.ReminderText
	}
	if loaded.Agent.SystemReminders.Interval == 0 {
		loaded.Agent.SystemReminders.Interval = defaults.Agent.SystemReminders.Interval
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
	mergeToolDescription(&loaded.WebFetch, &defaults.WebFetch)
	mergeToolDescription(&loaded.WebSearch, &defaults.WebSearch)
	mergeToolDescription(&loaded.Schedule, &defaults.Schedule)
	mergeToolDescription(&loaded.A2AQueryAgent, &defaults.A2AQueryAgent)
	mergeToolDescription(&loaded.A2AQueryTask, &defaults.A2AQueryTask)
	mergeToolDescription(&loaded.A2ASubmitTask, &defaults.A2ASubmitTask)
	mergeToolDescription(&loaded.MouseMove, &defaults.MouseMove)
	mergeToolDescription(&loaded.MouseClick, &defaults.MouseClick)
	mergeToolDescription(&loaded.MouseScroll, &defaults.MouseScroll)
	mergeToolDescription(&loaded.KeyboardType, &defaults.KeyboardType)
	mergeToolDescription(&loaded.GetFocusedApp, &defaults.GetFocusedApp)
	mergeToolDescription(&loaded.ActivateApp, &defaults.ActivateApp)
	mergeToolDescription(&loaded.GetLatestScreenshot, &defaults.GetLatestScreenshot)
}

func mergeToolDescription(loaded, defaults *PromptsToolDescription) {
	if loaded.Description == "" {
		loaded.Description = defaults.Description
	}
}

// SavePrompts writes the prompts configuration to disk, creating any
// missing parent directories.
func SavePrompts(path string, cfg *PromptsConfig) error {
	return utils.SaveYAML(path, "prompts", cfg)
}

// PromptsConfig holds every customisable LLM prompt the CLI ships with.
// It mirrors the nested key structure those prompts had when they lived
// under .infer/config.yaml so users can move existing values verbatim.
type PromptsConfig struct {
	Agent        PromptsAgentConfig        `yaml:"agent" mapstructure:"agent"`
	Git          PromptsGitConfig          `yaml:"git" mapstructure:"git"`
	Conversation PromptsConversationConfig `yaml:"conversation" mapstructure:"conversation"`
	Init         PromptsInitConfig         `yaml:"init" mapstructure:"init"`
	Tools        PromptsToolsConfig        `yaml:"tools" mapstructure:"tools"`
}

type PromptsAgentConfig struct {
	SystemPrompt          string                      `yaml:"system_prompt" mapstructure:"system_prompt"`
	SystemPromptPlan      string                      `yaml:"system_prompt_plan" mapstructure:"system_prompt_plan"`
	SystemPromptRemote    string                      `yaml:"system_prompt_remote" mapstructure:"system_prompt_remote"`
	SystemPromptHeartbeat string                      `yaml:"system_prompt_heartbeat" mapstructure:"system_prompt_heartbeat"`
	CustomInstructions    string                      `yaml:"custom_instructions" mapstructure:"custom_instructions"`
	SystemReminders       PromptsAgentRemindersConfig `yaml:"system_reminders" mapstructure:"system_reminders"`
}

type PromptsAgentRemindersConfig struct {
	Enabled      bool   `yaml:"enabled" mapstructure:"enabled"`
	Interval     int    `yaml:"interval" mapstructure:"interval"`
	ReminderText string `yaml:"reminder_text" mapstructure:"reminder_text"`
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
	WebFetch            PromptsToolDescription `yaml:"WebFetch" mapstructure:"WebFetch"`
	WebSearch           PromptsToolDescription `yaml:"WebSearch" mapstructure:"WebSearch"`
	Schedule            PromptsToolDescription `yaml:"Schedule" mapstructure:"Schedule"`
	A2AQueryAgent       PromptsToolDescription `yaml:"A2A_QueryAgent" mapstructure:"A2A_QueryAgent"`
	A2AQueryTask        PromptsToolDescription `yaml:"A2A_QueryTask" mapstructure:"A2A_QueryTask"`
	A2ASubmitTask       PromptsToolDescription `yaml:"A2A_SubmitTask" mapstructure:"A2A_SubmitTask"`
	MouseMove           PromptsToolDescription `yaml:"MouseMove" mapstructure:"MouseMove"`
	MouseClick          PromptsToolDescription `yaml:"MouseClick" mapstructure:"MouseClick"`
	MouseScroll         PromptsToolDescription `yaml:"MouseScroll" mapstructure:"MouseScroll"`
	KeyboardType        PromptsToolDescription `yaml:"KeyboardType" mapstructure:"KeyboardType"`
	GetFocusedApp       PromptsToolDescription `yaml:"GetFocusedApp" mapstructure:"GetFocusedApp"`
	ActivateApp         PromptsToolDescription `yaml:"ActivateApp" mapstructure:"ActivateApp"`
	GetLatestScreenshot PromptsToolDescription `yaml:"GetLatestScreenshot" mapstructure:"GetLatestScreenshot"`
}

// DefaultPromptsConfig returns the in-code default prompts. This is the
// single source of truth - `infer init` seeds prompts.yaml from this and
// the runtime overlay falls back to it when fields are missing.
func DefaultPromptsConfig() *PromptsConfig { //nolint:funlen
	return &PromptsConfig{
		Agent: PromptsAgentConfig{
			SystemPrompt: `Autonomous software engineering agent. Execute tasks iteratively until completion. For GitHub operations (issues, pull requests, releases, the API), use the gh CLI via the Bash tool - there is no built-in GitHub tool. When the user types "#N" in chat (e.g. "#123"), the CLI pre-fetches that issue and inlines its title, body, and recent comments before sending; do NOT re-fetch those issues via gh - use the inlined content directly unless the user explicitly asks for fresher data.`,
			SystemPromptPlan: `You are an AI planning assistant in PLAN MODE. Your role is to analyze user requests and create ACTIONABLE, EXECUTABLE plans WITHOUT executing them.

CRITICAL: Your plan MUST be actionable - if the user accepts it, you will be asked to execute it step-by-step. Plans that are not actionable are NOT plans.

CAPABILITIES IN PLAN MODE:
- Read, Grep, and Tree tools for gathering information
- TodoWrite for tracking planning progress
- RequestPlanApproval tool to submit your plan for user approval (also persists the plan as a Markdown file under <configDir>/plans/)
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
3. If you need clarification or more information, ASK the user in a regular assistant turn - do NOT call RequestPlanApproval yet
4. Iterate with the user until the plan is complete and unambiguous
5. When the plan is final, call RequestPlanApproval with both a short title AND the Markdown plan body

DECISION MAKING:
- Need more info? ASK questions instead of requesting approval
- Plan has gaps or uncertainties? ASK for clarification
- Plan is complete and specific? Call RequestPlanApproval tool

OUTPUT FORMAT - MARKDOWN PLAN:
The 'plan' argument MUST be a Markdown document using the following H2 sections, in this order. Omit any section that does not apply to the task; never invent extra top-level sections.

## Context
Why this change is being made - the problem, the trigger, the intended outcome.

## Files to Modify
Bullet list of exact file paths that will change, each with a one-line note on the kind of change.

## Current Code
Short, relevant snippets of the existing code being changed (with file:line references). Skip when not applicable (e.g. brand-new files).

## Changes
The concrete edits, grouped per file or per concern. Be specific: function names, signatures, what is added/removed/replaced.

## Performance Impact
Expected runtime, memory, I/O, or token-usage impact. Write "Negligible." if there isn't any.

## Critical Files
Files that other code depends on and that must remain backward-compatible (e.g. shared interfaces, public APIs). Skip when not applicable.

## Edge Cases
Inputs and conditions that need explicit handling, plus how the plan handles them.

## Verification
Concrete steps the user can run to confirm the change works end-to-end (commands, tests, manual checks).

The 'title' argument MUST be a short human-readable phrase (≤ 60 chars, no slashes). It becomes the H1 of the saved file and the basis of the on-disk filename.

REMEMBER:
- If accepted, YOU will execute this plan. Make it specific and actionable!
- Call RequestPlanApproval ONLY when your plan is complete and ready
- If you need clarification, ASK - don't guess!`,
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
			SystemReminders: PromptsAgentRemindersConfig{
				Enabled:  false,
				Interval: 4,
				ReminderText: `<system-reminder>
This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware. If you are working on tasks that would benefit from a todo list please use the TodoWrite tool to create one. If not, please feel free to ignore. Again do not mention this message to the user.
</system-reminder>`,
			},
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
Assume this tool is able to read all files on the machine. If the User provides a path to a file assume that path is valid. It is okay to read a file that does not exist; an error will be returned.

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
			Description: "A powerful search tool with configurable backend (ripgrep or Go implementation)\n\n Usage:\n - ALWAYS use Grep for search tasks. NEVER invoke `grep` or `rg` as a Bash command. The Grep tool has been optimized for correct permissions and access.\n - Supports full regex syntax (e.g., \"log.*Error\", \"function\\s+\\w+\")\n - Filter files with glob parameter (e.g., \"*.js\", \"**/*.tsx\") or type parameter (e.g., \"js\", \"py\", \"rust\")\n - Output modes: \"content\" shows matching lines, \"files_with_matches\" shows only file paths (default), \"count\" shows match counts\n - Use Task tool for open-ended searches requiring multiple rounds\n - Pattern syntax: When using ripgrep backend - literal braces need escaping (use `interface\\{\\}` to find `any` in Go code)\n - Multiline matching: By default patterns match within single lines only. For cross-line patterns like `struct \\{[\\s\\S]*?field`, use `multiline: true`\n",
		},
		Tree: PromptsToolDescription{
			Description: `Display directory structure in a tree format, similar to the Unix tree command`,
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
			Description: `Submit your completed plan for user approval and persist it to disk.

What happens:
- The plan is written as a Markdown file to <configDir>/plans/<timestamp>-<slug>.md
- The plan is displayed to the user with Accept / Reject / Accept-and-Auto-Approve options
- If approved, you'll switch to execution mode with full tool access
- If rejected, the file remains on disk as an audit trail and the user provides feedback

Required parameters:
- title: A short human-readable phrase (≤ 60 chars, no slashes). Becomes the H1 heading and the filename slug.
- plan: The full plan as Markdown using H2 sections in this order - ## Context, ## Files to Modify, ## Current Code, ## Changes, ## Performance Impact, ## Critical Files, ## Edge Cases, ## Verification. Omit any section that is not applicable.

Only call this tool when the plan is final. If you need clarification, ask the user in a normal assistant turn first.`,
		},
		WebFetch: PromptsToolDescription{
			Description: `Fetch content from allowed URLs. Set download=true to save the file to disk automatically. Useful for downloading A2A task artifacts or other files.`,
		},
		WebSearch: PromptsToolDescription{
			Description: `Search the web using Google or DuckDuckGo search engines`,
		},
		Schedule: PromptsToolDescription{
			Description: `Schedule a task that fires on a cron schedule and delivers its output through the same messaging channel that triggered the current session (e.g. Telegram).

IMPORTANT - clarify intent before creating: ALWAYS confirm with the user whether they want the task to run **once** (e.g. "remind me at 6pm today to call mum") or **recurring** (e.g. "send me a quote every morning"). If their request is ambiguous, ASK them - do not guess. Set run_once=true for one-off tasks; the scheduler will delete the job automatically after it fires once. Set run_once=false (or omit) for recurring tasks.

Each fire creates a brand-new agent session - no context is carried between runs. Choose narrow, specific prompts to avoid wasted compute.

Operations:
- create: Add a new scheduled job. Required: cron_expression, prompt. Optional: run_once, name, description, model.
- list: List all scheduled jobs.
- get: Fetch one job. Required: job_id.
- update: Modify an existing job. Required: job_id. Any of cron_expression, prompt, run_once, name, description, model can be updated.
- delete: Remove a job. Required: job_id.

Routing (channel + recipient) is derived automatically from the current session - you never pass it. The tool can therefore only be used from a channel-driven session (e.g. when responding to a Telegram message); it will fail with a clear error if invoked from any other context.

Cron expression format: standard 5-field crontab syntax (minute hour day-of-month month day-of-week). The "@every <duration>" descriptor is also supported. Examples:
- "0 8 * * *"       - every day at 08:00 (recurring)
- "*/15 * * * *"    - every 15 minutes (recurring)
- "0 9 * * 1-5"     - weekdays at 09:00 (recurring)
- "@every 1h"       - every hour (recurring)
- "0 18 26 4 *"     - April 26 at 18:00 (use with run_once=true for "today at 6pm")

For one-off jobs, build a cron expression that pinpoints the exact moment (use the current date's day/month) and set run_once=true. The job will fire once at that time and then be deleted automatically.

The scheduler runs inside the 'infer channels-manager' daemon. Jobs only fire while that daemon is running.`,
		},
		A2AQueryAgent: PromptsToolDescription{
			Description: `Retrieve an A2A agent's metadata card showing its capabilities and configuration. Use ONLY for discovering what an agent can do. For asking questions or requesting work from an agent, use the Task tool instead.`,
		},
		A2AQueryTask: PromptsToolDescription{
			Description: `Query the status and result of a specific A2A task. Returns the complete task object including status, artifacts, and message data. IMPORTANT: When you submit a task via A2A_SubmitTask, it automatically monitors the task in the background and emits an event when complete - you will be notified automatically. DO NOT manually query recently submitted tasks during background monitoring. Only use this tool to: 1) Check tasks from previous conversations, 2) Check tasks submitted outside this session, or 3) Get detailed results AFTER you receive a completion notification.`,
		},
		A2ASubmitTask: PromptsToolDescription{
			Description: `Submit work to an A2A agent server and delegate it to run in the background. IMPORTANT: This tool returns IMMEDIATELY after submission. DO NOT poll, query, or download artifacts right after submission. The system automatically monitors the task in the background and you will be AUTOMATICALLY NOTIFIED when it completes - the result will appear in the conversation. After submission, you MUST wait for the automatic notification before taking any follow-up actions. You can tell the user the task is running and you're waiting for it to complete. Use this for ANY interaction where you need an agent to respond with answers or complete work. The A2A_QueryTask tool is ONLY for retrieving metadata/capabilities or checking status of previously submitted tasks, NOT for polling just-submitted tasks.`,
		},
		MouseMove: PromptsToolDescription{
			Description: `Moves the mouse cursor to absolute screen coordinates. Requires user approval unless in auto-accept mode.`,
		},
		MouseClick: PromptsToolDescription{
			Description: `Performs a mouse click. Can click at current position or move to coordinates first. Supports left, right, and middle buttons. Requires user approval unless in auto-accept mode.`,
		},
		MouseScroll: PromptsToolDescription{
			Description: `Scrolls the mouse wheel up or down. Useful for navigating web pages, documents, and long content. Positive values scroll down, negative values scroll up.`,
		},
		KeyboardType: PromptsToolDescription{
			Description: `Types text or sends key combinations INTO GUI APPLICATIONS at the current cursor position (e.g., typing in a text editor, browser search box, or form field). DO NOT use this to run shell commands - use the Bash tool instead. To open applications on macOS, use Bash with 'open -a AppName'. Requires user approval unless in auto-accept mode. Note: Exactly one of 'text' or 'key_combo' must be provided.`,
		},
		GetFocusedApp: PromptsToolDescription{
			Description: `Gets the currently focused (frontmost) application. Returns the application name and bundle identifier. Use this before performing computer use actions to verify the correct application is in focus.`,
		},
		ActivateApp: PromptsToolDescription{
			Description: `Activates (brings to foreground/focus) a specific application by its bundle identifier. Use GetFocusedApp first to check the current state, then use this tool to switch to the target app before performing computer use actions. After activation, wait briefly before sending keyboard/mouse commands.`,
		},
		GetLatestScreenshot: PromptsToolDescription{
			Description: `Retrieves the latest screenshot from the buffer. This is a read-only operation that does NOT require approval. Use this tool to see the current state of the screen. Screenshots are automatically captured every few seconds when streaming is enabled.`,
		},
	}
}
