# AGENTS.md - Inference Gateway CLI

*Comprehensive guidance for AI agents working with this project.*

**Last updated:** April 27, 2026

---

## 1. Project Overview

**Inference Gateway CLI** (`infer`) is a Go-based command-line interface for managing and
interacting with AI inference services. It provides interactive chat (TUI), autonomous
agent capabilities, and extensive tool execution for LLMs.

### Key Technologies

| Technology | Purpose |
| --- | --- |
| **Go 1.26+** | Primary language |
| **Cobra** (`spf13/cobra`) | CLI command framework |
| **Viper** (`spf13/viper`) | Configuration management |
| **Bubble Tea** (`charmbracelet/bubbletea`) | Terminal UI framework |
| **Lip Gloss** (`charmbracelet/lipgloss`) | TUI styling |
| **Glamour** (`charmbracelet/glamour`) | Markdown rendering |
| **SDK** (`inference-gateway/sdk`) | Gateway API client |
| **ADK** (`inference-gateway/adk`) | Agent development kit |
| **SQLite** (`modernc.org/sqlite`) | Embedded storage (CGO-free) |
| **Task** (`go-task`) | Build automation |
| **Flox** | Development environment manager |

### Storage Backends

- **JSONL** (default) - file-based, human-readable, zero-config
- **SQLite** - embedded, structured queries
- **PostgreSQL** - production-grade, concurrent access
- **Redis** - fast, in-memory, distributed
- **Memory** - testing and ephemeral sessions

---

## 2. Architecture & Structure

### Full Project Layout

```text
.
├── cmd/                      # Cobra CLI commands
│   ├── agent.go              # Autonomous agent (background mode)
│   ├── agents.go             # A2A agent management commands
│   ├── channels.go           # Channel listener daemon command
│   ├── chat.go               # Interactive chat (TUI)
│   ├── claude_code.go        # Claude Code integration
│   ├── config.go             # Configuration management
│   ├── config_export.go      # Export configuration
│   ├── conversation_title.go # Title generation commands
│   ├── conversations.go      # Conversation list/view/delete
│   ├── defaults.go           # Auto-config defaults via reflection
│   ├── export.go             # Export tools
│   ├── floating_window_darwin.go  # macOS floating window
│   ├── floating_window_stub.go     # Non-macOS stub
│   ├── init.go               # `infer init` project setup
│   ├── keybindings.go        # Keybinding management
│   ├── mcp.go                # MCP server commands
│   ├── migrate.go            # Storage migrations
│   ├── prompts_load_test.go  # Prompt loading tests
│   ├── root.go               # Root command + viper init
│   ├── status.go             # Gateway health status
│   ├── version.go            # Version command
│   └── version_info.go       # Version info struct
│
├── config/                   # Configuration structs & defaults
│   ├── config.go             # Main Config struct
│   ├── agent_defaults.go     # Agent config defaults
│   ├── agents.go             # A2A agent config
│   ├── channels.go           # Channel config
│   ├── collection.go         # Config collection utilities
│   ├── computer_use.go       # Computer-use config
│   ├── keybindings.go        # Keybinding config
│   ├── mcp.go                # MCP config
│   ├── model_context.go      # Model context config
│   ├── pricing.go            # Pricing/tracking config
│   ├── prompts.go            # Prompts config
│   └── utils/                # YAML file utilities
│       └── yamlfile.go
│
├── internal/                 # Internal application code
│   ├── agent/                # Core agent engine
│   │   ├── agent.go          # AgentServiceImpl
│   │   ├── agent_event_driven.go  # Event-driven execution
│   │   ├── agent_state_machine.go  # State machine
│   │   ├── agent_streaming.go # LLM streaming
│   │   ├── agent_tools.go    # Tool execution coordination
│   │   ├── agent_utils.go    # Agent utilities (git context)
│   │   ├── states/           # State machine states
│   │   └── tools/            # Agent tool implementations
│   │       ├── bash.go, read.go, write.go, edit.go, …
│   │       ├── grep.go, tree.go, delete.go, …
│   │       ├── web_search.go, web_fetch.go
│   │       ├── github.go, schedule.go
│   │       ├── a2a_task.go, a2a_query_agent.go, …
│   │       ├── keyboard_type.go, mouse_*.go
│   │       ├── activate_app.go, get_focused_app.go
│   │       ├── mcp_tool.go
│   │       └── registry.go
│   ├── app/                  # Application bootstrap
│   ├── clipboard/            # Clipboard support
│   ├── constants/            # Agent & timing constants
│   ├── container/            # DI container
│   ├── display/              # Display/computer-use
│   │   └── macos/ComputerUse/  # macOS native Swift app
│   ├── domain/               # Domain interfaces & models
│   │   ├── interfaces.go     # Core interfaces
│   │   ├── filewriter/       # File-writing domain
│   │   └── ...               # Domain types
│   ├── handlers/             # Chat/event handlers
│   ├── infra/                # Infrastructure layer
│   │   └── storage/          # Storage backends
│   ├── logger/               # Structured logging
│   ├── services/             # Business logic
│   │   ├── channels/         # Channel implementations
│   │   ├── scheduler/        # Cron scheduler
│   │   ├── tools/            # (Legacy) tool implementations
│   │   └── filewriter/       # File writing service
│   ├── shortcuts/            # Shortcut system
│   ├── ui/                   # Terminal UI (Bubble Tea)
│   ├── utils/                # Shared utilities
│   └── web/                  # Web terminal interface
│
├── docs/                     # Documentation
│   ├── features/conversation-versioning.md
│   ├── security/binary-verification.md
│   ├── a2a-connections.md, agents-configuration.md
│   ├── channels.md, commands-reference.md
│   ├── configuration-reference.md
│   ├── conversation-storage.md, conversation-title-generation.md
│   ├── database-migrations.md, directory-structure.md
│   ├── mcp-integration.md, nix-distribution-overview.md
│   ├── nixpkgs-submission.md, scheduling.md
│   ├── shortcuts-guide.md, tasks-management.md
│   ├── tools-reference.md, web-terminal.md
│
├── examples/                 # Example deployments
│   ├── a2a/                  # A2A agent example
│   ├── basic/                # Basic setup
│   ├── computer-use/         # Computer-use example
│   ├── mcp/                  # MCP integration example
│   ├── model-switching/      # Model switching demo
│   ├── shortcuts/            # Shortcuts example
│   ├── telegram-channel/     # Telegram bot example
│   └── web-terminal/         # Web terminal example
│
├── nix/                      # Nix packaging
├── tests/                    # Test mocks (generated)
│   └── mocks/
├── dist/                     # Build artifacts
├── .github/workflows/        # CI/CD pipelines
├── .infer/                   # Project-level config (runtime)
├── .flox/                    # Flox environment
└── .vscode/                  # VS Code settings
```

