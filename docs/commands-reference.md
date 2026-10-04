# Commands Reference

[← Back to README](../README.md)

**What** - the main `infer` subcommands, their flags and examples, plus the global flags. Run `infer --help` for the full list.
**Why** - the chat TUI covers daily use, but setup, automation and maintenance happen on the command line.
**How** - find the command by group in the table of contents; each section lists its flags and examples.

## Table of Contents

- [Project Initialization](#project-initialization)
- [Configuration Management](#configuration-management)
- [Agent Management](#agent-management)
- [Chat and Agent Execution](#chat-and-agent-execution)
- [Utility Commands](#utility-commands)
- [Global Flags](#global-flags)

---

## Project Initialization

### `infer init`

Initializes the Inference Gateway CLI configuration in your userspace home
directory. This creates:

- `.infer/` under `~/.infer/` with:
  - `config.yaml` - Main configuration file (the shared baseline)
  - `prompts.yaml`, `keybindings.yaml`, `sandbox.yaml`, `tools.yaml`, `channels.yaml`, `heartbeat.yaml`,
    `judge.yaml`, `hooks.yaml`, `reminders.yaml`, `memory.yaml`, `computer_use.yaml`,
    `browser_use.yaml`, `agents.yaml`, `mcp.yaml`, `shortcuts/`, `skills/` - the split config
    files and directories
- `.env.example` template for provider API keys is written by `infer env`,
  not by init.

All state (conversations, logs, history, artifacts, ...) is written under
`~/.infer/`, never into a project directory. To override a setting for a
single project, use `infer config set --project`, which writes a sparse
`./.infer/config.yaml` override on top of the userspace baseline.

This is the recommended command to start working with Inference Gateway CLI in a new project.

**Options:**

- `--overwrite`: Overwrite existing files if they already exist
- `--skip-migrations`: Skip running database migrations

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

Initialize the userspace baseline `~/.infer/config.yaml` with default settings.

A project `./.infer/config.yaml` is an *override* layer, not a second full
config - create it with `infer config set --project <key> <value>`, which writes
only the keys you set. Seeding a full default config into a project would shadow
the entire userspace baseline, because project values win key-by-key and project
lists replace (rather than extend) userspace lists.

For complete initialization of the full baseline, use `infer init` instead.

**Options:**

- `--overwrite`: Overwrite existing configuration file

**Examples:**

```bash
infer config init
infer config init --overwrite
```

### `infer config get [key]`

Print the effective value of a configuration key, or the whole config when no key is given. The
value reflects what the CLI actually runs with: built-in defaults, the global `~/.infer/config.yaml`
merged key-by-key with the local `.infer/config.yaml` override when present, and `INFER_*`
environment overrides. Keys are dotted paths into `config.yaml`.

**Options:**

- `-f, --format <yaml|json>`: Output format (default `yaml`)

**Examples:**

```bash
infer config get                          # dump the whole effective config
infer config get agent.model
infer config get tools.bash               # print a whole subtree
infer config get tools.web_search
infer config get tools.web_fetch -f json
```

### `infer config set <key> <value>`

Set a configuration value in `config.yaml`. The value is parsed to the field's type (bool, integer,
number or string); list keys take a comma-separated value that replaces the whole list. Unknown keys
are rejected.

By default the userspace `~/.infer/config.yaml` baseline is updated; pass
`--project` to write a sparse override into the project `.infer/config.yaml`
instead. Project overrides are meant to be committed; they never receive
runtime-generated files.

A running `infer chat` picks the change up when you type `/reload`. Keys it cannot apply mid-session are
named in the status line and take effect on the next start (see the
[shortcuts guide](shortcuts-guide.md#core-shortcuts)). In chat, `/config <request>` (e.g.
`/config set the gateway timeout to 300`) runs this command for you after you confirm - see the
built-in [`config` skill](skills.md#built-in-skills).

**Examples:**

```bash
# Scalars
infer config set agent.model "openai/gpt-4-turbo"
infer config set agent.max_turns 100
infer config set agent.max_concurrent_tools 5
infer config set agent.skills.enabled true

# Tools
# The tools policy lives in ~/.infer/tools.yaml, not config.yaml.
# infer config get tools.* works, but infer config set tools.* is rejected.

# List values (comma-separated, replaces the whole list)
infer config set gateway.include_models "openai/gpt-4o,anthropic/claude-4-opus"

# Write a project-level override into ./.infer/config.yaml instead
infer config set agent.model "openai/gpt-4o" --project
```

> System prompts live in `prompts.yaml` (e.g. `prompts.agent.system_prompt`), which is edited
> directly rather than via `config set`.

Tool *configuration* (enable/disable, allow-lists, sandbox, backends, domains, approval) lives in
`~/.infer/tools.yaml`. `infer config get tools.*` reads it, but `infer config set tools.*` is rejected -
edit `tools.yaml` directly. To run a tool directly or check a command against the allowed list, use the
top-level `infer tools` command below.

### `infer tools`

Run agent tools directly or check whether a bash command is allowed, using the same execution and
validation path as the agent.

**Subcommands:**

- `execute <tool> [json-args] [--format text|json]`: Execute any enabled tool directly
- `validate <command>`: Check whether a bash command would be allowed, without running it

**`execute` flags:**

- `-f, --format text|json`: Output format (default `text`)
- `--approved`: Treat the call as user-approved, which skips the approval check and the bash allow-list
- `--session-id <id>`: Record the call and its result in this conversation, so later turns
  (for example `infer headless --session-id`) see it

**Examples:**

```bash
# Execute a tool (JSON args, exactly as the agent invokes it)
infer tools execute Bash '{"command":"ls -la"}'
infer tools execute Read '{"file_path":"README.md"}'
infer tools execute Tree '{"path":".", "max_depth":2}'

# Machine-readable result for callers that handle approval themselves
infer tools execute Bash '{"command":"gh api user"}' --format json
infer tools execute Bash '{"command":"gh api user"}' --format json --approved

# Validate a bash command against the allowed list
infer tools validate "git status"
```

### `infer mcp`

Manage MCP (Model Context Protocol) servers that extend the agent with external tools. Writes land in the
userspace baseline `~/.infer/mcp.yaml` unless `--project` is given, which writes to `./.infer/mcp.yaml`
instead.

**Subcommands:**

- `add <name> [url]`: Add a new MCP server
- `update <name>`: Update an existing MCP server
- `list`: List all configured MCP servers
- `enable <name>` / `disable <name>`: Enable or disable a single server
- `enable-global` / `disable-global`: Enable or disable MCP globally
- `remove <name>`: Remove an MCP server
- `start [server]`: Start `run: true` MCP servers as detached containers
- `stop [server]`: Stop detached MCP server containers
- `status [server]`: Probe configured MCP servers and report connection state and tool counts

**Options:**

- `--project`: Apply to the project configuration (`./.infer/`) instead of the userspace baseline (`~/.infer/`)

**`add` flags:**

- `--description <text>`, `--enabled`, `--run`: Metadata, enable the server immediately, and auto-start it in a container
- `--oci <image>` (required with `--run`), `--port <port>`: The container image and the port it exposes
- `--startup-timeout <seconds>` (default 60), `--timeout <seconds>`: Container startup budget and connection timeout
- `--include <tools>` / `--exclude <tools>`: Comma-separated tool filters

**`update` flags:**

- `--url`, `--description`, `--enabled`: Replace the URL, description or enabled state
- `--timeout <seconds>`: `-1` leaves it unchanged, `0` falls back to the global timeout
- `--include <tools>` / `--exclude <tools>`: Replace the tool filters, an empty value leaves them unchanged

**`status` flags:**

- `-f, --format <text|json>`: Output format (default `text`)

**Examples:**

```bash
# Add an external server, then an auto-starting container
infer mcp add filesystem http://localhost:3000/sse
infer mcp add demo --run --oci=mcp-demo-server:latest --port=3000

# Point an existing server at a new URL and probe it
infer mcp update filesystem --url=http://localhost:3002/sse
infer mcp status filesystem --format json
```

See [MCP Integration](mcp-integration.md) for the container lifecycle and the full config reference.

### `infer keybindings`

Manage the chat keybindings: assign keys to an action, enable or disable an action, reset to the defaults
and validate the configuration.

**Subcommands:**

- `list`: List all available keybindings with their current keys and enabled state
- `set <action-id> <key1> [key2...]`: Set custom keys for an action
- `enable <action-id>` / `disable <action-id>`: Enable or disable an action
- `reset`: Reset keybindings to defaults
- `validate`: Validate the keybinding configuration

**Options:**

- `--project`: Apply to the project configuration (`./.infer/`) instead of the userspace baseline (`~/.infer/`)

**Examples:**

```bash
infer keybindings set mode_cycle_agent_mode ctrl+m
infer keybindings set chat_enter_key_handler ctrl+enter enter
infer keybindings list --project
```

### `infer migrate`

Run database migrations to update the schema to the latest version. Applied migrations are tracked in the
`schema_migrations` table, so each one runs once. The backend is detected automatically: SQLite, PostgreSQL
and Cloudflare D1 have a relational schema (D1 creates its own on connect), while JSONL, Redis and the
in-memory backend need no migrations at all.

**Options:**

- `--status`: Show migration status without applying migrations

**Examples:**

```bash
infer migrate
infer migrate --status
```

See [Database Migrations](database-migrations.md) for the migration history and how to add one.

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
- `status [name]`: Probe configured agents and report readiness (`--format json` for scripts)
- `start [name]`: Start `run: true` agents as detached containers that chat and headless sessions reuse
- `stop [name]`: Stop those detached containers
- `remove <name>`: Remove an agent from configuration

**Options:**

- `--project`: Apply to the project configuration (`./.infer/`) instead of the userspace baseline (`~/.infer/`)

A running `infer chat` applies `add`, `update` and `remove` when you type `/reload`. In chat,
`/config add the mock-agent` runs `infer agents add` for you after you confirm.

**Update Flags:**

- `--url <url>`: Update agent URL
- `--model <model>`: Update model for the agent
- `--oci <image>`: Update OCI image reference
- `--tag <tag>`: Replace the tag of the agent's default image (known agents only, mutually exclusive with `--oci`)
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

# Add a known agent on a specific image tag (browser-agent ships one tag per browser engine)
infer agents add browser-agent --tag lightpanda

# Pin a known agent to a released version
infer agents add browser-agent --tag chromium-0.8.0

# Add a custom remote agent
infer agents add code-reviewer https://agent.example.com

# Add a local agent with OCI image
infer agents add test-runner https://localhost:8081 --oci ghcr.io/org/test-runner:latest --run

# List all agents
infer agents list

# Show agent details
infer agents show browser-agent

# Probe readiness of every configured agent (or one by name)
infer agents status
infer agents status browser-agent --format json

# Keep a run: true agent running across sessions instead of starting one per session
infer agents start browser-agent
infer agents stop browser-agent

# Update agent URL
infer agents update browser-agent --url http://browser-agent:9090

# Update agent model
infer agents update browser-agent --model "deepseek/deepseek-v4-pro"

# Switch to another image tag
infer agents update browser-agent --tag lightpanda

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
- **Inline history auto-completion** of previous inputs
- Select text by holding Shift (Option on macOS terminals) while dragging

**Navigation Controls:**

- **Mouse wheel**: Scroll up/down through chat history
- **Arrow keys** (`↑`/`↓`) or **Vim keys** (`k`/`j`): Scroll one line at a time
- **page up/page down**: Scroll by page
- **home/end**: Jump to top/bottom of chat history
- **shift+↑/shift+↓**: Half-page scrolling
- **ctrl+o** (default): Toggle expanded view of tool results (configurable via `tools_toggle_tool_expansion`)
- **ctrl+k** (default): Toggle expanded view of model thinking blocks (configurable via `display_toggle_thinking`)
- **shift+tab**: Cycle agent mode (Standard → Plan → Auto-Accept → Auto+Judge)
- **↓** (when not navigating input history): Select the status indicators below the input.
  `←`/`→` (or `tab`/`shift+tab`) move between the actionable indicators, **enter** opens the
  matching view (model indicator → model selection, theme indicator → theme selection,
  `A2A:` indicator → agents view, `Tools:` indicator → available tools),
  **↑**/**esc** return to the input, and
  typing any other key lands back in the input seamlessly
- **↓** (on the status indicators, while background jobs are listed below them): Select the job list.
  **↑**/**↓** pick a job, **enter** shows that job's transcript in place of the chat's, **esc** goes back to
  the chat's transcript and then to the input. On a sub-agent or A2A row, **c** asks the job to wrap up and
  report (see [Subagents](subagents.md#asking-a-job-to-wrap-up)). The hint under the rows lists the key
  whenever the selected row can take it

**Agent Modes:**

**Options:**

- `--web`: Start the web terminal interface instead of the in-terminal TUI
- `--port <port>`: Web server port (default 3000)
- `--host <host>`: Web server host (default localhost)
- `--ssh-host <host>`: Remote SSH server hostname, which runs the TUI on the remote host
- `--ssh-user <user>`: Remote SSH username
- `--ssh-port <port>`: Remote SSH port (default 22)
- `--ssh-no-install`: Disable auto-installation of infer on the remote host
- `--ssh-command <path>`: Path to the infer binary on the remote host (default infer)
- `--session-id <id>`: Resume an existing chat session by conversation ID

The web terminal flags are covered in [Web Terminal](web-terminal.md).

The chat interface supports four operational modes that can be toggled with **shift+tab**:

- **Standard Mode** (default): Normal operation with all configured tools and approval checks enabled.
  The agent has access to all tools defined in your configuration and will request approval for
  sensitive operations (Write, Edit, Delete, Bash, etc.).

- **Plan Mode**: Read-only mode designed for planning and analysis. In this mode, the agent:
  - Can only use Read, Grep, Tree, A2A_QueryAgent, Agent (read-only exploration
    subagents only), TodoWrite, AskUserQuestion, Wait, and RequestPlanApproval tools
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

- **⚖ Auto+Judge Mode**: Autonomous with a gate: tool calls that would prompt a human are decided by an LLM judge
  (one call per gated tool) instead of a human, so the agent runs unattended but not unrestricted:
  - Uses the standard approval rules - allow-listed bash commands pass without a judge call
  - Gated calls are decided by the judge against your latest request; rejections arrive with the judge's reason
  - The model can ask you to override a rejection with the `RequestApproval` tool: the regular approval box
    opens with the judge's reason; approve runs that one call with the judge bypassed, reject feeds the
    decision back to the model
  - Configured in `judge.yaml` (`model`, `timeout`, `max_tokens`, `on_error`, `prompt`) - see [Judge Mode](judge-mode.md)
  - Ideal for CI and headless runs where an approval prompt would deadlock

The current mode is displayed below the input field when not in Standard mode. Toggle between modes
anytime during a chat session.

**System Reminders:** short `<system-reminder>` messages injected at agent-loop hook points to keep
durable guidance in context. See [Reminders & Command Hooks](hooks.md).

**Examples:**

```bash
infer chat
```

### `infer headless`

Execute a task using an autonomous agent in headless (non-interactive) mode.
The CLI works iteratively until the task is considered complete. Particularly useful
for SCM tickets like GitHub issues, CI/CD pipelines, and automated workflows.

**Features:**

- **Autonomous execution**: Agent works independently to complete tasks
- **Iterative processing**: Continues until task completion criteria are met
- **Tool integration**: Full access to all available tools (Bash, Read, Write, etc.)
- **Parallel tool execution**: Executes multiple tool calls simultaneously for improved
  efficiency
- **Background operation**: Runs without interactive user input
- **Task completion detection**: Automatically detects when tasks are complete
- **Configurable concurrency**: Control the maximum number of parallel tool executions (default: 5)
- **Multiple output formats**: `--format json|json-pretty|ag-ui|text` for different consumption patterns
- **Multimodal support**: Process images and files with vision-capable models
- **Session resumption**: Resume previous sessions to continue work from where it left off
- **Slash commands**: The chat shortcuts work here too - see below

**Slash commands:**

A task that starts with a registered shortcut runs that command instead of being sent
to the model verbatim:

- Commands that produce a prompt (`/init`, a custom shortcut with a snippet) run that
  prompt as the task.
- Commands that answer by themselves (`/help`, `/context`, `/cost`, `/stats`, `/traces`,
  `/clear`, `/new`, `/compact`, custom shortcuts) print their output and exit without
  calling a model.
- Commands that only open a TUI panel (`/diff`, `/explorer`, `/tools`, `/conversations`,
  `/agents`, `/tasks`, `/theme`) say so and exit 0.
- Anything else keeping a leading slash - a skill invocation like `/maintainer`, a file
  path - is passed to the model unchanged.

```bash
infer headless "/init"      # writes AGENTS.md from the configured init prompt
infer headless "/cost"      # prints the session cost breakdown, no model call
```

**Options:**

- `-m, --model`: Model to use (e.g. openai/gpt-4)
- `-f, --files`: Files or images to include (can be specified multiple times)
- `--session-id`: Resume an existing session by conversation ID
- `--no-save`: Disable saving conversation to database
- `--require-approval`: Enable IPC-based tool approval via stdin/stdout (used by channel manager)
- `--heartbeat`: Use heartbeat system prompt (used by the heartbeat service)
- `--remote`: Use remote-control system prompt (used by the daemon)
- `--result-file`: Write the final assistant message and outcome as JSON to this path on exit
- `--format json|json-pretty|ag-ui|text`: Output format (default json)
- `--mode`: Agent mode: standard, plan, auto, auto-with-judge (env: `INFER_AGENT_MODE`); a value that fails
     validation, or `auto-with-judge` with no resolvable judge model, fails before the gateway or agent starts
- `--serve`: Run as a long-lived worker that takes no task, runs one turn per `run_agent_input` frame read on stdin
  and writes each turn as one AG-UI run (implies `--format ag-ui`, see [Serve worker](ag-ui-output.md#serve-worker))
- `--keep-alive`: After the task, keep running: each `run_agent_input` frame read on stdin runs as a further turn in
  the same session, every finished turn is reported as one `subagent_turn` line on stdout, and stdin EOF ends the
  run. The `Agent` tool spawns its headless subagents this way (see [Headless Subagents](subagents.md#headless-subagents))

**Examples:**

```bash
# Execute a task described in a GitHub issue
infer headless "Please fix the github issue 38"

# Use a specific model
infer headless --model "openai/gpt-4" "Implement the feature described in issue #42"

# Debug a failing test
infer headless "Debug the failing test in PR 15"

# Refactor code
infer headless "Refactor the authentication module to use JWT tokens"

# Analyze screenshots with vision-capable models
infer headless "Analyze this screenshot and identify the UI issue" --files screenshot.png

# Process multiple images
infer headless "Compare these diagrams and suggest improvements" -f diagram1.png -f diagram2.png

# Mix images and code files using @filename syntax
infer headless "Review @app.go and @architecture.png and suggest refactoring"

# Combine --files flag with @filename references
infer headless "Analyze @error.log and this screenshot" --files debug-screen.png

# Output as AG-UI protocol events
infer headless --format ag-ui "fix the failing test"

# Long-lived worker: one AG-UI run per run_agent_input frame on stdin
infer headless --serve --session-id abc-123-def

# Session resumption - list conversations to find session IDs
infer conversations list

# Resume an existing session with new instructions
infer headless "continue fixing the authentication bug" --session-id abc-123-def

# Resume with additional files
infer headless "analyze these new error logs" --session-id abc-123-def --files error.log

# Resume without saving (testing mode)
infer headless "try a different refactoring approach" --session-id abc-123-def --no-save
```

**Session Resumption:**

The headless command supports resuming previous sessions, allowing you to continue work from where it left off:

- Use `infer conversations list` to find available session IDs
- Pass `--session-id <id>` to resume a specific session
- The session history is loaded from storage and the new task description is appended
- Turn counter resets to full budget when resuming
- Session ID is preserved for continued persistence

**Image and File Support:**

The headless command supports multimodal content for vision-capable models:

- Use `--files` or `-f` flag to attach images or files
- Use `@filename` syntax in the task description to reference files
- Supported image formats: PNG, JPEG, GIF, WebP
- Images are automatically encoded as base64 and sent as multimodal content
- Text files are embedded in code blocks
- Requires gateway configuration: `VISION_ENABLED=true`

### `infer daemon`

Run the long-lived hub. It hosts whichever subsystems are enabled and refuses to start when none is:

- **channels** (`channels.enabled`), see [Channels](channels.md)
- **scheduler** (`tools.schedule.enabled`), see [Scheduling](scheduling.md)
- **heartbeat** (`heartbeat.enabled`), see [Heartbeat](heartbeat.md)
- **AG-UI binding** (`daemon.binding.enabled`, or `browser_use.enabled` with `backend: extension`): the WebSocket
  the opentask extension and the desktop app connect to. Each thread (a project dir plus a conversation id) runs
  in its own `infer headless --serve` worker started in that project dir. See
  [Daemon Binding Protocol](browser-extension-protocol.md)

```bash
INFER_DAEMON_BINDING_ENABLED=true INFER_DAEMON_BINDING_TOKEN=<secret> infer daemon
```

The pid file is `~/.infer/run/daemon.pid` and the log is `~/.infer/logs/daemon-<date>.log`, where the session
workers' and job runs' stderr lands too. [infer daemon](daemon.md) covers the workers, the on-demand start, the
pid file and the logs.

---

## Utility Commands

### `infer binaries`

Manage the prebuilt tools (`whisper-cli`, `ffmpeg`, `llama-tts`) published by
[inference-gateway/binaries](https://github.com/inference-gateway/binaries) in
`~/.infer/bin/tools`. The CLI is the single owner of these binaries on a local
machine: install.sh verifies against the release's `checksums.txt` and replaces
any binary whose sha256 differs (a matching one is kept).

**Subcommands:**

- `install [name...]`: Install or upgrade via the release's `install.sh`; no
  names means all tools. `--version <tag>` pins a release (default latest).
- `status [name...]`: Read-only report of each binary as `missing`, `stale` or
  `current` against the latest release; exits non-zero unless all are current.
  On the default approval-free bash allow-list, so agents can check without a
  prompt.

**Examples:**

```bash
infer binaries status ffmpeg
infer binaries install --version v0.5.0
```

### `infer status`

Check the status of the inference gateway including health checks and resource usage.

**Options:**

- `-f, --format <text|json|yaml>`: Output format (default `text`)

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
     Scoped to the current project by default; pass `--all-projects` for every project's conversations.
- `show <session-id>`: Print a single conversation's entries in chronological order.
- `delete <session-id>`: Delete a saved conversation.

**`list` flags:**

- `--limit`, `-l`: Maximum number of conversations to display (default 50).
- `--offset`: Number of conversations to skip, for pagination.
- `--format`, `-f` `text|json`: `text` (default) is human-readable; `json` is machine-readable.
- `--all-projects`: List every project's conversations.

**`show` flags:**

- `--include-hidden`: Include entries marked hidden - system reminders, plan-approval prompts,
  drained background-task results, and the synthetic verify message injected by `infer headless`.
  Off by default.
- `--format text|json`: `text` (default) is human-readable; `json` emits one pretty-printed
  object with `metadata` and `entries`, so a whole conversation parses in one `jq` call.

The `<session-id>` is resolved the same way as `infer headless --session-id`: a literal UUID is
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

- `generate`: Generate titles for all conversations that need them (no arguments)
- `status`: Show title generation status and statistics
- `daemon`: Run title generation daemon in background

**Examples:**

```bash
# Generate titles for all conversations without titles
infer conversation-title generate

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
    model: "" # falls back to agent.model
    batch_size: 10
    interval: 300  # seconds between generation attempts (default: 300 = 5 minutes)
```

For more details, see the [Conversation Title Generation](conversation-title-generation.md) documentation.

### `infer version`

Display version information for the Inference Gateway CLI.

**Examples:**

```bash
infer version
```

### `infer skills`

Manage Agent Skills (reusable `SKILL.md` instruction folders): `list`, `search`, `install`, `uninstall`. See
[Agent Skills](skills.md#installing-skills-from-github) for the flags, the authoring format and discovery locations.

### `infer plugins`

Manage Claude Code-format plugins (skills plus an always-on `AGENTS.md` ruleset): `install`, `list`, `enable`,
`disable`, `update`, `remove`, `enable-hooks`, `disable-hooks`. See [Plugins](plugins.md) for the flags, the mapping and
the security model.

### `infer avatars`

Create and manage the TextToVideo avatar library (`~/.infer/avatars/<name>/` portrait folders): `create`, `list`,
`delete`. See [Text-to-Video](text-to-video.md#avatar-library) for the commands and the layout.

### `infer export`

Export a conversation to a Markdown file.

**Examples:**

```bash
infer conversations list      # Find the session ID
infer export <session-id>     # Writes ~/.infer/projects/<slug>/exports/chat_export_<timestamp>.md
```

### `infer insights`

Analyze past sessions for repeatable workflows and recurring tool failures.

**Examples:**

```bash
infer insights                # Every saved session
infer insights 7d             # Only the last 7 days (also 24h, 30d)
infer insights --model <id>   # Pick the model; defaults to agent.model
```

Writes a markdown report to `~/.infer/insights/`. Needs conversation storage enabled.

Reads the conversation store, the telemetry directory, the log directory and the persistent memory
index (`MEMORY.md`, capped at `memory.max_chars`). `infer reset insights` runs the analysis before the
wipe, so the facts survive in the report even though the memory directory does not.

The logs are the only source that sees a failure which never reached a saved session - a startup
crash, a gateway that never came up, a background job that died. They are deduplicated before the
model sees them: records at or above `logging.insights_min_level` (default `warn`) inside the window
are folded by normalized message, so lines differing only in a path, an id or a number become one
group with a count, a first/last timestamp and one verbatim sample. The report's frontmatter states
how many records were read (`log_records`) and how many groups survived (`log_groups`). Only the
structured `app-*.log` and `daemon-*.log` files and their `.gz` archives are read; `gateway-*.log` is
raw subprocess output with no level or timestamp to filter on, and the gateway's own failures are
logged through zap into `app-*.log` anyway.

The analysis runs on `agent.max_tokens` with a floor of 32000 tokens, because a reasoning model spends that
budget thinking before it answers and too small a value fails the run outright. A larger `agent.max_tokens` raises it. The
written analysis is capped at 200 lines / 16000 characters, so a model that ignores the requested
length cannot flood the report.

Each report records what it cost to produce in its own frontmatter (`analysis_prompt_tokens`,
`analysis_completion_tokens`, `analysis_total_tokens`, and `analysis_reasoning_tokens` where the
provider breaks it out), so the price of a run is visible in the artifact rather than guessed at.

### `infer reset`

Wipe all local runtime state and start fresh.

**Examples:**

```bash
infer reset             # Preview every path that would be deleted; deletes nothing
infer reset confirm     # Perform the wipe
infer reset insights    # Analyze past sessions first, then preview (takes --model)
```

Clears the runtime directories of **every project on this machine**, the persistent memory directory
and the local conversation store. Per-project runtime directories are deleted rather than recreated
empty, and a project whose working directory no longer exists is removed entirely.

Config, custom shortcuts, skills and saved insights are preserved; remote stores (postgres, redis, d1)
are skipped, and a git-backed memory directory syncs back from its remote on the next run.

The `/reset` and `/insights` chat shortcuts are thin YAML wrappers over these commands, written to
`~/.infer/shortcuts/` by `infer init` - edit them like any other shortcut.

### `infer debug`

Diagnostic commands that surface internal agent state.

**Subcommands:**

- `agent system_prompt`: Print the prompt context a chat session would send to the LLM. Pass
  `--tokens` to print per-section size stats (characters, lines, estimated tokens) instead of the
  prompt.

**Examples:**

```bash
# Print the assembled system prompt and context for the current project
infer debug agent system_prompt
```

### `infer stats`

Aggregate the local telemetry recorded under `<config-dir>/telemetry` into a summary: tool calls by name
(count, failure rate, average duration), token usage and cost by model, and sessions by execution and agent
mode. Telemetry is recorded locally when `telemetry.enabled` is true, and optionally pushed to an OTLP
collector as well.

**Options:**

- `-f, --format <text|json>`: Output format (default `text`)
- `--since <window>`: Only include telemetry newer than this window (for example `7d`, `24h`, `30m`). All time by default

**Examples:**

```bash
infer stats
infer stats --since 7d
infer stats --since 24h --format json
```

### `infer traces`

Render the span tree of a session (root session span, then LLM turns, then tool calls) with per-span
durations, read from the local per-session trace file under `<config-dir>/telemetry`. With no argument the
most recent session is shown. Traces need `telemetry.enabled: true` but no OTLP collector to be viewed.

**Options:**

- `-f, --format <text|json>`: Output format (default `text`)
- `--list`: List the sessions that have trace files instead of rendering a tree

**Examples:**

```bash
infer traces
infer traces 1783977086-aac06edf
infer traces --list
infer traces --format json
```

### `infer plans`

View and manage the plans saved by plan-mode sessions. Each plan is persisted to the configured storage
backend when the agent uses the `RequestPlanApproval` tool, and gets an `infer://plans/<id>` URI.

**Subcommands:**

- `list`: List all saved plans with their title and creation time
- `show <plan-id>`: Print the full content of a plan, by ID or by its `infer://plans/<id>` URI

**`list` flags:**

- `-f, --format <text|json>`: Output format (default `text`)

**Examples:**

```bash
infer plans list
infer plans list --format json
infer plans show 2026-07-17-153000-add-user-auth
infer plans show infer://plans/2026-07-17-153000-add-user-auth
```

Output is rendered as styled markdown on a terminal and printed as raw markdown when piped, redirected or
run with `--no-colors`. See [Plan Mode](plan-mode.md) for the workflow that produces the plans.

### `infer shortcuts`

Inspect the slash commands the chat accepts: the built-ins plus custom shortcuts from
`.infer/shortcuts/*.yaml` and `~/.infer/shortcuts/*.yaml`.

**Subcommands:**

- `list`: List the available slash commands

**`list` flags:**

- `-f, --format <text|json>`: Output format (default `text`)

**Examples:**

```bash
infer shortcuts list
infer shortcuts list --format json
```

See [Shortcuts Guide](shortcuts-guide.md) for the built-ins and how to write your own.

### `infer workflow`

Manage the OpenTask Agent GitHub workflow.

**Subcommands:**

- `install [owner/repo]`: Install or update `.github/workflows/tasks.yml` in a GitHub repository via an LLM agent

**`install` flags:**

- `-m, --model <model>`: Model for the install agent and the workflow default
- `--github-app`: Use the GitHub App token variant of the workflow
- `--context <text>` / `--context-file <path>`: Extra instructions for the install agent

**Examples:**

```bash
infer workflow install                       # current repository
infer workflow install owner/repo
infer workflow install owner/repo --model anthropic/claude-fable-5
infer workflow install owner/repo --context "the repo deploys with bun"
```

The agent clones the repository, reads the existing workflow when there is one and applies only
infer-action-related changes, preserving repo-specific customizations. The change lands as a pull request,
and re-running pushes onto the same branch and updates the open install PR instead of creating a new one.

### `infer gpu`

Provision, inspect and destroy on-demand GPU instances running a llama.cpp server, so you pay only for the
hours used. The instance is exposed through the standard llamacpp provider environment variables
(`LLAMACPP_API_URL` / `LLAMACPP_API_KEY`), which makes connecting to it indistinguishable from any other
llamacpp backend.

The RunPod API key is management-plane only, used for the create, list and destroy calls. It is asked for on
first provision and stored as `provisioner.runpod.api_key` in `config.yaml`, or supplied through
`INFER_PROVISIONER_RUNPOD_API_KEY`.

**Subcommands:**

- `provision`: Provision a GPU pod running llama.cpp (interactive)
- `list`: List the instances provisioned by infer
- `status <pod-id>`: Show state, uptime and cost of a pod
- `destroy <pod-id>`: Destroy a pod, which stops billing

**`provision` flags:**

- `--gpu-type <id>`: GPU type id, which skips the interactive picker
- `--model <repo>:<quant>`: Hugging Face GGUF to serve
- `-y, --yes`: Skip the confirmation prompt

**`status` flags:**

- `--wait`: Block until llama.cpp answers, with a progress heartbeat

**`destroy` flags:**

- `-y, --yes`: Skip the confirmation prompt

**Examples:**

```bash
infer gpu provision --model "bartowski/Qwen2.5-7B-Instruct-GGUF:Q4_K_M" -y
infer gpu status <pod-id> --wait
infer gpu list
infer gpu destroy <pod-id> -y
```

## Global Flags

These flags are available on every command:

- `-v, --verbose`: Enable verbose output
- `--no-colors`: Disable ANSI colors in command output (colors are also auto-disabled when stdout is not a terminal or `NO_COLOR` is set)
- `--tools-bash-allow-append <cmds>`: Comma/newline-separated commands added to the bash allow-list in every mode
  (`standard`, `plan`, `auto`); `INFER_TOOLS_BASH_ALLOW_APPEND` takes precedence
- `--reminders-file <path>`: Path to a reminders YAML file, overriding project `.infer/` and `~/.infer/` reminders,
  `INFER_REMINDERS_CONFIG` takes precedence

---

[← Back to README](../README.md)
