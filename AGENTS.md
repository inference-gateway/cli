
# AGENTS.md

## Project Overview

The Inference Gateway CLI is a command-line interface for interacting with the Inference Gateway. It is built in Go
and provides tools for managing and configuring inference services, monitoring agent status, and facilitating
development workflows. Key features include an interactive chat interface, extensible shortcut system, and support
for AI agents.

## Architecture & Structure

- **`cmd/`**: Contains the implementation of CLI commands, such as `agent`, `chat`, `config`, and `status`.
- **`internal/`**: Houses the core application logic, including:
  - **`app/`**: Application layer components.
  - **`domain/`**: Core domain models, interfaces, and business logic.
  - **`handlers/`**: Command and event handlers for processing user input and system events.
  - **`services/`**: Business logic services that orchestrate operations.
  - **`shortcuts/`**: Implements the extensible shortcut system for custom commands.
  - **`ui/`**: Contains components for the terminal user interface.
- **`config/`**: Manages configuration loading and manipulation for agents and the gateway.
- **`docs/`**: Project documentation, including guides on agent configuration, A2A connections, and conversation management.
- **`examples/`**: Provides example usage of the CLI, including A2A agent setups and basic configurations.

**Architectural Patterns**:

- **Clean Architecture**: Emphasizes separation of concerns with layers like domain, application, and infrastructure.
- **Domain-Driven Design (DDD)**: Core business logic is modeled around the domain.
- **Command Pattern**: Used for structuring CLI commands.
- **Repository Pattern**: Abstracting data access for domain entities.
- **Dependency Injection**: Utilized for managing dependencies, likely via a container.

## Development Environment

- **Setup Instructions**:
  - **Go Version**: Requires Go 1.25.0 or later.
  - **Dependencies**: Install project dependencies using `task setup`.
  - **Pre-commit Hooks**: Install pre-commit hooks with `task precommit:install`.
- **Required Tools**:
  - Go (1.25.0+)
  - `golangci-lint` (for linting)
  - `Task` (taskfile.dev)
  - `pre-commit`
  - Docker (for building container images)

## Development Workflow

- **Build Process**: Typically managed via `Task` tasks defined in `Taskfile.yml` (or similar).
- **Testing**: Unit tests are present (e.g., `*_test.go` files). Running tests is likely done via `task test` or
  `go test ./...`.
- **Code Quality**: `golangci-lint` is used for static analysis and linting. Pre-commit hooks ensure code quality
  before commits.
- **Git Workflow**: Standard Git workflows are expected. Pre-commit hooks likely enforce commit message formats
  and code style.

## Key Commands

- **Setup**: `task setup`
- **Install Pre-commit**: `task precommit:install`
- **Run Tests**: `task test` or `go test ./...`
- **Lint Code**: `task lint` or `golangci-lint run`
- **Build**: `task build` (specific target may vary)
- **Run CLI**: `go run ./cmd/cli` or executable from `bin/` after building.

## Testing Instructions

- **Running Tests**: Use `task test` or `go test ./...` to execute all unit tests.
- **Organization**: Tests are co-located with their respective source files (`*_test.go`).
- **Coverage**: Specific coverage requirements are not detailed here but are typically checked during CI. Use
  `go test -cover ./...` for local coverage checks.

## Deployment & Release

- **Builds**: Dockerfiles are present (e.g., in `examples/a2a/demo-site/`), indicating containerized deployments.
- **CI/CD**: Likely configured via GitHub Actions or similar, triggered by commits to the main branch and tag
  creation for releases.
- **Release Procedures**: Involves building binaries, creating Docker images, and tagging releases. Specific commands
  are likely defined in the `Taskfile.yml`.

## Project Conventions

- **Coding Standards**: Follows Go best practices. `golangci-lint` enforces specific rules.
- **Naming Conventions**: Standard Go naming conventions (CamelCase for exported, snake_case for unexported where appropriate).
- **File Organization**: Structured into `cmd`, `internal`, `config`, `docs`, and `examples` directories, with
  subdirectories reflecting logical components.
- **Commit Messages**: Likely follow a convention enforced by pre-commit hooks (e.g., Conventional Commits).

## Important Files & Configurations

- **`Taskfile.yml`**: Defines automation tasks for building, testing, linting, and setup.
- **`go.mod` / `go.sum`**: Go module dependency management files.
- **`.pre-commit-config.yaml`**: Configuration for pre-commit hooks, specifying linters and formatters.
- **`golangci.yml` (or similar)**: Configuration for `golangci-lint`.
- **`*.go` files**: Source code files. Key entry points are in `cmd/`.
- **`docs/`**: Markdown files containing project documentation.
- **`config/`**: Go files related to application configuration.
- **`internal/domain`**: Core business logic and interfaces.
- **`internal/shortcuts`**: Implementation of the shortcut system.
- **`Dockerfile` (in examples/)**: Example Dockerfiles for building container images.