### Agent State Machine

The agent uses a formal state machine (`internal/agent/agent_state_machine.go`) with these states:

```text
Idle → CheckingQueue → StreamingLLM → PostStream → EvaluatingTools
     ↕                                         ↓
  Completing ← PostToolExecution ← ExecutingTools/ApprovingTools
```

**States:**

- `Idle` - Agent is waiting for work
- `CheckingQueue` - Checking for queued messages or completion criteria
- `StreamingLLM` - Streaming responses from the LLM
- `PostStream` - Processing LLM response, checking for tool calls
- `EvaluatingTools` - Determining if tool calls need approval
- `ApprovingTools` - Waiting for user approval (chat mode only)
- `ExecutingTools` - Executing approved tool calls
- `PostToolExecution` - Processing tool results, checking for completion
- `Completing` - Finalizing execution
- `Error` - Error occurred
- `Cancelled` - User cancelled
- `Stopped` - Tool execution indicated stop

### Core Architectural Patterns

**DI Container** (`internal/container/container.go`):
All services initialized once and injected where needed.

**Tool Interface** (`internal/domain/interfaces.go`):
Every tool implements `Execute()`, `Definition()`, `Validate()`, `IsEnabled()`.

**Message Flow (Chat Mode):**

1. User input → `ChatHandler.Handle()` → routes to handler
2. `ChatMessageProcessor` processes user message
3. Tool calls → `ToolService.Execute()` → Tool registry → Individual tool
4. Tool approval (if required) → Approval UI → Execute or reject
5. LLM response → Stream to UI via Bubble Tea messages
6. Conversation saved to storage backend

**Agent vs Chat Mode:**

- **Chat Mode:** Interactive TUI with real-time user input and approval
- **Agent Mode:** Autonomous background execution (`infer agent "task"`)
- Both use the same `AgentService` but different handlers and UI flows

