# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The Inference Gateway CLI is a Go-based command-line interface for managing AI model interactions.
It provides interactive chat, autonomous agent execution, and extensive tool integration for LLMs.
The project uses Clean Architecture with domain-driven design patterns.

## Build & Test Commands

### Development Commands

```bash
# Build the binary
task build

# Run all tests
task test

# Run tests with coverage
task test:coverage

# Format code
task fmt

# Run linter
task lint

# Run all quality checks (format, vet, lint, test)
task check

# Complete development workflow (format, build, test)
task dev
```

### Module Management

```bash
# Tidy go modules
task mod:tidy

# Generate mocks for testing
task mocks:generate

# Clean generated mocks
task mocks:clean
```

### Running Specific Tests

```bash
# Run tests for a specific package
go test ./internal/services/tools

# Run specific test with verbose output
go test -v ./internal/services -run TestSpecificFunction

# Run tests with race detection
go test -race ./...
```

## Architecture

### Clean Architecture Layers

The codebase follows Clean Architecture principles with clear separation:

1. **Domain Layer** (`internal/domain/`): Core business logic and interfaces
   - Contains domain models, interfaces, and business rules
   - No dependencies on external frameworks or infrastructure
   - Defines contracts for services through interfaces

2. **Application Layer** (`cmd/`, `internal/handlers/`): Application-specific logic
   - CLI command implementations using Cobra
   - Event handlers for chat and tool execution
   - Orchestrates domain services

3. **Infrastructure Layer** (`internal/infra/`, `internal/services/`): External concerns
   - Storage implementations (SQLite, PostgreSQL, Redis)
   - Tool implementations (Bash, Read, Write, Grep, etc.)
   - SDK integration for model communication

4. **UI Layer** (`internal/ui/`): Terminal user interface
   - BubbleTea components for interactive chat
   - Approval modals for tool execution
   - Theme management and styling

### Dependency Injection

The project uses a **ServiceContainer** pattern (`internal/container/container.go`):

- Centralizes dependency creation and wiring
- Manages service lifecycles
- Provides access to all services through getter methods
- Initialized once at application startup

Example:

```go
container := container.NewServiceContainer(cfg, viper)
chatService := container.ChatService()
toolService := container.ToolService()
```

### Tool System Architecture

Tools are implemented as plugins following a registry pattern:

1. **Tool Interface** (`internal/domain/tools.go`): Defines contract for all tools
2. **Tool Registry** (`internal/services/tools/registry.go`): Manages tool registration and execution
3. **Individual Tools** (`internal/services/tools/*.go`): Specific tool implementations

Each tool must implement:

- `Definition()`: Returns tool schema for LLM
- `Execute(ctx, args)`: Executes the tool with given arguments
- `Validate(args)`: Validates arguments before execution
- `IsEnabled()`: Reports if tool is enabled in config

## Configuration System

### Configuration Precedence (highest to lowest)

1. Environment variables (`INFER_*`)
2. Command line flags
3. Project config (`.infer/config.yaml`)
4. Userspace config (`~/.infer/config.yaml`)
5. Built-in defaults

### Important Config Files

- **`config/config.go`**: Config struct definitions and defaults
- **`config/agents.go`**: A2A agent configuration
- **`.infer/config.yaml`**: Project-level configuration (committed)
- **`~/.infer/config.yaml`**: User-level configuration (not committed)

### Environment Variable Pattern

All config fields can be overridden via environment variables:

- Format: `INFER_<PATH>` where dots become underscores
- Example: `gateway.url` → `INFER_GATEWAY_URL`
- Example: `tools.bash.enabled` → `INFER_TOOLS_BASH_ENABLED`

### Keybinding Configuration

The CLI supports customizable keybindings for the chat interface with a namespace-based organization
system. All keybindings are visible in the config file by default for self-documentation.

**Configuration Location:** `chat.keybindings` in config.yaml

**Default State:** Keybindings are **disabled by default**. Users must explicitly enable them.

**Namespace System:**

