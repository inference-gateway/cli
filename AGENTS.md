# AGENTS.md - Inference Gateway CLI

This document provides comprehensive guidance for AI agents working with the Inference Gateway CLI project.
It covers project structure, development workflows, configuration, and best practices.

## Project Overview

**Inference Gateway CLI** is a powerful command-line interface for managing and interacting with the Inference Gateway.
It provides:

- **Interactive Chat**: Chat with AI models using an interactive interface
- **Autonomous Agents**: Execute tasks iteratively until completion
- **Tool Execution**: LLMs can execute whitelisted commands and tools
- **Configuration Management**: Manage gateway settings via YAML config
- **Agent-to-Agent (A2A) Communication**: Delegate tasks to specialized agents

**Main Technologies:**

- **Language**: Go 1.25+
- **Framework**: Cobra CLI framework
- **Configuration**: Viper with YAML support
- **UI**: Charmbracelet Bubble Tea for TUI
- **Storage**: SQLite, PostgreSQL, Redis support
- **Container**: Docker for gateway and agents

## Architecture and Structure

### Project Layout

```text
.
├── cmd/                    # CLI command implementations
│   ├── root.go            # Main CLI entry point
│   ├── chat.go            # Interactive chat command
│   ├── agent.go           # Autonomous agent command
│   ├── config.go          # Configuration management
│   └── agents.go          # A2A agent management
├── config/                # Configuration structures
│   ├── config.go          # Main configuration types
│   └── agent_defaults.go  # Default agent configurations
├── internal/              # Internal application logic
│   ├── app/               # Application layer
│   ├── domain/            # Domain models and interfaces
│   ├── handlers/          # Command and event handlers
│   ├── services/          # Business logic services
│   ├── infra/             # Infrastructure adapters
│   └── formatting/        # Text formatting utilities
├── docs/                  # Documentation
│   ├── agents-configuration.md
│   ├── a2a-connections.md
│   ├── conversation-storage.md
│   └── conversation-title-generation.md
├── examples/              # Example configurations
│   ├── basic/             # Basic setup examples
│   └── a2a/               # A2A agent examples
├── Taskfile.yml           # Build and development tasks
├── go.mod                 # Go module dependencies
└── main.go               # Application entry point
```

### Key Components

1. **CLI Commands**: Implemented using Cobra framework
2. **Configuration System**: 2-layer system (project + userspace) with Viper
3. **Tool System**: Secure execution of whitelisted commands
4. **A2A Integration**: Agent-to-agent communication protocol
5. **Storage Backends**: Multiple conversation storage options
6. **UI Components**: Interactive chat interface with Bubble Tea

## Development Environment Setup

### Prerequisites

- **Go 1.25+**: Required for building from source
- **Docker**: Optional for containerized gateway and agents
- **Task**: Build tool (optional, Taskfile.yml provided)

### Quick Setup

```bash
# Clone the repository
git clone https://github.com/inference-gateway/cli.git
cd cli

# Install dependencies
go mod download

# Install development tools
task setup

# Build the binary
task build
```

### Development Dependencies

Install required tools:

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Install pre-commit hooks
task precommit:install
```

## Key Commands

### Build Commands

```bash
# Build the CLI binary
task build

# Install from source
task install

# Run locally without building
task run

# Run with specific command
task run -- --help
```

### Development Commands

```bash
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

# Run all quality checks
task check

# Development workflow (fmt, build, test)
task dev
```

### Dependency Management

```bash
# Tidy go modules
task mod:tidy

# Download dependencies
task mod:download
```

### Mock Generation

```bash
# Generate mocks for testing
task mocks:generate

# Clean generated mocks
task mocks:clean
```

## Testing Instructions

### Test Structure

Tests are organized alongside the code they test:

- Unit tests in `*_test.go` files
- Integration tests in `cmd/` and `internal/` packages
- Mock generation using counterfeiter

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test ./cmd
```

### Test Best Practices

1. **Use Table-Driven Tests**: For comprehensive test coverage
2. **Mock External Dependencies**: Use generated mocks for services
3. **Test Edge Cases**: Include boundary conditions and error scenarios
4. **Parallel Execution**: Use `t.Parallel()` where appropriate
5. **Clean Test Data**: Ensure tests don't leave artifacts

## Project Conventions and Coding Standards

### Code Style

- **Go Format**: Use `gofmt` or `task fmt` for consistent formatting
- **Linting**: Follow `golangci-lint` rules
- **Naming**: Use descriptive names following Go conventions
- **Error Handling**: Return errors, don't panic

### File Organization

- **Package Structure**: Group related functionality in packages
- **Interface Segregation**: Define focused interfaces
- **Dependency Injection**: Use interfaces for testability

### Configuration Management

- **2-Layer System**: Project config overrides userspace config
- **Environment Variables**: Use `INFER_` prefix for overrides
- **Secure Storage**: Never commit sensitive data to config files

### Commit Conventions

```bash
# Conventional commit format
type: Brief description

# Examples:
feat: Add new tool for file operations
fix: Resolve memory leak in chat handler
docs: Update AGENTS.md with development workflow
refactor: Simplify configuration loading
test: Add unit tests for agent service
```

## Important Files and Configurations

### Configuration Files

#### Main Configuration (.infer/config.yaml)

