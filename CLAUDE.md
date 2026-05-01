# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the **Inference Gateway CLI** - a Go-based command-line interface for managing and interacting with AI inference services.
It provides interactive chat, autonomous agent capabilities, and extensive tool execution for AI models.

**Key Technology Stack:**

- **Language**: Go 1.26+
- **UI Framework**: Bubble Tea (TUI framework)
- **Gateway Integration**: Via `inference-gateway/sdk` and `inference-gateway/adk`
- **Storage Backends**: JSONL (default), SQLite, PostgreSQL, Redis, In-memory
- **Build Tool**: Task (Taskfile)
- **Environment**: Flox (development environment manager)

## Common Commands

### Building and Testing

```bash
# Build the binary
task build

# Run all tests
task test

# Run tests with verbose output
task test:verbose

# Run tests with coverage
task test:coverage

# Format code
task fmt

# Run linter
task lint
```

### Running the CLI

```bash
# Run locally without building
task run CLI_ARGS="chat"
task run CLI_ARGS="status"
task run CLI_ARGS="version"

# Or after building
./infer chat
./infer agent "task description"
./infer status
```

### Development Setup

```bash
# Download Go modules
task mod:download

# Install pre-commit hooks
task precommit:install

# Run pre-commit on all files
task precommit:run
```

### Mock Generation

```bash
# Regenerate all mocks (uses counterfeiter)
task mocks:generate

# Clean generated mocks
task mocks:clean
```

### Release Builds

```bash
# Build for current platform
task release:build

# Build macOS binary
task release:build:darwin

# Build portable Linux binary (via Docker)
task release:build:linux

# Build and push container images
task container:build
task container:push
```

## Architecture Overview

### Core Package Structure

```text
cmd/                    # CLI commands (cobra-based)
├── agent.go           # Autonomous agent command
├── channels.go        # Channel listener daemon command
├── chat.go            # Interactive chat command
├── config.go          # Configuration management commands
├── agents.go          # A2A agent management
└── root.go            # Root command and global flags

internal/
├── app/               # Application initialization
├── container/         # Dependency injection container
├── domain/            # Domain interfaces and models
│   ├── interfaces.go  # Core service interfaces
│   └── filewriter/    # File writing domain logic
├── handlers/          # Message/event handlers
│   ├── chat_handler.go              # Main chat orchestrator
│   ├── chat_message_processor.go    # Message processing logic
│   └── chat_shortcut_handler.go     # Shortcut command handling
├── services/          # Business logic implementations
│   ├── agent.go                     # Agent service
│   ├── conversation.go              # Conversation management
│   ├── conversation_optimizer.go    # Conversation compaction
│   ├── approval_policy.go           # Tool approval logic
│   ├── tools/                       # Tool implementations
│   │   ├── registry.go              # Tool registry
│   │   ├── bash.go                  # Bash execution
│   │   ├── read.go, write.go        # File I/O
│   │   ├── edit.go, multiedit.go    # File editing
│   │   ├── web_search.go            # Web search
│   │   └── mcp_tool.go              # MCP integration
│   ├── channels/                    # Pluggable messaging channels
│   │   └── telegram.go              # Telegram Bot API channel
│   └── filewriter/                  # File writing services
├── infra/             # Infrastructure layer
│   ├── storage/       # Conversation storage backends
│   │   ├── factory.go               # Storage factory
│   │   ├── sqlite.go                # SQLite implementation
│   │   ├── postgres.go              # PostgreSQL implementation
│   │   ├── redis.go                 # Redis implementation
│   │   └── memory.go                # In-memory implementation
│   └── adapters/      # External service adapters
├── ui/                # Terminal UI components
│   ├── components/    # Reusable UI components
│   ├── styles/        # Theme and styling
│   └── keybinding/    # Keyboard handling
├── shortcuts/         # Shortcut system
│   └── registry.go    # Shortcut management
├── web/               # Web terminal interface
└── utils/             # Shared utilities

config/                # Configuration structs
└── config.go          # Main config definition
```

### Architectural Patterns

#### Dependency Injection Container

The application uses a service container pattern (`internal/container/container.go`) for dependency management.
All services are initialized once and injected where needed:

- Configuration service
- Model service
- Agent service
- Tool service
- Conversation repository
- Storage backends
- MCP manager

#### Tool System Architecture

Tools are self-contained modules that implement the `domain.Tool` interface:

1. **Tool Interface** (`internal/domain/interfaces.go`): Defines `Execute()`, `Definition()`, `Validate()`, `IsEnabled()`
2. **Tool Registry** (`internal/services/tools/registry.go`): Manages tool registration and lookup
3. **Tool Implementations** (`internal/services/tools/*.go`): Individual tool logic
4. **Approval System** (`internal/services/approval_policy.go`): Handles user approval for sensitive operations

#### Message Flow (Chat Mode)

1. User input → `ChatHandler.Handle()` → routes to appropriate handler
2. `ChatMessageProcessor` processes user message
3. Tool calls → `ToolService.Execute()` → Tool registry → Individual tool
4. Tool approval (if required) → Approval UI → Execute or reject
5. LLM response → Stream to UI via Bubble Tea messages
6. Conversation saved to storage backend

#### Agent vs Chat Mode

- **Chat Mode**: Interactive TUI with real-time user input and approval
- **Agent Mode**: Autonomous background execution with minimal user interaction
- Both use the same `AgentService` but different handlers and UI flows

#### Storage Backend Strategy

The conversation storage uses a factory pattern with pluggable backends:

- JSONL: Default, file-based, human-readable, zero-config
- SQLite: SQL-based, file-based, structured queries
- PostgreSQL: Production-grade, concurrent access
- Redis: Fast, in-memory, distributed setups
- Memory: Testing and ephemeral sessions

Backend selection is config-driven via `config.yaml` or environment variables.

### Handler Architecture

**ChatHandler Responsibilities:**

- Orchestrates message flow between user, LLM, and tools
- Manages conversation state
- Routes shortcuts to `ChatShortcutHandler`
- Handles tool approval workflow
- Manages background bash shells
- Integrates with message queue for async operations

**Key Handler Methods:**

- `Handle()`: Main entry point, routes messages
- `handleUserMessage()`: Processes user input
- `handleToolCalls()`: Executes tool requests from LLM
- `handleShortcut()`: Delegates to shortcut handler

## Tool Development

When adding a new tool:

1. **Create tool file**: `internal/services/tools/your_tool.go`
2. **Implement `domain.Tool` interface**:
   - `Definition()`: Returns SDK tool definition with JSON schema
   - `Execute(ctx, args)`: Tool execution logic
   - `Validate(args)`: Parameter validation
   - `IsEnabled()`: Check if tool is enabled
3. **Register tool**: Add to `registry.go` in `registerTools()`
4. **Add config**: Update `config/config.go` if tool needs configuration
5. **Write tests**: Create `your_tool_test.go`
6. **Update approval policy**: If tool needs approval, configure in `approval_policy.go`

**Tool Parameter Extraction:**

Use `ParameterExtractor` for type-safe parameter extraction:

```go
extractor := tools.NewParameterExtractor(args)
filePath, err := extractor.GetString("file_path")
lineNum, err := extractor.GetInt("line_number")
```

**Important Tool Conventions:**

- Always respect `ctx` for cancellation
- Return `*domain.ToolExecutionResult` with meaningful output
- Use `config` to check if tool is enabled
- File operations should use absolute paths
- Validate all user inputs before execution

## Configuration System

The CLI uses a 2-layer configuration system:

1. **Project config**: `.infer/config.yaml` (project-specific)
2. **Userspace config**: `~/.infer/config.yaml` (user defaults)
3. **Environment variables**: `INFER_*` prefix (highest priority)
4. **Command flags**: Override config values

**Key Config Sections:**

- `gateway.*`: Gateway connection settings
- `agent.*`: Agent behavior (model, max_turns, system_prompt, custom_instructions)
- `tools.*`: Tool-specific configuration
- `chat.*`: Chat UI settings (theme, keybindings, status bar)
- `web.*`: Web terminal settings
- `pricing.*`: Cost tracking configuration
- `computer_use.*`: Computer use tool settings