Action IDs use the format `namespace_action` (e.g., `global_quit`, `mode_cycle_agent_mode`). This
allows the same key to be used in different namespaces without conflict, as actions are
context-specific. The namespace is extracted from the first part of the action ID before the
underscore.

**Environment Variable Support:**

Keybindings can be configured via environment variables using comma-separated or newline-separated
lists:

```bash
# Enable keybindings
export INFER_CHAT_KEYBINDINGS_ENABLED=true

# Set keys for an action (comma-separated or newline-separated)
export INFER_CHAT_KEYBINDINGS_BINDINGS_GLOBAL_QUIT_KEYS="ctrl+q,ctrl+x"

# Multiline format
export INFER_CHAT_KEYBINDINGS_BINDINGS_MODE_CYCLE_AGENT_MODE_KEYS="shift+tab
ctrl+m"

# Enable/disable specific actions
export INFER_CHAT_KEYBINDINGS_BINDINGS_DISPLAY_TOGGLE_RAW_FORMAT_ENABLED=false
```

Format: `INFER_CHAT_KEYBINDINGS_BINDINGS_<ACTION_ID>_<FIELD>`

- `<ACTION_ID>`: Uppercase namespaced action ID (e.g., `GLOBAL_QUIT`, `MODE_CYCLE_AGENT_MODE`)
- `<FIELD>`: Either `KEYS` (comma/newline-separated) or `ENABLED` (true/false)

**Configuration Structure:**

```yaml
chat:
  theme: tokyo-night
  keybindings:
    enabled: false  # Set to true to enable custom keybindings
    bindings:
      # All keybindings are listed with their defaults
      # Format: namespace_action
      global_quit:
        keys:
          - ctrl+c
        description: "exit application"
        category: "global"
        enabled: true
      mode_cycle_agent_mode:
        keys:
          - shift+tab  # Modify this to change the key
        description: "cycle agent mode (Standard/Plan/Auto-Accept)"
        category: "mode"
        enabled: true
      tools_toggle_tool_expansion:
        keys:
          - ctrl+o
        enabled: false  # Disable specific actions
      # ... all other keybindings visible
```

**Key Features:**

- **Namespace-Based Organization**: Actions organized by namespace for context-specific bindings
- **Context-Aware Conflicts**: Same key allowed across namespaces, validated within namespaces
- **Self-Documenting**: All keybindings are visible in config with descriptions
- **Fallback to Defaults**: Remove an entry from config to use the default
- **No Runtime Validation**: Config is loaded once at startup for performance
- **Explicit Validation**: Run `infer keybindings validate` before committing changes

**Available Commands:**

```bash
# List all keybindings with their current assignments
infer keybindings list

# Set custom key for an action (use namespaced action ID)
infer keybindings set mode_cycle_agent_mode ctrl+m

# Disable a specific action
infer keybindings disable display_toggle_raw_format

# Enable a specific action
infer keybindings enable display_toggle_raw_format

# Reset all keybindings to defaults
infer keybindings reset

# Validate configuration for conflicts, invalid keys, and unknown actions
infer keybindings validate
```

**Validation:**

The `validate` command checks for:

- Unknown action IDs not in the registry
- Invalid keys not in the known keys list
- Key conflicts **within the same namespace** (cross-namespace conflicts are allowed)

Validation is namespace-aware: actions in different namespaces can share the same key without
triggering a conflict, as they operate in different contexts.

**Key Action Namespaces:**

Actions are organized by namespace. Each namespace represents a specific context or domain:

- **global**: Application-level actions (e.g., `global_quit`, `global_cancel`)
- **chat**: Chat-specific actions (e.g., `chat_enter_key_handler`)
- **mode**: Agent mode controls (e.g., `mode_cycle_agent_mode`)
- **tools**: Tool-related actions (e.g., `tools_toggle_tool_expansion`)
- **display**: Display toggles (e.g., `display_toggle_raw_format`, `display_toggle_todo_box`)
- **text_editing**: Text manipulation (e.g., `text_editing_move_cursor_left`,
  `text_editing_history_up`)
