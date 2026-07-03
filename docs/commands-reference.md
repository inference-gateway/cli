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
- `.env.example` - Template with all provider API environment variables (if not already exists)

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

### `infer env`

Generate a `.env.example` file in the current directory with all the different provider API
environment variables needed by the Inference Gateway. This is a convenient shortcut so you
don't need to remember which providers are available or what environment variables to set.

If `.env.example` already exists, the command will error. Use `--overwrite` to replace it.

If no `.gitignore` exists in the project root, one is created with `.env` added to it.

**Options:**

- `--overwrite`: Overwrite `.env.example` if it already exists

**Examples:**

```bash
# Create .env.example with all provider API keys
infer env

# Overwrite existing .env.example
infer env --overwrite
```

**Next steps after creation:**

```bash
cp .env.example .env
# Edit .env and add your API keys
```

---

## Configuration Management

### `infer config`

Manage CLI configuration with a uniform interface: read any value with `config get`, write any
value with `config set`, and create the file with `config init`. There are no per-setting
subcommands - every `config.yaml` key is reachable by its dotted path.

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

### `infer config get [key]`

Print the effective value of a configuration key, or the whole config when no key is given. The
value reflects what the CLI actually runs with: built-in defaults, the global `~/.infer/config.yaml`
and the local `.infer/config.yaml` (sandbox directories are merged), and `INFER_*` environment
overrides. Keys are dotted paths into `config.yaml`.

**Options:**

- `-f, --format <yaml|json>`: Output format (default `yaml`)

**Examples:**

```bash
infer config get                          # dump the whole effective config
infer config get agent.model
infer config get tools.bash               # print a whole subtree
infer config get tools.sandbox.directories
infer config get tools.web_fetch -f json
```

### `infer config set <key> <value>`

Set a configuration value in `config.yaml`. The value is parsed to the field's type (bool, integer,
number or string); list keys take a comma-separated value that replaces the whole list. Unknown keys
are rejected.

By default the project `.infer/config.yaml` is updated; pass `--userspace` to update
`~/.infer/config.yaml` instead.

**Examples:**

```bash
# Scalars
infer config set agent.model "openai/gpt-4-turbo"
infer config set agent.max_turns 100
infer config set agent.max_concurrent_tools 5
infer config set agent.verbose_tools true
infer config set agent.skills.enabled true
infer config set export.summary_model "anthropic/claude-4.1-haiku"

# Tools
infer config set tools.enabled true
infer config set tools.bash.enabled true
infer config set tools.web_search.enabled true
infer config set tools.grep.backend ripgrep
infer config set tools.safety.require_approval true

# List values (comma-separated, replaces the whole list)
infer config set tools.sandbox.directories ".,/tmp,/data"
infer config set tools.web_fetch.allowed_domains "example.com,github.com"

# Write to userspace (~/.infer/config.yaml) instead of the project
infer config set agent.model "openai/gpt-4o" --userspace
```

> System prompts and per-tool descriptions live in `prompts.yaml` (e.g.
> `prompts.agent.system_prompt`), which is edited directly rather than via `config set`.

Tool *configuration* (enable/disable, allowed, sandbox, backends, domains, approval) is done with
`config get`/`config set` on the `tools.*` keys - see the examples above. To run a tool directly or
check a command against the allowed list, use the top-level `infer tools` command below.

### `infer tools`

Run agent tools directly or check whether a bash command is allowed, using the same execution and
validation path as the agent.

**Subcommands:**

- `execute <tool> [json-args] [--format text|json]`: Execute any enabled tool directly
- `validate <command>`: Check whether a bash command would be allowed, without running it

**Examples:**

```bash
# Execute a tool (JSON args, exactly as the agent invokes it)
infer tools execute Bash '{"command":"ls -la"}'
infer tools execute Read '{"file_path":"README.md"}'
infer tools execute Tree '{"path":".", "max_depth":2}'

# Validate a bash command against the allowed list
infer tools validate "git status"
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
infer agents update browser-agent --model "deepseek/deepseek-v4-pro"

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
- **↓** (when not navigating input history): Select the status indicators below the input.
  `←`/`→` (or `tab`/`shift+tab`) move between the actionable indicators, **enter** opens the
  matching view (model indicator → model selection, theme indicator → theme selection,
  background-jobs `⚙` indicator → task management), **↑**/**esc** return to the input, and
  typing any other key lands back in the input seamlessly

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
    - **Accept** (Enter/y): Accept the plan and switch to Auto-Accept mode for execution
    - **Reject** (n or Esc): Reject the plan and provide feedback or changes
    - **Approve Each Step** (s): Accept the plan but stay in Standard mode, approving each action
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

### `infer conversations`

Inspect saved conversation history from the configured storage backend (works with `jsonl`,
`sqlite`, `postgres`, `redis`, and `memory` - the command loads through the storage layer
rather than reading files directly).

**Subcommands:**

- `list`: List saved conversations with metadata (id, title, message/request counts, tokens, cost).
- `show <session-id>`: Print a single conversation's entries in chronological order.

**`show` flags:**

- `--include-hidden`: Include entries marked hidden - system reminders, plan-approval prompts,
  drained background-task results, and the synthetic verify message injected by `infer agent`.
  Off by default.
- `--format text|json`: `text` (default) is human-readable; `json` emits one JSON object per
  line (NDJSON), matching the `infer agent` stdout shape for piping into `jq` or log scrapers.

The `<session-id>` is resolved the same way as `infer agent --session-id`: a literal UUID is
used as-is, while any other value is treated as a session group key and resolved to that
group's current session id (registering the group if it is new).

**Examples:**

```bash
# List conversations to find a session id
infer conversations list

# Show a conversation's entries (hidden entries omitted)
infer conversations show 12345678-1234-1234-1234-123456789abc

# Show by session group name (e.g. a channel group key)
infer conversations show channel-telegram-12345

# Include hidden entries such as system reminders
infer conversations show <session-id> --include-hidden

# One JSON object per line for piping into jq
infer conversations show <session-id> --format json | jq .
```

See [conversation-storage.md](conversation-storage.md) for backend configuration.

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
    model: "deepseek/deepseek-v4-pro"
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
