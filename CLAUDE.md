# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The Inference Gateway CLI (`infer`) is a command-line tool for managing and interacting with the Inference Gateway.
It provides an interactive chat interface, autonomous agent capabilities, and extensive tool integration for
AI-assisted development workflows. Built in Go with a terminal UI powered by Bubble Tea.

## Build and Test Commands

### Building

```bash
# Build the binary (default: ./infer)
task build

# Install to GOPATH/bin
task install

# Build multi-platform release binaries
task release:build
```

### Testing

```bash
# Run all tests
task test

# Run tests with verbose output
task test:verbose

# Run tests with coverage
task test:coverage

# Run single test file
go test -v ./internal/handlers/event_registry_test.go

# Run specific test by name
go test -v -run TestEventRegistry_AutoRegisterHandlers ./internal/handlers
```

### Code Quality

```bash
# Format code
task fmt

# Run linters (golangci-lint + markdownlint)
task lint

# Run go vet
task vet

# Run all quality checks (fmt + vet + test)
task check
```

### Mocks

```bash
# Generate mocks using counterfeiter
task mocks:generate

# Clean generated mocks
task mocks:clean
```

## Architecture and Key Design Patterns

### Clean Architecture Layers

The codebase follows **Clean Architecture** with clear separation:

1. **Domain Layer** (`internal/domain/`): Core business entities, interfaces, and value objects
   - `interfaces.go`: Central registry of all service interfaces and domain types
   - Event types (ChatEvent, UIEvent) for asynchronous communication
   - Domain models (Tool, ConversationEntry, FileInfo, etc.)

2. **Application Layer** (`internal/app/`): Application-specific logic and coordination

3. **Services Layer** (`internal/services/`): Business logic implementation
   - `agent.go`: Autonomous agent execution logic
   - `conversation.go`: Conversation management
   - `tools.go`: Tool factory and service
   - `tool_formatter.go`: Tool result formatting for UI/LLM contexts
   - `state_manager.go`: Application state management

4. **Handlers Layer** (`internal/handlers/`): Event handlers for chat and commands
   - `chat_handler.go`: Main chat interface logic
   - `chat_message_processor.go`: Message processing and streaming
   - `event_registry.go`: Automatic event handler registration using reflection

5. **Infrastructure Layer** (`internal/infra/`): External concerns (storage, etc.)

6. **UI Layer** (`internal/ui/`): Terminal UI components using Bubble Tea

### Event-Driven Architecture

The chat interface uses an **event-driven pattern** with:

- **Event Registry** (`event_registry.go`): Automatically registers handler methods using reflection
  - Convention: Handler methods must be named `Handle{EventTypeName}` (e.g., `HandleChatStartEvent`)
  - All event types must be registered in `autoRegisterHandlers()` or the system will panic at startup
  - This strict registration prevents silent event drops

- **Event Types** (defined in `internal/domain/interfaces.go`):
  - `ChatEvent`: Chat operations (ChatStart, ChatChunk, ChatComplete, ChatError, etc.)
  - `UIEvent`: UI updates (UpdateHistory, SetStatus, ShowError, etc.)
  - Tool execution events (ToolCallPreview, ToolExecutionStarted, ToolExecutionCompleted)
  - A2A events (A2ATaskSubmitted, A2ATaskStatusUpdate, A2ATaskCompleted)

### Tool System Architecture

Tools are implemented using the **Factory Pattern** and **Strategy Pattern**:

- **Tool Interface** (`domain.Tool`): Common interface for all tools with:
  - `Definition()`: Returns tool schema for LLM
  - `Execute()`: Executes the tool with given arguments
  - `Validate()`: Validates arguments before execution
  - `FormatResult()`: Formats results for different contexts (UI, LLM, Short)

- **Tool Factory** (`services.ToolFactory`): Creates tool instances by name

- **Tool Services**: Individual tool implementations in `internal/services/tools/`:
  - `bash.go`: Command execution
  - `read.go`, `write.go`, `edit.go`: File operations
  - `grep.go`: Code search
  - `websearch.go`, `webfetch.go`: Web operations
  - `github.go`: GitHub API integration
  - `a2a_*.go`: Agent-to-agent communication tools

### State Management

The `StateManager` (`internal/services/state_manager.go`) centralizes application state using concurrent-safe patterns:

- Tracks conversation state, tool execution status, task tracking
- Manages A2A task state and context IDs
- Provides state observers for reactive updates

## Important Conventions

### Adding New Event Types

When adding a new event type, you MUST:

1. Define the event struct in `internal/domain/interfaces.go`
2. Add the event to the `eventTypes` slice in `event_registry.go:autoRegisterHandlers()`
3. Implement a handler method in the appropriate handler file named `Handle{EventTypeName}`

**Failure to follow this pattern will cause a panic at startup**, which is intentional to prevent silent event drops.

