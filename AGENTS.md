# AGENTS.md

## Project Overview

The Inference Gateway CLI is a powerful command-line interface for managing and interacting with the
Inference Gateway. It provides tools for configuration, monitoring, and management of inference
services. It is developed in Go and is in an early development stage, with breaking changes expected
until a stable version is reached.

## Architecture & Structure

The project primarily uses Go and follows a typical Go project structure.

- **cmd**: Contains the main application entry point and CLI commands.
- **config**: Likely holds configuration related code and defaults.
- **docs**: Project documentation.
- **examples**: Example usage of the CLI.
- **internal**: Internal packages and modules, not intended for external use. This is where most of
  the core logic and domain interfaces reside.
- **tests**: Contains project tests and generated mocks.
- **.infer**: This directory is created by the `infer init` command and holds project-level
  configuration files (e.g., `config.yaml`) and custom shortcuts (`shortcuts/custom-*.yaml`).

## Development Environment

- **Setup instructions and dependencies**:
  - Go version 1.24.5 or higher.
  - `golangci-lint` for linting.
  - `pre-commit` for Git hooks.
  - `counterfeiter/v6` for mock generation.
- **Required tools and versions**: Go (1.24.5+), `golangci-lint`, `pre-commit`, `counterfeiter/v6`.
- **Environment variables and configuration**:
  - Configuration is managed through a 2-layer system: userspace (`~/.infer/config.yaml`) and
    project-level (`.infer/config.yaml`). Project-level configuration takes precedence.
  - `GOOGLE_SEARCH_API_KEY` and `GOOGLE_SEARCH_ENGINE_ID` for Google Custom Search.
  - `DUCKDUCKGO_SEARCH_API_KEY` for DuckDuckGo (optional).
  - `GITHUB_TOKEN` for GitHub API interactions.

## Development Workflow

- **Build commands and processes**:
  - `task build`: Builds the CLI binary with version, commit, and date information embedded.
  - `go build -o infer .`: Basic Go build.
  - `task install`: Installs the CLI binary to `$(go env GOPATH)/bin/infer`.
  - `task release:build`: Builds release binaries for multiple platforms (darwin-amd64,
    darwin-arm64, linux-amd64, linux-arm64) and generates checksums and Cosign signatures.
- **Testing procedures and commands**:
  - `task test`: Runs all Go tests in the project (`go test ./...`).
  - `task test:verbose`: Runs tests with verbose output (`go test -v ./...`).
  - `task test:coverage`: Runs tests with coverage report (`go test -cover ./...`).
- **Code quality tools (linting, formatting)**:
  - `task lint`: Runs `golangci-lint` and `markdownlint`.
  - `task fmt`: Formats Go code using `go fmt ./...`.
  - `task vet`: Runs `go vet ./...`.
  - `task precommit:run`: Runs pre-commit hooks on all files.
- **Git workflow and branching strategy**: Not explicitly defined, but pre-commit hooks and a
  release workflow are present. The agent system prompt indicates a preference for branching and
  PRs: "You NEVER push to main or master or to the current branch - instead you create a branch
  and push to a branch."

## Key Commands

- Build: `task build` or `go build -o infer .`
- Test: `task test` or `go test ./...`
- Lint: `task lint`
- Format: `task fmt`
- Run: `task run` or `go run .`
- Setup Dev Environment: `task setup`
- Generate Mocks: `task mocks:generate`
- Initialize Project: `infer init`
- Check Status: `infer status`
- Start Chat: `infer chat`
- Run Agent: `infer agent "your task description"`

## Testing Instructions

- **How to run tests**:
  - To run all tests: `go test ./...` or `task test`
  - To run tests with verbose output: `go test -v ./...` or `task test:verbose`
  - To run tests with coverage: `go test -cover ./...` or `task test:coverage`
- **Test organization and patterns**: Tests are located in the `tests/` directory. Mock interfaces
  are generated using `counterfeiter` into `tests/mocks/generated/`.
- **Coverage requirements**: Not explicitly stated, but coverage can be generated using
  `task test:coverage`.

## Deployment & Release

- **Deployment processes**: Binaries are released on GitHub. Installation can be done via
  `go install`, an `install.sh` script, or manual download.
- **Release procedures**:
  - `task release:build`: Creates multi-platform binaries, SHA256 checksums, and Cosign signatures.
  - Release binaries are signed with Cosign for supply chain security.
- **CI/CD pipeline information**: The `README.md` mentions a GitHub Actions workflow `ci.yml` for
  build status, and `release.yml` for Cosign verification during releases.

## Project Conventions

- **Coding standards and style guides**: Go standards, enforced by `go fmt` and `golangci-lint`.
- **Naming conventions**: Not explicitly defined beyond Go's standard practices.
- **File organization patterns**: Standard Go project layout with `cmd`, `internal`, `config`,
  `docs`, `tests` directories. Configuration files are in `.infer/config.yaml`. Custom shortcuts
  are in `.infer/shortcuts/custom-*.yaml`.
- **Commit message formats**: The agent's system prompt specifies "use conventional commits".

## Important Files & Configurations

- `go.mod`: Go module definition and dependencies.
- `go.sum`: Go module checksums.
- `Taskfile.yml`: Defines various development tasks using Task.
- `README.md`: Project overview, installation, commands, available tools, configuration, and development information.
- `.infer/config.yaml`: Project-level configuration.
- `~/.infer/config.yaml`: User-level (global) configuration.
- `.infer/shortcuts/custom-*.yaml`: User-defined CLI shortcuts.
- `.golangci.yml`: Configuration for `golangci-lint`.
- `.pre-commit-config.yaml`: Configuration for pre-commit hooks.
- `.env.example`: Example environment variables.
- `install.sh`: Script for installing the CLI.
- `LICENSE`: Project license (MIT).
- `.github/workflows/ci.yml`: GitHub Actions workflow for continuous integration.
- `.github/workflows/release.yml`: GitHub Actions workflow for releases and Cosign signing.
- `.gitignore`: Specifies files and directories to be ignored by Git.
- `.editorconfig`: Defines coding styles for various editors.
- `.commitlintrc.json`: Configuration for commit message linting.
- `.releaserc.json`: Configuration for semantic release.