**Storage Backend Strategy:**
Factory pattern with pluggable backends. Backend selection via config or env vars.

---

## 3. Development Environment Setup

### Prerequisites

- **Go 1.26+** (from `go.mod`)
- **Flox** (recommended) - consistent dev environment
- **Task** (`go-task`) - build automation
- **Docker** - for container builds and some release targets

### Quick Start (Recommended)

```bash
# Activate Flox environment (installs Go, tools, linters, etc.)
flox activate

# Download Go modules
task mod:download

# Install pre-commit hooks
task precommit:install

# Build the binary
task build

# Run the CLI
./infer chat
```

### Without Flox

```bash
# Ensure Go 1.26+ is installed
go version

# Download modules
go mod download

# Build
go build -o infer .
```

### Flox Environment Includes

| Tool | Purpose |
| --- | --- |
| Go 1.26 | Compiler |
| Node.js 24 | semantic-release tools |
| Git 2.53 | Version control |
| gh 2.90 | GitHub CLI |
| golangci-lint 2.11 | Linter |
| pre-commit 4.5 | Pre-commit hooks |
| go-task 3.48 | Task runner |
| ripgrep 15.1 | File search |
| markdownlint-cli 0.48 | MD linting |
| Claude Code 2.1 | AI pair programming |
| Docker / Compose | Container builds |
| gopls 0.21 | Go language server |

---

## 4. Key Commands

### Build & Development

```bash
task build              # Build binary (outputs ./infer)
task install            # Install to $GOPATH/bin
task run CLI_ARGS="..." # Run without building (e.g. "chat", "status")
task clean              # Clean build artifacts
```

### Testing

```bash
task test               # Run all tests
task test:verbose       # Tests with verbose output
task test:coverage      # Tests with coverage

# Run specific tests
go test ./internal/agent/tools -run TestBashTool
go test -race ./...     # With race detector
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Code Quality

```bash
task fmt                # go fmt ./...
task vet                # go vet ./...
task lint               # golangci-lint run + markdownlint
task precommit:run      # Run all pre-commit hooks
```

### Running the CLI

```bash
./infer chat                          # Interactive TUI chat
./infer agent "fix issue #42"         # Autonomous agent
./infer status                        # Gateway health
./infer version                       # Version info
./infer config show                   # Current config
./infer init                          # Initialize project
./infer conversations list            # List conversations
```

### Mock Generation

```bash
task mocks:generate      # Regenerate all mocks (counterfeiter)
task mocks:clean         # Remove generated mocks
```

### Release Builds

```bash
task release:build          # Build for native platform
task release:build:darwin   # macOS binary
task release:build:linux    # Linux via Docker
task container:build        # Build container image
task container:push         # Push to registry
```

### Module Management

```bash
task mod:tidy              # go mod tidy
task mod:download          # go mod download
task verify:deps           # Verify dependencies
```

### Debugging

```bash
INFER_LOGGING_LEVEL=debug ./infer chat   # Verbose logging
./infer config show                      # View resolved config

---

## 5. Configuration System

### Configuration Layers (Precedence)

1. **CLI flags** (highest priority)
2. **Environment variables** (`INFER_*` prefix, dots → underscores)
3. **Project config:** `.infer/config.yaml`
4. **Userspace config:** `~/.infer/config.yaml`
5. **Built-in defaults** (lowest priority)

The defaults are registered automatically via reflection in `cmd/defaults.go` - the
`registerConfigDefaults()` function walks the `config.Config` struct using `mapstructure`
tags and calls `v.SetDefault()` for every non-zero leaf field.

### Configuration Files (in `.infer/`)

| File | Purpose |
| --- | --- |
| `config.yaml` | Main config (gateway, tools, storage, agent, chat, web, pricing) |
| `prompts.yaml` | LLM system prompts (agent, git, conversation, tool descriptions) |
| `keybindings.yaml` | Keyboard shortcuts for the chat TUI |
| `channels.yaml` | Remote messaging transports (Telegram, etc.) |
| `computer_use.yaml` | Computer-use / vision tool settings |
| `agents.yaml` | A2A agent registry (URLs, models, env vars) |
| `mcp.yaml` | MCP server registry and liveness probes |
| `shortcuts/*.yaml` | `/`-prefixed chat shortcuts (git, scm, mcp, shells, export, a2a) |

