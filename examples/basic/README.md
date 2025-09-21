# Inference Gateway CLI Examples

This directory contains practical examples for using the `infer` CLI tool to interact with the Inference Gateway.

## Prerequisites

1. Start the Inference Gateway server with Docker Compose:

```bash
# Copy the example environment file and edit with your API keys
cp .env.example .env

# Start the gateway (add --profile local-models to include Ollama)
docker-compose up -d

# Check status
docker-compose ps
```

Alternative single container setup:

```bash
docker run --rm -it --env-file .env -p 8080:8080 ghcr.io/inference-gateway/inference-gateway:latest
```

2. Install the CLI:

```bash
# Build from source (recommended for development)
flox activate -- task build
flox activate -- task install

# Or use install script
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --install-dir $HOME/.local/bin
```

## Configuration

Set up your CLI configuration at `.infer/config.yaml`:

```yaml
gateway:
  url: http://localhost:8080
  api_key: ""
  timeout: 30
logging:
  debug: false
tools:
  enabled: true
  bash:
    enabled: true
    whitelist:
      commands:
        - ls
        - pwd
        - echo
        - grep
        - wc
        - sort
        - uniq
      patterns:
        - ^git status$
        - ^git log --oneline -n [0-9]+$
        - ^docker ps$
        - ^kubectl get pods$
  read:
    enabled: true
    require_approval: false
  file_search:
    enabled: true
    require_approval: false
  tree:
    enabled: true
    require_approval: false
  web_fetch:
    enabled: true
    whitelisted_domains:
      - golang.org
    safety:
      max_size: 8192
      timeout: 30
      allow_redirect: true
    cache:
      enabled: true
      ttl: 3600
      max_size: 52428800
  web_search:
    enabled: true
    default_engine: duckduckgo
    max_results: 10
    engines:
      - duckduckgo
      - google
    timeout: 10
  safety:
    require_approval: true
compact:
  output_dir: .infer
agent:
  model: ""
  system_prompt: ""
chat:
  theme: tokyo-night
```

## Basic Usage Examples

### Check Gateway Status

```bash
# Basic status check
infer status

# Status with JSON output
infer status --format json
```

### Interactive Chat

```bash
# Start interactive chat (will show model selection)
infer chat

# Set a default model to skip selection
infer config agent set-model anthropic/claude-3.5-sonnet
infer chat
```

### Configuration Management

```bash
# Initialize project configuration
infer init

# Set default model for chat sessions
infer config agent set-model anthropic/claude-3.5-sonnet
infer config agent set-model openai/gpt-4
infer config agent set-model google/gemini-pro

# Set system prompt for all chat sessions
infer config agent set-system "You are a helpful assistant."
```

### Tool Management

```bash
# Enable/disable tool execution
infer config tools enable
infer config tools disable

# List whitelisted commands and tool status
infer config tools list
infer config tools list --format json

# Validate if a command is whitelisted
infer config tools validate "ls"

# Execute tools directly with JSON arguments
infer config tools exec Bash '{"command":"git status"}'
infer config tools exec Tree '{"path":"."}'
infer config tools exec Read '{"file_path":"README.md"}'
infer config tools exec WebSearch '{"query":"golang tutorial"}'

# Manage safety settings
infer config tools safety enable   # Require approval for all tools
infer config tools safety disable  # Execute immediately
infer config tools safety status   # Show current settings

# Tool-specific safety settings
infer config tools safety set bash enabled
infer config tools safety set websearch disabled
infer config tools safety unset bash

# Manage excluded paths
infer config tools sandbox list
infer config tools sandbox add ".github/"
infer config tools sandbox remove "test.txt"
```

### Version Information

```bash
infer version
```

## Global Flags

All commands support these global flags:

- `--config, -c`: Specify custom config file path
- `--verbose, -v`: Enable verbose logging
- `--format`: Output format (text, json) - available on specific commands

## Available Commands

- `infer status` - Check gateway status and health
- `infer chat` - Start interactive chat with model selection
- `infer config` - Manage CLI configuration
  - `init [--overwrite]` - Initialize project configuration
  - `set-model <MODEL>` - Set default model (format: provider/model)
  - `set-system <PROMPT>` - Set system prompt
  - `tools` - Manage tool execution settings
- `infer version` - Show version information

## Security Features

- **Tool Whitelisting**: Only pre-approved commands can be executed
- **Approval Prompts**: Optional user confirmation before tool execution
- **Path Exclusions**: Protect sensitive directories from tool access
- **Tool-Specific Safety**: Configure approval requirements per tool
- **Safe Defaults**: Tools enabled with read-only commands and approval prompts
