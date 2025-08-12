<div align="center">

# Inference Gateway CLI

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/inference-gateway/cli/ci.yml?style=for-the-badge&logo=github)](https://github.com/inference-gateway/cli/actions)
[![Release](https://img.shields.io/github/v/release/inference-gateway/cli?style=for-the-badge&logo=github)](https://github.com/inference-gateway/cli/releases)
[![Go Report Card](https://img.shields.io/badge/Go%20Report%20Card-A+-brightgreen?style=for-the-badge&logo=go&logoColor=white)](https://goreportcard.com/report/github.com/inference-gateway/cli)

A powerful command-line interface for managing and interacting with the Inference Gateway. This CLI provides tools for configuration, monitoring, and management of inference services.

## ⚠️ Warning

> **Early Development Stage**: This project is in its early development stage and breaking changes are expected until it reaches a stable version.
>
> Always use pinned versions by specifying a specific version tag when downloading binaries or using install scripts.

</div>

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [`infer init`](#infer-init)
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
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --version v0.1.1
```

**Custom install directory:**
```bash
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --install-dir $HOME/.local/bin
```

The install script will:
- Detect your operating system and architecture automatically
- Download the appropriate binary from GitHub releases
- Install to `/usr/local/bin` by default (or custom directory with `--dir`)
- Make the binary executable
- Verify the installation

### Manual Download

Download the latest release binary for your platform from the [releases page](https://github.com/inference-gateway/cli/releases).

### Build from Source

```bash
git clone https://github.com/inference-gateway/cli.git
cd cli
go build -o infer .
```

## Quick Start

1. **Initialize project configuration:**
   ```bash
   infer init
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

### `infer init`
Initialize a new `.infer/config.yaml` configuration file in the current directory. This creates a local project configuration with default settings.

**Options:**
- `--overwrite`: Overwrite existing configuration file

**Examples:**
```bash
infer init
infer init --overwrite
```

### `infer status`
Check the status of the inference gateway including health checks and resource usage.

**Examples:**
```bash
infer status
```

### `infer chat`
Start an interactive chat session with model selection. Provides a conversational interface where you can select models and have conversations.

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

The CLI uses a YAML configuration file located at `.infer/config.yaml`. You can also specify a custom config file using the `--config` flag.

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
  safety:
    require_approval: true  # Prompt user before executing any command
compact:
  output_dir: ".infer"  # Directory for compact command exports
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
- **tools.enabled**: Enable/disable tool execution for LLMs (default: false)
- **tools.whitelist.commands**: List of allowed commands (supports arguments)
- **tools.whitelist.patterns**: Regex patterns for complex command validation
- **tools.safety.require_approval**: Prompt user before executing any command (default: true)

**Compact Settings:**
- **compact.output_dir**: Directory for compact command exports (default: ".infer")

## Global Flags

- `-c, --config`: Config file (default is `./.infer.yaml`)
- `-v, --verbose`: Verbose output
- `-h, --help`: Help for any command

## Examples

### Basic Workflow

```bash
# Initialize project configuration
infer init

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
