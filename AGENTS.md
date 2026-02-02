# AGENTS.md - Inference Gateway CLI

This document provides comprehensive guidance for AI agents working with the Inference Gateway CLI
project. It covers project structure, development workflow, coding standards, and operational details.

## Project Overview

**Inference Gateway CLI** is a powerful command-line interface for managing and interacting with AI
inference services. It provides interactive chat, autonomous agent capabilities, and extensive tool
execution for AI models.

### Key Technologies

- **Language**: Go 1.25+
- **UI Framework**: Bubble Tea (TUI framework)
- **Gateway Integration**: Via `inference-gateway/sdk` and `inference-gateway/adk`
- **Storage Backends**: JSONL (default), SQLite, PostgreSQL, Redis, In-memory
- **Build Tool**: Task (Taskfile)
- **Environment**: Flox (development environment manager)
- **Containerization**: Docker for deployment and testing

## Architecture and Structure

### Project Layout

```text
.
├── cmd/                    # CLI commands (cobra-based)
│   ├── agent.go           # Autonomous agent command
│   ├── chat.go            # Interactive chat command
│   ├── config.go          # Configuration management
│   ├── agents.go          # A2A agent management
│   └── root.go            # Root command and global flags
├── config/                # Configuration structs
│   └── config.go          # Main config definition
├── internal/              # Internal application code
│   ├── app/               # Application initialization
│   ├── container/         # Dependency injection container
│   ├── domain/            # Domain interfaces and models
│   ├── handlers/          # Message/event handlers
│   ├── services/          # Business logic implementations
│   ├── infra/             # Infrastructure layer
│   ├── ui/                # Terminal UI components
│   ├── shortcuts/         # Shortcut system
│   ├── web/               # Web terminal interface
│   └── utils/             # Shared utilities
├── docs/                  # Comprehensive documentation
├── examples/              # Example configurations
├── tests/                 # Test files and mocks
└── dist/                  # Build artifacts
```

### Core Architectural Patterns

#### Dependency Injection Container

The application uses a service container pattern (`internal/container/container.go`) for dependency
management. All services are initialized once and injected where needed.

#### Tool System Architecture

Tools are self-contained modules that implement the `domain.Tool` interface:

1. **Tool Interface** (`internal/domain/interfaces.go`): Defines `Execute()`, `Definition()`, `Validate()`, `IsEnabled()`
2. **Tool Registry** (`internal/services/tools/registry.go`): Manages tool registration and lookup
3. **Tool Implementations** (`internal/services/tools/*.go`): Individual tool logic
4. **Approval System** (`internal/services/approval_policy.go`): Handles user approval for sensitive operations

#### Storage Backend Strategy

The conversation storage uses a factory pattern with pluggable backends:

- **JSONL**: Default, file-based, human-readable, zero-config
- **SQLite**: SQL-based, file-based, structured queries
- **PostgreSQL**: Production-grade, concurrent access
- **Redis**: Fast, in-memory, distributed setups
- **Memory**: Testing and ephemeral sessions

## Development Environment Setup

### Prerequisites

- **Go 1.25+**: Required for building and development
- **Flox**: Development environment manager (recommended)
- **Docker**: For container builds and testing
- **Task**: Build automation tool

### Quick Setup

```bash
# Clone the repository
git clone https://github.com/inference-gateway/cli.git
cd cli

# Activate development environment (if using Flox)
flox activate

# Download dependencies
task mod:download

# Install pre-commit hooks
task precommit:install
```

### Using Flox (Recommended)

Flox provides a consistent development environment:

```bash
# Activate the environment
flox activate

# All development commands work within the environment
task build
task test
```

## Key Commands

### Build and Development

```bash
# Build the binary
task build

# Install from source
task install

# Run locally without building
task run CLI_ARGS="chat"
task run CLI_ARGS="agent 'task description'"

# Clean build artifacts
task clean
```

### Testing

```bash
# Run all tests
task test

# Run tests with verbose output
task test:verbose

# Run tests with coverage
task test:coverage

# Test specific package
go test ./internal/services/tools

# Test specific function
go test ./internal/services/tools -run TestBashTool
```

### Code Quality

```bash
# Format Go code
task fmt

# Run linter (requires golangci-lint)
task lint

# Run go vet
task vet

# Run all quality checks (pre-commit hooks)
task precommit:run
```

### Release Builds

```bash
# Build for current platform
task release:build

# Build macOS binary
task release:build:darwin

# Build portable Linux binary (via Docker)
task release:build:linux

# Build container image locally
task container:build

# Push container images to registry
task container:push
```

### Mock Generation

```bash
# Regenerate all mocks (uses counterfeiter)
task mocks:generate

# Clean generated mocks
task mocks:clean
```

## Testing Instructions

### Test Organization

- Unit tests: `*_test.go` files alongside implementation
- Mocks: `tests/mocks/` (generated via counterfeiter)
- Integration tests: Separate test files with `_integration_test.go` suffix

### Running Tests

```bash
# Run all tests
task test

# Run tests with race detector
go test -race ./...

# Run specific test suite
go test ./internal/services/tools -v

# Run tests with coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Test Conventions

1. Use `t.Parallel()` for independent tests
2. Follow table-driven test patterns for multiple test cases
3. Use testify assertions for readability
4. Mock external dependencies using counterfeiter
5. Clean up test resources in `t.Cleanup()` or `defer`

## Project Conventions and Coding Standards

### Commit Message Convention

This project uses **Conventional Commits**:

```text
<type>[optional scope]: <description>

[optional body]
[optional footer]
```

**Types**: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`

**Breaking changes**: Add `!` after type (e.g., `feat!:`) or footer `BREAKING CHANGE:`