Environment variable format: `INFER_<PATH>` (dots become underscores)
Example: `agent.model` → `INFER_AGENT_MODEL`

**Customisable LLM prompts** live in `.infer/prompts.yaml` (loaded
separately from `config.yaml`). Top-level keys: `agent`, `git`,
`conversation`, `init`, `tools`. Tool descriptions surfaced to the LLM
are configurable under `tools.<ToolName>.description` — e.g.
`tools.Bash.description`, `tools.Read.description`. MCP tool
descriptions are not configurable here (they come from the MCP server
at runtime). Any field left empty falls back to the in-code default in
`config.DefaultPromptsConfig`. Env-var overrides use the form
`INFER_PROMPTS_TOOLS_<UPPER_SNAKE_NAME>_DESCRIPTION` (e.g.
`INFER_PROMPTS_TOOLS_BASH_DESCRIPTION`,
`INFER_PROMPTS_TOOLS_A2A_SUBMIT_TASK_DESCRIPTION`).

## Model Context System

The CLI automatically enhances the model's context with project awareness to reduce confusion and improve accuracy.

### Git Context

When operating in a git repository, the model receives:

- **Repository name** (extracted from remote URL, e.g., "inference-gateway/cli")
- **Current branch** (e.g., "main", "feature/xyz")
- **Main branch** name (detected as "main" or "master")
- **Recent commits** (last 5 commits with hashes and messages)

This context is automatically injected into the system prompt on every request.
The git context is cached and refreshed every N turns (configurable) to balance performance with up-to-date information.

### Working Directory

The model receives the current working directory path, helping it understand:

- Where files should be read from or written to
- Which directory commands will execute in
- Project location context

### Performance Characteristics

- **First prompt:** +50-100ms (git command execution)
- **Subsequent prompts:** <1ms (cached)
- **Token overhead:** ~100-300 tokens (depends on git history)
- **Git refresh:** Every 10 turns by default (configurable)

### Configuration

Control via `.infer/config.yaml`:

```yaml
agent:
  context:
    git_context_enabled: true        # Enable git repository context
    working_dir_enabled: true        # Enable working directory context
    git_context_refresh_turns: 10    # Refresh git context every N turns
```

Or via environment variables:

```bash
INFER_AGENT_CONTEXT_GIT_CONTEXT_ENABLED=true
INFER_AGENT_CONTEXT_WORKING_DIR_ENABLED=true
INFER_AGENT_CONTEXT_GIT_CONTEXT_REFRESH_TURNS=10
```

### Benefits

**Before:**

- Model confused about repository name ("inference-gateway" vs "inference-gateway/cli" vs "inference-gateway/infer")
- No awareness of current branch or git state
- Unclear about working directory

**After:**

- Model knows exact repository: `inference-gateway/cli`
- Aware of current branch and recent commits
- Understands working directory context
- Reduced need for clarifying questions

### Technical Implementation

- **Location:** `internal/services/agent_utils.go`
- **Context builders:** `buildGitContextInfo()`, `buildWorkingDirectoryInfo()`
- **Git helpers:** `isGitRepository()`, `getGitRepositoryName()`, `getGitBranch()`, `getGitMainBranch()`, `getRecentCommits()`
- **Caching:** Thread-safe caching via `sync.RWMutex` in `AgentServiceImpl`
- **Error handling:** All git operations fail gracefully (log debug, return empty string)

## Shortcuts System

Shortcuts are YAML-defined commands stored in `.infer/shortcuts/`:

- **Built-in shortcuts**: `/clear`, `/exit`, `/help`, `/switch`, `/theme`, `/cost`
- **Git shortcuts**: `/git status`, `/git commit`, `/git push`
- **SCM shortcuts**: `/scm issues`, `/scm pr-create`
- **Custom shortcuts**: User-defined in project

Shortcuts support:

- Subcommands (e.g., `/git commit`)
- AI-powered snippets (LLM-generated content)
- Command chaining
- Dynamic context injection

## Testing Guidelines

**Test Organization:**

- Unit tests: `*_test.go` files alongside implementation
- Mocks: `tests/mocks/` (generated via counterfeiter)

**Running Specific Tests:**