### Config Sections (`config.yaml`)

```yaml
container_runtime:   # Docker/Podman auto-detect
gateway:             # Gateway connection (URL, API key, run mode, models)
claude_code:         # Claude Code CLI integration
client:              # HTTP client settings
logging:             # Log level and directory
tools:               # Tool enable/disable, approval, MCP, schedule
image:               # Image processing
export:              # Export settings
agent:               # Agent behavior (model, max_turns, system_prompt, context)
git:                 # Git integration
storage:             # Storage backend selection and connection
conversation:        # Conversation settings
chat:                # Chat UI (theme, status bar)
a2a:                 # A2A agent configuration
mcp:                 # MCP settings
pricing:             # Cost tracking configuration
compact:             # Conversation compaction
web:                 # Web terminal settings
```

### Environment Variable Substitution

Config files support `%ENV_VAR%` syntax for injecting environment variables into YAML
values (useful for API keys). Env var format: `INFER_<PATH>` where dots become
underscores. Example: `agent.model` → `INFER_AGENT_MODEL`.

### Customisable Prompts

`.infer/prompts.yaml` has these top-level keys:

- `agent` - Agent system prompt
- `git` - Git context prompt template
- `conversation` - Conversation system prompt
- `init` - Init command prompts
- `tools.<ToolName>.description` - Override individual tool descriptions

Env var override: `INFER_PROMPTS_TOOLS_BASH_DESCRIPTION="Custom bash description"`

---

## 6. Testing Instructions

### Test Organization

- **Unit tests:** `*_test.go` files alongside implementation code
- **Mocks:** `tests/mocks/` - generated via **counterfeiter** (`task mocks:generate`)
- **Integration tests:** Files with `_integration_test.go` suffix

### Running Tests

```bash
task test               # Run all tests
task test:verbose       # Verbose output
task test:coverage      # Coverage report

# With race detector
go test -race ./...

# Specific package
go test ./internal/agent/tools

# Specific test function
go test ./internal/agent/tools -run TestBashTool

# Coverage HTML report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Mock Sources

Mocks are generated from interface definitions in:

- `internal/domain/interfaces.go`
- `internal/domain/agent.go`
- `internal/domain/config_service.go`
- `internal/infra/storage/interfaces.go`
- `internal/ui/interfaces.go`
- `internal/services/background_jobs.go`
- `internal/shortcuts/interfaces.go`

```bash
task mocks:generate      # Regenerate mocks
task mocks:clean         # Remove mocks
```

### Test Conventions

1. Use `t.Parallel()` for independent tests
2. Follow table-driven test patterns for multiple cases
3. Use testify (`github.com/stretchr/testify`) assertions
4. Mock external dependencies using counterfeiter
5. Clean up resources in `t.Cleanup()` or `defer`
6. Timing constants for test sleeps: `constants.TestSleepDelay` (100ms)

---

## 7. Project Conventions & Coding Standards

### Go Code Style

- **Formatting:** `gofmt` (enforced by pre-commit hooks)
- **Indentation:** tabs for `.go`, 2-space for YAML/MD/JSON (per `.editorconfig`)
- **Line endings:** LF (enforced by pre-commit)
- **Trailing whitespace:** stripped in code files (preserved in `.md`, `.txt`)
- **Final newline:** required for all files

### Linting Rules (`.golangci.yml`)

| Linter | Setting |
| --- | --- |
| `gocyclo` | max complexity: 25 |
| `cyclop` | max complexity: 25, package avg: 15 |
| `funlen` | max lines: 150, max statements: 80 |
| `gocognit` | min complexity: 45 |
| `nestif` | min complexity: 4 |
| **Disabled** | `exhaustruct`, `varnamelen`, `wrapcheck`, `paralleltest`, `testpackage` |

Run: `golangci-lint run` (via `task lint`)

### Markdown Linting (`.markdownlint.json`)

- Max line length: 150
- Ordered list style: disabled
- Inline HTML: allowed
- First line heading: allowed

### Conventional Commits

Every commit **must** follow the [Conventional Commits](https://www.conventionalcommits.org/) format:

```text
<type>[optional scope]: <description>

[optional body]
[optional footer]
```

**Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`

**Breaking changes:** Add `!` after type (e.g., `feat!:`) or add `BREAKING CHANGE:` footer.

