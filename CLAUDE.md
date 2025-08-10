# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the Inference Gateway CLI (`infer`), a Go-based command-line tool for managing and interacting with machine learning inference services. The CLI provides functionality for model deployment, status monitoring, prompt testing, and configuration management.

## Development Commands

**Note: All commands should be run with `flox activate -- <command>` to ensure the proper development environment is activated.**

### Setup Development Environment
```bash
flox activate -- task setup
```

### Building
```bash
flox activate -- task build
```

### Testing
```bash
# Run all tests
flox activate -- task test

# Run tests with verbose output
flox activate -- task test:verbose

# Run tests with coverage report
flox activate -- task test:coverage
```

### Running locally
```bash
# Run the CLI with arguments
flox activate -- task run CLI_ARGS="[command]"

# Run specific commands
flox activate -- task run:status
flox activate -- task run:version
flox activate -- task run:help
```

### Installing from source
```bash
flox activate -- task install
```

### Code Quality
```bash
# Format code
flox activate -- task fmt

# Run linter
flox activate -- task lint

# Run go vet
flox activate -- task vet

# Run all quality checks
flox activate -- task check
```

### Module Management
```bash
# Tidy modules
flox activate -- task mod:tidy

# Download modules
flox activate -- task mod:download
```

### Development Workflow
```bash
# Complete development workflow (format, build, test)
flox activate -- task dev
```

### Release
```bash
# Build release binaries for multiple platforms
flox activate -- task release:build

# Clean release artifacts
flox activate -- task clean:release
```

### Cleanup
```bash
flox activate -- task clean
```

### Available Tasks
```bash
# Show all available tasks
flox activate -- task --list
```

## Architecture

The project follows the standard Go CLI architecture using Cobra framework:

- `main.go`: Entry point that calls `cmd.Execute()`
- `cmd/`: Contains all CLI command implementations using Cobra
  - `root.go`: Root command setup with global flags (`--config`, `--verbose`)
  - `deploy.go`: Model deployment commands (`infer deploy model`)
  - `status.go`: Status monitoring (`infer status`, `infer list`)
  - `prompt.go`: Prompt testing (`infer prompt`)
  - `version.go`: Version information (`infer version`)
- `internal/config.go`: Configuration management with YAML support

### Configuration System
The CLI uses a YAML configuration file at `~/.infer.yaml` with the following structure:
```yaml
gateway:
  url: "http://localhost:8080"
  api_key: ""
  timeout: 30
output:
  format: "text"  # text, json, yaml
  quiet: false
tools:
  enabled: false  # Set to true to enable tool execution for LLMs
  whitelist:
    commands:  # Exact command matches
      - "ls"
      - "pwd"
      - "echo"
      - "cat"
      - "head"
      - "tail"
      - "grep"
      - "find"
      - "wc"
      - "sort"
      - "uniq"
    patterns:  # Regex patterns for more complex commands
      - "^git status$"
      - "^git log --oneline -n [0-9]+$"
      - "^docker ps$"
      - "^kubectl get pods$"
compact:
  output_dir: ".infer"  # Directory for compact command exports (default: project root/.infer)
```

### Command Structure
- Root command: `infer`
- Global flags: `--config`, `--verbose`
- Subcommands:
  - `status [--detailed] [--format]`: Gateway status
  - `deploy model <name> [--endpoint] [--replicas] [--config]`: Model deployment
  - `list`: List deployed models
  - `prompt <text>`: Send prompts to models
  - `chat`: Interactive chat with model selection and tool support
    - `/compact`: Export conversation to markdown file
  - `tools`: Tool management and execution
  - `version`: Version information

## Dependencies

- **Cobra** (`github.com/spf13/cobra`): CLI framework for command structure
- **YAML v3** (`gopkg.in/yaml.v3`): Configuration file parsing
- Go 1.24+ required

## Implementation Notes

- All commands currently contain placeholder implementations with mock outputs
- The actual inference gateway communication logic is not yet implemented (marked as TODO)
- Configuration loading handles missing config files gracefully by returning defaults
- The project uses standard Go project structure with `internal/` for private packages

## Code Style Guidelines

- **No Redundant Comments**: The codebase has been cleaned of redundant inline comments. Avoid adding comments that simply restate what the code does or explain obvious operations.
- **Comment Policy**: Only add comments for:
  - Complex business logic that isn't immediately clear
  - External API interactions or protocol specifications
  - Workarounds for specific issues
  - Public package-level documentation
- **Removed Comment Types**:
  - Obvious explanatory comments (e.g., "// Get flags")
  - TODO comments for unimplemented features
  - Comments describing the next line of code
  - Function descriptions that don't add value beyond the function signature

## Commit Message Guidelines

Follow conventional commit format with proper capitalization:

- **Format**: `type: Capitalize the first letter of the description`
- **Word Choice**: Never use the word "enhance" - use "improve", "update", "refine", or similar alternatives instead
- **Examples**:
  - `feat: Add new chat command with interactive mode`
  - `fix: Resolve configuration loading error on Windows`
  - `docs: Improve README with installation instructions`
  - `chore: Update dependencies to latest versions`
- **Types**: feat, fix, docs, style, refactor, test, chore, perf, ci, build, revert
- **Scope**: Optional, use when changes affect specific components (e.g., `feat(cli): Add new command`)
- **Body**: Use bullet points for multiple changes, maintain proper capitalization
- **Breaking Changes**: Use `BREAKING CHANGE:` footer when introducing breaking changes