```bash
# Test specific package
go test ./internal/services/tools

# Test specific function
go test ./internal/services/tools -run TestBashTool

# With race detector
go test -race ./...
```

## MCP (Model Context Protocol) Integration

The CLI supports MCP servers for extended tool capabilities:

- MCP manager: `internal/services/mcp_manager.go`
- MCP tools: `internal/services/tools/mcp_tool.go`
- Configuration: `config.Tools.MCPServers`

MCP servers are configured in `.infer/config.yaml` and tools are dynamically registered at runtime.

## A2A (Agent-to-Agent) System

A2A enables agents to delegate tasks to specialized agents:

- Agent registry: `~/.infer/agents.yaml`
- A2A tools: `A2A_SubmitTask`, `A2A_QueryAgent`, `A2A_QueryTask`
- Agent polling: Background monitor for task status
- Configuration: Via `infer agents` commands

## Channels (Remote Messaging)

Channels provide pluggable messaging transports (Telegram, WhatsApp, etc.)
for remote-controlling the agent from external platforms. The
`infer channels-manager` command runs as a standalone daemon, completely
decoupled from the agent. Each incoming message triggers
`infer agent --session-id <id>` as a subprocess.

- Channels command: `cmd/channels.go`
- Channel Manager: `internal/services/channel_manager.go`
- Telegram channel: `internal/services/channels/telegram.go`
- Domain types: `Channel`, `InboundMessage`, `OutboundMessage` in `internal/domain/interfaces.go`
- Configuration: `config.Channels` in `config/config.go`

Channels are configured in `.infer/config.yaml` under the `channels` key.
Each channel has its own allowlist for security.
See `docs/channels.md` for full documentation.

