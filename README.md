<div align="center">

# Inference Gateway CLI

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/inference-gateway/cli/ci.yml?style=for-the-badge&logo=github)](https://github.com/inference-gateway/cli/actions)
[![Release](https://img.shields.io/github/v/release/inference-gateway/cli?style=for-the-badge&logo=github)](https://github.com/inference-gateway/cli/releases)
[![Go Report Card](https://img.shields.io/badge/Go%20Report%20Card-A+-brightgreen?style=for-the-badge&logo=go&logoColor=white)](https://goreportcard.com/report/github.com/inference-gateway/cli)

A powerful command-line interface for managing and interacting with the
Inference Gateway. This CLI provides tools for configuration, monitoring,
and management of inference services.

</div>

## ⚠️ Warning

> **Early Development Stage**: This project is in its early development
> stage and breaking changes are expected until it reaches a stable version.
>
> Always use pinned versions by specifying a specific version tag when
> downloading binaries or using install scripts.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
  - [Verifying Release Binaries](#verifying-release-binaries)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [`infer config`](#infer-config)
    - [`infer config init`](#infer-config-init)
    - [`infer config set-model`](#infer-config-set-model)
    - [`infer config set-system`](#infer-config-set-system)
    - [`infer config tools`](#infer-config-tools)
  - [`infer status`](#infer-status)
  - [`infer chat`](#infer-chat)
  - [`infer version`](#infer-version)
- [Configuration](#configuration)
  - [Default Configuration](#default-configuration)
  - [Configuration Options](#configuration-options)
  - [Web Search API Setup (Optional)](#web-search-api-setup-optional)
- [Global Flags](#global-flags)
- [Examples](#examples)
- [Development](#development)
  - [Building](#building)
  - [Testing](#testing)
  - [Dependencies](#dependencies)
- [License](#license)

## Features

- **Status Monitoring**: Check gateway health and resource usage
- **Interactive Chat**: Chat with models using an interactive interface
- **Configuration Management**: Manage gateway settings via YAML config
- **Project Initialization**: Set up local project configurations
- **Tool Execution**: LLMs can execute whitelisted commands and tools including:
  - **Bash**: Execute safe shell commands
  - **Read**: Read file contents with optional line ranges
  - **FileSearch**: Search for files using regex patterns
  - **WebSearch**: Search the web using DuckDuckGo or Google
  - **Fetch**: Fetch content from URLs and GitHub

## Installation

### Using Go Install

```bash
go install github.com/inference-gateway/cli@latest
```

### Using Install Script

For quick installation, you can use our install script:

**Unix/macOS/Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash
```

**With specific version:**

```bash
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --version v0.1.1
```

**Custom install directory:**

```bash
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --install-dir $HOME/.local/bin
```

The install script will:

- Detect your operating system and architecture automatically
- Download the appropriate binary from GitHub releases
- Install to `/usr/local/bin` by default (or custom directory with
  `--dir`)
- Make the binary executable
- Verify the installation

### Manual Download

Download the latest release binary for your platform from the [releases page](https://github.com/inference-gateway/cli/releases).

#### Verifying Release Binaries

All release binaries are signed with [Cosign](https://github.com/sigstore/cosign) for supply
chain security. You can verify the integrity and authenticity of downloaded binaries using the
following steps:

**1. Download the binary, checksums, and signature files:**

```bash
# Download binary (replace with your platform)
curl -L -o infer-darwin-amd64 \
  https://github.com/inference-gateway/cli/releases/download/v0.12.0/infer-darwin-amd64

# Download checksums and signature files
curl -L -o checksums.txt \
  https://github.com/inference-gateway/cli/releases/download/v0.12.0/checksums.txt
curl -L -o checksums.txt.pem \
  https://github.com/inference-gateway/cli/releases/download/v0.12.0/checksums.txt.pem
curl -L -o checksums.txt.sig \
  https://github.com/inference-gateway/cli/releases/download/v0.12.0/checksums.txt.sig
```

**2. Verify SHA256 checksum:**

```bash
# Calculate checksum of downloaded binary
shasum -a 256 infer-darwin-amd64

# Compare with checksums in checksums.txt
grep infer-darwin-amd64 checksums.txt
```

**3. Verify Cosign signature (requires [Cosign](https://github.com/sigstore/cosign) to be installed):**

```bash
# Decode base64 encoded certificate
cat checksums.txt.pem | base64 -d > checksums.txt.pem.decoded

# Verify the signature
cosign verify-blob \
  --certificate checksums.txt.pem.decoded \
  --signature checksums.txt.sig \
  --certificate-identity "https://github.com/inference-gateway/cli/.github/workflows/release.yml@refs/heads/main" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

**4. Make binary executable and install:**

```bash
chmod +x infer-darwin-amd64
sudo mv infer-darwin-amd64 /usr/local/bin/infer
```

> **Note**: Replace `v0.12.0` with the desired release version and `infer-darwin-amd64` with your platform's binary name.

### Build from Source

```bash
git clone https://github.com/inference-gateway/cli.git
cd cli
go build -o infer .
```

## Quick Start

1. **Initialize project configuration:**

   ```bash
   infer config init
   ```

2. **Check gateway status:**

   ```bash
   infer status
   ```

3. **Start an interactive chat:**

   ```bash
   infer chat
   ```

## Commands

### `infer config`

Manage CLI configuration settings including models, system prompts, and tools.

### `infer config init`

Initialize a new `.infer/config.yaml` configuration file in the current
directory. This creates a local project configuration with default settings.

**Options:**

- `--overwrite`: Overwrite existing configuration file

**Examples:**

```bash
infer config init
infer config init --overwrite
```

### `infer config set-model`

Set the default model for chat sessions. When set, chat sessions will
automatically use this model without showing the model selection prompt.

**Examples:**

```bash
infer config set-model openai/gpt-4-turbo
infer config set-model anthropic/claude-opus-4-1-20250805
```

### `infer config set-system`

Set a system prompt that will be included with every chat session, providing
context and instructions to the AI model.

**Examples:**

```bash
infer config set-system "You are a helpful assistant."
infer config set-system "You are a Go programming expert."
```

### `infer config tools`

Manage tool execution settings for LLMs, including enabling/disabling tools,
managing whitelists, and security settings.

**Subcommands:**

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

**Examples:**

```bash
# Enable/disable tool execution
infer config tools enable
infer config tools disable

# List whitelisted commands
infer config tools list
infer config tools list --format json

# Validate and execute commands
infer config tools validate "ls -la"
infer config tools exec "git status"

# Manage global safety settings (approval prompts)
infer config tools safety enable   # Enable approval prompts for all tool execution
infer config tools safety disable  # Disable approval prompts (execute tools immediately)
infer config tools safety status   # Show current safety approval status

# Manage tool-specific safety settings (granular control)
infer config tools safety set Bash enabled        # Require approval for Bash tool only
infer config tools safety set WebSearch disabled  # Skip approval for WebSearch tool
infer config tools safety unset Bash              # Remove tool-specific setting (use global)

# Manage excluded paths
infer config tools exclude-path list
infer config tools exclude-path add ".github/"
infer config tools exclude-path remove "test.txt"
```

### `infer status`

Check the status of the inference gateway including health checks and
resource usage.

**Examples:**

```bash
infer status
```

### `infer chat`

Start an interactive chat session with model selection. Provides a
conversational interface where you can select models and have conversations.

**Features:**

- Interactive model selection
- Conversational interface
- Real-time streaming responses
- **Scrollable chat history** with mouse wheel and keyboard support

**Navigation Controls:**

- **Mouse wheel**: Scroll up/down through chat history
- **Arrow keys** (`↑`/`↓`) or **Vim keys** (`k`/`j`): Scroll one line at a time
- **Page Up/Page Down**: Scroll by page
- **Home/End**: Jump to top/bottom of chat history
- **Shift+↑/Shift+↓**: Half-page scrolling
- **Ctrl+R**: Toggle expanded view of tool results

**Examples:**

```bash
infer chat
```

### `infer version`

Display version information for the Inference Gateway CLI.

**Examples:**

```bash
infer version
```

## Available Tools for LLMs

When tool execution is enabled, LLMs can use the following tools to interact with the system:

### FileSearch Tool

Search for files in the filesystem using regex patterns on file names and paths.
This tool is particularly useful for finding files before reading them.

**Parameters:**

- `pattern` (required): Regex pattern to match against file paths
- `include_dirs` (optional): Whether to include directories in results (default: false)
- `case_sensitive` (optional): Whether pattern matching is case sensitive (default: true)
- `format` (optional): Output format - "text" or "json" (default: "text")

**Examples:**

- Find Go source files: `\.go$`
- Find config files: `.*config.*\.(yaml|yml|json)$`
- Find test files: `.*test.*\.go$`
- Find files in cmd directory: `^cmd/.*\.go$`

**Security:**

- Respects the same exclusion rules as other file operations
- Skips binary files, hidden directories, and configured excluded paths
- Limited to reasonable search depth to prevent excessive resource usage

### Tree Tool

Display directory structure in a tree format, similar to the Unix `tree` command. Provides a polyfill
implementation when the native `tree` command is unavailable.

**Parameters:**

- `path` (optional): Directory path to display tree structure for (default: current directory)
- `max_depth` (optional): Maximum depth to traverse (unlimited by default)
- `exclude_patterns` (optional): Array of glob patterns to exclude from the tree (e.g., `["*.log", "node_modules"]`)
- `show_hidden` (optional): Whether to show hidden files and directories (default: false)
- `format` (optional): Output format - "text" or "json" (default: "text")

**Examples:**

- Basic tree: Uses current directory with default settings
- Tree with depth limit: `max_depth: 2` - Shows only 2 levels deep
- Tree excluding patterns: `exclude_patterns: ["*.log", "node_modules", ".git"]`
- Tree with hidden files: `show_hidden: true`
- JSON output: `format: "json"` - Returns structured data

**Features:**

- **Native Integration**: Uses system `tree` command when available for optimal performance
- **Polyfill Implementation**: Falls back to custom implementation when `tree` is not installed
- **Pattern Exclusion**: Supports glob patterns to exclude specific files and directories
- **Depth Control**: Limit traversal depth to prevent overwhelming output
- **Hidden File Control**: Toggle visibility of hidden files and directories
- **Multiple Formats**: Text output for readability, JSON for structured data

**Security:**

- Respects configured path exclusions for security
- Validates directory access permissions
- Limited by the same security restrictions as other file tools

### Bash Tool

Execute whitelisted bash commands securely with validation against configured command patterns.

### Read Tool

Read file content from the filesystem with optional line range specification.

### WebSearch Tool

Search the web using DuckDuckGo or Google search engines to find information.

### Fetch Tool

Fetch content from whitelisted URLs or GitHub references using the format `github:owner/repo#123`.

**Security Notes:**

- All tools respect configured safety settings and exclusion patterns
- Commands require approval when safety approval is enabled
- File access is restricted to allowed paths and excludes sensitive directories

## Configuration

The CLI uses a YAML configuration file located at `.infer/config.yaml`.
You can also specify a custom config file using the `--config` flag.

### Default Configuration

```yaml
gateway:
  url: http://localhost:8080
  api_key: ""
  timeout: 30
output:
  format: text
  quiet: false
tools:
  enabled: true # Tools are enabled by default with safe read-only commands
  whitelist:
    commands: # Exact command matches
      - ls
      - pwd
      - echo
      - grep
      - find
      - wc
      - sort
      - uniq
    patterns: # Regex patterns for more complex commands
      - ^git status$
      - ^git log --oneline -n [0-9]+$
      - ^docker ps$
      - ^kubectl get pods$
  safety:
    require_approval: true
  exclude_paths:
    - .infer/ # Protect infer's own configuration directory
    - .infer/* # Protect all files in infer's configuration directory
compact:
  output_dir: .infer # Directory for compact command exports
chat:
  default_model: "" # Default model for chat sessions (when set, skips model selection)
  system_prompt: "" # System prompt included with every chat session
web_search:
  enabled: true # Enable web search tool for LLMs
  default_engine: duckduckgo # Default search engine (duckduckgo, google)
  max_results: 10 # Default maximum number of search results
  engines: # Available search engines
    - duckduckgo
    - google
  timeout: 10 # Search timeout in seconds
fetch:
  enabled: false
  whitelisted_domains:
    - github.com
  github:
    enabled: false
    token: ""
    base_url: https://api.github.com
  safety:
    max_size: 8192
    timeout: 30
    allow_redirect: true
  cache:
    enabled: true
    ttl: 3600
    max_size: 52428800
```

### Configuration Options

**Gateway Settings:**

- **gateway.url**: The URL of the inference gateway
- **gateway.api_key**: API key for authentication (if required)
- **gateway.timeout**: Request timeout in seconds

**Output Settings:**

- **output.format**: Default output format (text, json, yaml)
- **output.quiet**: Suppress non-essential output

**Tool Settings:**

- **tools.enabled**: Enable/disable tool execution for LLMs (default: true)
- **tools.whitelist.commands**: List of allowed commands (supports arguments)
- **tools.whitelist.patterns**: Regex patterns for complex command validation
- **tools.safety.require_approval**: Prompt user before executing any command
  (default: true)
- **tools.exclude_paths**: Paths excluded from tool access for security
  (default: [".infer/", ".infer/*"])

**Compact Settings:**

- **compact.output_dir**: Directory for compact command exports
  (default: ".infer")

**Chat Settings:**

- **chat.default_model**: Default model for chat sessions (skips model
  selection when set)
- **chat.system_prompt**: System prompt included with every chat session

**Web Search Settings:**

- **web_search.enabled**: Enable/disable web search tool for LLMs (default: true)
- **web_search.default_engine**: Default search engine to use ("duckduckgo" or "google", default: "duckduckgo")
- **web_search.max_results**: Maximum number of search results to return (1-50, default: 10)
- **web_search.engines**: List of available search engines
- **web_search.timeout**: Search timeout in seconds (default: 10)

#### Web Search API Setup (Optional)

Both search engines work out of the box, but for better reliability and performance in production, you can
configure API keys:

**Google Custom Search Engine:**

1. **Create a Custom Search Engine:**
   - Go to [Google Programmable Search Engine](https://programmablesearchengine.google.com/)
   - Click "Add" to create a new search engine
   - Enter a name for your search engine
   - In "Sites to search", enter `*` to search the entire web
   - Click "Create"

2. **Get your Search Engine ID:**
   - In your search engine settings, note the "Search engine ID" (cx parameter)

3. **Get a Google API Key:**
   - Go to the [Google Cloud Console](https://console.cloud.google.com/)
   - Create a new project or select an existing one
   - Enable the "Custom Search JSON API"
   - Go to "Credentials" and create an API key
   - Restrict the API key to the Custom Search JSON API for security

4. **Configure Environment Variables:**

   ```bash
   export GOOGLE_SEARCH_API_KEY="your_api_key_here"
   export GOOGLE_SEARCH_ENGINE_ID="your_search_engine_id_here"
   ```

**DuckDuckGo API (Optional):**

```bash
export DUCKDUCKGO_SEARCH_API_KEY="your_api_key_here"
```

**Note:** Both engines have built-in fallback methods that work without API configuration. However, using
official APIs provides better reliability and performance for production use.

## Global Flags

- `-c, --config`: Config file (default is `./.infer/config.yaml`)
- `-v, --verbose`: Verbose output
- `-h, --help`: Help for any command

## Examples

### Basic Workflow

```bash
# Initialize project configuration
infer config init

# Check if gateway is running
infer status

# Start interactive chat
infer chat
```

### Configuration Management

```bash
# Use custom config file
infer --config ./my-config.yaml status

# Get verbose output
infer --verbose status

# Set default model for chat sessions
infer config set-model openai/gpt-4-turbo

# Set system prompt
infer config set-system "You are a helpful assistant."

# Enable tool execution with safety approval
infer config tools enable
infer config tools safety enable
```

## Development

### Building

```bash
go build -o infer .
```

### Testing

```bash
go test ./...
```

### Dependencies

- [Cobra](https://github.com/spf13/cobra) - CLI framework
- [YAML v3](https://gopkg.in/yaml.v3) - YAML parsing

## License

This project is licensed under the MIT License.
