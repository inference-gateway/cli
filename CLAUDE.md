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

### A2A (Agent-to-Agent) Communication

Enables task delegation to specialized agents:

- Configuration in `.infer/agents.yaml`
- Tools: `A2A_SubmitTask`, `A2A_QueryAgent`, `A2A_QueryTask`, `A2A_DownloadArtifacts`
- Background task monitoring with exponential backoff polling

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

## Development Environment

- **Go version**: 1.25.2+ required
- **Task**: Task runner (taskfile.dev) for build automation
- **golangci-lint**: Linting and static analysis
- **pre-commit**: Git hooks for code quality
- **Docker**: Optional, for gateway Docker mode
- **Flox**: Optional, for reproducible dev environment