- **navigation**: Viewport navigation (e.g., `navigation_scroll_to_top`, `navigation_page_down`)
- **clipboard**: Copy/paste operations (e.g., `clipboard_copy_text`, `clipboard_paste_text`)
- **selection**: Selection mode controls (e.g., `selection_toggle_mouse_mode`)
- **plan_approval**: Plan approval navigation (e.g., `plan_approval_plan_approval_accept`)
- **help**: Help system (e.g., `help_toggle_help`)

**Implementation Details:**

- Registry pattern in `internal/ui/keybinding/`
- Layer-based priority system for context-aware bindings
- Namespace definitions in `config/config.go` with `ActionID()` helper function
- Config loaded once at chat startup (no runtime reloading)
- Conflict resolution: layer system handles priority, last binding wins at runtime
- Key validation uses `internal/ui/keys/` package
- Namespace-aware validation in `cmd/keybindings.go`

## Code Conventions

### File Organization

- **Snake case** for file names: `conversation_title.go`
- **Camel case** for Go identifiers: `ConversationTitle`
- **Grouped imports**: stdlib, third-party, local
- **Tests co-located**: `file.go` + `file_test.go`

### Naming Patterns

- **Services**: End with `Service` (e.g., `ChatService`, `ToolService`)
- **Repositories**: End with `Repository` (e.g., `ConversationRepository`)
- **Interfaces**: Descriptive names, often ending in `-er` (e.g., `FileWriter`, `PathValidator`)
- **Config structs**: End with `Config` (e.g., `GatewayConfig`, `ToolsConfig`)

### Error Handling

- Use `fmt.Errorf` with `%w` for error wrapping
- Return errors explicitly, don't panic
- Provide context in error messages
- Use domain-specific error types when appropriate

### Testing Patterns

1. **Table-driven tests** for multiple scenarios:

```go
tests := []struct {
    name    string
    input   string
    want    string
    wantErr bool
}{
    // test cases
}
```

2. **Mock generation** with counterfeiter:

- Mocks stored in `tests/mocks/`
- Regenerate with `task mocks:generate`

3. **Test helpers** for common setup:

```go
func setupTestConfig() *config.Config {
    return &config.Config{
        Tools: config.ToolsConfig{Enabled: true},
    }
}
```

## Key Design Patterns

### Repository Pattern

Storage implementations abstracted behind interfaces:

- Interface: `domain.ConversationRepository`
- Implementations: `storage.SQLiteStorage`, `storage.PostgresStorage`, `storage.RedisStorage`
- Factory: `storage.NewConversationStorage(cfg)`

### Strategy Pattern

Tools and services use strategy pattern for different implementations:

- Grep backend: ripgrep vs Go implementation
- Gateway mode: Docker vs binary mode
- Storage backend: SQLite vs PostgreSQL vs Redis

### Observer Pattern

Event-driven architecture for chat and agent execution:

- Events defined in `internal/domain/events.go`
- Handlers in `internal/handlers/`
- State management through `StateManager`

### Registry Pattern

Tools registered and accessed through central registry:

```go
registry := tools.NewRegistry(cfg, services...)
registry.GetTool("Bash")
registry.ListTools()
```

## Adding New Tools

See `CONTRIBUTING.md` for detailed guide. Key steps:

1. Create `internal/services/tools/your_tool.go`
2. Implement `Tool` interface (Definition, Execute, Validate, IsEnabled)
3. Register in `internal/services/tools/registry.go`
4. Add config section to `config/config.go` if needed
5. Write tests in `your_tool_test.go`

## Working with UI Components

The TUI uses BubbleTea framework:

- Models in `internal/ui/components/`
- Shared styles in `internal/ui/styles/`
- Theme system in `internal/domain/theme_provider.go`

### Testing UI Components

Use the internal `test-view` command for rapid iteration:

```bash
./infer test-view approval    # Test approval modal
./infer test-view diff         # Test diff renderer
./infer test-view multiedit    # Test MultiEdit formatting
```

