# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Core Development Rules

NEVER use fmt statements for Debugging - instead you use the logger.

NEVER write inline comments.

ALWAYS use table-driven tests for testing your code.

ALWAYS use early returns for handling errors and special cases - use protective programming techniques to avoid deep nesting.

ALWAYS generate mocks for interfaces using counterfeiter - NEVER create custom mocks yourself.

ALWAYS review Taskfile.yml for available developer tasks.

ALWAYS use named imports for nonstandard library packages.

Please refer to AGENTS.md for more information.

## Project Overview

The Inference Gateway CLI is a comprehensive command-line interface for managing and interacting with
inference services. It provides an interactive chat interface, autonomous agent capabilities, and
extensive tool execution for AI models.

**Architecture**: Clean Architecture pattern with dependency injection
**Language**: Go 1.24+
**Framework**: Cobra CLI with Bubble Tea TUI
**Testing**: Table-driven tests with counterfeiter mocks

## Development Commands

All development tasks are managed through [Task](https://taskfile.dev). Key commands:

### Core Development

```bash
task build          # Build the CLI binary
task test           # Run all tests
task test:verbose   # Run tests with verbose output
task test:coverage  # Run tests with coverage
task fmt            # Format Go code
task lint           # Run golangci-lint + markdownlint
task vet            # Run go vet
task check          # Run fmt + vet + test
task dev            # Development workflow (fmt + build + test)
```

### Mock Generation

```bash
task mocks:generate # Generate all mocks using counterfeiter
task mocks:clean    # Clean generated mocks
```

### Release & Quality

```bash
task release:build     # Multi-platform release builds
task precommit:run     # Run pre-commit hooks
task setup            # Full development environment setup
```

### UI Testing

```bash
task test:ui:snapshots  # Generate UI component snapshots
task test:ui:verify     # Verify UI snapshots match
task test:ui:interactive # Interactive UI component testing
```

## Code Architecture

### Directory Structure

```text
├── cmd/                    # Cobra CLI commands
├── internal/
│   ├── domain/            # Domain interfaces & entities
│   ├── container/         # Dependency injection container
│   ├── services/          # Business logic implementations
│   │   └── tools/         # Tool implementations (Bash, Read, Write, etc.)
│   ├── handlers/          # Message routing & event handling
│   ├── ui/               # Bubble Tea TUI components
│   ├── infra/            # Infrastructure (storage adapters)
│   └── shortcuts/        # Extensible command shortcuts
└── tests/mocks/generated/ # Counterfeiter-generated mocks
```

### Key Patterns

**Dependency Injection**: Central `ServiceContainer` manages all dependencies

```go
// Services are injected via container
type ServiceContainer struct {
    AgentService      domain.AgentService
    ToolService       domain.ToolService
    ConversationRepo  domain.ConversationRepository
    // ...
}
```

**Tool System**: Extensible tool registry with common interface

```go
type Tool interface {
    Name() string
    Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error)
    IsEnabled(config *Config) bool
    // ...
}
```

**Event-Driven UI**: Bubble Tea with custom message types

```go
type UIEvent struct {
    Type string
    Data interface{}
}
```

## Coding Standards

### Required Practices

- **NEVER use fmt statements for debugging** - Use the logger instead
- **NEVER write inline comments** - Code should be self-documenting
- **ALWAYS use table-driven tests** for all test functions
- **ALWAYS use early returns** for error handling (protective programming)
- **ALWAYS generate mocks using counterfeiter** - Never create custom mocks
- **ALWAYS use named imports** for non-standard library packages

### Example Table-Driven Test

```go
func TestBashTool_Execute(t *testing.T) {
    tests := []struct {
        name     string
        command  string
        expected *ToolResult
        wantErr  bool
    }{
        {
            name:     "valid command",
            command:  "echo hello",
            expected: &ToolResult{Output: "hello\n"},
            wantErr:  false,
        },
        // More test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Error Handling Pattern

```go
// Good: Early return with protective programming
func ProcessRequest(req *Request) (*Response, error) {
    if req == nil {
        return nil, ErrInvalidRequest
    }

    if !req.IsValid() {
        return nil, ErrValidationFailed
    }

    result, err := performOperation(req)
    if err != nil {
        return nil, fmt.Errorf("operation failed: %w", err)
    }

    return result, nil
}
```

## Configuration System

**2-Layer Configuration**:

1. Userspace: `~/.infer/config.yaml` (global fallback)
2. Project: `.infer/config.yaml` (takes precedence)

**Key Configuration Areas**:

- `gateway`: Connection settings, API keys, timeouts
- `tools`: Security controls, whitelisting, approval settings
- `agent`: Model selection, system prompts, optimization
- `storage`: Database configurations for conversation persistence
- `chat`: Default models, system prompts, reminders

## Security & Safety

### Multi-layered Security

1. **Tool Whitelisting**: Command pattern matching and validation
2. **Path Exclusions**: Protected directories (`.git/`, `.infer/`, `*.env`)
3. **Approval Workflows**: User confirmation for destructive operations
4. **Sandbox Directories**: Restricted file access
5. **Input Validation**: Comprehensive parameter validation

### Security-by-Default

- Write/Edit/Delete operations require approval
- Read operations are safe by default
- Environment variable substitution with validation

## Testing Strategy

### Test Types

1. **Unit Tests**: Table-driven tests for all components
2. **Integration Tests**: End-to-end command testing
3. **UI Regression Tests**: Snapshot-based TUI testing
4. **Mock Generation**: Automated with counterfeiter

### Running Tests

```bash
task test                    # All tests
task test:coverage          # With coverage
task test:ui:verify         # UI regression tests
task mocks:generate         # Regenerate mocks
```

## Tools & Dependencies

### Core Dependencies

- **cobra**: CLI framework
- **bubbletea**: Terminal UI framework
- **yaml.v3**: YAML configuration parsing
- **counterfeiter**: Mock generation
- **golangci-lint**: Code linting

### Tool System

- **Bash**: Execute whitelisted shell commands
- **Read/Write/Edit**: File operations with security controls
- **Grep**: Ripgrep-powered search with regex support
- **WebSearch/WebFetch**: Web integration with domain whitelisting
- **GitHub**: GitHub API integration
- **TodoWrite**: Task management for LLM workflows
- **A2A/MCP Tools**: Gateway middleware tools with client-side visualization

#### A2A and MCP Tool Call Handling

Tools prefixed with `a2a_*` or `mcp_*` receive special handling based on configuration:

- **Chat Mode**: Tools are visualized but execution is skipped when `gateway.middlewares.a2a` or `gateway.middlewares.mcp` is `true` (default)
- **Agent Mode**: Tools are executed normally (main agent has full access)
- **Visualization**: Skipped tools show as "executed on Gateway" with appropriate metadata
- **Configuration**: Execution behavior is configurable via `gateway.middlewares.a2a` and `gateway.middlewares.mcp` (when `true`, tools execute on Gateway; when `false`, tools execute on client)
- **Purpose**: Maintains simple clients while centralizing operations on Gateway, with flexibility to enable client execution when needed

## Agent System

The CLI includes an autonomous agent mode (`infer agent`) for iterative task completion:

- **JSON Output**: Structured conversation stream
- **Tool Integration**: Full access to all available tools
- **GitHub Integration**: Issue recognition and SCM workflows
- **Iterative Processing**: Continues until task completion

See AGENTS.md for detailed agent patterns and workflows.

## Development Workflow

### Standard Workflow

1. **Setup**: `task setup` (installs deps, pre-commit hooks)
2. **Development**: `task dev` (format + build + test)
3. **Quality**: `task lint` and `task precommit:run`
4. **Mocks**: `task mocks:generate` when interfaces change
5. **UI Testing**: `task test:ui:verify` for TUI changes

### Pre-commit Quality Gates

- Go formatting (`gofmt`)
- Linting (`golangci-lint`)
- Markdown linting
- Test execution
- Generated mock validation

### Release Process

- Multi-platform builds with `task release:build`
- Cosign signature verification for binaries
- Automated releases via GitHub Actions