```yaml
gateway:
  url: http://localhost:8080
  run: true
  docker: true
  oci: ghcr.io/inference-gateway/inference-gateway:latest

tools:
  enabled: true
  sandbox:
    directories: [".", "/tmp"]
    protected_paths: [".infer/", ".git/", "*.env"]
  bash:
    enabled: true
    whitelist:
      commands: ["ls", "pwd", "git", "task"]
      patterns: ["^git status$", "^git branch"]

agent:
  model: ""
  system_prompt: |
    Autonomous software engineering agent. Execute tasks iteratively until completion.

    IMPORTANT: You NEVER push to main or master or to the current branch...
  max_concurrent_tools: 5

a2a:
  enabled: true
  agents: []
```

#### A2A Agents Configuration (.infer/agents.yaml)

```yaml
agents:
  - name: security-agent
    url: http://localhost:8081
    oci: ghcr.io/org/security-agent:latest
    run: true
    model: deepseek/deepseek-chat
    environment:
      API_KEY: "%SECURITY_API_KEY%"
```

### Environment Variables

Key environment variables for configuration:

```bash
# Gateway Configuration
INFER_GATEWAY_URL=http://localhost:8080
INFER_GATEWAY_RUN=true
INFER_GATEWAY_DOCKER=true

# Agent Configuration
INFER_AGENT_MODEL=deepseek/deepseek-chat
INFER_AGENT_MAX_CONCURRENT_TOOLS=5

# Tool Configuration
INFER_TOOLS_ENABLED=true
INFER_TOOLS_BASH_ENABLED=true

# A2A Configuration
INFER_A2A_ENABLED=true
INFER_A2A_AGENTS="http://agent1:8080,http://agent2:8080"
```

### Build Configuration (Taskfile.yml)

Key build tasks:

- `task build`: Build binary with version info
- `task test`: Run all tests
- `task lint`: Run linter and markdown lint
- `task dev`: Development workflow (fmt, build, test)
- `task release:build`: Build release binaries

## Tool Execution System

### Available Tools

AI agents can use these tools when tool execution is enabled:

- **Bash**: Execute whitelisted commands
- **Read**: Read file contents
- **Write**: Write files (requires approval)
- **Edit**: Modify files (requires approval)
- **Delete**: Delete files (requires approval)
- **Grep**: Search code with regex
- **Tree**: Display directory structure
- **WebSearch**: Search the web
- **WebFetch**: Fetch web content
- **Github**: Interact with GitHub API
- **TodoWrite**: Manage task lists
- **A2A Tools**: Agent-to-agent communication

### Security Controls

- **Sandbox Directories**: Restricted to `.` and `/tmp` by default
- **Protected Paths**: `.infer/`, `.git/`, `*.env` are excluded
- **Approval System**: Dangerous operations require user approval
- **Command Whitelist**: Only pre-approved commands can execute

## A2A (Agent-to-Agent) Integration

### Core A2A Tools

- **A2A_SubmitTask**: Delegate tasks to specialized agents
- **A2A_QueryAgent**: Discover agent capabilities
- **A2A_QueryTask**: Monitor task progress

### Agent Configuration

```bash
# Add A2A agents
infer agents add security http://security-agent:8080
infer agents add docs http://docs-agent:8080

# List configured agents
infer agents list

# Show agent details
infer agents show security
```

## Development Workflow

### Typical Development Process

1. **Plan**: Use Plan Mode to analyze tasks without execution
2. **Implement**: Use Standard Mode with approval for changes
3. **Test**: Run tests with `task test`
4. **Lint**: Check code quality with `task lint`
5. **Format**: Ensure consistent formatting with `task fmt`
6. **Commit**: Use conventional commit messages

### Agent Mode Guidelines

- **Standard Mode**: Normal operation with approval checks
- **Plan Mode**: Read-only analysis and planning
- **Auto-Accept Mode**: All tools auto-approved (use with caution)

### Code Review Checklist

- [ ] Tests pass: `task test`
- [ ] Code formatted: `task fmt`
- [ ] Linting passes: `task lint`
- [ ] No sensitive data in commits
- [ ] Conventional commit message
- [ ] Documentation updated if needed

## Troubleshooting

### Common Issues

1. **Configuration Not Loading**: Check file permissions and YAML syntax
2. **Gateway Connection Issues**: Verify gateway is running and accessible
3. **Tool Execution Failing**: Check command whitelist and sandbox settings
4. **A2A Agent Connection**: Verify agent URLs and network connectivity

### Debug Mode

Enable debug logging for troubleshooting:

```bash
# Set debug environment variable
export INFER_LOGGING_DEBUG=true

# Or use verbose flag
infer --verbose chat
```

## Best Practices for AI Agents

### When Working with This Project

1. **Always Use TodoWrite**: Create structured task lists for complex work
2. **Prefer Grep Over Read**: Search for specific patterns rather than reading entire files
3. **Batch Tool Calls**: Use parallel execution for efficiency
4. **Follow Security Rules**: Never access protected paths
5. **Use Conventional Commits**: Follow the project's commit message format
6. **Test Your Changes**: Always run tests before considering work complete

### File Modification Guidelines

1. **Read First**: Always read files before editing
2. **Use MultiEdit**: For multiple changes to the same file
3. **Preview Changes**: Review diffs before approval
4. **Follow Patterns**: Match existing code style and patterns
5. **Update Tests**: Include test updates with feature changes

### Configuration Best Practices

1. **Use Environment Variables**: For sensitive data and CI/CD
2. **Respect Precedence**: Understand config layer hierarchy
3. **Validate Changes**: Test configuration changes thoroughly
4. **Document Changes**: Update documentation when adding new features

---

This documentation is maintained by AI agents working on the project.
Please update it when making significant changes to the project structure or development workflow.
