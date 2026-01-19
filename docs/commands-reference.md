# Commands Reference

[← Back to README](../README.md)

This document provides comprehensive documentation for all commands available in the Inference Gateway CLI.

## Table of Contents

- [Project Initialization](#project-initialization)
- [Configuration Management](#configuration-management)
- [Agent Management](#agent-management)
- [Chat and Agent Execution](#chat-and-agent-execution)
- [Utility Commands](#utility-commands)

---

## Project Initialization

### `infer init`

Initialize a new project with Inference Gateway CLI. This creates:

- `.infer/` directory with:
  - `config.yaml` - Main configuration file for the project
  - `.gitignore` - Ensures sensitive files are not committed to version control

This is the recommended command to start working with Inference Gateway CLI in a new project.

**Options:**

- `--overwrite`: Overwrite existing files if they already exist
- `--userspace`: Initialize configuration in user home directory (`~/.infer/`)

**Examples:**

```bash
# Initialize project-level configuration (default)
infer init
infer init --overwrite

# Initialize userspace configuration (global fallback)
infer init --userspace
```

---

## Configuration Management

### `infer config`

Manage CLI configuration settings including models, system prompts, and tools.

### `infer config init`

Initialize a new `.infer/config.yaml` configuration file in the current directory. This creates only the
configuration file with default settings.

For complete project initialization, use `infer init` instead.

**Options:**

- `--overwrite`: Overwrite existing configuration file
- `--userspace`: Initialize configuration in user home directory (`~/.infer/`)

**Examples:**

```bash
# Initialize project-level configuration (default)
infer config init
infer config init --overwrite

# Initialize userspace configuration (global fallback)
infer config init --userspace
```

### `infer config agent set-model`

Set the default model for chat sessions. When set, chat sessions will automatically use this model
without showing the model selection prompt.

**Examples:**

```bash
infer config agent set-model openai/gpt-4-turbo
infer config agent set-model anthropic/claude-opus-4-1-20250805
```

### `infer config agent set-system`

Set a system prompt that will be included with every chat session, providing context and instructions to the AI model.

**Examples:**

```bash
infer config agent set-system "You are a helpful assistant."
infer config agent set-system "You are a Go programming expert."
```

### `infer config agent set-max-turns`

Set the maximum number of turns for agent sessions.

**Examples:**

```bash
infer config agent set-max-turns 100
```

### `infer config agent set-max-concurrent-tools`

Set the maximum number of tools that can execute concurrently.

**Examples:**

```bash
infer config agent set-max-concurrent-tools 5
```

### `infer config agent verbose-tools`

Enable or disable verbose tool output for agent sessions.

**Examples:**

```bash
infer config agent verbose-tools enable
infer config agent verbose-tools disable
```

### `infer config export`

Manage export settings for conversation exports.

**Subcommands:**

- `set-model <model>`: Set the model used for generating export summaries
- `show`: Display current export configuration

**Examples:**

```bash
infer config export set-model anthropic/claude-4.1-haiku
infer config export show
```

### `infer config tools`

Manage tool execution settings for LLMs, including enabling/disabling tools, managing whitelists, and security settings.

**Subcommands:**

- `enable`: Enable tool execution for LLMs
- `disable`: Disable tool execution for LLMs
- `list [--format text|json]`: List whitelisted commands and patterns
- `validate <command>`: Validate if a command is whitelisted
- `exec <command> [--format text|json]`: Execute a whitelisted command directly
- `safety`: Manage safety approval settings
  - `enable`: Enable safety approval prompts
  - `disable`: Disable safety approval prompts
  - `status`: Show current safety approval status
- `sandbox`: Manage sandbox directories for security
  - `list`: List all sandbox directories
  - `add <path>`: Add a protected path to the sandbox
  - `remove <path>`: Remove a protected path from the sandbox
- `bash`: Manage Bash tool settings
  - `enable`: Enable Bash tool
  - `disable`: Disable Bash tool
- `grep`: Manage Grep tool settings
  - `enable`: Enable Grep tool
  - `disable`: Disable Grep tool
  - `set-backend <backend>`: Set grep backend ("ripgrep" or "go")
  - `status`: Show current Grep tool configuration
- `web-search`: Manage WebSearch tool settings
  - `enable`: Enable WebSearch tool
  - `disable`: Disable WebSearch tool
- `web-fetch`: Manage WebFetch tool settings
  - `enable`: Enable WebFetch tool
  - `disable`: Disable WebFetch tool
  - `list`: List whitelisted domains
  - `add-domain <domain>`: Add a domain to whitelist
  - `remove-domain <domain>`: Remove a domain from whitelist
  - `cache`: Manage WebFetch cache
    - `status`: Show cache status
    - `clear`: Clear cache
- `github`: Manage GitHub tool settings
  - `enable`: Enable GitHub tool
  - `disable`: Disable GitHub tool
  - `status`: Show current GitHub tool configuration
  - `set-token <token>`: Set GitHub personal access token
  - `set-owner <owner>`: Set default GitHub owner/organization
  - `set-repo <repo>`: Set default GitHub repository

**Examples:**

```bash
# Enable/disable tool execution
infer config tools enable
infer config tools disable

# List whitelisted commands
infer config tools list
infer config tools list --format json

# Validate and execute commands
infer config tools validate "ls -la"
infer config tools exec "git status"

# Manage global safety settings (approval prompts)
infer config tools safety enable   # Enable approval prompts for all tool execution
infer config tools safety disable  # Disable approval prompts (execute tools immediately)
infer config tools safety status   # Show current safety approval status

# Manage excluded paths
infer config tools sandbox list
infer config tools sandbox add ".github/"
infer config tools sandbox remove "test.txt"

# Manage individual tools
infer config tools bash enable
infer config tools bash disable

infer config tools grep set-backend ripgrep
infer config tools grep status

infer config tools web-search enable
infer config tools web-search disable

infer config tools web-fetch add-domain "example.com"
infer config tools web-fetch list
infer config tools web-fetch cache status
infer config tools web-fetch cache clear

infer config tools github set-token "%GITHUB_TOKEN%"
infer config tools github set-owner "my-org"
infer config tools github set-repo "my-repo"
infer config tools github status
```

---

## Agent Management

### `infer agents`

Manage A2A (Agent-to-Agent) agent configurations. This command allows you to configure and manage
connections to specialized A2A agents for task delegation and distributed processing.

**Subcommands:**

- `init`: Initialize agents.yaml configuration file
- `add <name> [url]`: Add a new A2A agent endpoint
- `update <name> [flags]`: Update an existing agent's configuration
- `list`: List all configured agents
- `show <name>`: Show details for a specific agent
- `remove <name>`: Remove an agent from configuration

**Update Flags:**

- `--url <url>`: Update agent URL
- `--model <model>`: Update model for the agent
- `--oci <image>`: Update OCI image reference
- `--artifacts-url <url>`: Update artifacts server URL
- `--environment <KEY=VALUE>`: Set environment variables
- `--run`: Enable local execution with Docker

**Examples:**

```bash
# Initialize agents configuration
infer agents init

# Add a known agent (with defaults)
infer agents add browser-agent

# Add a known agent with custom model
infer agents add documentation-agent --model "anthropic/claude-4-5-sonnet"

# Add a custom remote agent
infer agents add code-reviewer https://agent.example.com

# Add a local agent with OCI image
infer agents add test-runner https://localhost:8081 --oci ghcr.io/org/test-runner:latest --run

# List all agents
infer agents list

# Show agent details
infer agents show browser-agent

# Update agent URL
infer agents update browser-agent --url http://browser-agent:9090

# Update agent model
infer agents update browser-agent --model "deepseek/deepseek-chat"

# Update multiple settings
infer agents update browser-agent --url http://browser-agent:9090 --model "openai/gpt-4"

# Remove agent
infer agents remove browser-agent
```

For more details on A2A agents, see the [Tools Reference - A2A Tools](tools-reference.md#agent-to-agent-communication) section.

---

## Chat and Agent Execution

### `infer chat`

Start an interactive chat session with model selection. Provides a conversational interface where you can
select models and have conversations.

**Features:**

- Interactive model selection
- Conversational interface
- Real-time streaming responses
- **Scrollable chat history** with mouse wheel and keyboard support

**Navigation Controls:**

- **Mouse wheel**: Scroll up/down through chat history
- **Arrow keys** (`↑`/`↓`) or **Vim keys** (`k`/`j`): Scroll one line at a time
- **page up/page down**: Scroll by page
- **home/end**: Jump to top/bottom of chat history
- **shift+↑/shift+↓**: Half-page scrolling
- **ctrl+o** (default): Toggle expanded view of tool results (configurable via `tools_toggle_tool_expansion`)
- **ctrl+k** (default): Toggle expanded view of model thinking blocks (configurable via `display_toggle_thinking`)
- **shift+tab**: Cycle agent mode (Standard → Plan → Auto-Accept)

**Agent Modes:**

The chat interface supports three operational modes that can be toggled with **shift+tab**:

- **Standard Mode** (default): Normal operation with all configured tools and approval checks enabled.
  The agent has access to all tools defined in your configuration and will request approval for
  sensitive operations (Write, Edit, Delete, Bash, etc.).

- **Plan Mode**: Read-only mode designed for planning and analysis. In this mode, the agent:
  - Can only use Read, Grep, Tree, and A2A_QueryAgent tools to gather information
  - Is instructed to analyze tasks and create detailed plans without executing changes
  - Provides step-by-step breakdowns of what would be done in Standard mode
  - **Plan Approval**: When the agent completes planning, you'll be prompted to:
    - **Accept**: Approve the plan and continue (stays in Plan Mode)
    - **Reject** (n or Esc): Reject the plan and provide feedback or changes
    - **Accept & Auto-Approve** (a): Accept the plan AND switch to Auto-Accept mode for execution
  - Useful for understanding codebases or previewing changes before implementation

- **⚡ Auto-Accept Mode** (YOLO mode): All tool executions are automatically approved without prompting. The agent:
  - Has full access to all configured tools
  - Bypasses all approval checks and safety guardrails
  - Executes modifications immediately without confirmation
  - Ideal for trusted workflows or when rapid iteration is needed
  - **Use with caution** - ensure you have backups and version control

The current mode is displayed below the input field when not in Standard mode. Toggle between modes
anytime during a chat session.

**System Reminders:**

The chat interface supports configurable system reminders that can provide periodic contextual
information to the AI model during conversations. These reminders help maintain context and provide
relevant guidance throughout the session.

- **Customizable interval**: Set how often reminders appear (in number of messages)
- **Dynamic content**: Reminders can contain contextual information based on the current state
- **Non-intrusive**: Reminders are sent to the AI model but don't interrupt the user experience
- **Configurable**: Enable/disable and customize reminder content through configuration

**Examples:**

```bash
infer chat
```

### `infer agent`

Execute a task using an autonomous agent in background mode. The CLI will work iteratively until the
task is considered complete. Particularly useful for SCM tickets like GitHub issues.

**Features:**

- **Autonomous execution**: Agent works independently to complete tasks
- **Iterative processing**: Continues until task completion criteria are met
- **Tool integration**: Full access to all available tools (Bash, Read, Write, etc.)
- **Parallel tool execution**: Executes multiple tool calls simultaneously for improved
  efficiency
- **Background operation**: Runs without interactive user input
- **Task completion detection**: Automatically detects when tasks are complete
- **Configurable concurrency**: Control the maximum number of parallel tool executions (default: 5)
- **JSON output**: Structured JSON output for easy parsing and integration
- **Multimodal support**: Process images and files with vision-capable models
- **Session resumption**: Resume previous agent sessions to continue work from where it left off

**Options:**

- `-m, --model`: Model to use for the agent (e.g., openai/gpt-4)
- `-f, --files`: Files or images to include (can be specified multiple times)
- `--session-id`: Resume an existing agent session by conversation ID
- `--no-save`: Disable saving conversation to database

**Examples:**

```bash
# Execute a task described in a GitHub issue
infer agent "Please fix the github issue 38"

# Use a specific model for the agent
infer agent --model "openai/gpt-4" "Implement the feature described in issue #42"

# Debug a failing test
infer agent "Debug the failing test in PR 15"

# Refactor code
infer agent "Refactor the authentication module to use JWT tokens"

# Analyze screenshots with vision-capable models
infer agent "Analyze this screenshot and identify the UI issue" --files screenshot.png

# Process multiple images
infer agent "Compare these diagrams and suggest improvements" -f diagram1.png -f diagram2.png

# Mix images and code files using @filename syntax
infer agent "Review @app.go and @architecture.png and suggest refactoring"

# Combine --files flag with @filename references
infer agent "Analyze @error.log and this screenshot" --files debug-screen.png

# Session resumption - list conversations to find session IDs
infer conversations list

# Resume an existing session with new instructions
infer agent "continue fixing the authentication bug" --session-id abc-123-def

# Resume with additional files
infer agent "analyze these new error logs" --session-id abc-123-def --files error.log

# Resume without saving (testing mode)
infer agent "try a different refactoring approach" --session-id abc-123-def --no-save
```

**Session Resumption:**

The agent command supports resuming previous sessions, allowing you to continue work from where it left off:

- Use `infer conversations list` to find available session IDs
- Pass `--session-id <id>` to resume a specific session
- The agent will load the full conversation history and continue from there
- Your task description is appended as a new user message
- Turn counter resets to full budget when resuming
- Session ID is preserved for continued persistence
- If session ID is invalid or not found, a warning is shown and a fresh session starts

**Example JSON Status Messages:**

When resuming a session, the agent outputs structured JSON status messages:

```json
// Successful resume
{"type":"info","message":"Resumed agent session","session_id":"abc-123","message_count":15,"timestamp":"2025-12-11T..."}

// Failed resume (warning)
{
  "type": "warning",
  "message": "Could not load session, starting fresh",
  "session_id": "invalid",
  "error": "failed to load conversation: not found",
  "timestamp": "2025-12-11T..."
}

// New session
{"type":"info","message":"Starting new agent session","session_id":"new-uuid","model":"openai/gpt-4","timestamp":"2025-12-11T..."}
```

**Image and File Support:**

The agent command supports multimodal content for vision-capable models:

- Use `--files` or `-f` flag to attach images or files
- Use `@filename` syntax in the task description to reference files
- Supported image formats: PNG, JPEG, GIF, WebP
- Images are automatically encoded as base64 and sent as multimodal content
- Text files are embedded in code blocks
- Requires gateway configuration: `ENABLE_VISION=true`

---

## Utility Commands

### `infer status`

Check the status of the inference gateway including health checks and resource usage.

**Examples:**

```bash
infer status
```

### `infer conversation-title`

Manage AI-powered conversation title generation. The CLI can automatically generate descriptive titles
for conversations to improve organization and searchability.

**Subcommands:**

- `generate [conversation-id]`: Generate titles for conversations (all or specific)
- `status`: Show title generation status and statistics
- `daemon`: Run title generation daemon in background

**Examples:**

```bash
# Generate titles for all conversations without titles
infer conversation-title generate

# Generate title for a specific conversation
infer conversation-title generate conv-12345

# Check title generation status
infer conversation-title status

# Run daemon for automatic title generation
infer conversation-title daemon
```

**Features:**

- **Automatic Generation**: Titles are generated based on conversation content
- **Batch Processing**: Generate titles for multiple conversations at once
- **Configurable Model**: Use any available model for title generation
- **Background Daemon**: Optional daemon mode for continuous title generation

**Configuration:**

```yaml
conversation:
  title_generation:
    enabled: true
    model: "deepseek/deepseek-chat"
    batch_size: 5
    interval: 30  # seconds between generation attempts
```

For more details, see the [Conversation Title Generation](conversation-title-generation.md) documentation.

### `infer version`

Display version information for the Inference Gateway CLI.

**Examples:**

```bash
infer version
```

---

[← Back to README](../README.md)