The channels-manager daemon also hosts the **scheduler service** when
`tools.schedule.enabled: true` — see [Scheduling (Cron-driven Tasks)](#scheduling-cron-driven-tasks)
below for the full design.

### Tool Approval Flow

When `channels.require_approval` is `true` (default), the channel manager
enables interactive tool approval via stdin/stdout IPC with the agent subprocess:

1. Channel manager passes `--require-approval` to `infer agent`
2. Agent emits `ApprovalRequest` JSON on stdout, blocks reading stdin
3. Channel manager detects request, sends approval prompt to user
4. User replies "yes"/"no"; reply intercepted in `routeInbound()` before
   `handleMessage()` to avoid sender mutex deadlock
5. Channel manager writes `ApprovalResponse` JSON to agent stdin
6. 5-minute timeout auto-rejects if no reply

- IPC types: `internal/domain/ipc.go` (`ApprovalRequest`, `ApprovalResponse`)
- Agent side: `cmd/agent.go` (`executeToolCallsWithApproval`, `readApprovalResponses`, `outputApprovalRequest`)
- Channel manager side: `internal/services/channel_manager.go` (`handleApprovalRequest`, `parseApprovalRequest`, `isApprovalReply`)
- Reuses existing `tools.*.require_approval` and `tools.safety.require_approval` config
- Read-only tools (Tree, Read, Grep) default to `require_approval: false`

### Adding a New Channel

1. Implement `domain.Channel` interface in `internal/services/channels/`
2. Add config type to `config/config.go`
3. Register in `registerChannels()` in `cmd/channels.go`
4. Add allowlist case in `channel_manager.go` `isAllowedUser()`

## Scheduling (Cron-driven Tasks)

The `Schedule` tool lets the LLM create recurring or one-off jobs that fire
on a cron schedule and deliver their output back through the messaging
channel that triggered the current session (e.g. Telegram). The scheduler
runs **inside the channels-manager daemon** — there is no separate process —
and is cross-platform (uses `robfig/cron/v3`, not system crontab).

- Schedule tool: `internal/agent/tools/schedule.go`
- Scheduler service: `internal/services/scheduler/scheduler.go`
- YAML store: `internal/services/scheduler/store.go`
- Domain types: `ScheduledJob`, `SchedulerService` in `internal/domain/scheduler.go`
- Session-ID parser: `domain.ParseChannelSessionID` in `internal/domain/session.go`
- Wiring: `cmd/channels.go` `startScheduler()` constructs and lifecycles the service
- Configuration: `config.Tools.Schedule` (`ScheduleToolConfig`) in `config/config.go`

See `docs/scheduling.md` for the user-facing guide.

### Architecture

```text
┌─ infer channels-manager (daemon) ─────────────────────────┐
│  ChannelManagerService                                     │
│   ├─ inbound msgs    → spawn `infer agent`                 │
│   └─ SchedulerService (when tools.schedule.enabled)        │
│        ├─ robfig/cron/v3 scheduler                         │
│        ├─ fsnotify watcher on ~/.infer/schedules/          │
│        └─ on fire: spawn `infer agent --session-id <uuid>` │
│                    capture stdout → channel.Send(...)      │
└────────────────────────────────────────────────────────────┘
            ▲                                  ▲
            │ writes YAML                      │ reads YAML on startup
            │                                  │   + fsnotify reload
┌───────────┴─────────┐              ┌─────────┴──────────────┐
│ Schedule tool       │ create/del   │ ~/.infer/schedules/    │
│ (runs in any agent) │ ──────────►  │   <job-id>.yaml        │
└─────────────────────┘              └────────────────────────┘
```

Key properties:

- **Tool-only file I/O.** The `Schedule` tool never talks directly to the
  daemon — it just writes YAML. The daemon's fsnotify watcher
  (`scheduler.startWatcher`) picks up changes within ~150ms (debounced) and
  registers/unregisters cron entries.
- **Fresh session per fire.** Each scheduled run gets a new UUID session ID;
  no context carries between fires (acceptance criterion of issue #418).
- **Daemon-bound execution.** Jobs only fire while `infer channels-manager`
  is running. If the daemon is down, the YAML stays on disk and resumes on
  next startup.
- **One-off jobs.** When `RunOnce: true` on the job YAML, the scheduler
  deletes the file after the first fire (regardless of delivery success).
  Used for "remind me at 6pm today"-style requests.

### Routing context (channel + recipient)

The Schedule tool **does not accept** `channel` or `recipient_id` parameters
from the LLM. They are derived deterministically from the agent's session ID
via `domain.ParseChannelSessionID`. Channels-manager session IDs are
formatted `channel-<name>-<sender_id>` (see `channel_manager.go:177`), so
parsing is unambiguous.

Wiring chain:

1. `cmd/agent.go executeToolCall()` injects the agent's `sessionID` into the
   tool-call context via `domain.WithSessionID`.
2. `Schedule.execCreate` calls `resolveRouting(ctx)` which reads the session
   ID with `domain.GetSessionID(ctx)` and parses it.
3. If the session is not channel-formatted (e.g. chat-mode or a generic
   agent run), the tool returns a clear error — it cannot guess where to
   deliver.

This means the LLM literally cannot route to the wrong recipient.

### Job YAML format

`~/.infer/schedules/<uuid>.yaml`:

```yaml
id: 01HG7K2N3M4P5Q6R7S8T9V0W1X
name: Daily morning quote
cron_expression: "0 8 * * *"      # standard 5-field crontab or @every <duration>
prompt: |
  Find an inspiring quote and respond with quote + author.
channel: telegram
recipient_id: "12345"
model: ""                          # empty = use cfg.Agent.Model
run_once: false                    # true → deleted after first fire
created_at: 2026-04-25T10:30:00Z
updated_at: 2026-04-25T10:30:00Z
last_run: 2026-04-26T08:00:01Z
last_error: ""                     # set when delivery fails
```

`Save` is atomic (write to `<id>.yaml.tmp`, then `os.Rename`) so the
fsnotify watcher never sees half-written files.

### Timezone

Cron expressions are interpreted in `time.Local`, which honours the `TZ`
environment variable. The binary imports `_ "time/tzdata"` in `main.go`,
embedding the IANA zone DB so `TZ=Europe/Berlin` works on minimal container
images that don't ship `/usr/share/zoneinfo`.

### Adding a new Schedule operation

1. Add the op to the `scheduleOp*` constants in
   `internal/agent/tools/schedule.go`.
2. Add an `enum` entry in the `operation` parameter and a `case` in
   `Execute()`'s switch.
3. Implement `execMyOp(ctx, args, store, start)` returning a
   `*ScheduleToolResult`. Use `requireString` / `optionalString` /
   `optionalBool` for arg extraction.
4. Add validation in `Validate()`'s switch.
5. Update the tool description + table in `docs/scheduling.md` and
   `docs/tools-reference.md`.

### Defaults & approval

- Disabled by default (`ScheduleToolConfig.Enabled = false`); enable
  explicitly via `tools.schedule.enabled: true` or
  `INFER_TOOLS_SCHEDULE_ENABLED=true`.
- Requires approval by default (`ScheduleToolConfig.RequireApproval =
  ptr(true)`); the `IsApprovalRequired("Schedule")` switch case in
  `config/config.go` honours this.
- Defaults are registered with viper via four `v.SetDefault("tools.schedule.*",
  ...)` calls in `cmd/root.go` — without those, viper unmarshals an empty
  config and the defaults function's values are ignored.

## Heartbeat (Periodic Wake-Up)

The **Heartbeat** wakes the agent on a fixed interval to check for
pending work. It is a peer of the scheduler — both run inside the
`infer channels-manager` daemon, both spawn `infer agent`
subprocesses, but heartbeat is a single global tick (vs. many
user-defined cron jobs) and logs output (vs. routing to a channel).
Disabled by default.

- Config struct: `config.HeartbeatConfig` in `config/heartbeat.go`
- Config file: `~/.infer/heartbeat.yaml` (separate file, mirrors
  channels.yaml; `yaml:"-"` on `Config.Heartbeat`).
- System prompt: `cfg.Prompts.Agent.SystemPromptHeartbeat` in
  `prompts.yaml` — separate from `system_prompt`/`system_prompt_plan`.
- Service: `internal/services/heartbeat/heartbeat.go` (`Service`
  with `Start(ctx)` / `Stop(ctx)`, ticker-driven, no cron).
- Daemon wiring: `cmd/channels.go` `startHeartbeat()` next to
  `startScheduler()`.
- Init wiring: `cmd/init.go` `createHeartbeatConfigFile()`.
- Env vars: `INFER_HEARTBEAT_*` applied via
  `applyHeartbeatEnvOverrides` in `cmd/config.go`.

### Heartbeat architecture

```text
┌─ infer channels-manager (daemon) ─────────────────────────┐
│  ChannelManagerService     (channels — optional)           │
│  SchedulerService           (cron jobs — optional)         │
│  HeartbeatService                                          │
│   ├─ time.Ticker(interval)                                 │
│   └─ on tick: spawn `infer agent --heartbeat               │
│                          --session-id <uuid> <prompt>`     │
│               log stdout                                   │
└────────────────────────────────────────────────────────────┘
```

Key properties:

- **Off by default.** `Heartbeat.Enabled = false` in
  `DefaultHeartbeatConfig()`.
- **Daemon gate is relaxed.** `infer channels-manager` boots if
  *any* of channels / scheduler / heartbeat is enabled. Heartbeat
  alone is a valid run mode.
- **Fresh session per fire.** UUID-format session ID (not channel
  prefixed); the Schedule tool's `resolveRouting` will refuse to
  operate from a heartbeat run, which is intentional — heartbeat
  should not directly create scheduled jobs without explicit
  channel context.
- **Overlap guard.** `atomic.Int32` flag suppresses concurrent
  ticks when the agent run takes longer than `interval`. Logs a
  warning when skipped.
- **System prompt selection.** `infer agent --heartbeat` (cmd flag
  added in `cmd/agent.go`) swaps `cfg.Prompts.Agent.SystemPrompt`
  for `cfg.Prompts.Agent.SystemPromptHeartbeat` *before* the
  service container is built. The agent service stays oblivious to
  the new mode.
- **Output.** Agent stdout is logged via the standard logger. No
  channel routing — if the user wants a channel notification, the
  agent itself uses its tools to send one.

See `docs/heartbeat.md` for the user-facing guide.

## Plan Mode

Plan mode (`AgentModePlan` in `internal/domain/state.go`) is a read-only
operating mode the user enters via Shift+Tab in the chat TUI. The model
gets `Read`/`Grep`/`Tree`/`TodoWrite` plus the `RequestPlanApproval` tool
and is otherwise blocked from any mutating tools (enforced in
`internal/services/tools.go::FilterToolsForMode`).

When the model calls `RequestPlanApproval`, the tool persists the plan as
a Markdown file under `<configDir>/plans/<YYYY-MM-DD-HHMMSS>-<slug>.md`
(atomic write: `.tmp` → `os.Rename`). The plan body must follow a fixed
8-section H2 template (Context, Files to Modify, Current Code, Changes,
Performance Impact, Critical Files, Edge Cases, Verification) — see
`config/prompts.go::DefaultPromptsConfig` for the prompt that pins this
contract.

- Tool: `internal/agent/tools/request_plan_approval.go`
- System prompt: `config/prompts.go` (`agent.system_prompt_plan`)
- Approval event flow: `internal/agent/agent.go` →
  `PlanApprovalRequestedEvent` → `internal/handlers/chat_handler.go`
  `HandlePlanApprovalRequestedEvent` / `HandlePlanApprovalResponseEvent`
- UI state: `domain.PlanApprovalUIState`, `ViewStatePlanApproval`

Rejected plans stay on disk as an audit trail — by design.

See `docs/plan-mode.md` for the full user-facing guide.

## Model Thinking Visualization

When models use extended thinking (reasoning), their internal thought process is displayed as collapsible blocks above responses.

### Implementation Details

- **Data Storage**: Thinking content is stored in `ConversationEntry.ThinkingContent` field
- **Event Flow**: Reasoning content flows through `StreamingContentEvent.ReasoningContent` during streaming
- **Rendering**: Thinking blocks are rendered before assistant message content in `renderStandardEntry()` and `renderAssistantWithToolCalls()`
- **Display State**: Collapsed by default, showing first sentence with ellipsis
- **Styling**: Rendered using dim color (theme-aware) with 💭 icon
- **Expansion**: Toggled via keybinding (configurable as `display_toggle_thinking`, defaults to `ctrl+k`)

### Key Files

- `internal/domain/interfaces.go`: `ConversationEntry.ThinkingContent` field
- `internal/domain/ui_events.go`: `StreamingContentEvent.ReasoningContent` field
- `internal/ui/components/conversation_view.go`: Rendering logic and expansion state
- `config/keybindings.go`: Keybinding definition
- `internal/ui/keybinding/actions.go`: Action handler registration

### User Controls

- Toggle thinking block expansion/collapse using the configured keybinding (default: `ctrl+k`)
- Default state: collapsed (first sentence visible)
- Expanded state: full thinking content with word wrapping
- Keybinding can be customized via `chat.keybindings.bindings.display_toggle_thinking` in config

## Commit Message Convention

This project uses **Conventional Commits**:

```text
<type>[optional scope]: <description>

[optional body]
[optional footer]
```

**Types:** feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert

**Breaking changes:** Add `!` after type (e.g., `feat!:`) or footer `BREAKING CHANGE:`

Pre-commit hooks automatically validate commit messages.

## Development Workflow

1. **Make changes** following Go best practices
2. **Run quality checks**: `task precommit:run` (runs formatting, linting, validation)
3. **Test thoroughly**: `task test`
4. **Commit with conventional commit message**
5. **Pre-commit hooks** run automatically on commit
6. **Push and create PR**

**Release Process:**

Automated via semantic-release on `main` branch:

- Commit types determine version bumps
- Binaries built for macOS (Intel/ARM64) and Linux (AMD64/ARM64)
- GitHub releases created automatically with changelogs

## Important Notes

- **No CGO**: Project uses pure Go dependencies for portability
- **Flox environment**: Use `flox activate` for consistent dev environment
- **Binary name**: Built as `infer` (not `cli`)
- **Gateway dependency**: CLI requires Inference Gateway (auto-managed in Docker/binary mode)
- **Storage migrations**: SQLite and PostgreSQL use automatic schema migrations
- **Tool safety**: File modification tools require user approval by default
- **Context limits**: Conversation optimizer handles token limits automatically