Generate snapshots for regression testing:

```bash
task test:ui:snapshots    # Generate baseline snapshots
task test:ui:verify       # Verify against snapshots
```

## Commit Message Format

Follow Conventional Commits specification:

```text
<type>[optional scope]: <description>

[optional body]

[optional footer]
```

**Types**: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert

**Examples**:

- `feat: add WebSearch tool for DuckDuckGo and Google`
- `fix(tools): resolve Bash command validation edge case`
- `docs: update tool configuration guide`
- `feat!: change tool approval system interface` (breaking change)

## Important Implementation Details

### Tool Approval System

Tools requiring user approval use a two-phase execution:

1. **Validation Phase**: `Validate(args)` checks arguments
2. **Approval Phase**: UI shows modal with tool details and diff preview
3. **Execution Phase**: `Execute(ctx, args)` runs only if approved

Controlled by `require_approval` config per tool.

### Conversation Management Commands

The CLI provides commands to manage saved conversation history:

**Command:** `infer conversations list [flags]`

**Purpose:** List all saved conversations from the database with pagination support

**Flags:**

- `--limit, -l int` (default: 50) - Maximum conversations to display
- `--offset int` (default: 0) - Number of conversations to skip
- `--format, -f string` (default: "text") - Output format (text, json)

**Output Columns (text format):**

- **ID**: Full conversation ID (36 chars)
- **Summary**: Conversation title (truncated to 25 chars)
- **Messages**: Total message count
- **Requests**: API request count
- **Input Tokens**: Total input tokens used
- **Output Tokens**: Total output tokens generated
- **Cost**: Formatted cost with adaptive precision (e.g., "$0.023" or "-")

**JSON Output:**

```json
{
  "conversations": [
    {
      "id": "...",
      "title": "...",
      "created_at": "...",
      "updated_at": "...",
      "message_count": 10,
      "token_stats": {...},
      "cost_stats": {...}
    }
  ],
  "count": 42
}
```

**Storage Backend:** Uses `ConversationStorage.ListConversations(ctx, limit, offset)` interface

**Implementation Files:**

- `cmd/conversations.go` - Command implementation
- `cmd/conversations_test.go` - Unit tests
- `internal/formatting/formatting.go` - Contains `FormatCost()` helper

**Examples:**

```bash
# List all conversations (default: 50)
infer conversations list

# Pagination
infer conversations list --limit 20 --offset 40

# JSON output for scripting
infer conversations list --format json
```

### A2A (Agent-to-Agent) Communication

Enables task delegation to specialized agents:

- Configuration in `.infer/agents.yaml`
- Tools: `A2A_SubmitTask`, `A2A_QueryAgent`, `A2A_QueryTask`, `A2A_DownloadArtifacts`
- Background task monitoring with exponential backoff polling

### MCP (Model Context Protocol) Integration

Direct integration with MCP servers for stateless tool execution:

**Configuration**: `.infer/mcp.yaml`

**Key Features**:

- Direct CLI → MCP server connections (bypasses gateway)
- Stateless HTTP SSE transport
- **Auto-start MCP servers in OCI/Docker containers**
- **Automatic port assignment** (no manual configuration needed)
- Per-server enable/disable toggle
- Tool filtering with include/exclude lists
- Concurrent tool discovery at startup
- Automatic exclusion from Plan mode

**Global Settings**:

```yaml
enabled: true
connection_timeout: 30  # seconds
discovery_timeout: 30   # seconds
```

**Server Configuration (External)**:

```yaml
servers:
  - name: "filesystem"
    url: "http://localhost:3000/sse"
    enabled: true
    timeout: 60  # Override global timeout
    description: "File system operations"
    exclude_tools:  # Blacklist dangerous operations
      - "delete_file"
      - "format_disk"
```

**Server Configuration (Auto-start)**:

