# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the Inference Gateway CLI (`infer`), a Go-based command-line tool for managing and interacting with machine learning inference services. The CLI provides functionality for status monitoring, interactive chat, and configuration management.

## Development Commands

**Note: All commands should be run with `flox activate -- <command>` to ensure the proper development environment is activated.**

**IMPORTANT: Always run `task setup` first when working with a fresh checkout of the repository to ensure all dependencies are properly installed.**

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

The project follows a modern SOLID architecture using Bubble Tea for the TUI and dependency injection:

- `main.go`: Entry point that calls `cmd.Execute()`
- `cmd/`: Contains all CLI command implementations using Cobra
  - `root.go`: Root command setup with global flags (`--config`, `--verbose`)
  - `config.go`: Configuration management (`infer config`)
  - `status.go`: Status monitoring (`infer status`)
  - `chat.go`: Interactive chat (`infer chat`)
  - `version.go`: Version information (`infer version`)
- `config/config.go`: Configuration management with YAML support
- `internal/`: Internal application architecture
  - `app/`: Application layer with Bubble Tea models
  - `handlers/`: Request handlers and routing
  - `services/`: Business logic services
  - `ui/`: UI components and interfaces
  - `domain/`: Domain models and interfaces
  - `container/`: Dependency injection container

### Configuration System
The CLI uses a project-based YAML configuration file at `.infer/config.yaml` in the current directory with the following structure:
```yaml
gateway:
  url: "http://localhost:8080"
  api_key: ""
  timeout: 30
output:
  format: "text"  # text, json, yaml
  quiet: false
tools:
  enabled: true  # Tools are enabled by default with safe read-only commands
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
  safety:
    require_approval: true  # Prompt user before executing any command
  exclude_paths:  # Paths excluded from tool access for security
    - ".infer/"     # Protect infer's own configuration directory
    - ".infer/*"    # Protect all files in infer's configuration directory
compact:
  output_dir: ".infer"  # Directory for compact command exports (default: project root/.infer)
chat:
  default_model: ""  # Default model for chat sessions (when set, skips model selection)
  system_prompt: ""  # System prompt included with every chat session
web_search:
  enabled: true  # Enable web search tool for LLMs
  default_engine: "google"  # Default search engine (google, duckduckgo)
  max_results: 10  # Default maximum number of search results
  engines:  # Available search engines
    - "google"
    - "duckduckgo"
  timeout: 10  # Search timeout in seconds
```

### Command Structure
- Root command: `infer`
- Global flags: `--config`, `--verbose`
- Subcommands:
  - `status`: Gateway status
  - `chat`: Interactive chat with model selection (or uses default model if configured)
  - `config`: Manage CLI configuration
    - `init [--overwrite]`: Initialize local project configuration
    - `set-model [MODEL_NAME]`: Set default model for chat sessions
    - `set-system [SYSTEM_PROMPT]`: Set system prompt for chat sessions
    - `tools`: Manage tool execution settings
      - `enable`: Enable tool execution for LLMs
      - `disable`: Disable tool execution for LLMs
      - `list [--format text|json]`: List whitelisted commands and patterns
      - `validate <command>`: Validate if a command is whitelisted
      - `exec <command> [--format text|json]`: Execute a whitelisted command directly
      - `safety`: Manage safety approval settings
        - `enable`: Enable safety approval prompts
        - `disable`: Disable safety approval prompts
        - `status`: Show current safety approval status
      - `exclude-path`: Manage excluded paths for security
        - `list`: List all excluded paths
        - `add <path>`: Add a path to the exclusion list
        - `remove <path>`: Remove a path from the exclusion list
  - `version`: Version information

## Dependencies

- **Cobra** (`github.com/spf13/cobra`): CLI framework for command structure
- **Bubble Tea** (`github.com/charmbracelet/bubbletea`): TUI framework for interactive chat
- **YAML v3** (`gopkg.in/yaml.v3`): Configuration file parsing
- Go 1.24+ required

## Implementation Notes

- The chat command uses Bubble Tea for interactive TUI experience
- Architecture follows SOLID principles with dependency injection
- Configuration loading handles missing config files gracefully by returning defaults
- The project uses modern Go project structure with `internal/` for private packages
- Default model configuration allows skipping model selection in chat sessions when a preferred model is set

## Usage Examples

### Setting a Default Model
```bash
# Set a default model for chat sessions
infer config set-model gpt-4-turbo

# Now chat will automatically use this model without showing selection
infer chat
```

### Setting a System Prompt
```bash
# Set a system prompt for chat sessions
infer config set-system "You are a helpful assistant."

# The system prompt will now be included with every chat session
# providing context and instructions to the AI model
infer chat
```

### Configuration Management
```bash
# Initialize a new project configuration
infer config init

# Initialize with overwrite existing config
infer config init --overwrite

# View current configuration (check .infer/config.yaml)
cat .infer/config.yaml

# The default model and system prompt will be saved in the chat section:
# chat:
#   default_model: "gpt-4-turbo"
#   system_prompt: "You are a helpful assistant."
```

### Tool Management
```bash
# Enable tool execution
infer config tools enable

# Disable tool execution
infer config tools disable

# List whitelisted commands and patterns
infer config tools list

# List in JSON format
infer config tools list --format json

# Validate if a command is whitelisted
infer config tools validate "ls -la"

# Execute a whitelisted command directly
infer config tools exec "git status"

# Manage safety approval settings
infer config tools safety enable
infer config tools safety disable
infer config tools safety status

# Manage excluded paths for security
infer config tools exclude-path list
infer config tools exclude-path add ".github/"
infer config tools exclude-path remove "test.txt"
```

### Web Search Tool

The CLI includes a web search tool that allows LLMs to search the web using Google or DuckDuckGo search engines. This tool is enabled by default and provides search results with configurable limits.

#### Features

- **Multiple Search Engines**: Supports Google and DuckDuckGo
- **Result Limiting**: Configure maximum number of results (default: 10)
- **Format Options**: Return results in text or JSON format
- **Mock Results**: Falls back to mock results for demonstration when APIs are unavailable
- **Configurable**: Enable/disable and customize through configuration

#### Configuration

```yaml
web_search:
  enabled: true                    # Enable/disable web search tool
  default_engine: "google"         # Default search engine
  max_results: 10                  # Default maximum results
  engines: ["google", "duckduckgo"] # Available engines
  timeout: 10                      # Search timeout (seconds)
```

#### Tool Parameters

When the LLM uses the WebSearch tool, it can specify:

- `query` (required): The search query string
- `engine` (optional): Search engine to use ("google" or "duckduckgo")
- `limit` (optional): Maximum number of results (1-50)
- `format` (optional): Output format ("text" or "json")

#### Example LLM Usage

The LLM can invoke the web search tool like this:

```json
{
  "name": "WebSearch",
  "parameters": {
    "query": "golang web development tutorial",
    "engine": "google",
    "limit": 5,
    "format": "text"
  }
}
```

#### Implementation Notes

- The current implementation uses mock results for demonstration purposes
- In production, you would integrate with actual search APIs:
  - Google: Custom Search JSON API (requires API key)
  - DuckDuckGo: Uses the instant answer API when available
- Non-200 HTTP responses fall back to mock results instead of failing
- Results include title, URL, and snippet for each search result

## Code Style Guidelines

- **Inline Comments**: Do not write inline comments unless the code is genuinely unclear or requires specific explanation.
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