### Tool Result Formatting

Tool results support multiple formatting contexts:

- **FormatterUI**: Compact display for terminal UI (limited width)
- **FormatterLLM**: Full details for LLM consumption
- **FormatterShort**: Brief summary for inline display

Implement `FormatResult()` in your Tool implementation to support all contexts.

### Testing Event Handlers

Test event handlers by:

1. Creating mock dependencies using counterfeiter
2. Instantiating the handler with mocks
3. Calling the specific `Handle{EventType}` method directly
4. Asserting on returned model state and commands

Example:

```go
handler := NewChatHandler(mockRepo, mockToolService, ...)
model, cmd := handler.HandleChatStartEvent(event, stateManager)
// Assert on model and cmd
```

## Key Files and Their Purpose

### Command Layer (`cmd/`)

- `root.go`: CLI root command and global flags
- `chat.go`: Interactive chat command
- `agent.go`: Autonomous agent command
- `config.go`: Configuration management commands
- `init.go`: Project initialization with AI analysis

### Domain Layer (`internal/domain/`)

- `interfaces.go`: **Central registry** of all interfaces and domain types
  - All service interfaces (ChatService, FileService, ToolService, etc.)
  - All domain types (Tool, ToolExecutionResult, ConversationEntry, etc.)
  - All event types (ChatEvent, UIEvent types)

### Handlers Layer (`internal/handlers/`)

- `event_registry.go`: Reflection-based event handler registration
- `chat_handler.go`: Main chat interface orchestration
- `chat_message_processor.go`: Message processing and streaming logic
- `chat_command_handler.go`: Chat command processing (/clear, /exit, etc.)
- `chat_shortcut_handler.go`: Custom shortcut execution

### Services Layer (`internal/services/`)

- `agent.go`: Autonomous agent with iterative task execution
- `conversation.go`: Conversation history management
- `persistent_conversation.go`: Storage backend integration
- `state_manager.go`: Centralized state management
- `tool_formatter.go`: Tool result formatting strategies
- `tools/`: Individual tool implementations

### UI Layer (`internal/ui/`)

- Bubble Tea components for terminal rendering
- Model-View-Update pattern
- Theme support with multiple color schemes

## Development Workflow Tips

### Running the CLI During Development

```bash
# Run chat command with verbose logging
go run . chat --verbose

# Run agent command
go run . agent "Fix the failing tests"

# Run with custom config
go run . --config ./test-config.yaml chat
```

### Debugging Event Flow

The event registry will panic if a handler is missing. This is intentional:

- **Before adding an event**: Check `event_registry.go` for the registration pattern
- **When debugging events**: Look for the event type in `internal/domain/interfaces.go`
- **When events aren't firing**: Check that the event is in the `eventTypes` slice

### Working with Tools

When adding a new tool:

1. Create implementation in `internal/services/tools/{toolname}.go`
2. Implement the `domain.Tool` interface
3. Add tool to factory in `internal/services/tools.go`
4. Update config schema in `config/config.go`
5. Add tool definition to relevant documentation

### Dependency Injection

The application uses a container pattern (`internal/container/`):

- Dependencies are injected via constructors
- Services depend on interfaces, not implementations
- Mock generation via `task mocks:generate` for testing

## Configuration System

The CLI uses a 2-layer configuration system:

1. **Userspace**: `~/.infer/config.yaml` (global defaults)
2. **Project**: `.infer/config.yaml` (project-specific, takes precedence)

Configuration is loaded using Viper with environment variable overrides (`INFER_*` prefix).

## Testing Strategy

- **Unit tests**: Co-located with source files (`*_test.go`)
- **Mocks**: Generated via counterfeiter in `tests/mocks/generated/`
- **Integration tests**: Minimal, focused on critical paths
- **Test coverage**: Not enforced, but expected for new features

Run specific test patterns:

```bash
# Test a specific package
go test -v ./internal/handlers

# Test with race detector
go test -race ./...

# Test with coverage
go test -cover ./internal/services/...
```

## Common Gotchas

1. **Event handler naming**: Must be exact `Handle{EventTypeName}` or will panic
2. **Tool registration**: Tools must be registered in factory or they won't be available
3. **State synchronization**: Use StateManager for shared state, not direct field access
4. **Context handling**: Always pass context through for cancellation support
5. **SDK compatibility**: This project uses `github.com/inference-gateway/sdk` - ensure compatibility when updating

## Git Workflow

- Main development branch: `main`
- Feature branches: `feature/*` or `claude/*` for Claude Code work
- Pre-commit hooks enforce formatting and linting
- Conventional commits are preferred but not enforced
- CI runs on all PRs (linting, testing, building)

## Related Documentation

- Full README: `README.md`
- Configuration details: `CONFIG.md`
- Contributing guidelines: `CONTRIBUTING.md`
- Agent architecture: `AGENTS.md`
