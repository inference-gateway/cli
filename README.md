<div align="center">

# Inference Gateway CLI

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
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
- [Quick Start](#quick-start)
- [Commands](#commands)
- [Tools for LLMs](#tools-for-llms)
- [Configuration](#configuration)
- [Cost Tracking](#cost-tracking)
- [Tool Approval System](#tool-approval-system)
- [Shortcuts](#shortcuts)
- [Global Flags](#global-flags)
- [Examples](#examples)
- [Development](#development)
- [License](#license)

## Features

- **Automatic Gateway Management**: Automatically downloads and runs the Inference Gateway binary (no Docker required!)
- **Zero-Configuration Setup**: Start chatting immediately with just your API keys in a `.env` file
- **Interactive Chat**: Chat with models using an interactive interface
- **Status Monitoring**: Check gateway health and resource usage
- **Conversation History**: Store and retrieve past conversations with multiple storage backends
  - [Conversation Storage](docs/conversation-storage.md) - Detailed storage backend documentation
  - [Conversation Title Generation](docs/conversation-title-generation.md) - AI-powered title generation system
- **Configuration Management**: Manage gateway settings via YAML config
- **Project Initialization**: Set up local project configurations
- **Tool Execution**: LLMs can execute whitelisted commands and tools - [See all tools →](docs/tools-reference.md)
- **Tool Approval System**: User approval workflow for sensitive operations with real-time diff visualization
- **Agent Modes**: Three operational modes for different workflows:
  - **Standard Mode** (default): Normal operation with all configured tools and approval checks
  - **Plan Mode**: Read-only mode for planning and analysis without execution
  - **Auto-Accept Mode**: All tools auto-approved for rapid execution (YOLO mode)
  - Toggle between modes with **Shift+Tab**
- **Token Usage Tracking**: Accurate token counting with polyfill support for providers that don't return usage metrics
- **Cost Tracking**: Real-time cost calculation for API usage with per-model breakdown and configurable pricing
- **Inline History Auto-Completion**: Smart command history suggestions with inline completion
- **Customizable Keybindings**: Fully configurable keyboard shortcuts for the chat interface
- **Extensible Shortcuts System**: Create custom commands with AI-powered snippets - [Learn more →](docs/shortcuts-guide.md)
- **MCP Server Support**: Direct integration with Model Context Protocol servers for extended tool capabilities -
  [Learn more →](docs/mcp-integration.md)

## Installation

### Using Go Install

```bash
go install github.com/inference-gateway/cli@latest
```

This installs the binary as `cli`. To rename it to `infer`:

```bash
mv $(go env GOPATH)/bin/cli $(go env GOPATH)/bin/infer
```

Or use an alias:

```bash
alias infer="$(go env GOPATH)/bin/cli"
```

### Using Container Image

```bash
# Create network and deploy inference gateway first
docker network create inference-gateway
docker run -d --name inference-gateway --network inference-gateway \
  --env-file .env \
  ghcr.io/inference-gateway/inference-gateway:latest

# Pull and run the CLI
docker pull ghcr.io/inference-gateway/cli:latest
docker run -it --rm --network inference-gateway ghcr.io/inference-gateway/cli:latest chat
```

### Using Install Script

```bash
# Latest version
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash

# Specific version
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --version v0.77.0

# Custom installation directory
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --install-dir $HOME/.local/bin
```

### Manual Download

Download the latest release binary for your platform from the [releases page](https://github.com/inference-gateway/cli/releases).

**Verify the binary** (recommended for security):

```bash
# Download binary and checksums
curl -L -o infer-darwin-amd64 \
  https://github.com/inference-gateway/cli/releases/latest/download/infer-darwin-amd64
curl -L -o checksums.txt \
  https://github.com/inference-gateway/cli/releases/latest/download/checksums.txt

# Verify checksum
shasum -a 256 infer-darwin-amd64
grep infer-darwin-amd64 checksums.txt

# Install
chmod +x infer-darwin-amd64
sudo mv infer-darwin-amd64 /usr/local/bin/infer
```

For advanced verification with Cosign signatures, see [Binary Verification Guide](docs/security/binary-verification.md).

### Build from Source

```bash
git clone https://github.com/inference-gateway/cli.git
cd cli
go build -o infer cmd/infer/main.go
sudo mv infer /usr/local/bin/
```

## Quick Start

1. **Initialize your project**:

```bash
infer init
```

This creates a `.infer/` directory with configuration and shortcuts.

2. **Set up your environment** (create `.env` file):

```env
ANTHROPIC_API_KEY=your_key_here
OPENAI_API_KEY=your_key_here
DEEPSEEK_API_KEY=your_key_here
```

3. **Start chatting**:

```bash
infer chat
```

### Next Steps

Now that you're up and running, explore these guides:

- **[Commands Reference](docs/commands-reference.md)** - Complete command documentation
- **[Tools Reference](docs/tools-reference.md)** - Available tools for LLMs
- **[Configuration Guide](docs/configuration-reference.md)** - Full configuration options
- **[Shortcuts Guide](docs/shortcuts-guide.md)** - Custom shortcuts and AI-powered snippets
- **[A2A Agents](docs/agents-configuration.md)** - Agent-to-agent communication setup

## Commands

The CLI provides several commands for different workflows. For detailed documentation, see [Commands Reference](docs/commands-reference.md).

### Core Commands

**`infer init`** - Initialize a new project with configuration and shortcuts

```bash
infer init              # Initialize project configuration
infer init --userspace  # Initialize user-level configuration
```

**`infer chat`** - Start an interactive chat session with model selection

```bash
infer chat
```

**Features:** Model selection, real-time streaming, scrollable history, three agent modes (Standard/Plan/Auto-Accept).

**`infer agent`** - Execute autonomous tasks in background mode

```bash
# Start new agent sessions
infer agent "Please fix the github issue 38"
infer agent --model "openai/gpt-4" "Implement feature from issue #42"
infer agent "Analyze this UI issue" --files screenshot.png

# Resume existing sessions
infer conversations list  # Find session IDs
infer agent "continue fixing the bug" --session-id abc-123-def
infer agent "analyze new logs" --session-id abc-123 --files error.log
```

**Features:** Autonomous execution, multimodal support (images/files), parallel tool execution, **session resumption**.

### Configuration Commands

**`infer config`** - Manage CLI configuration settings

```bash
# Agent configuration
infer config agent set-model "deepseek/deepseek-chat"
infer config agent set-system "You are a helpful assistant"
infer config agent set-max-turns 100
infer config agent verbose-tools enable

# Tool management
infer config tools enable
infer config tools bash enable
infer config tools safety enable

# Export configuration
infer config export set-model "anthropic/claude-4.1-haiku"
```

See [Commands Reference](docs/commands-reference.md#configuration-management) for all configuration options.

### Agent Management

**`infer agents`** - Manage A2A (Agent-to-Agent) agent configurations

```bash
infer agents init                    # Initialize agents configuration
infer agents add browser-agent       # Add an agent from the registry with defaults
infer agents add custom https://...  # Add a custom agent
infer agents list                    # List all agents
```

For detailed A2A setup, see [A2A Agents Configuration](docs/agents-configuration.md).

### Utility Commands

**`infer status`** - Check gateway health and resource usage

```bash
infer status
```

**`infer conversations`** - List and manage conversation history

```bash
infer conversations list                    # List all saved conversations
infer conversations list --limit 20         # List first 20 conversations
infer conversations list --offset 40 -l 20  # Paginate: conversations 41-60
infer conversations list --format json      # Output as JSON
```

**`infer conversation-title`** - Manage AI-powered conversation titles

```bash
infer conversation-title generate  # Generate titles for all conversations
infer conversation-title status    # Show generation status
```

**`infer version`** - Display CLI version information

```bash
infer version
```

## Tools for LLMs

When tool execution is enabled, LLMs can use various tools to interact with your system. Below is a
summary of available tools. For detailed documentation, parameters, and examples, see
[Tools Reference](docs/tools-reference.md).

| Tool | Purpose | Approval Required | Documentation |
| ------ | --------- | ------------------- | --------------- |
| **Bash** | Execute whitelisted shell commands | Optional | [Details](docs/tools-reference.md#bash-tool) |
| **Read** | Read file contents with line ranges | No | [Details](docs/tools-reference.md#read-tool) |
| **Write** | Write content to files | Yes | [Details](docs/tools-reference.md#write-tool) |
| **Edit** | Exact string replacements in files | Yes | [Details](docs/tools-reference.md#edit-tool) |
| **MultiEdit** | Multiple atomic edits to files | Yes | [Details](docs/tools-reference.md#multiedit-tool) |
| **Delete** | Delete files and directories | Yes | [Details](docs/tools-reference.md#delete-tool) |
| **Tree** | Display directory structure | No | [Details](docs/tools-reference.md#tree-tool) |
| **Grep** | Search files with regex (ripgrep/Go) | No | [Details](docs/tools-reference.md#grep-tool) |
| **WebSearch** | Search the web (DuckDuckGo/Google) | No | [Details](docs/tools-reference.md#websearch-tool) |
| **WebFetch** | Fetch content from URLs | No | [Details](docs/tools-reference.md#webfetch-tool) |
| **Github** | Interact with GitHub API | No | [Details](docs/tools-reference.md#github-tool) |
| **TodoWrite** | Create and manage task lists | No | [Details](docs/tools-reference.md#todowrite-tool) |
| **A2A_SubmitTask** | Submit tasks to A2A agents | No | [Details](docs/tools-reference.md#a2a_submittask-tool) |
| **A2A_QueryAgent** | Query A2A agent capabilities | No | [Details](docs/tools-reference.md#a2a_queryagent-tool) |
| **A2A_QueryTask** | Check A2A task status | No | [Details](docs/tools-reference.md#a2a_querytask-tool) |

**Tool Configuration:**

Tools can be enabled/disabled and configured individually:

```bash
# Enable/disable specific tools
infer config tools bash enable
infer config tools write enable

# Configure tool settings
infer config tools grep set-backend ripgrep
infer config tools web-fetch add-domain "example.com"
```

See [Tools Reference](docs/tools-reference.md) for complete documentation.

## Configuration

The CLI uses a powerful 2-layer configuration system with environment variable support.

### Configuration Quick Start

Create a minimal configuration:

```yaml
# .infer/config.yaml
gateway:
  url: http://localhost:8080
  docker: true  # Use Docker mode (or false for binary mode)

tools:
  enabled: true
  bash:
    enabled: true

agent:
  model: "deepseek/deepseek-chat"
  max_turns: 50

chat:
  theme: tokyo-night
```

### Configuration Layers

1. **Environment Variables** (`INFER_*`) - Highest priority
2. **Command Line Flags**
3. **Project Config** (`.infer/config.yaml`)
4. **Userspace Config** (`~/.infer/config.yaml`)
5. **Built-in Defaults** - Lowest priority

**Example:**

```bash
# Set via environment variable (highest priority)
export INFER_AGENT_MODEL="openai/gpt-4"

# Or via config file
infer config agent set-model "deepseek/deepseek-chat"

# Or via command flag
infer chat --model "anthropic/claude-4"
```

### Key Configuration Options

- **gateway.url** - Gateway URL (default: `http://localhost:8080`)
- **gateway.docker** - Use Docker mode vs binary mode (default: `true`)
- **tools.enabled** - Enable/disable all tools (default: `true`)
- **agent.model** - Default model for agent operations
- **agent.max_turns** - Maximum turns for agent sessions (default: `50`)
- **chat.theme** - Chat interface theme (default: `tokyo-night`)
- **chat.status_bar.enabled** - Enable/disable status bar (default: `true`)
- **chat.status_bar.indicators** - Configure individual status indicators (all enabled by default except `max_output`)

### Environment Variables

All configuration can be set via environment variables with the `INFER_` prefix:

```bash
export INFER_GATEWAY_URL="http://localhost:8080"
export INFER_AGENT_MODEL="deepseek/deepseek-chat"
export INFER_TOOLS_BASH_ENABLED=true
export INFER_CHAT_THEME="tokyo-night"
```

**Format:** `INFER_<PATH>` where dots become underscores.
Example: `agent.model` → `INFER_AGENT_MODEL`

For complete configuration documentation, including all options and environment variables, see [Configuration Reference](docs/configuration-reference.md).

## Cost Tracking

The CLI automatically tracks API costs based on token usage for all providers and models.
Costs are calculated in real-time with support for both aggregate totals and per-model breakdowns.

### Viewing Costs

Use the `/cost` command in any chat session to see the cost breakdown:

```bash
# In chat, use the /cost shortcut
/cost
```

This displays:

- **Total session cost** in USD
- **Input/output costs** separately
- **Per-model breakdown** when using multiple models
- **Token usage** for each model

**Status Bar**: Session costs are also displayed in the status bar (e.g., `💰 $0.0234`) if enabled.

### Configuring Pricing

The CLI includes hardcoded pricing for 30+ models across all major providers
(Anthropic, OpenAI, Google, DeepSeek, Groq, Mistral, Cohere, etc.).
Prices are updated regularly to match current provider pricing.

**Override pricing** for specific models or add pricing for custom models:

```yaml
# .infer/config.yaml
pricing:
  enabled: true
  currency: "USD"
  custom_prices:
    # Override existing model pricing
    "openai/gpt-4o":
      input_price_per_mtoken: 2.50    # Price per million input tokens
      output_price_per_mtoken: 10.00  # Price per million output tokens

    # Add pricing for custom/local models
    "ollama/llama3.2":
      input_price_per_mtoken: 0.0
      output_price_per_mtoken: 0.0

    "custom-fine-tuned-model":
      input_price_per_mtoken: 5.00
      output_price_per_mtoken: 15.00
```

**Via environment variables:**

```bash
# Disable cost tracking entirely
export INFER_PRICING_ENABLED=false

# Override specific model pricing (use underscores in model names)
export INFER_PRICING_CUSTOM_PRICES_OPENAI_GPT_4O_INPUT_PRICE_PER_MTOKEN=3.00
export INFER_PRICING_CUSTOM_PRICES_OPENAI_GPT_4O_OUTPUT_PRICE_PER_MTOKEN=12.00

# Hide cost from status bar
export INFER_CHAT_STATUS_BAR_INDICATORS_COST=false
```

**Status Bar Configuration:**

```yaml
# .infer/config.yaml
chat:
  status_bar:
    enabled: true
    indicators:
      cost: true  # Show/hide cost indicator
```

### Cost Calculation

- Costs are calculated as: `(tokens / 1,000,000) × price_per_million_tokens`
- Prices are per million tokens (input and output priced separately)
- Models without pricing data (Ollama, free tiers) show $0.00
- Token counts use actual usage from providers or polyfilled estimates

## Tool Approval System

The CLI includes a comprehensive approval system for sensitive tool operations, providing security and
visibility into what actions LLMs are taking.

### How It Works

When a tool requiring approval is executed:

1. **Validation**: Tool arguments are validated
2. **Approval Prompt**: User sees tool details with:
   - Tool name and parameters
   - Real-time diff preview (for file modifications)
   - Approve/Reject/Auto-approve options
3. **Execution**: Tool runs only if approved

### Default Approval Requirements

| Tool | Requires Approval | Reason |
| ------ | ------------------- | --------- |
| Write | Yes | Creates/modifies files |
| Edit | Yes | Modifies file contents |
| MultiEdit | Yes | Multiple file modifications |
| Delete | Yes | Removes files/directories |
| Bash | Optional | Executes system commands |
| Read, Grep, Tree | No | Read-only operations |
| WebSearch, WebFetch | No | External read-only |
| A2A Tools | No | Agent delegation |

### Approval Configuration

Configure approval requirements per tool:

```bash
# Enable/disable approval for specific tools
infer config tools safety enable    # Global approval
infer config tools bash enable       # Enable bash tool
```

Or via configuration file:

```yaml
tools:
  safety:
    require_approval: true  # Global default
  write:
    require_approval: true
  bash:
    require_approval: false  # Override for bash
```

### Approval UI Controls

- **y / Enter** - Approve execution
- **n / Esc** - Reject execution
- **a** - Auto-approve (disables approval for session)

## Shortcuts

The CLI provides an extensible shortcuts system for quickly executing common commands with `/shortcut-name` syntax.

### Built-in Shortcuts

**Core:**

- `/clear` - Clear conversation history
- `/exit` - Exit chat session
- `/help [shortcut]` - Show available shortcuts
- `/switch` - Switch to different model
- `/theme` - Switch chat theme
- `/cost` - Show session cost breakdown with per-model details
- `/compact` - Compact conversation
- `/export` - Export conversation

**Git Shortcuts** (created by `infer init`):

- `/git-status` - Show working tree status
- `/git-commit` - Generate AI commit message from staged changes
- `/git-push` - Push commits to remote
- `/git-log` - Show commit logs

**SCM Shortcuts** (GitHub integration):

- `/scm-issues` - List GitHub issues
- `/scm-issue <number>` - Show issue details
- `/scm-pr-create [context]` - Generate AI-powered PR plan

### AI-Powered Snippets

Create shortcuts that use LLMs to transform data:

```yaml
# .infer/shortcuts/custom-example.yaml
shortcuts:
  - name: analyze-diff
    description: "Analyze git diff with AI"
    command: bash
    args:
      - -c
      - |
        diff=$(git diff)
        jq -n --arg diff "$diff" '{diff: $diff}'
    snippet:
      prompt: |
        Analyze this diff and suggest improvements:
        ```diff
        {diff}
        ```
      template: |
        ## Analysis
        {llm}
```

### Custom Shortcuts

Create custom shortcuts by adding YAML files to `.infer/shortcuts/`:

```yaml
# .infer/shortcuts/custom-dev.yaml
shortcuts:
  - name: tests
    description: "Run all tests"
    command: go
    args:
      - test
      - ./...

  - name: build
    description: "Build the project"
    command: go
    args:
      - build
      - -o
      - infer
      - .
```

Use with `/tests` or `/build`.

For complete shortcuts documentation, including advanced features and examples, see [Shortcuts Guide](docs/shortcuts-guide.md).

## Global Flags

- `-v, --verbose`: Enable verbose output
- `--config <path>`: Specify custom config file path

## Examples

### Basic Workflow

```bash
# Initialize project
infer init

# Start interactive chat
infer chat

# Execute autonomous task
infer agent "Fix the bug in issue #42"

# Check gateway status
infer status
```

### Working on a GitHub Issue

```bash
# Start chat
infer chat

# In chat, use shortcuts to get context
/scm-issue 123

# Discuss with AI, let it use tools to:
# - Read files
# - Search codebase
# - Make changes
# - Run tests

# Generate PR plan when ready
/scm-pr-create Fixes the authentication timeout issue
```

### Configuration Example

```bash
# Set default model
infer config agent set-model "deepseek/deepseek-chat"

# Enable bash tool
infer config tools bash enable

# Configure web search
infer config tools web-search enable

# Check current configuration
infer config show
```

## Development

For development, use [Task](https://taskfile.dev) for build automation:

```bash
task dev   # Format, build, and test
task build # Build binary
task test  # Run tests
```

See [CLAUDE.md](CLAUDE.md) for detailed development documentation.

## License

MIT License - see [LICENSE](LICENSE) file for details.
