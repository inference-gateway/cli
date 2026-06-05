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
infer config set agent.model anthropic/claude-4.5-sonnet
infer chat
```

### Configuration Management

```bash
# Initialize project configuration
infer init

# Set default model for chat sessions
infer config set agent.model anthropic/claude-4.5-sonnet
infer config set agent.model openai/gpt-4
infer config set agent.model google/gemini-pro

# Read a value back (or `infer config get` to dump everything)
infer config get agent.model
```

### Tool Management

```bash
# Enable/disable tool execution (config get/set on tools.* keys)
infer config set tools.enabled true
infer config set tools.enabled false

# Inspect tool configuration and status
infer config get tools
infer config get tools -f json

# Validate if a command is whitelisted (top-level tools command)
infer tools validate "ls"

# Execute tools directly with JSON arguments
infer tools execute Bash '{"command":"git status"}'
infer tools execute Tree '{"path":"."}'
infer tools execute Read '{"file_path":"README.md"}'
infer tools execute WebSearch '{"query":"golang tutorial"}'

# Manage safety settings
infer config set tools.safety.require_approval true    # Require approval for all tools
infer config set tools.safety.require_approval false   # Execute immediately
infer config get tools.safety                          # Show current settings

# Tool-specific safety settings
infer config set tools.bash.require_approval true
infer config set tools.web_search.require_approval false

# Manage sandbox directories (comma-separated, replaces the whole list)
infer config get tools.sandbox.directories
infer config set tools.sandbox.directories ".,/tmp,.github"
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