```yaml
servers:
  - name: "demo-server"
    enabled: true
    run: true                      # Auto-start in container
    oci: "mcp-demo-server:latest"  # OCI/Docker image
    port: 3000                     # Auto-assigned if omitted
    path: /mcp
    startup_timeout: 60
    env:
      LOG_LEVEL: "debug"
    volumes:
      - "/data:/mnt/data"
```

**Tool Naming**: MCP tools use `MCP_<servername>_<toolname>` format

- Example: `MCP_filesystem_read_file`

**Environment Variables**:

- `INFER_MCP_ENABLED` - Enable/disable MCP globally
- `INFER_MCP_CONNECTION_TIMEOUT` - Override connection timeout
- Server URLs support ${VAR} expansion

**Implementation**:

- Client manager: `internal/services/mcp_client_manager.go`
- Server manager: `internal/services/mcp_server_manager.go` (OCI container lifecycle)
- Tool wrapper: `internal/services/tools/mcp_tool.go`
- Config service: `internal/services/mcp_config.go`
- Uses `github.com/metoro-io/mcp-golang` library

**Mode Behavior**:

- Standard mode: All MCP tools available
- Auto-Accept mode: All MCP tools available
- Plan mode: MCP tools excluded (read-only)

**Resilience**:

- Failed servers log warnings but don't prevent CLI startup
- Partial tool discovery continues if some servers fail
- Timeouts prevent hanging connections
- Background server startup (non-blocking)

**CLI Commands**:

```bash
# Add server with auto-start (automatic port assignment)
infer mcp add <name> --run --oci=<image>

# Add server with specific port
infer mcp add <name> --run --oci=<image> --port=8080

# Add external server (manual start)
infer mcp add <name> <url>

# List all MCP servers
infer mcp list

# Remove server
infer mcp remove <name>

# Toggle server enable/disable
infer mcp toggle <name>
```

**See Also**: `docs/mcp-integration.md` for comprehensive guide

### Shortcuts System

User-defined commands in `.infer/shortcuts/`:

- Built-in: `/clear`, `/exit`, `/help`, `/switch`, `/theme`
- Git: `/git-status`, `/git-commit`, `/git-push`
- SCM: `/scm-issues`, `/scm-pr-create`
- Custom: `custom-*.yaml` files in shortcuts directory

**AI-powered snippets**: Execute commands, send output to LLM, use response in template

### State Management

Conversation state managed through `StateManager`:

- Tracks agent modes (Standard, Plan, Auto-Accept)
- Manages conversation history
- Handles tool execution state
- Coordinates background tasks

### Gateway Management

Automatic gateway lifecycle management:

- Downloads gateway binary if not present
- Starts gateway in background (Docker or binary mode)
- Monitors health via `/status` endpoint
- Shuts down on CLI exit

## Common Gotchas

1. **Config changes**: After modifying config structs, update defaults in `config/defaults.go`
2. **Tool registration**: New tools must be registered in registry's `registerTools()` method
3. **Viper environment variables**: Use underscores, not dots (e.g., `INFER_GATEWAY_URL`)
4. **Mock regeneration**: Run `task mocks:generate` after changing interfaces
5. **Read before Edit**: Edit tools require Read tool to be used first on the file
6. **Context propagation**: Always pass context through to allow cancellation

## External Dependencies

Key third-party libraries:

- **Cobra**: CLI framework for commands and flags
- **Viper**: Configuration management with env var support
- **BubbleTea**: Terminal UI framework
- **Lipgloss**: Styling for terminal output
- **go-redis**: Redis client for storage backend
- **lib/pq**: PostgreSQL driver
- **modernc.org/sqlite**: CGO-free SQLite driver
- **metoro-io/mcp-golang**: MCP (Model Context Protocol) client library

## Development Environment

- **Go version**: 1.25.4+ required
- **Task**: Task runner (taskfile.dev) for build automation
- **golangci-lint**: Linting and static analysis
- **pre-commit**: Git hooks for code quality
- **Docker**: Optional, for gateway Docker mode
- **Flox**: Optional, for reproducible dev environment