### Code Style Guidelines

#### Go Code

- Use `gofmt` formatting (enforced by pre-commit)
- Follow standard Go naming conventions
- Use interfaces for dependencies
- Keep functions focused and small (max 150 lines)
- Maintain cyclomatic complexity under 25

#### Configuration

- Use YAML for configuration files
- Environment variables use `INFER_` prefix
- Configuration follows hierarchical structure

#### Error Handling

- Return errors with context using `fmt.Errorf`
- Use sentinel errors for specific error conditions
- Log errors at appropriate levels
- Handle panics gracefully

### Tool Development Guidelines

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

### Tool Parameter Extraction

Use `ParameterExtractor` for type-safe parameter extraction:

```go
extractor := tools.NewParameterExtractor(args)
filePath, err := extractor.GetString("file_path")
lineNum, err := extractor.GetInt("line_number")
```

## Important Files and Configurations

### Configuration Files

- `.infer/config.yaml`: Project configuration
- `~/.infer/config.yaml`: User-level configuration
- `.env`: Environment variables (not committed)
- `.env.example`: Example environment variables

### Build Configuration

- `Taskfile.yml`: Build automation tasks
- `go.mod`: Go module dependencies
- `Dockerfile`: Container build configuration
- `.github/workflows/`: CI/CD pipelines

### Code Quality Configuration

- `.golangci.yml`: Linter configuration
- `.pre-commit-config.yaml`: Pre-commit hooks
- `.commitlintrc.json`: Commit message validation
- `.markdownlint.json`: Markdown linting rules

### Documentation

- `README.md`: Main project documentation
- `CLAUDE.md`: Development guide for AI assistants
- `CONTRIBUTING.md`: Contribution guidelines
- `docs/`: Comprehensive feature documentation

## Development Workflow

### Standard Workflow

1. **Make changes** following Go best practices
2. **Run quality checks**: `task precommit:run` (runs formatting, linting, validation)
3. **Test thoroughly**: `task test`
4. **Commit with conventional commit message**
5. **Pre-commit hooks** run automatically on commit
6. **Push and create PR**

### Adding New Features

1. Analyze requirements and design solution
2. Create or modify domain interfaces if needed
3. Implement business logic in services
4. Add configuration options if required
5. Write comprehensive tests
6. Update documentation

### Debugging Tips

```bash
# Run with verbose logging
INFER_LOGGING_LEVEL=debug infer chat

# Check configuration
infer config show
```

## Security Considerations

### Tool Safety

- File modification tools require user approval by default
- Bash tool has whitelisted commands only
- Web tools have domain restrictions
- A2A tools validate agent URLs

### Configuration Security

- Never commit `.env` files
- Use environment variables for secrets
- Validate configuration on load
- Use secure defaults

### Runtime Security

- Run with minimal privileges
- Validate all user inputs
- Implement rate limiting
- Use context timeouts

## Performance Guidelines

### Memory Management

- Use pointers for large structs
- Implement proper cleanup in defer
- Monitor goroutine leaks
- Use connection pooling

### Concurrency

- Use context for cancellation
- Implement proper synchronization
- Avoid global state
- Use worker pools for heavy operations

### Storage Optimization

- Use appropriate storage backend for use case
- Implement pagination for large datasets
- Clean up old conversations
- Use indexes for frequent queries

## Troubleshooting

### Common Issues

#### Build Issues

```bash
# Clean and rebuild
task clean
task build

# Update dependencies
task mod:tidy
task mod:download
```

#### Test Failures

```bash
# Run tests with verbose output
task test:verbose

# Check for race conditions
go test -race ./...

# Regenerate mocks if interfaces changed
task mocks:generate
```

#### Configuration Issues

```bash
# Check current configuration
infer config show

# Validate configuration
infer config validate

# Reset to defaults
rm .infer/config.yaml
infer init
```

### Debug Logging

```bash
# Enable debug logging
export INFER_LOGGING_LEVEL=debug
infer chat
```

## Release Process

Releases are automated using semantic-release:

- Commits to `main` branch trigger automatic releases
- Version numbers are determined by commit types:
  - `fix:` → patch version (1.0.1)
  - `feat:` → minor version (1.1.0)
  - `feat!:` or `BREAKING CHANGE:` → major version (2.0.0)
- Binaries are built for macOS (Intel/ARM64) and Linux (AMD64/ARM64)
- GitHub releases are created automatically with changelogs

## Additional Resources

- [Tools Reference](docs/tools-reference.md): Complete tool documentation
- [Configuration Reference](docs/configuration-reference.md): All configuration options
- [Commands Reference](docs/commands-reference.md): CLI command documentation
- [A2A Connections](docs/a2a-connections.md): Agent-to-agent communication
- [MCP Integration](docs/mcp-integration.md): Model Context Protocol setup
- [Web Terminal](docs/web-terminal.md): Browser-based terminal interface

## Notes for AI Agents

### Working with This Project

1. **Always read CLAUDE.md first** for project-specific guidance
2. **Use Task commands** instead of raw Go commands
3. **Follow conventional commits** for any changes
4. **Run tests before committing** changes
5. **Check existing patterns** before implementing new features

### Tool Usage Guidelines

1. **Prefer direct tools** over GUI automation when possible
2. **Use parallel execution** for multiple operations
3. **Respect approval system** for sensitive operations
4. **Clean up resources** after operations
5. **Validate inputs** before processing

### Configuration Management

1. **Use environment variables** for sensitive data
2. **Respect configuration precedence**: Env vars > CLI flags > Config files > Defaults
3. **Test configuration changes** before committing
4. **Document new configuration options**

---

*Last updated: January 18, 2026*
*Project version: See `infer version` or `go.mod`*