Version bumps are automatic based on commit type:

- `fix:` → patch version (1.0.1)
- `feat:` → minor version (1.1.0)
- `feat!:` or `BREAKING CHANGE:` → major version (2.0.0)

### Pre-commit Hooks

Configured in `.pre-commit-config.yaml`, runs automatically on `git commit`:

1. **Trailing whitespace** removal
2. **End-of-file fixer** (ensures final newline)
3. **YAML/JSON/TOML** syntax validation
4. **Merge conflict** detection
5. **Large file** detection
6. **Mixed line ending** fixer (→ LF)
7. **Go mod tidy** (local hook)
8. **Go fmt** (local hook)
9. **golangci-lint** (local hook)
10. **Mock generation** (triggered by changes to `internal/domain/interfaces.go`)

### Dependency Management

- **No CGO** - project uses pure Go dependencies for maximum portability
- Go 1.26 toolchain with `tool` directive for counterfeiter
- Dependencies verified via `go mod verify`
- Module cache handled by Flox environment

### Naming Conventions

- **Binary:** `infer` (not `cli`)
- **Module:** `github.com/inference-gateway/cli`
- **Config directories:** `.infer/` (project) and `~/.infer/` (userspace)
- **Env prefix:** `INFER_` for all environment variables

---

## 8. CI/CD & Release

### GitHub Actions Workflows

| Workflow | Trigger | Purpose |
| --- | --- | --- |
| `ci.yml` | Push/PR to `main` | Build, lint, vet, test |
| `release.yml` | Push to `main` | Semantic release + binary publishing |
| `artifacts.yml` | Push to `main` | Build artifacts |
| `claude.yml` | Push to `main` | Claude Code integration checks |
| `infer.yml` | Push to `main` | Additional infer-specific checks |
| `nix-build.yml` | PR/push | Nix package build verification |
| `nix-version-sync.yml` | New release | Auto-update Nix package hashes |

### CI Pipeline (`ci.yml`)

```text
prepare → lint + vet → build → test
```

1. **Prepare:** `go mod tidy`, `go fmt`, check for dirty git
2. **Lint:** golangci-lint v2.11.4
3. **Vet:** `go vet ./...`
4. **Build:** `go build` with version ldflags
5. **Test:** `go test ./...`

### Release Process (Automated)

Releases are fully automated via **semantic-release** (`semantic-release@25`):

1. Commit pushed to `main` triggers release workflow
2. `@semantic-release/commit-analyzer` determines version bump
3. `@semantic-release/release-notes-generator` generates changelog
4. `@semantic-release/changelog` writes `CHANGELOG.md`
5. `@semantic-release/git` commits changelog
6. `@semantic-release/github` creates GitHub release with binaries

**Release assets:** macOS (Intel/ARM64) and Linux (AMD64/ARM64) binaries, checksums, cosign signatures.

### Version Info Injection

Build-time ldflags inject version metadata:

```go
-X github.com/inference-gateway/cli/cmd.version=<version>
-X github.com/inference-gateway/cli/cmd.commit=<sha>
-X github.com/inference-gateway/cli/cmd.date=<timestamp>
```

### Nix Packaging

Located in `nix/` directory:

- `default.nix` - entry point for `nix-build`
- `package.nix` - Go build expression with vendor hash
- `README.md` - detailed Nix build instructions
- `update-hashes.sh` - hash update script

Nix builds support Linux (x86_64, aarch64) and macOS (x86_64, aarch64 with CGO for clipboard).

### Container Builds

- **Dockerfile:** Multi-stage (binary from dist/ → Alpine runtime)
- **Registry:** `ghcr.io/inference-gateway/cli`
- **Base image:** `alpine:3.23.3` with `ca-certificates`, `git`, `sqlite-libs`
- **Container env:** `INFER_IN_CONTAINER=true`, `INFER_GATEWAY_RUN=false`

---

## 9. Important Files & Configurations

### Root Configuration Files

