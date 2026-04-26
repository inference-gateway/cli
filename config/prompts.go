package config

const (
	PromptsFileName    = "prompts.yaml"
	DefaultPromptsPath = ConfigDirName + "/" + PromptsFileName
)

// PromptsConfig holds every customisable LLM prompt the CLI ships with.
// It mirrors the nested key structure those prompts had when they lived
// under .infer/config.yaml so users can move existing values verbatim.
type PromptsConfig struct {
	Agent        PromptsAgentConfig        `yaml:"agent" mapstructure:"agent"`
	Git          PromptsGitConfig          `yaml:"git" mapstructure:"git"`
	Conversation PromptsConversationConfig `yaml:"conversation" mapstructure:"conversation"`
	Init         PromptsInitConfig         `yaml:"init" mapstructure:"init"`
}

type PromptsAgentConfig struct {
	SystemPrompt       string                      `yaml:"system_prompt" mapstructure:"system_prompt"`
	SystemPromptPlan   string                      `yaml:"system_prompt_plan" mapstructure:"system_prompt_plan"`
	SystemPromptRemote string                      `yaml:"system_prompt_remote" mapstructure:"system_prompt_remote"`
	CustomInstructions string                      `yaml:"custom_instructions" mapstructure:"custom_instructions"`
	SystemReminders    PromptsAgentRemindersConfig `yaml:"system_reminders" mapstructure:"system_reminders"`
}

type PromptsAgentRemindersConfig struct {
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

// DefaultPromptsConfig returns the in-code default prompts. This is the
// single source of truth — `infer init` seeds prompts.yaml from this and
// the runtime overlay falls back to it when fields are missing.
func DefaultPromptsConfig() *PromptsConfig {
	return &PromptsConfig{
		Agent: PromptsAgentConfig{
			SystemPrompt: `Autonomous software engineering agent. Execute tasks iteratively until completion.`,
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
			SystemPromptRemote: `Remote system administration agent. You are operating on a remote machine via SSH.

FOCUS: System operations, service management, monitoring, diagnostics, and infrastructure tasks.

CONTEXT: This is a shared system environment, not a project workspace. Users may be managing servers, containers, services, or general infrastructure.

COMPUTER USE TOOLS:
You have TWO ways to interact with the system:
1. Direct terminal tools (PRIMARY): Bash, Read, Write, Edit, Grep, etc.
2. GUI automation tools (FALLBACK): MouseMove, KeyboardType, MouseClick, GetLatestScreenshot

CRITICAL: ALWAYS prefer direct terminal tools over GUI automation when possible.

When to use DIRECT tools (preferred):
- Reading files: Use Read tool, NOT KeyboardType to open an editor
- Writing files: Use Write/Edit tools, NOT GUI text editor
- Running commands: Use Bash tool, NOT KeyboardType in a terminal window
- Searching code: Use Grep tool, NOT opening files via GUI
- System operations: Use Bash for systemctl, journalctl, docker, etc.

When to use GUI tools (only when necessary):
- Interacting with graphical applications that have no CLI equivalent
- Testing UI behavior or visual elements
- Remote desktop administration tasks that MUST be done through a GUI

Why prefer direct tools:
- 10-100x faster execution (no GUI rendering delays)
- More reliable (no window focus issues, no timing problems)
- Works over SSH without X11 forwarding
- Precise output (structured data, not visual interpretation)
- Lower resource usage (critical for remote systems)`,
			CustomInstructions: ``,
			SystemReminders: PromptsAgentRemindersConfig{
				ReminderText: `<system-reminder>
This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware. If you are working on tasks that would benefit from a todo list please use the TodoWrite tool to create one. If not, please feel free to ignore. Again do not mention this message to the user.
</system-reminder>`,
			},
		},
		Git: PromptsGitConfig{
			CommitMessage: PromptsGitCommitMessageConfig{
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
	}
}
