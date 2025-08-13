# Inference Gateway CLI

<div align="center">

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/inference-gateway/cli/ci.yml?style=for-the-badge&logo=github)](https://github.com/inference-gateway/cli/actions)
[![Release](https://img.shields.io/github/v/release/inference-gateway/cli?style=for-the-badge&logo=github)](https://github.com/inference-gateway/cli/releases)
[![Go Report Card](https://img.shields.io/badge/Go%20Report%20Card-A+-brightgreen?style=for-the-badge&logo=go&logoColor=white)](https://goreportcard.com/report/github.com/inference-gateway/cli)

A powerful command-line interface for managing and interacting with the
Inference Gateway. This CLI provides tools for configuration, monitoring,
and management of inference services.

## ⚠️ Warning

> **Early Development Stage**: This project is in its early development
> stage and breaking changes are expected until it reaches a stable version.
>
> Always use pinned versions by specifying a specific version tag when
> downloading binaries or using install scripts.

</div>

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
curl -fsSL \
  https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --version v0.1.1
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

# Manage safety settings
infer config tools safety enable
infer config tools safety status

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

## Configuration

The CLI uses a YAML configuration file located at `.infer/config.yaml`.
You can also specify a custom config file using the `--config` flag.

### Default Configuration

```yaml
gateway:
  url: "http://localhost:8080"
  api_key: ""
  timeout: 30
output:
  format: "text"
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
  output_dir: ".infer"  # Directory for compact command exports
chat:
  default_model: ""  # Default model for chat sessions (when set, skips model selection)
  system_prompt: ""  # System prompt included with every chat session
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
