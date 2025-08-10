<div align="center">

# Inference Gateway CLI

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/inference-gateway/cli/release.yml?style=for-the-badge&logo=github)](https://github.com/inference-gateway/cli/actions)
[![Release](https://img.shields.io/github/v/release/inference-gateway/cli?style=for-the-badge&logo=github)](https://github.com/inference-gateway/cli/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/inference-gateway/cli?style=for-the-badge)](https://goreportcard.com/report/github.com/inference-gateway/cli)

A powerful command-line interface for managing and interacting with the Inference Gateway. This CLI provides tools for configuration, deployment, monitoring, and management of inference services.

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
  - [`infer status`](#infer-status)
  - [`infer models list`](#infer-models-list)
  - [`infer chat`](#infer-chat)
  - [`infer tools`](#infer-tools)
  - [`infer prompt`](#infer-prompt-text)
  - [`infer version`](#infer-version)
- [Configuration](#configuration)
  - [Default Configuration](#default-configuration)
  - [Configuration Options](#configuration-options)
- [Global Flags](#global-flags)
- [Tool System](#tool-system)
  - [Tool Security Features](#tool-security-features)
  - [Available Tools](#available-tools)
  - [Tool Usage in Chat](#tool-usage-in-chat)
  - [Customizing Tool Whitelist](#customizing-tool-whitelist)
  - [Troubleshooting Tools](#troubleshooting-tools)
- [Examples](#examples)
  - [Complete Workflow with Tools](#complete-workflow-with-tools)
  - [Tool Management](#tool-management)
  - [Configuration Management](#configuration-management)
- [Development](#development)
  - [Building](#building)
  - [Testing](#testing)
  - [Dependencies](#dependencies)
- [License](#license)

## Features

- **Model Deployment**: Deploy and manage machine learning models
- **Status Monitoring**: Check gateway health and resource usage
- **Interactive Chat**: Chat with models using an interactive interface
- **Tool Execution**: Allow LLMs to execute whitelisted bash commands securely
- **Prompt Testing**: Send prompts directly to deployed models
- **Configuration Management**: Manage gateway settings via YAML config
- **Multiple Output Formats**: Support for text, JSON, and YAML output

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

1. **Check gateway status:**
   ```bash
   infer status
   ```

2. **Start an interactive chat:**
   ```bash
   infer chat
   ```

3. **Enable tools for LLM interaction:**
   ```bash
   infer tools enable
   ```

4. **Send a one-shot prompt:**
   ```bash
   infer prompt --model <model-name> "What is machine learning?"
   ```

5. **List deployed models:**
   ```bash
   infer models list
   ```

## Commands

### `infer status`
Check the status of the inference gateway including running services, model deployments, health checks, and resource usage.

**Options:**
- `-d, --detailed`: Show detailed status information
- `-f, --format`: Output format (text, json, yaml)

**Examples:**
```bash
infer status
infer status --detailed
infer status --format json
```


### `infer models list`
List all deployed models and services on the inference gateway.

**Examples:**
```bash
infer models list
```

### `infer chat`
Start an interactive chat session with model selection and tool support. Provides a conversational interface where you can switch models, view history, and leverage LLM tools.

**Features:**
- Model selection with search
- File references using `@filename` syntax
- Tool execution (when enabled)
- Conversation history management
- Real-time streaming responses

**Examples:**
```bash
infer chat
```

**Chat Commands:**
- `/exit`, `/quit` - Exit the chat session
- `/clear` - Clear conversation history
- `/history` - Show conversation history
- `/models` - Show available models
- `/switch` - Switch to a different model
- `/help` - Show help information

### `infer tools`
Manage and execute tools that LLMs can use during chat sessions. Tools provide secure execution of whitelisted bash commands.

#### `infer tools enable`
Enable tool execution for LLMs.

#### `infer tools disable`
Disable tool execution for LLMs.

#### `infer tools list`
List whitelisted commands and patterns.

#### `infer tools validate <command>`
Validate if a command is whitelisted without executing it.

#### `infer tools exec <command>`
Execute a whitelisted command directly.

**Options:**
- `-f, --format`: Output format (text, json)

**Examples:**
```bash
# Enable tools
infer tools enable

# List whitelisted commands
infer tools list

# Validate a command
infer tools validate "ls -la"

# Execute a command
infer tools exec "pwd"

# Get tool definitions for LLMs
infer tools llm list --format=json
```

### `infer prompt [text]`
Send a text prompt to the inference gateway for processing.

**Examples:**
```bash
infer prompt --model "deepseek/deepseek-chat" "Hello, world!"
infer prompt --model "deepseek/deepseek-chat" "Translate this text to French: Hello"
```

### `infer version`
Display version information for the Inference Gateway CLI.

**Examples:**
```bash
infer version
```

## Configuration

The CLI uses a YAML configuration file located at `~/.infer.yaml`. You can also specify a custom config file using the `--config` flag.

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
  enabled: false  # Set to true to enable tool execution
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
    patterns:  # Regex patterns for complex commands
      - "^git status$"
      - "^git log --oneline -n [0-9]+$"
      - "^docker ps$"
      - "^kubectl get pods$"
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

## Global Flags

- `-c, --config`: Config file (default: `$HOME/.infer.yaml`)
- `-v, --verbose`: Verbose output
- `-h, --help`: Help for any command

## Tool System

The Inference Gateway CLI includes a secure tool execution system that allows LLMs to execute whitelisted bash commands during chat conversations.

### Tool Security Features

- **Whitelist-Only Execution**: Only explicitly allowed commands can be executed
- **Command Validation**: Support for exact matches and regex patterns
- **Timeout Protection**: Commands timeout after 30 seconds
- **Secure Environment**: Tools run with CLI user permissions
- **Disabled by Default**: Tools must be explicitly enabled

### Available Tools

**Bash Tool**: Allows LLMs to execute whitelisted bash commands
- **Parameters**:
  - `command` (required): The bash command to execute
  - `format` (optional): Output format ("text" or "json")

### Tool Usage in Chat

1. **Enable tools**:
   ```bash
   infer tools enable
   ```

2. **Start chat**:
   ```bash
   infer chat
   ```

3. **Example interaction**:
   ```
   You: Can you list the files in this directory?

   Model: 🔧 Calling tool: Bash with arguments: {"command":"ls"}
   ✅ Tool result:
   Command: ls
   Exit Code: 0
   Output:
   README.md
   main.go
   cmd/

   I can see the files in your directory: README.md, main.go, and a cmd/ folder.
   ```

### Customizing Tool Whitelist

Edit `~/.infer.yaml` to add custom commands:

```yaml
tools:
  enabled: true
  whitelist:
    commands:
      - "ls"
      - "pwd"
      - "your-custom-command"
    patterns:
      - "^git status$"
      - "^your-pattern.*$"
```

### Troubleshooting Tools

**"Command not whitelisted" error:**
- Check allowed commands: `infer tools list`
- Add command to your config file
- Validate with: `infer tools validate "your-command"`

**"Tools are disabled" error:**
- Enable tools: `infer tools enable`
- Verify config: `tools.enabled: true` in `~/.infer.yaml`

## Examples

### Complete Workflow with Tools

```bash
# Check if gateway is running
infer status

# Enable tools for LLM interaction
infer tools enable

# List whitelisted commands
infer tools list

# Start interactive chat with tools
infer chat
```

### Tool Management

```bash
# Enable tool execution
infer tools enable

# List allowed commands and patterns
infer tools list

# Validate a command before use
infer tools validate "git status"

# Execute a command directly
infer tools exec "pwd"

# Disable tool execution
infer tools disable
```

### Configuration Management

```bash
# Use custom config file
infer --config ./my-config.yaml status

# Get verbose output
infer --verbose tools list
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
