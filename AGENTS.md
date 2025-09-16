# AGENTS.md

## Project Overview

The Inference Gateway CLI is a powerful command-line interface for managing and interacting with the Inference Gateway.
It provides tools for configuration, monitoring, and management of inference services. The project is built in Go
and features an interactive chat interface, autonomous agent capabilities, and extensive tool integration for
AI-assisted development.

## Architecture & Structure

**Key Directories:**

- `cmd/`: CLI command implementations
- `internal/`: Core application logic
  - `app/`: Application layer
  - `domain/`: Domain models and interfaces
  - `handlers/`: Command and event handlers
  - `services/`: Business logic services
  - `shortcuts/`: Extensible shortcut system
  - `ui/`: Terminal UI components
- `config/`: Configuration management
- `docs/`: Documentation
- `examples/`: Usage examples

**Architectural Patterns:**

- Clean Architecture with domain-driven design
- Command pattern for CLI operations
- Repository pattern for data access
- Service layer for business logic
- Dependency injection via container

## Development Environment

**Setup Instructions:**

- Go 1.24.5+ required
- Install dependencies: `task setup`
- Install pre-commit hooks: `task precommit:install`

**Required Tools:**

- Go 1.24.5+
- golangci-lint
- Task (taskfile.dev)
- pre-commit
- Docker (for container builds)

**Environment Variables:**

- `GITHUB_TOKEN`: For GitHub API access
- `GOOGLE_SEARCH_API_KEY`: Optional Google search API
- `GOOGLE_SEARCH_ENGINE_ID`: Optional Google search engine ID
- `DUCKDUCKGO_SEARCH_API_KEY`: Optional DuckDuckGo API

## Development Workflow

**Build Commands:**

- `task build`: Build binary with version info
- `task install`: Install to GOPATH/bin
- `task release:build`: Build multi-platform release binaries

**Testing Procedures:**

- `task test`: Run all tests
- `task test:verbose`: Run tests with verbose output
- `task test:coverage`: Run tests with coverage
- `task vet`: Run go vet

**Code Quality Tools:**

- `task fmt`: Format Go code
- `task lint`: Run golangci-lint and markdownlint
- `task check`: Run all quality checks (fmt, vet, test)

**Git Workflow:**

- Main branch development
- Pre-commit hooks for code quality
- Conventional commits recommended
- GitHub Actions for CI/CD

## Key Commands

**Build:** `task build`
**Test:** `task test`
**Lint:** `task lint`
**Run:** `task run -- <args>`
**Format:** `task fmt`
**Clean:** `task clean`

## Testing Instructions

**How to Run Tests:**

- `go test ./...`: Run all tests
- `go test -v ./...`: Verbose output
- `go test -cover ./...`: With coverage

**Test Organization:**

- Tests co-located with source files (`*_test.go`)
- Mock generation using counterfeiter
- Integration tests in separate packages

**Coverage Requirements:**

- No specific coverage threshold enforced
- Tests required for all new features
- Integration tests for critical paths

## Deployment & Release

**Deployment Processes:**

- Multi-platform binary builds
- Docker container images
- GitHub Releases with signed artifacts

**Release Procedures:**

- Automated via GitHub Actions
- Version tagging with semantic versioning
- Cosign signatures for security

**CI/CD Pipeline:**

- GitHub Actions workflows for CI
- Automated testing and linting
- Multi-platform build validation

## Project Conventions

**Coding Standards:**

- Go standard formatting (`gofmt`)
- golangci-lint configuration
- Maximum cyclomatic complexity: 25
- Maximum function length: 150 lines

**Naming Conventions:**

- Go idiomatic naming (camelCase for variables, PascalCase for exports)
- Clear, descriptive names
- Interface names end with "er" (e.g., `ToolService`)

**File Organization:**

- Domain-driven structure
- One type per file (with exceptions for small related types)
- Test files co-located with source

**Commit Message Formats:**

- Conventional commits preferred
- Descriptive commit messages
- Reference issues when applicable

## Important Files & Configurations

**Key Configuration Files:**

- `go.mod`: Go module dependencies
- `Taskfile.yml`: Build and development tasks
- `.golangci.yml`: Linter configuration
- `.pre-commit-config.yaml`: Pre-commit hooks
- `.github/workflows/ci.yml`: CI pipeline

**Critical Source Files:**

- `cmd/root.go`: Main CLI entry point
- `internal/domain/interfaces.go`: Core domain interfaces
- `internal/services/agent.go`: Autonomous agent logic
- `internal/handlers/chat_handler.go`: Chat interface handling

**Security Considerations:**

- Path exclusions: `.infer/`, `.git/`, `*.env`
- Tool execution requires approval by default
- Sandboxed directory access
- Command whitelisting for Bash tool
