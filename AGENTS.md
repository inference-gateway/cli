# AGENTS.md

## Project Overview

The Inference Gateway CLI is a powerful command-line interface for managing and interacting with the Inference Gateway.
Built in Go, it provides tools for configuration, monitoring, and management of inference services with AI agent capabilities.
Key features include interactive chat, autonomous agent execution, extensive tool integration, and A2A (Agent-to-Agent) communication.

**Main Technologies:**

- **Language**: Go 1.25.2+
- **CLI Framework**: Cobra
- **Configuration**: Viper with YAML
- **UI**: Charmbracelet BubbleTea for TUI
- **Testing**: Go testing framework with testify
- **Linting**: golangci-lint
- **Build System**: Taskfile

## Architecture & Structure

### Directory Structure

```text
├── cmd/                # CLI command implementations
│   ├── agent.go        # Autonomous agent execution
│   ├── chat.go         # Interactive chat interface
│   ├── config.go       # Configuration management
│   ├── init.go         # Project initialization
│   └── root.go         # Root command and config setup
├── config/             # Configuration management
├── internal/           # Core application logic
│   ├── app/            # Application layer components
│   ├── domain/         # Domain models and interfaces
│   ├── handlers/       # Command and event handlers
│   ├── services/       # Business logic services
│   └── ui/             # Terminal user interface components
├── docs/               # Project documentation
├── examples/           # Usage examples and configurations
└── tests/              # Test files and mocks
```

### Architectural Patterns

- **Clean Architecture**: Separation of concerns with domain, application, and infrastructure layers
- **Domain-Driven Design**: Core business logic modeled around domain concepts
- **Command Pattern**: Structured CLI commands using Cobra
- **Repository Pattern**: Abstracted data access for domain entities
- **Dependency Injection**: Container-based dependency management

## Development Environment

### Setup Instructions

1. **Go Version**: Requires Go 1.25.2 or later

2. **Install Dependencies**:

   ```bash
   task setup
   ```

3. **Install Pre-commit Hooks**:

   ```bash
   task precommit:install
   ```

### Required Tools

- **Go**: 1.25.2+
- **Task**: Taskfile runner (taskfile.dev)
- **golangci-lint**: For code linting and static analysis
- **pre-commit**: For git hooks
- **Docker**: For container builds and testing
- **Flox**: For development environment management (optional)

### Environment Variables

Key environment variables for development:

- `GITHUB_TOKEN`: For GitHub API access
- `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GOOGLE_API_KEY`: For model providers
- `INFER_*`: CLI configuration overrides

## Development Workflow

### Build Process

The project uses Taskfile for build automation:

```bash
# Build the binary
task build

# Install from source
task install

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

# Run specific test package
go test ./internal/services/...
```

### Code Quality

```bash
# Format Go code
task fmt

# Run linter
task lint

# Run go vet
task vet

# Run all quality checks
task check

# Development workflow (format, build, test)
task dev
```

### Git Workflow

- **Branching**: Feature branches from main
- **Commits**: Conventional Commits format
- **Pre-commit Hooks**: Auto-format and lint on commit
- **CI/CD**: GitHub Actions for testing and releases

## Key Commands

### Development Commands

- **Setup**: `task setup` - Install all dependencies
- **Build**: `task build` - Build the binary
- **Test**: `task test` - Run all tests
- **Lint**: `task lint` - Run golangci-lint
- **Format**: `task fmt` - Format Go code
- **Clean**: `task clean` - Remove build artifacts
- **Dev**: `task dev` - Complete development workflow

### Module Management

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

### Release Process

```bash
# Build release binaries for multiple platforms
task release:build

# Push container images to registry
task container:push
```

## Testing Instructions

### Test Organization

- **Location**: Tests co-located with source files (`*_test.go`)
- **Framework**: Standard Go testing with testify assertions
- **Mocking**: Counterfeiter for interface mocks
- **Coverage**: Built-in Go coverage tools

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/services

# Run tests with verbose output
go test -v ./...
```

### Test Patterns

- Use table-driven tests for multiple scenarios
- Mock external dependencies using counterfeiter
- Test both success and error cases
- Follow Go testing best practices

## Deployment & Release

### CI/CD Pipeline

- **GitHub Actions**: Automated testing and releases
- **Main Branch**: Protected with required checks
- **Release Process**: Semantic versioning with automated changelog
- **Container Images**: Multi-architecture builds for Linux

### Release Steps

1. **Version Bump**: Automated via semantic-release
2. **Build**: Multi-platform binaries
3. **Signing**: Cosign for supply chain security
4. **Container**: Docker images pushed to GHCR
5. **Documentation**: Changelog and release notes

### Container Deployment

```bash
# Build and push container images
task container:push

# Run locally
docker run --rm -it ghcr.io/inference-gateway/cli:latest
```

## Project Conventions

### Coding Standards

- **Go Standards**: Follow standard Go conventions and idioms
- **Linting**: Enforced by golangci-lint with custom configuration
- **Formatting**: `go fmt` for consistent code style
- **Imports**: Group standard library, third-party, and local imports

### Naming Conventions

- **Packages**: Lowercase, single-word names
- **Files**: snake_case for multi-word names
- **Variables**: camelCase for local variables
- **Constants**: camelCase or UPPER_CASE depending on scope
- **Interfaces**: Use descriptive names ending with "er" when appropriate

### File Organization

- **Domain Logic**: `internal/domain/` for core business logic
- **Services**: `internal/services/` for business logic services
- **Handlers**: `internal/handlers/` for command and event handling
- **UI Components**: `internal/ui/` for terminal interface
- **Configuration**: `config/` for configuration management

### Commit Message Format

Follow Conventional Commits specification:

```text
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

**Types**: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert

## Important Files & Configurations

### Core Configuration Files

- **`Taskfile.yml`**: Build automation and development tasks
- **`go.mod` / `go.sum`**: Go module dependencies
- **`.golangci.yml`**: Linting configuration
- **`.pre-commit-config.yaml`**: Git hooks configuration
- **`.github/workflows/`**: CI/CD pipeline definitions

### Project Structure Files

- **`cmd/`**: CLI command implementations
- **`config/`**: Configuration management
- **`internal/`**: Core application logic
- **`docs/`**: Project documentation
- **`examples/`**: Usage examples

### Build and Release Files

- **`Dockerfile`**: Container image definition
- **`.releaserc.yaml`**: Release configuration
- **`install.sh`**: Installation script

### Documentation Files

- **`README.md`**: Main project documentation
- **`AGENTS.md`**: AI agent documentation (this file)
- **`CONFIG.md`**: Configuration guide
- **`CONTRIBUTING.md`**: Contribution guidelines
- **`CHANGELOG.md`**: Release history
