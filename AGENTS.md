# AGENTS.md

## Project Overview

The Inference Gateway CLI is a powerful command-line interface for managing and interacting with the Inference
Gateway. It provides tools for configuration, monitoring, and management of inference services. Built with Go 1.24+,
it features an interactive chat interface, conversation history management, and extensive tool execution capabilities
for LLMs.

## Architecture & Structure

```text
/go/src/
├── cmd/                 # CLI command implementations
│   ├── agent.go         # Agent command handler
│   ├── chat.go          # Interactive chat interface
│   ├── config.go        # Configuration management
│   └── root.go          # Root command and CLI setup
├── config/              # Configuration handling
├── docs/                # Documentation
├── examples/            # Usage examples and templates
├── internal/            # Internal application logic
│   ├── app/             # Application core
│   ├── domain/          # Domain models and interfaces
│   ├── handlers/        # Command and event handlers
│   ├── infra/           # Infrastructure adapters
│   ├── logger/          # Logging utilities
│   ├── services/        # Business logic services
│   ├── shortcuts/       # Shortcuts system
│   └── ui/              # User interface components
└── .github/             # GitHub workflows and templates
```

Key architectural patterns:

- Clean Architecture with domain-driven design
- Command pattern for CLI operations
- Event-driven architecture for chat interactions
- Dependency injection via container system

## Development Environment

### Setup Instructions

- **Go Version**: 1.24.5 or later
- **Dependencies**: Managed via Go modules (go.mod)
- **Required Tools**: Go toolchain, Git

### Environment Variables

- `INFER_CONFIG_PATH`: Custom config file path
- `INFER_LOG_LEVEL`: Logging level (debug, info, warn, error)
- `INFER_NO_COLOR`: Disable colored output
- `INFER_VERBOSE`: Enable verbose logging

### Configuration Files

- `.infer.yaml`: Project-level configuration
- `~/.infer/config.yaml`: User-level configuration
- `.env` files for environment-specific settings

## Development Workflow

### Build Commands

```bash
# Build the binary
go build -o infer ./cmd

# Build with specific version
go build -ldflags "-X main.version=1.0.0" -o infer ./cmd

# Install globally
go install ./cmd
```

### Testing Procedures

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific test package
go test ./internal/services/...
```

### Code Quality Tools

- **Formatting**: `go fmt ./...`
- **Vet**: `go vet ./...`
- **Static Analysis**: Built-in Go tooling
- **Testing**: Native Go testing framework

### Git Workflow

- Main branch: `main`
- Feature branches: `feature/*`
- Release branches: `release/*`
- Conventional commits recommended

## Key Commands

- **Build**: `go build -o infer ./cmd`
- **Test**: `go test ./...`
- **Run**: `./infer [command]`
- **Install**: `go install ./cmd`
- **Format**: `go fmt ./...`

## Testing Instructions

### Test Organization

- Unit tests in `*_test.go` files alongside source
- Integration tests in dedicated test files
- Test coverage requirements: 80%+ for critical components

### Running Tests

```bash
# Run all tests with verbose output
go test -v ./...

# Run tests with race detection
go test -race ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Deployment & Release

### CI/CD Pipeline

- GitHub Actions workflows in `.github/workflows/`
- Automated testing on push and PR
- Release automation via GitHub Releases
- Docker image builds for container deployment

### Release Procedures

1. Version bump in code
2. Automated testing via CI
3. Binary builds for multiple platforms
4. GitHub Release creation
5. Docker image publishing

## Project Conventions

### Coding Standards

- Go standard formatting (`go fmt`)
- Error handling: explicit error returns
- Documentation: Godoc comments for exported symbols
- Naming: camelCase for variables, PascalCase for exports

### File Organization

- One package per directory
- `internal/` for application-specific code
- `cmd/` for CLI entry points
- `pkg/` for reusable packages (if any)

### Commit Message Format

```text
feat: add new feature
fix: repair bug
docs: update documentation
chore: maintenance tasks
test: add or update tests
```

## Important Files & Configurations

### Key Configuration Files

- `go.mod` - Go module dependencies
- `.github/workflows/ci.yml` - CI/CD pipeline
- `cmd/root.go` - CLI root command setup
- `internal/container/container.go` - Dependency injection

### Critical Source Files

- `internal/services/chat.go` - Chat service implementation
- `internal/handlers/chat_handler.go` - Chat event handling
- `internal/shortcuts/registry.go` - Shortcuts system
- `internal/tools/` - LLM tool implementations

### Documentation Files

- `README.md` - Comprehensive project documentation
- `docs/` - Detailed feature documentation
- `examples/` - Usage examples and templates

## Security Considerations

- Tool execution with whitelist validation
- Protected path restrictions
- Environment variable sanitization
- Configuration file security