| File | Purpose |
| --- | --- |
| `go.mod` | Go module definition and dependencies |
| `go.sum` | Dependency checksums |
| `Taskfile.yml` | Build automation tasks |
| `Dockerfile` | Container image definition |
| `main.go` | Application entry point |
| `.env.example` | Template for API keys |
| `.golangci.yml` | Linter configuration |
| `.pre-commit-config.yaml` | Pre-commit hooks |
| `.commitlintrc.json` | Commit message validation |
| `.releaserc.yaml` | Semantic release configuration |
| `.editorconfig` | Editor settings |
| `.markdownlint.json` | Markdown linting rules |
| `.cspell.yaml` | Spell check dictionary |
| `install.sh` | One-line install script |
| `CLAUDE.md` | Agent development guide for Claude Code |
| `CONTRIBUTING.md` | Contribution guidelines |
| `AGENTS.md` | This file - AI agent guidance |

### `.infer/` Runtime Directory

Created by `infer init`. Key files:

| File | Description |
| --- | --- |
| `config.yaml` | Main configuration |
| `prompts.yaml` | Customisable LLM prompts |
| `keybindings.yaml` | Keyboard shortcuts |
| `channels.yaml` | Channel configurations |
| `computer_use.yaml` | Computer-use settings |
| `agents.yaml` | A2A agent registry |
| `mcp.yaml` | MCP server registry |
| `shortcuts/*.yaml` | Chat shortcuts |
| `.gitignore` | Ignores runtime files |
| `conversations.db` | SQLite storage (runtime) |
| `logs/` | Debug logs (runtime) |
| `tmp/` | Scratch space (runtime) |

### Documentation Reference

| Document | Topic |
| --- | --- |
| `docs/tools-reference.md` | All tools and their parameters |
| `docs/configuration-reference.md` | All config options |
| `docs/commands-reference.md` | CLI command documentation |
| `docs/directory-structure.md` | File/directory map |
| `docs/channels.md` | Remote messaging |
| `docs/a2a-connections.md` | Agent-to-agent communication |
| `docs/mcp-integration.md` | MCP server setup |
| `docs/shortcuts-guide.md` | Shortcut system |
| `docs/scheduling.md` | Cron-driven tasks |
| `docs/conversation-storage.md` | Storage backends |
| `docs/conversation-title-generation.md` | Title generation |
| `docs/database-migrations.md` | Schema migrations |
| `docs/web-terminal.md` | Browser-based terminal |
| `docs/tasks-management.md` | Task management |
| `docs/security/binary-verification.md` | Binary verification |
| `docs/features/conversation-versioning.md` | Conversation versioning |
| `docs/nix-distribution-overview.md` | Nix distribution |
| `docs/nixpkgs-submission.md` | nixpkgs submission guide |
| `docs/agents-configuration.md` | A2A agent configuration |

### Examples Reference

| Directory | Description |
| --- | --- |
| `examples/basic/` | Minimal docker-compose setup |
| `examples/a2a/` | A2A agent demo with browser and n8n agents |
| `examples/computer-use/` | Computer-use with VNC |
| `examples/mcp/` | MCP server integration |
| `examples/model-switching/` | Model switching demo |
| `examples/shortcuts/` | Shortcuts example |
| `examples/telegram-channel/` | Telegram bot integration |
| `examples/web-terminal/` | Web terminal with SSH |

### Key Internal Packages

| Package | Responsibility |
| --- | --- |
| `internal/agent/` | Agent engine, state machine, tools |
| `internal/container/` | Dependency injection |
| `internal/domain/` | Interfaces, models, types |
| `internal/handlers/` | Chat message handling |
| `internal/services/` | Business logic (agent, conversation, approval, channels, scheduler) |
| `internal/infra/storage/` | Storage backends (JSONL, SQLite, PostgreSQL, Redis, Memory) |
| `internal/ui/` | Bubble Tea components, styles, keybindings |
| `internal/shortcuts/` | `/`-command shortcut system |
| `internal/web/` | Web terminal interface |
| `internal/logger/` | Structured logging (zap) |
| `internal/display/` | Computer-use display (macOS Swift app) |
| `config/` | Configuration structs and defaults |

---

## 10. Special Systems

### A2A (Agent-to-Agent)

Agents can delegate tasks to specialized agents:

- **Tools:** `A2A_SubmitTask`, `A2A_QueryAgent`, `A2A_QueryTask`
- **Config:** `~/.infer/agents.yaml` (managed via `infer agents` commands)
- **Background monitoring:** Automatic task status polling
- **Delegate agent URLs:** `http://localhost:8081`, `http://localhost:8083` (configurable)

### MCP (Model Context Protocol)

Extends tool capabilities via MCP servers:

