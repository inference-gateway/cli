<div align="center">

# Inference Gateway CLI

[![Go Version](https://img.shields.io/github/go-mod/go-version/inference-gateway/cli?style=flat-square&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg?style=flat-square)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/inference-gateway/cli/ci.yml?style=flat-square&logo=github)](https://github.com/inference-gateway/cli/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/inference-gateway/cli?style=flat-square)](https://goreportcard.com/report/github.com/inference-gateway/cli)
[![Release](https://img.shields.io/github/v/release/inference-gateway/cli?style=flat-square&logo=github)](https://github.com/inference-gateway/cli/releases)

An agentic command-line assistant that writes code, understands project context, and uses tools to perform real tasks.

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
- [Claude Code Mode (Subscription)](#claude-code-mode-subscription)
- [Commands](#commands)
- [Tools for LLMs](#tools-for-llms)
- [Configuration](#configuration)
- [Cost Tracking](#cost-tracking)
- [Tool Approval System](#tool-approval-system)
- [Shortcuts](#shortcuts)
- [Channels (Remote Messaging)](#channels-remote-messaging)
- [Heartbeat (Periodic Wake-Up)](#heartbeat-periodic-wake-up)
- [Agent Skills](#agent-skills)
- [Computer Use](#computer-use)
- [Persistent Memory](#persistent-memory)
- [Reminders & Command Hooks](#reminders--command-hooks)
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
  - [Database Migrations](docs/database-migrations.md) - Schema migration system for the SQLite/Postgres backends
- **Conversation Versioning**: Navigate back in time to previous conversation points (double ESC)
  - View message history with timestamps
  - Restore conversation to any previous user message
  - Permanent deletion of messages after restore point
  - [Learn more →](docs/conversation-versioning.md)
- **Configuration Management**: Manage gateway settings via YAML config
- **Project Initialization**: Set up local project configurations
- **Tool Execution**: LLMs can execute allowed commands and tools - [See all tools →](docs/tools-reference.md)
- **Tool Approval System**: User approval workflow for sensitive operations with real-time diff visualization
- **Agent Modes**: Three operational modes for different workflows:
  - **Standard Mode** (default): Normal operation with all configured tools and approval checks
  - **Plan Mode**: Read-only mode for planning and analysis without execution - [Learn more →](docs/plan-mode.md)
  - **Auto-Accept Mode**: All tools auto-approved for rapid execution (YOLO mode)
  - Toggle between modes with **Shift+Tab**
- **Token Usage Tracking**: Accurate token counting with polyfill support for providers that don't return usage metrics
- **Cost Tracking**: Real-time cost calculation for API usage with per-model breakdown and configurable pricing
- **Inline History Auto-Completion**: Smart command history suggestions with inline completion
- **GitHub Issue References (`#`)**: Type `#` in chat to open a dropdown of the current
  repo's open issues. Selecting one inserts a `#N` token that is highlighted in the input
  and, on submit, expanded inline into the issue's title, body, and recent comments - so
  the agent works from full context without a redundant `gh issue view` lookup. Gracefully
  no-ops when `gh` is not installed or the repo has no remote.
- **Customizable Keybindings**: Fully configurable keyboard shortcuts for the chat interface
- **Selectable Status Indicators**: Press `↓` in chat to select the indicators below the input and
  open the matching view with `enter` (model → model selection, theme → theme selection,
  `A2A:` → registered agents, `Tools:` → available tools, `⚙` jobs → task management)
- **Model Thinking Visualization**: When models use extended thinking,
  their internal reasoning process is displayed as collapsible blocks above responses (toggle with **ctrl+k** by default, configurable via `display_toggle_thinking`)
- **Extensible Shortcuts System**: Create custom commands with AI-powered snippets - [Learn more →](docs/shortcuts-guide.md)
- **MCP Server Support**: Direct integration with Model Context Protocol servers for extended tool capabilities -
  [Learn more →](docs/mcp-integration.md)
- **Web Terminal Interface**: Browser-based terminal access with tabbed sessions for remote access and multi-session workflows - [Learn more →](docs/web-terminal.md)
- **Remote Messaging Channels**: Control the agent from Telegram, WhatsApp, and other platforms via a pluggable channel system - [Learn more →](docs/channels.md)
- **Speech-to-Text (Whisper)**: Dictate into chat with `/voice` and transcribe inbound Telegram voice messages, locally and offline -
  off by default - [Learn more →](docs/speech-to-text.md)
- **Scheduled Tasks**: Ask the agent (over Telegram, etc.) to run a prompt on a cron schedule and deliver the result back through the same channel -
  recurring ("send me a quote every morning") or one-off ("remind me at 6pm today") - [Learn more →](docs/scheduling.md)
- **Heartbeat (Periodic Wake-Up)**: Wake the agent on a fixed interval to check for pending todos and background work,
  with a separate configurable system prompt - off by default - [Learn more →](docs/heartbeat.md)
- **Agent Skills**: Drop-in `SKILL.md` instruction folders the agent discovers and loads on demand;
  install them straight from GitHub with `infer skills install` - on by default - [Learn more →](docs/skills.md)
- **Persistent Memory**: Cross-session memory stored as individual Markdown fact-files with an
  auto-maintained `MEMORY.md` index that is injected at session start - on by default - [Learn more →](#persistent-memory)
- **Subagents**: Spawn parallel `infer agent` subprocesses from chat with the `Agent` tool to fan out
  independent work (research, edits, investigations) and fold their results back into the conversation
- **Computer Use**: Let the agent drive the desktop - mouse, keyboard, screenshots, app focus - across
  macOS, X11, and Wayland - off by default - [Learn more →](#computer-use)
- **Reminders & Command Hooks**: Inject system reminders or run shell commands at agent-loop hook
  points to enforce project conventions - [Learn more →](#reminders--command-hooks)

## Installation

### Using npm/npx (Recommended)

If you already have Node.js (>= 18), run the CLI with npx - no Go toolchain or manual
download required. The matching native binary is fetched and cached on first use:

```bash
# Run without installing
npx @inference-gateway/cli@latest --help
npx @inference-gateway/cli@latest chat
```

Or install it globally:

```bash
npm install -g @inference-gateway/cli
infer --help
```

> **Not recommended for production.** For production or CI, prefer the
> [install script](#using-install-script), [container image](#using-container-image), or
> [building from source](#build-from-source). Prebuilt binaries cover Linux, macOS, and Windows on
> amd64/arm64.

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

**Linux/macOS:**

```bash
# Latest version
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash

# Specific version
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --version v0.77.0

# Custom installation directory
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --install-dir $HOME/.local/bin
```

**Windows (PowerShell 5.1+ / pwsh):**

```powershell
# Latest version
.\install.ps1

# Specific version
.\install.ps1 -Version v0.1.0

# Custom installation directory
$env:INSTALL_DIR = "C:\tools"; .\install.ps1
```

Or run directly from GitHub:

```powershell
# Download and run
iex ((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/inference-gateway/cli/main/install.ps1'))
```

### Manual Download

Download the latest release binary for your platform from the [releases page](https://github.com/inference-gateway/cli/releases).

Available binaries:

| Platform | Binary |
| -------- | ------ |
| Linux amd64 | `infer-linux-amd64` |
| Linux arm64 | `infer-linux-arm64` |
| macOS amd64 (Intel) | `infer-darwin-amd64` |
| macOS arm64 (Apple Silicon) | `infer-darwin-arm64` |
| Windows amd64 | `infer-windows-amd64.exe` |
| Windows arm64 | `infer-windows-arm64.exe` |

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

For advanced verification with Cosign signatures, see [Binary Verification Guide](docs/binary-verification.md).

### Build from Source

```bash
git clone https://github.com/inference-gateway/cli.git
cd cli
go build -o infer cmd/infer/main.go
sudo mv infer /usr/local/bin/
```

On Windows, build with:

```powershell
git clone https://github.com/inference-gateway/cli.git
cd cli
go build -o infer.exe cmd/infer/main.go
# The binary is at .\infer.exe
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
- **[Directory Structure](docs/directory-structure.md)** - Map of every file and subdirectory the CLI creates under `.infer/` and `~/.infer/`
- **[Web Terminal](docs/web-terminal.md)** - Browser-based terminal interface
- **[Shortcuts Guide](docs/shortcuts-guide.md)** - Custom shortcuts and AI-powered snippets
- **[A2A Agents](docs/agents-configuration.md)** - Agent-to-agent communication setup

## Claude Code Mode (Subscription)

Save on API costs by using your Claude Max or Pro subscription instead of pay-as-you-go API pricing.

### Overview

Claude Code mode enables you to use your **Claude Max or Pro subscription** ($100-200/month fixed cost)
instead of paying per token via the Anthropic API. This is ideal for heavy users who want
predictable monthly costs.

**Cost Comparison:**

| Mode                 | Pricing                                 | Best For                                |
| -------------------- | --------------------------------------- | --------------------------------------- |
| **Gateway Mode**     | Pay per token ($3-$75 per million)      | API users, multi-provider needs         |
| **Claude Code Mode** | Fixed monthly ($100-200)                | Heavy Claude users, cost predictability |

### Prerequisites

- **Claude Max or Pro subscription** - Required ($100-200/month)
- **Claude Code CLI** - Official CLI from Anthropic

Install the Claude Code CLI:

```bash
npm install -g @anthropic-ai/claude-code
```

### Setup

1. **Configure for Claude Code mode**:

Edit `.infer/config.yaml`:

```yaml
# Enable Claude Code mode
claude_code:
  enabled: true
  cli_path: claude  # or /usr/local/bin/claude if not in PATH
  timeout: 600
  max_output_tokens: 32000
  thinking_budget: 10000

# Disable gateway mode
gateway:
  run: false

# Set model (no provider prefix needed)
agent:
  model: claude-sonnet-4-5-20250929
```

2. **Authenticate with your subscription**:

```bash
infer claude-code setup
```

This opens your browser to authenticate with your Claude Max/Pro account.

3. **Verify authentication**:

```bash
infer claude-code test
```

4. **Use normally**:

```bash
infer chat  # Now using your subscription!
```

### Available Commands

- `infer claude-code setup` - Authenticate with Claude subscription
- `infer claude-code test` - Test authentication and CLI integration

### Configuration Options

```yaml
claude_code:
  enabled: true                  # Enable/disable Claude Code mode
  cli_path: claude               # Path to claude binary
  timeout: 600                   # Command timeout in seconds
  max_output_tokens: 32000       # Maximum output tokens per request
  thinking_budget: 10000         # Token budget for extended thinking
  extra_args:                    # Extra arguments appended verbatim to the claude CLI invocation
    - --max-turns
    - "5"
```

**Environment Variables:**

```bash
export INFER_CLAUDE_CODE_ENABLED=true
export INFER_CLAUDE_CODE_CLI_PATH=/usr/local/bin/claude
export INFER_CLAUDE_CODE_TIMEOUT=600
export INFER_CLAUDE_CODE_EXTRA_ARGS="--max-turns,5"  # comma/newline-separated; wins over --claude-code-extra-args
```

**Pass-through behavior:**

Claude Code mode is a pure pass-through: infer does not inject its system prompt, context blocks, or
system reminders, and does not re-execute claude's tool calls locally - claude runs with its own
defaults and native tools. Infer's `prompts.yaml` and `reminders.yaml` do not apply in this mode.

To add instructions on top of claude's built-in system prompt (passed via `--append-system-prompt`),
set the dedicated prompt in `.infer/prompts.yaml` (empty by default):

```yaml
agent:
  system_prompt_claude_code: "Always answer in English."
```

Or via environment variable:

```bash
export INFER_PROMPTS_AGENT_SYSTEM_PROMPT_CLAUDE_CODE="Always answer in English."
```

### Features and Limitations

| Feature               | Gateway Mode                            | Claude Code Mode                         |
| --------------------- | --------------------------------------- | ---------------------------------------- |
| **Cost**              | Pay-per-token                           | Fixed monthly                            |
| **Providers**         | All providers (Anthropic, OpenAI, etc.) | Claude only                              |
| **Models**            | All provider models                     | Claude models only                       |
| **Images**            | ✓ Supported                             | ✗ Not supported (stripped from messages) |
| **Prompt Caching**    | ✓ Supported                             | ✗ Not available via CLI                  |
| **Streaming**         | ✓ Supported                             | ✓ Supported                              |
| **Tool Execution**    | ✓ Supported                             | ✓ Supported                              |
| **Extended Thinking** | ✓ Supported                             | ✓ Supported                              |
| **Authentication**    | API keys                                | Browser login                            |

**Supported Models:**

The following Claude models are available via Claude Code subscription mode:

**Claude 4.5 Series (Latest):**

- `claude-opus-4-5` - Most capable Claude model (vision support)
- `claude-haiku-4-5-20251001` - Fastest Claude model (vision support)
- `claude-sonnet-4-5-20250929` - Latest Sonnet model (default, vision support)

**Claude 4.1 Series:**

- `claude-opus-4-1-20250805` - Claude 4.1 Opus (vision support)
- `claude-sonnet-4-1-20250805` - Claude 4.1 Sonnet (vision support)

**Claude 4 Series:**

- `claude-opus-4-20250514` - Claude 4 Opus (vision support)
- `claude-sonnet-4-20250514` - Claude 4 Sonnet (vision support)

**Claude 3.7 Series:**

- `claude-3-7-sonnet-20250219` - Claude 3.7 Sonnet (vision support)

**Claude 3.5 Series:**

- `claude-3-5-haiku-20241022` - Claude 3.5 Haiku (vision support)

**Claude 3 Series:**

- `claude-3-haiku-20240307` - Claude 3 Haiku (vision support)
- `claude-3-opus-20240229` - Claude 3 Opus (vision support)

**Note:** All modern Claude models support vision capabilities. The Claude Code CLI automatically strips images
from messages when using subscription mode.

### Troubleshooting

**CLI Not Found:**

```bash
# Check if Claude CLI is installed
which claude

# If not found, install it
npm install -g @anthropic-ai/claude-code

# Or set custom path in config
claude_code:
  cli_path: /full/path/to/claude
```

**Authentication Issues:**

```bash
# Re-authenticate
infer claude-code setup

# Test authentication
infer claude-code test
```

**Update CLI:**

```bash
npm update -g @anthropic-ai/claude-code
```

### Switching Between Modes

You can easily switch between gateway and Claude Code modes:

**To Claude Code mode:**

```yaml
# .infer/config.yaml
claude_code:
  enabled: true
gateway:
  run: false
agent:
  model: claude-sonnet-4-5-20250929  # No provider prefix
```

**To Gateway mode:**

```yaml
# .infer/config.yaml
claude_code:
  enabled: false
gateway:
  run: true
agent:
  model: anthropic/claude-sonnet-4-5-20250929  # With provider prefix
```

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
# Terminal mode (default)
infer chat

# Resume a previous chat session
infer conversations list  # Find session IDs
infer chat --session-id abc-123-def

# Web terminal mode with browser interface
infer chat --web
infer chat --web --port 8080  # Custom port
```

**Features:** Model selection, real-time streaming, scrollable history, three agent modes (Standard/Plan/Auto-Accept).

**Web Mode Features:**

- Browser-based terminal using xterm.js
- Multiple independent tabbed sessions
- Automatic session cleanup on inactivity
- Each tab manages its own `infer chat` process with isolated containers
- Access from any device on the network
- Responsive terminal sizing with horizontal padding

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
# Read any value (effective config: defaults + ~/.infer + .infer + env)
infer config get agent.model
infer config get                       # dump the whole effective config

# Agent configuration
infer config set agent.model "deepseek/deepseek-v4-pro"
infer config set agent.max_turns 100
infer config set agent.verbose_tools true

# Tool management
infer config set tools.enabled true
infer config set tools.bash.enabled true
infer config set tools.safety.require_approval true

# Export configuration
infer config set export.summary_model "anthropic/claude-4.1-haiku"

# Write to userspace (~/.infer/config.yaml) instead of the project
infer config set agent.model "openai/gpt-4o" --userspace
```

> System prompts live in `prompts.yaml` (e.g. `prompts.agent.system_prompt`), not
> in `config.yaml`, so they are edited there rather than via `config set`.

See [Commands Reference](docs/commands-reference.md#configuration-management) for all configuration options.

### Agent Management

**`infer agents`** - Manage A2A (Agent-to-Agent) agent configurations

```bash
infer agents init                    # Initialize agents configuration
infer agents add browser-agent       # Add an agent from the registry with defaults
infer agents add custom https://...  # Add a custom agent
infer agents list                    # List all agents
```

For detailed A2A setup, see [A2A Agents Configuration](docs/agents-configuration.md); for how
connections are established and tasks polled, see [A2A Connections](docs/a2a-connections.md).

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

infer conversations show <session-id>                  # Show a conversation's entries
infer conversations show <session-id> --include-hidden # Include hidden entries (e.g. system reminders)
infer conversations show <session-id> --format json    # One JSON object per line (jq-friendly)
```

**`infer conversation-title`** - Manage AI-powered conversation titles

```bash
infer conversation-title generate  # Generate titles for all conversations
infer conversation-title status    # Show generation status
```

**`infer skills`** - Manage Agent Skills (reusable `SKILL.md` instruction folders)

```bash
infer skills list                                # List discovered skills
infer skills install skill-creator               # Install a skill from GitHub
infer skills install acme/internal-comms --user  # Install to ~/.infer/skills
infer skills uninstall pdf                        # Remove a skill by name
```

See [Agent Skills](#agent-skills) and [docs/skills.md](docs/skills.md) for the format.

**`infer export`** - Export a conversation to a Markdown file

```bash
infer conversations list      # Find the session ID
infer export <session-id>     # Writes .infer/chat_export_<timestamp>.md
```

**`infer version`** - Display CLI version information

```bash
infer version
```

## Tools for LLMs

When tool execution is enabled, LLMs can use various tools to interact with your system. Below is a
summary of available tools. For detailed documentation, parameters, and examples, see
[Tools Reference](docs/tools-reference.md).

Tools are grouped by category below. Many are gated behind a config flag (noted per group);
the always-available set is registered for every session. There is **no built-in GitHub tool** -
use the `gh` CLI through Bash (or the built-in `/scm` shortcuts) for GitHub operations.

**Core file & search** (always available):

| Tool | Purpose | Approval |
| ------ | --------- | ---------- |
| **Read** | Read file contents with line ranges | No |
| **Write** | Write content to files | Yes |
| **Edit** | Exact string replacements in files | Yes |
| **MultiEdit** | Multiple atomic edits to a single file | Yes |
| **Delete** | Delete files and directories | Yes |
| **Grep** | Search files with regex (ripgrep/Go) | No |
| **Tree** | Display directory structure | No |

**Shell** (Bash is always available; the background-shell trio needs `tools.bash.background_shells.enabled`):

| Tool | Purpose | Approval |
| ------ | --------- | ---------- |
| **Bash** | Execute shell commands (per-mode allow-list) | Optional |
| **BashOutput** | Read new output from a running background shell | Yes |
| **KillShell** | Terminate a background shell | Yes |
| **ListShells** | List background shells and their state | Yes |

**Task & planning** (`AskUserQuestion` needs `tools.ask_user_question.enabled`):

| Tool | Purpose | Approval |
| ------ | --------- | ---------- |
| **TodoWrite** | Create and manage task lists | No |
| **RequestPlanApproval** | Submit a plan for approval and persist it (plan mode) | No |
| **AskUserQuestion** | Ask the user multiple-choice questions (plan mode) | No |

**Web** (`WebSearch`/`WebFetch` need their respective config flag):

| Tool | Purpose | Approval |
| ------ | --------- | ---------- |
| **WebSearch** | Search the web (DuckDuckGo/Google) | Yes |
| **WebFetch** | Fetch content from a URL | Yes |

**Subagents** (the `Agent` tool and its companions, enabled by default):

| Tool | Purpose | Approval |
| ------ | --------- | ---------- |
| **Agent** | Spawn an `infer agent` subprocess to run work in parallel | Yes |
| **ListSubagents** | List spawned subagents and their status | No |
| **GetSubagentResult** | Re-read a finished subagent's last message | No |
| **ReadSubagentScreen** | Capture an interactive subagent's terminal screen | No |
| **SendSubagentInput** | Type into an interactive subagent's TUI | Yes |
| **CloseSubagent** | Stop a subagent or tidy a finished pane | Yes |
| **ApproveSubagent** | Relay an approval decision to a waiting subagent | Yes |

**Computer Use** (require `computer_use.enabled`; these bypass the approval prompt and run silently):

| Tool | Purpose | Approval |
| ------ | --------- | ---------- |
| **MouseMove** / **MouseClick** / **MouseScroll** | Control the mouse | No |
| **KeyboardType** | Type text or send key combinations | No |
| **GetFocusedApp** / **ActivateApp** | Query or focus an application | No |
| **GetLatestScreenshot** | Read the latest streamed screenshot | No |

**Memory, scheduling & A2A** (each gated by its own flag):

| Tool | Purpose | Approval | Enabled by |
| ------ | --------- | ---------- | ------------ |
| **Memory** | Persistent, cross-session fact storage | No | `memory.enabled` (default on) |
| **Schedule** | Cron-driven recurring/one-off tasks via the originating channel | Yes | `tools.schedule.enabled` |
| **A2A_SubmitTask** | Submit a task to an A2A agent | Yes | A2A enabled |
| **A2A_QueryAgent** | Query an A2A agent's capabilities | No | A2A enabled |
| **A2A_QueryTask** | Check an A2A task's status | No | A2A enabled |

> **Approval** reflects the default policy. The global default is `tools.safety.require_approval: true`,
> so a tool is **No** only where it is explicitly exempt in code (read-only file/search tools, Memory,
> the plan/question tools, subagent reads, and computer-use). Bash is governed instead by the per-mode
> bash allow-list. Override any tool with `tools.<name>.require_approval`.
>
> **MCP tools** are not listed here - they are discovered and registered dynamically at runtime from your
> configured MCP servers and surface as `MCP_<server>_<tool>` (see [MCP Integration](docs/mcp-integration.md)).

**Tool Configuration:**

Tools can be enabled/disabled and configured individually:

```bash
# Enable/disable specific tools
infer config set tools.bash.enabled true
infer config set tools.write.enabled true

# Configure tool settings
infer config set tools.grep.backend ripgrep
# List values are comma-separated and replace the whole list
infer config set tools.web_fetch.allowed_domains "example.com,github.com"
```

**Customising Tool Descriptions:**

The description each tool exposes to the LLM is configurable in
`.infer/prompts.yaml` under the `tools` key - useful when a model
misinterprets a default or when you want to nudge usage:

```yaml
# .infer/prompts.yaml
tools:
  Bash:
    description: |-
      Execute allowed bash commands securely. Only pre-approved
      commands from the allowed list can be executed.
  Read:
    description: |-
      Reads a file from the local filesystem. Always prefer reading
      whole files unless the file is very large.
```

Any tool you omit falls back to the in-code default. Env-var override:
`INFER_PROMPTS_TOOLS_<UPPER_SNAKE_NAME>_DESCRIPTION` (e.g.
`INFER_PROMPTS_TOOLS_BASH_DESCRIPTION`). MCP tool descriptions are not
configurable here - they come from the MCP server at runtime.

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
  model: "deepseek/deepseek-v4-pro"
  system_prompt: "You are a helpful assistant"  # Base identity
  custom_instructions: ""  # Additional instructions appended to system prompt
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
infer config set agent.model "deepseek/deepseek-v4-pro"

# Or via command flag
infer chat --model "anthropic/claude-4"
```

### Key Configuration Options

- **gateway.url** - Gateway URL (default: `http://localhost:8080`)
- **gateway.docker** - Use Docker mode vs binary mode (default: `true`)
- **tools.enabled** - Enable/disable all tools (default: `true`)
- **agent.model** - Default model for agent operations
- **agent.system_prompt** - Base identity for the agent (e.g., `"You are a helpful assistant"`)
- **agent.custom_instructions** - Additional instructions appended after the system prompt
- **agent.max_turns** - Maximum turns for agent sessions (default: `50`)
- **chat.theme** - Chat interface theme (default: `tokyo-night`)
- **chat.status_bar.enabled** - Enable/disable status bar (default: `true`)
- **chat.status_bar.indicators** - Configure individual status indicators (all enabled by default except `max_output`)
- **web.enabled** - Enable web terminal mode (default: `false`)
- **web.port** - Web server port (default: `3000`)
- **web.host** - Web server host (default: `localhost`)
- **web.session_inactivity_mins** - Session timeout in minutes (default: `5`)

### Environment Variables

All configuration can be set via environment variables with the `INFER_` prefix:

```bash
export INFER_GATEWAY_URL="http://localhost:8080"
export INFER_AGENT_MODEL="deepseek/deepseek-v4-pro"
export INFER_TOOLS_BASH_ENABLED=true
export INFER_CHAT_THEME="tokyo-night"

# Web terminal configuration
export INFER_WEB_PORT=3000
export INFER_WEB_HOST="localhost"
export INFER_WEB_SESSION_INACTIVITY_MINS=5
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

The model picker groups models into three categories you can filter with the
`[1] All` / `[2] Free` / `[3] Paid` / `[4] Pro` tabs:

- **Free** - no per-token cost (e.g. local Ollama, Gemma).
- **Paid** - billed per token at the listed `$input/$output per MTok` rate.
- **Pro** - gated behind a paid **Pro subscription** (some Ollama Cloud models).
  These have no per-token price but are not free, so they are marked
  `pro subscription` instead of `free` to avoid the misleading label.

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

    # Mark a model as Pro-subscription only (no per-token cost, but gated)
    "ollama_cloud/deepseek-v4-pro":
      input_price_per_mtoken: 0.0
      output_price_per_mtoken: 0.0
      requires_pro: true
```

> **Note:** A custom entry fully replaces the default for that model. Omitting
> `requires_pro` in a custom override resets it to `false`, so re-state
> `requires_pro: true` if you override a model the CLI flags as Pro by default.

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

The global default is `tools.safety.require_approval: true`, so **any tool not explicitly exempt requires
approval**; override per tool with `tools.<name>.require_approval`.

| Tool | Requires Approval | Reason |
| ------ | ------------------- | --------- |
| Write, Edit, MultiEdit, Delete | Yes | Create / modify / remove files |
| Schedule, Agent | Yes | Side effects (scheduled jobs, spawned subprocesses) |
| WebSearch, WebFetch | Yes | Make external requests (global default) |
| A2A_SubmitTask | Yes | Dispatches work to another agent |
| Bash | Optional | Governed by the per-mode bash allow-list |
| Read, Grep, Tree | No | Read-only operations |
| Memory, TodoWrite | No | Local agent state (explicitly exempt) |
| Computer-use tools | No | Run silently in the background |
| A2A_QueryAgent, A2A_QueryTask | No | Read-only A2A queries |

### Approval Configuration

Configure approval requirements per tool:

```bash
# Enable/disable approval for specific tools
infer config set tools.safety.require_approval true   # Global approval
infer config set tools.bash.enabled true              # Enable bash tool
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

**Subcommands:** Shortcuts can have subcommands for organized command groups (e.g., `/git status`,
`/scm issues`). This allows related operations to be grouped under a single shortcut name with multiple
actions.

### Built-in Shortcuts

**Conversation & session:**

- `/new [title]` - Start a new conversation (optionally titled)
- `/clear` - Save the current conversation and start a new one
- `/compact` - Save the conversation and start a new session seeded with a summary
- `/conversations` - Open the conversation selection dropdown
- `/context` - Show context-window usage
- `/cost` - Show session cost breakdown with per-model details
- `/copy [text|markdown|json]` - Copy the conversation to the clipboard (aliases: `txt`, `md`)
- `/model [model-name] [prompt]` - Switch model, or run a single prompt against a specific model then restore
- `/theme` - Switch chat theme
- `/voice [seconds]` - Record from the microphone and transcribe to the input with Whisper (requires `speech_to_text.enabled`)
- `/help [shortcut]` - Show available shortcuts
- `/exit` - Exit the chat session

**Panels & views:**

- `/diff` - Open the changes panel (interactive diff viewer)
- `/explorer` - Open the file explorer (tree + fuzzy finder) - [Learn more →](docs/explorer.md)
- `/tools` - Show the tools available to the agent (read-only, filterable list)
- `/a2a` - Show registered A2A agents and their status (requires A2A)
- `/tasks` - Show the A2A task-management interface (requires A2A) - [Learn more →](docs/tasks-management.md)
- `/release-notes [version]` - Show release notes from GitHub Releases (latest, or a specific version)

**Project setup:**

- `/init` - Generate an `AGENTS.md` by analyzing the project
- `/init-github-action` - Set up a GitHub Action via an interactive wizard

**Git Shortcuts** (created by `infer init`):

- `/git status` - Show working tree status
- `/git commit` - Generate AI commit message from staged changes
- `/git push` - Push commits to remote
- `/git log` - Show commit logs

**SCM Shortcuts** (GitHub integration):

- `/scm issues` - List GitHub issues
- `/scm issue <number>` - Show issue details
- `/scm pr-create [context]` - Generate AI-powered PR plan

**Other Shortcuts** (created by `infer init`):

- `/mcp [list|add|remove|enable|disable]` - Manage MCP servers
- `/shells` - List running and recent background shell processes
- `/export` - Export the current conversation to markdown
- `/env` - Generate a `.env.example` with all provider API keys
- `/agents [list|add|remove|enable|disable]` - Manage A2A agents
- `/skills [list|install|uninstall]` - Manage Agent Skills

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

**With Subcommands:**

```yaml
# .infer/shortcuts/custom-docker.yaml
shortcuts:
  - name: docker
    description: "Docker operations"
    command: docker
    subcommands:
      - name: build
        description: "Build Docker image"
        args:
          - build
          - -t
          - myapp
          - .
      - name: run
        description: "Run Docker container"
        args:
          - run
          - -p
          - "8080:8080"
          - myapp
```

Use as: `/docker build` or `/docker run`

Use with `/tests` or `/build`.

For complete shortcuts documentation, including advanced features and examples, see [Shortcuts Guide](docs/shortcuts-guide.md).

## Channels (Remote Messaging)

Control the agent remotely from messaging platforms like Telegram or
WhatsApp. Messages sent to a bot are forwarded to the agent, and the
agent's responses are sent back through the same platform.

### Setup (Telegram)

**1. Create a Telegram bot** by messaging [@BotFather](https://t.me/BotFather) and sending `/newbot`. Copy the bot token.

**2. Get your chat ID** by messaging your bot, then visiting:

```text
https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates
```

Find `"chat":{"id":123456789}` in the response.

**3. Configure** in `.infer/channels.yaml` (seeded by `infer init`):

```yaml
---
enabled: true

telegram:
  enabled: true
  bot_token: "${INFER_CHANNELS_TELEGRAM_BOT_TOKEN}"
  allowed_users:
    - "123456789"
  poll_timeout: 30
```

Or via environment variables:

```bash
export INFER_CHANNELS_ENABLED=true
export INFER_CHANNELS_TELEGRAM_ENABLED=true
export INFER_CHANNELS_TELEGRAM_BOT_TOKEN="123456:ABC-DEF..."
export INFER_CHANNELS_TELEGRAM_ALLOWED_USERS="123456789"
```

**4. Start the channel listener:**

```bash
infer channels-manager
```

**5. Send a message** to your bot in Telegram - the agent will respond.

Each incoming message triggers `infer agent --session-id <id>` as a
subprocess with a persistent session per sender.

### Tool Approval

By default, sensitive tools (Write, Edit, Delete, Bash) require user approval
before executing. The agent sends an approval prompt to the channel and waits
for the user to reply "yes" or "no". Read-only tools (Read, Grep, Tree) execute
without approval.

This reuses the existing `tools.*.require_approval` configuration. To disable, set in `.infer/channels.yaml`:

```yaml
require_approval: false  # default: true
```

Or: `INFER_CHANNELS_REQUIRE_APPROVAL=false`

Approvals time out after 5 minutes and are automatically rejected.

### Security

- **Allowlist-only access**: Only chat IDs in `allowed_users` can interact with the agent
- **Empty allowlist = reject all**: If no users are configured, all messages are rejected (secure by default)
- **Per-channel allowlists**: Each channel (Telegram, WhatsApp) has its own independent allowlist
- **Tool approval by default**: Sensitive tools require explicit user approval before executing
- **Use environment variables** for tokens - never commit secrets to config files

### Supported Channels

| Channel  | Status    | Transport                   |
| -------- | --------- | --------------------------- |
| Telegram | Available | Long-polling (Bot API)      |
| WhatsApp | Planned   | Webhook (Meta Business API) |

For a complete working example with Docker Compose, see [examples/telegram-channel](examples/telegram-channel/).

For detailed documentation including custom channel development, see [Channels Documentation](docs/channels.md).

### Scheduled Tasks

When the channels-manager daemon is running, you can ask the bot to schedule
prompts on a cron schedule. The agent's response is delivered back through
the originating channel (e.g. the Telegram chat where you set it up).

> *"Send me an inspiring quote every day at 8 AM"* - recurring
> *"Remind me at 6pm today to call mum"* - one-off (deletes itself after firing)

Enable in `.infer/config.yaml`:

```yaml
tools:
  schedule:
    enabled: true               # disabled by default
    require_approval: true      # default; recommended
```

Or via env var: `INFER_TOOLS_SCHEDULE_ENABLED=true`.

Jobs are persisted as YAML in `~/.infer/schedules/<id>.yaml` and hot-reloaded
by the daemon (no restart needed). Channel + recipient are derived
automatically from the session - the LLM never has to guess them.

Container deployments must set `TZ=Europe/Berlin` (or your zone) so cron
expressions are interpreted in local time. The binary embeds the IANA zone
database so this works on any base image.

For the full guide, including the cron syntax primer and end-to-end Telegram
walkthroughs, see [Scheduling Documentation](docs/scheduling.md).

## Heartbeat (Periodic Wake-Up)

Heartbeat wakes the agent on a fixed interval - without any user input -
so it can check for pending todos, background tasks, or anything else
your system prompt tells it to monitor. It runs alongside the scheduler
inside the `infer channels-manager` daemon and is **disabled by default**.

Unlike the [Schedule](docs/scheduling.md) tool (which the LLM uses to
create user-driven cron jobs that deliver to a channel), heartbeat is a
single global tick the operator configures once. Output goes to logs;
the agent itself decides whether to send a Telegram message, open a PR,
or just no-op.

Enable in `.infer/heartbeat.yaml` (seeded by `infer init`):

```yaml
---
enabled: true
interval: 1h            # Go duration: 30s, 5m, 1h, 24h
initial_delay: 1m       # delay before first tick
model: ""               # optional override; empty = agent.model
prompt: "Heartbeat tick - check for any pending tasks, todos, or background work and act on them."
```

The **system prompt** for heartbeat runs lives in `.infer/prompts.yaml`
under `agent.system_prompt_heartbeat` so you can tune the agent's
wake-up behaviour separately from chat-mode behaviour.

Then start the daemon:

```bash
infer channels-manager
```

Heartbeat alone is a valid run mode - you don't need any channel
enabled to use it. The daemon hosts whichever of channels / scheduler /
heartbeat are turned on.

Or via env vars:

```bash
export INFER_HEARTBEAT_ENABLED=true
export INFER_HEARTBEAT_INTERVAL=30m
```

For the full guide, including configuration reference and common
patterns (TODO sweeps, CI watchdogs), see [Heartbeat Documentation](docs/heartbeat.md).

## Agent Skills

Agent Skills are reusable, model-readable instruction folders. Each skill is a folder containing a
`SKILL.md` file with YAML frontmatter (`name`, `description`) - the same contract used by the open
`.agents/skills/` standard, so existing skill folders drop in unchanged. The agent discovers skills at
startup and loads a skill's full instructions on demand when they are relevant.

Skills are scanned from three locations (highest precedence first; first match wins on a name collision):

- `.infer/skills/<name>/SKILL.md` - project
- `.agents/skills/<name>/SKILL.md` - open standard
- `~/.infer/skills/<name>/SKILL.md` - user-global

Skills are **enabled by default**; disable with `agent.skills.enabled=false` (or `INFER_AGENT_SKILLS_ENABLED=false`).

```bash
infer skills list                          # Discover skills (works even when disabled)
infer skills install skill-creator         # Install from github.com/inference-gateway/skills
infer skills install acme/internal-comms   # Install from github.com/acme/skills
infer skills uninstall pdf                 # Remove a skill folder by name
```

`install` accepts a skill name, an `org/skill` pair, or a full GitHub tree URL; set `GITHUB_TOKEN`
(or `GH_TOKEN`) to raise the rate limit and reach private repositories. See
[docs/skills.md](docs/skills.md) for the authoring format.

## Computer Use

When enabled, the agent can control the desktop - move and click the mouse, scroll, type text and key
combinations, focus applications, and read screenshots. The display backend is detected automatically
across **macOS** (via a bundled Swift bridge), **X11**, and **Wayland**.

Computer Use is **off by default**. Turn it on in `computer_use.yaml` (or `infer config set computer_use.enabled true`):

```yaml
# .infer/computer_use.yaml
enabled: true
rate_limit:
  enabled: true
screenshot:
  streaming_enabled: true   # also registers the GetLatestScreenshot tool
```

Tools: `MouseMove`, `MouseClick`, `MouseScroll`, `KeyboardType`, `GetFocusedApp`, `ActivateApp`, and
`GetLatestScreenshot`. They run silently in the background (bypassing the approval prompt) and are
governed by `computer_use.enabled` plus the configured rate limits. On macOS an optional **floating
progress window** can mirror what the agent is doing. For a sandboxed desktop to drive, see
[examples/computer-use](examples/computer-use/).

> **⚠️ Windows note:** Computer use (mouse, keyboard, screenshot tools) is **not supported on Windows**.
> The agent will log a warning and disable these tools when running on Windows. All other features
> work normally.

## Persistent Memory

The agent keeps a durable, cross-session memory: individual Markdown **fact-files** under a global
directory (`~/.infer/memory` by default), catalogued by a `MEMORY.md` index. The index is injected
into context at session start, and the agent reads or writes individual facts on demand through the
`Memory` tool. A session reminder nudges it to consult and keep memory up to date.

Memory is **enabled by default**. Configure it in `memory.yaml`:

```yaml
# .infer/memory.yaml
enabled: true
dir: ""           # "" => ~/.infer/memory
max_chars: 4000   # cap on the injected MEMORY.md index
```

Turn it off with `memory.enabled=false` (or `INFER_MEMORY_ENABLED=false`); the memory-consult
reminder below is pruned automatically when memory is disabled.

## Reminders & Command Hooks

Two lightweight extension points fire at fixed **agent-loop hook points** - `pre_session`,
`pre_stream`, `post_stream`, `pre_tool`, `post_tool`, `pre_queue_drain`, `post_queue_drain`, and
`post_session`:

- **Reminders** (`reminders.yaml`) inject a `<system-reminder>` text block at a hook point, gated by a
  trigger (`always`, `interval`, `turns_before_max`, or `once`). Reminders ship **enabled** with two
  defaults: `todo-hygiene` (nudges the agent to keep a todo list) and `memory-consult` (points it at
  the memory index; auto-pruned when memory is off).
- **Command Hooks** (`hooks.yaml`) run a shell command at a hook point - the executable sibling of
  reminders. They are **off by default**; each command still faces the per-mode bash allow-list when
  the agent runs it, so allow-list the command and set `enabled: true` to turn hooks on.

```yaml
# .infer/hooks.yaml
enabled: true
hooks:
  - name: gofmt
    hook: post_session
    command: "gofmt -w ."
    timeout: 30   # seconds; 0 -> default 30
```

## Global Flags

- `-v, --verbose`: Enable verbose output
- `--config <path>`: Specify custom config file path

## Examples

### Docker Compose Examples

Each directory under [`examples/`](examples/) is a self-contained, runnable setup with its own README
and Docker Compose file:

| Example | Demonstrates |
| --------- | -------------- |
| [basic](examples/basic/) | Minimal gateway + CLI setup to get started |
| [a2a](examples/a2a/) | Agent-to-Agent: multiple agents, a demo site, and a VNC container |
| [mcp](examples/mcp/) | MCP server integration with a sample server and config |
| [computer-use](examples/computer-use/) | Computer Use driving a sandboxed Ubuntu GUI container |
| [model-switching](examples/model-switching/) | Switching models mid-session, with a small frontend |
| [shortcuts](examples/shortcuts/) | Custom `/`-shortcuts wired through config |
| [web-terminal](examples/web-terminal/) | Browser-based, multi-tab web terminal |
| [telegram-channel](examples/telegram-channel/) | Driving the agent from a Telegram channel |

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
/scm issue 123

# Discuss with AI, let it use tools to:
# - Read files
# - Search codebase
# - Make changes
# - Run tests

# Generate PR plan when ready
/scm pr-create Fixes the authentication timeout issue
```

### Configuration Example

```bash
# Set default model
infer config set agent.model "deepseek/deepseek-v4-pro"

# Enable bash tool
infer config set tools.bash.enabled true

# Configure web search
infer config set tools.web_search.enabled true

# Check current configuration
infer config get
```

### Web Terminal Example

```bash
# Start web terminal server
infer chat --web

# Open browser to http://localhost:3000
# Click "+" to create new terminal tabs
# Each tab is an independent chat session

# Custom port for remote access
infer chat --web --port 8080 --host 0.0.0.0

# Configure via config file
cat > .infer/config.yaml <<EOF
web:
  enabled: true
  port: 3000
  host: "localhost"
  session_inactivity_mins: 10  # Auto-cleanup after 10 minutes
EOF

infer chat --web  # Uses config file settings
```

**Use Cases:**

- Remote access to CLI from any device
- Multiple parallel chat sessions in browser tabs
- Team collaboration with shared terminal access
- Persistent sessions with automatic cleanup

## Development

For development, use [Task](https://taskfile.dev) for build automation:

```bash
task build # Build binary
task test  # Run tests
task fmt   # Format code
task lint  # Run linter
```

See [CLAUDE.md](CLAUDE.md) for detailed development documentation.

## License

Apache 2.0 License - see [LICENSE](LICENSE) file for details.