- **Config:** `.infer/mcp.yaml` (managed via `infer mcp` commands)
- **Dynamic registration:** Tools loaded at runtime from MCP server definitions
- **Manager:** `internal/services/mcp_manager.go`

### Channels (Remote Messaging)

Pluggable messaging transports for remote agent control:

- **Daemon:** `infer channels-manager` (standalone process)
- **Channel Manager:** `internal/services/channel_manager.go`
- **Telegram:** `internal/services/channels/telegram.go`
- **IPC:** Approval requests flow via stdin/stdout JSON
- **Security:** Per-channel allowlists

### Scheduling (Cron-driven Tasks)

Recurring/one-off jobs with cron expressions:

- **Tool:** `Schedule` in `internal/agent/tools/schedule.go`
- **Scheduler:** `internal/services/scheduler/scheduler.go` (runs in channels-manager)
- **Store:** YAML files in `~/.infer/schedules/`
- **Hot-reload:** fsnotify watcher picks up changes

### Computer Use

Desktop automation capabilities:

- **Tools:** `mouse_click`, `mouse_move`, `mouse_scroll`, `keyboard_type`, `screenshot`, `activate_app`
- **macOS:** Native Swift app in `internal/display/macos/ComputerUse/`
- **Cross-platform:** Uses `robotgo` and `go-vgo/robotgo` libraries
- **Rate limiting:** Built-in rate limiter for safety

### Git Context

The agent automatically injects git repository context into system prompts:

- Repository name (from remote URL)
- Current branch
- Main branch
- Recent commits (last 5)
- Working directory path
- **File:** `internal/agent/agent_utils.go`
- **Config:** `agent.context.git_context_enabled`, `agent.context.git_context_refresh_turns: 10`

### Model Thinking Visualization

When models use extended thinking, reasoning is displayed as collapsible blocks:

- **Storage:** `ConversationEntry.ThinkingContent`
- **Toggle:** Configurable keybinding (default: `ctrl+k`)
- **Default:** Collapsed (first sentence visible)

### Conversation Versioning

Navigate back to previous conversation points (double ESC):

- View message history with timestamps
- Restore to any previous user message
- Permanent deletion after restore point
- **Docs:** `docs/features/conversation-versioning.md`

---

## Notes for AI Agents

### Working with This Project

1. **Always use `task` commands** instead of raw Go commands when possible
2. **Follow Conventional Commits** for any changes
3. **Run `task test` before committing** changes
4. **Check existing patterns** before implementing new features
5. **Read `CLAUDE.md`** for additional project-specific guidance
6. **Regenerate mocks** with `task mocks:generate` when interfaces change
7. **Use `flox activate`** for a consistent development environment

### Adding a New Tool

1. Create tool file in `internal/agent/tools/your_tool.go`
2. Implement `domain.Tool` interface: `Definition()`, `Execute()`, `Validate()`, `IsEnabled()`
3. Use `tools.NewParameterExtractor(args)` for type-safe parameter extraction
4. Register in `internal/agent/tools/registry.go`
5. Add config to `config/config.go` if needed
6. Update approval policy in `config/config.go` (`IsApprovalRequired`)
7. Write tests in `your_tool_test.go`

### Adding a New Channel

1. Implement `domain.Channel` interface in `internal/services/channels/`
2. Add config type to `config/config.go`
3. Register in `registerChannels()` in `cmd/channels.go`
4. Add allowlist case in `channel_manager.go` `isAllowedUser()`

### Configuration Management Rules

1. **Never commit `.env` files** - use `%ENV_VAR%` substitution in YAML
2. **Respect precedence:** Env vars > CLI flags > Config files > Defaults
3. **Use `INFER_` prefix** for all environment variables
4. **Document new config options** in both `docs/` and CLI help text

### Common Pitfalls

- **Mock regeneration:** Always run `task mocks:generate` after changing interfaces
- **Config defaults:** Don't manually call `v.SetDefault()` - use `registerConfigDefaults()` in `cmd/defaults.go`
- **CGO:** Project must remain CGO-free for portability (exception: macOS clipboard)
- **Storage migrations:** SQLite and PostgreSQL use automatic schema migrations via `cmd/migrate.go`
- **Concurrent access:** Agent state machine is protected by `sync.RWMutex`

---

*Generated: April 27, 2026*
*Project version: See `infer version` or `go.mod`*
