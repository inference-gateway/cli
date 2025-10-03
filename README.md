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
  - [Verifying Release Binaries](#verifying-release-binaries)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [`infer init`](#infer-init)
  - [`infer config`](#infer-config)
    - [`infer config init`](#infer-config-init)
    - [`infer config agent set-model`](#infer-config-agent-set-model)
    - [`infer config agent set-system`](#infer-config-agent-set-system)
    - [`infer config tools`](#infer-config-tools)
  - [`infer status`](#infer-status)
  - [`infer chat`](#infer-chat)
  - [`infer agent`](#infer-agent)
  - [`infer version`](#infer-version)
- [Extensible Shortcuts System](#extensible-shortcuts-system)
- [Available Tools for LLMs](#available-tools-for-llms)
  - [Bash Tool](#bash-tool)
  - [Read Tool](#read-tool)
  - [Write Tool](#write-tool)
  - [Grep Tool](#grep-tool)
  - [WebSearch Tool](#websearch-tool)
  - [WebFetch Tool](#webfetch-tool)
  - [Github Tool](#github-tool)
  - [Tree Tool](#tree-tool)
  - [Delete Tool](#delete-tool)
  - [Edit Tool](#edit-tool)
  - [MultiEdit Tool](#multiedit-tool)
  - [TodoWrite Tool](#todowrite-tool)
  - [A2A Tools (Agent-to-Agent Communication)](#a2a-tools-agent-to-agent-communication)
- [Configuration](#configuration)
  - [Default Configuration](#default-configuration)
  - [Configuration Options](#configuration-options)
  - [Web Search API Setup (Optional)](#web-search-api-setup-optional)
- [Global Flags](#global-flags)
- [Examples](#examples)
  - [Basic Workflow](#basic-workflow)
  - [Configuration Management](#configuration-management)
- [Development](#development)
  - [Building](#building)
  - [Testing](#testing)
  - [Dependencies](#dependencies)
- [License](#license)

## Features

- **Status Monitoring**: Check gateway health and resource usage
- **Interactive Chat**: Chat with models using an interactive interface
- **Conversation History**: Store and retrieve past conversations with multiple storage backends
  - [Conversation Storage](docs/conversation-storage.md) - Detailed storage backend documentation
  - [Conversation Title Generation](docs/conversation-title-generation.md) - AI-powered title generation system
- **Configuration Management**: Manage gateway settings via YAML config
- **Project Initialization**: Set up local project configurations
- **Tool Execution**: LLMs can execute whitelisted commands and tools including:
  - **Bash**: Execute safe shell commands
  - **Read**: Read file contents with optional line ranges
  - **Write**: Write content to files with security controls
  - **Grep**: Fast ripgrep-powered search with regex support and multiple output modes
  - **WebSearch**: Search the web using DuckDuckGo or Google
  - **WebFetch**: Fetch content from whitelisted URLs
  - **Github**: Interact with GitHub API to fetch issues, pull requests, and create content
  - **Tree**: Display directory structure with polyfill support
  - **Delete**: Delete files and directories with security controls
  - **Edit**: Perform exact string replacements in files
  - **MultiEdit**: Make multiple edits to files in atomic operations
  - **TodoWrite**: Create and manage structured task lists
  - **A2A Tools**: Agent-to-agent communication for task delegation and coordination

## Installation

### Using Go Install

```bash
go install github.com/inference-gateway/cli@latest
```

### Using Container Image

For containerized environments, you can use the official container image:

```bash
# Run the CLI directly
docker run --rm -it ghcr.io/inference-gateway/cli:latest --help

# With volume mount for config persistence
docker run --rm -it -v ~/.infer:/home/infer/.infer ghcr.io/inference-gateway/cli:latest

# Example: Run chat command
docker run --rm -it -v ~/.infer:/home/infer/.infer ghcr.io/inference-gateway/cli:latest chat
```

**Using specific version:**

```bash
docker run --rm -it ghcr.io/inference-gateway/cli:0.48.12
```

**Available architectures:** `linux/amd64`, `linux/arm64`

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
  https://github.com/inference-gateway/cli/releases/download/v0.29.1/infer-darwin-amd64

# Download checksums and signature files
curl -L -o checksums.txt \
  https://github.com/inference-gateway/cli/releases/download/v0.29.1/checksums.txt
curl -L -o checksums.txt.pem \
  https://github.com/inference-gateway/cli/releases/download/v0.29.1/checksums.txt.pem
curl -L -o checksums.txt.sig \
  https://github.com/inference-gateway/cli/releases/download/v0.29.1/checksums.txt.sig
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

> **Note**: Replace `v0.29.1` with the desired release version and `infer-darwin-amd64` with your platform's binary name.

### Build from Source

```bash
git clone https://github.com/inference-gateway/cli.git
cd cli
go build -o infer .
```

## Quick Start

1. **Initialize project configuration:**

   ```bash
   infer init --model deepseek/deepseek-chat
   ```

   Using `--model` is recommended as it enables AI project analysis and generates a comprehensive AGENTS.md file
   tailored to your specific project.

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

Initialize a new project with Inference Gateway CLI. This creates:

- `.infer/` directory with:
  - `config.yaml` - Main configuration file for the project
  - `.gitignore` - Ensures sensitive files are not committed to version control
- `AGENTS.md` - AI-generated project documentation in the repository root (only when `--model` is specified)

This is the recommended command to start working with Inference Gateway CLI in a new project.

**Options:**

- `--overwrite`: Overwrite existing files if they already exist
- `--userspace`: Initialize configuration in user home directory (`~/.infer/`)

**Examples:**

```bash
# Initialize project-level configuration (default)
infer init
infer init --overwrite

# Initialize userspace configuration (global fallback)
infer init --userspace
```

### `infer config`

Manage CLI configuration settings including models, system prompts, and tools.

### `infer config init`

Initialize a new `.infer/config.yaml` configuration file in the current
directory. This creates only the configuration file with default settings.

For complete project initialization, use `infer init` instead.

**Options:**

- `--overwrite`: Overwrite existing configuration file
- `--userspace`: Initialize configuration in user home directory (`~/.infer/`)

**Examples:**

```bash
# Initialize project-level configuration (default)
infer config init
infer config init --overwrite

# Initialize userspace configuration (global fallback)
infer config init --userspace
```

### `infer config agent set-model`

Set the default model for chat sessions. When set, chat sessions will
automatically use this model without showing the model selection prompt.

**Examples:**

```bash
infer config agent set-model openai/gpt-4-turbo
infer config agent set-model anthropic/claude-opus-4-1-20250805
```

### `infer config agent set-system`

Set a system prompt that will be included with every chat session, providing
context and instructions to the AI model.

**Examples:**

```bash
infer config agent set-system "You are a helpful assistant."
infer config agent set-system "You are a Go programming expert."
```

```bash
infer config agent set-system "You are a helpful assistant."
infer config agent set-system "You are a Go programming expert."
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
- `sandbox`: Manage sandbox directories for security
  - `list`: List all sandbox directories
  - `add <path>`: Add a protected path to the sandbox
  - `remove <path>`: Remove a protected path from the sandbox

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
infer config tools sandbox list
infer config tools sandbox add ".github/"
infer config tools sandbox remove "test.txt"
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

**System Reminders:**

The chat interface supports configurable system reminders that can provide periodic contextual information to the AI
model during conversations. These reminders help maintain context and provide relevant guidance throughout the session.

- **Customizable interval**: Set how often reminders appear (in number of messages)
- **Dynamic content**: Reminders can contain contextual information based on the current state
- **Non-intrusive**: Reminders are sent to the AI model but don't interrupt the user experience
- **Configurable**: Enable/disable and customize reminder content through configuration

**Examples:**

```bash
infer chat
```

### `infer agent`

Execute a task using an autonomous agent in background mode. The CLI will work iteratively
until the task is considered complete. Particularly useful for SCM tickets like GitHub issues.

**Features:**

- **Autonomous execution**: Agent works independently to complete tasks
- **Iterative processing**: Continues until task completion criteria are met
- **Tool integration**: Full access to all available tools (Bash, Read, Write, etc.)
- **Parallel tool execution**: Executes multiple tool calls simultaneously for improved efficiency
- **Background operation**: Runs without interactive user input
- **Task completion detection**: Automatically detects when tasks are complete
- **Configurable concurrency**: Control the maximum number of parallel tool executions (default: 5)
- **JSON output**: Structured JSON output for easy parsing and integration

**Options:**

- `-m, --model`: Model to use for the agent (e.g., openai/gpt-4)

**Examples:**

```bash
# Execute a task described in a GitHub issue
infer agent "Please fix the github issue 38"

# Use a specific model for the agent
infer agent --model "openai/gpt-4" "Implement the feature described in issue #42"

# Debug a failing test
infer agent "Debug the failing test in PR 15"

# Refactor code
infer agent "Refactor the authentication module to use JWT tokens"
```

### `infer version`

Display version information for the Inference Gateway CLI.

**Examples:**

```bash
infer version
```

## Available Tools for LLMs

When tool execution is enabled, LLMs can use the following tools to interact with the system:

### Tree Tool

Display directory structure in a tree format, similar to the Unix `tree` command. Provides a polyfill
implementation when the native `tree` command is unavailable.

**Parameters:**

- `path` (optional): Directory path to display tree structure for (default: current directory)
- `max_depth` (optional): Maximum depth to traverse (unlimited by default)
- `show_hidden` (optional): Whether to show hidden files and directories (default: false)
- `respect_gitignore` (optional): Whether to exclude patterns from .gitignore (default: true)
- `format` (optional): Output format - "text" or "json" (default: "text")

**Examples:**

- Basic tree: Uses current directory with default settings
- Tree with depth limit: `max_depth: 2` - Shows only 2 levels deep
- Tree with hidden files: `show_hidden: true`
- Tree ignoring gitignore: `respect_gitignore: false` - Shows all files including those in .gitignore
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

### Write Tool

Write content to files on the filesystem with security controls and directory creation support.

**Parameters:**

- `file_path` (required): The path to the file to write
- `content` (required): The content to write to the file
- `create_dirs` (optional): Whether to create parent directories if they don't exist (default: true)
- `overwrite` (optional): Whether to overwrite existing files (default: true)
- `format` (optional): Output format - "text" or "json" (default: "text")

**Features:**

- **Directory Creation**: Automatically creates parent directories when needed
- **Overwrite Control**: Configurable behavior for existing files
- **Security Validation**: Respects path exclusions and security restrictions
- **Performance Optimized**: Efficient file writing with proper error handling

**Security:**

- **Approval Required**: Write operations require approval by default (secure by default)
- **Path Exclusions**: Respects configured excluded paths (e.g., `.infer/` directory)
- **Pattern Matching**: Supports glob patterns for path exclusions
- **Validation**: Validates file paths and content before writing

**Examples:**

- Create new file: `file_path: "output.txt"`, `content: "Hello, World!"`
- Write to subdirectory: `file_path: "logs/app.log"`, `content: "log entry"`, `create_dirs: true`
- Safe overwrite: `file_path: "config.json"`, `content: "{...}"`, `overwrite: false`

### WebSearch Tool

Search the web using DuckDuckGo or Google search engines to find information.

### WebFetch Tool

WebFetch content from whitelisted URLs or GitHub references using the format `example.com`.

### Github Tool

Interact with GitHub API to fetch issues, pull requests, create comments,
and create pull requests with authentication support. This is a standalone
tool separate from WebFetch.

**Parameters:**

- `owner` (required): Repository owner (username or organization)
- `repo` (required): Repository name
- `resource` (optional): Resource type to fetch or create (default: "issue")
  - `issue`: Fetch a specific issue
  - `issues`: Fetch a list of issues
  - `pull_request`: Fetch a specific pull request
  - `comments`: Fetch comments for an issue/PR
  - `create_comment`: Create a comment on an issue/PR
  - `create_pull_request`: Create a new pull request
- `issue_number` (required for issue/pull_request/comments/create_comment): Issue or PR number
- `comment_body` (required for create_comment): Comment body text
- `title` (required for create_pull_request): Pull request title
- `body` (optional for create_pull_request): Pull request body/description
- `head` (required for create_pull_request): Head branch name
- `base` (optional for create_pull_request): Base branch name (default: "main")
- `state` (optional): Filter by state for issues list ("open", "closed", "all", default: "open")
- `per_page` (optional): Number of items per page for lists (1-100, default: 30)

**Features:**

- **GitHub API Integration**: Direct access to GitHub's REST API v3
- **Authentication**: Supports GitHub personal access tokens via environment variables
- **Multiple Resources**: Fetch issues, pull requests, comments, and create new content
- **Structured Data**: Returns properly typed GitHub data structures
- **Error Handling**: Comprehensive error handling with GitHub API error messages
- **Rate Limiting**: Respects GitHub API rate limits
- **Security**: Configurable timeout and response size limits
- **Environment Variables**: Supports token resolution via `%GITHUB_TOKEN%` syntax
- **Security Controls**: Owner validation for secure repository access

**Configuration:**

```yaml
tools:
  github:
    enabled: true
    token: "%GITHUB_TOKEN%"  # Environment variable reference
    base_url: "https://api.github.com"
    owner: "your-username"  # Default owner for security
    repo: "your-repo"       # Default repository (optional)
    safety:
      max_size: 1048576  # 1MB
      timeout: 30        # 30 seconds
    require_approval: false
```

**Examples:**

- Fetch specific issue: `owner: "octocat", repo: "Hello-World", resource: "issue", issue_number: 1`
- List open issues: `owner: "octocat", repo: "Hello-World", resource: "issues", state: "open", per_page: 10`
- Fetch pull request: `owner: "octocat", repo: "Hello-World", resource: "pull_request", issue_number: 5`
- Get issue comments: `owner: "octocat", repo: "Hello-World", resource: "comments", issue_number: 1`
- Create comment: `owner: "octocat", repo: "Hello-World", resource: "create_comment",
  issue_number: 1, comment_body: "Great work!"`
- Create pull request: `owner: "octocat", repo: "Hello-World", resource: "create_pull_request",
  title: "Add feature", body: "New feature implementation", head: "feature-branch", base: "main"`

### Delete Tool

Delete files or directories from the filesystem with security controls. Supports wildcard patterns for batch operations.

**Parameters:**

- `path` (required): The path to the file or directory to delete
- `recursive` (optional): Whether to delete directories recursively (default: false)
- `force` (optional): Whether to force deletion (ignore non-existent files, default: false)

**Features:**

- **Wildcard Support**: Delete multiple files using patterns like `*.txt` or `temp/*`
- **Recursive Deletion**: Remove directories and their contents
- **Safety Controls**: Respects configured path exclusions and security restrictions
- **Validation**: Validates file paths and permissions before deletion

**Security:**

- **Approval Required**: Delete operations require approval by default
- **Path Exclusions**: Respects configured excluded paths for security
- **Pattern Matching**: Supports glob patterns for path exclusions
- **Validation**: Validates file paths and prevents deletion of protected directories

**Examples:**

- Delete single file: `path: "temp.txt"`
- Delete directory recursively: `path: "temp/", recursive: true`
- Delete with wildcard: `path: "*.log"`
- Force delete: `path: "missing.txt", force: true`

### Edit Tool

Perform exact string replacements in files with security validation and preview support.

**Parameters:**

- `file_path` (required): The path to the file to modify
- `old_string` (required): The text to replace (must match exactly)
- `new_string` (required): The text to replace it with
- `replace_all` (optional): Replace all occurrences of old_string (default: false)

**Features:**

- **Exact Matching**: Requires exact string matches for safety
- **Preview Support**: Shows diff preview before applying changes
- **Atomic Operations**: Either all changes succeed or none are applied
- **Security Validation**: Respects path exclusions and file permissions

**Security:**

- **Read Tool Requirement**: Requires Read tool to be used first on the file
- **Approval Required**: Edit operations require approval by default
- **Path Exclusions**: Respects configured excluded paths
- **Validation**: Validates file paths and prevents editing protected files

**Examples:**

- Single replacement: `file_path: "config.txt", old_string: "port: 3000", new_string: "port: 8080"`
- Replace all occurrences: `file_path: "script.py", old_string: "print", new_string: "logging.info", replace_all: true`

### MultiEdit Tool

Make multiple edits to a single file in atomic operations. All edits succeed or none are applied.

**Parameters:**

- `file_path` (required): The path to the file to modify
- `edits` (required): Array of edit operations to perform sequentially
  - `old_string`: The text to replace (must match exactly)
  - `new_string`: The text to replace it with
  - `replace_all` (optional): Replace all occurrences (default: false)

**Features:**

- **Atomic Operations**: All edits succeed or none are applied
- **Sequential Processing**: Edits are applied in the order provided
- **Preview Support**: Shows comprehensive diff preview
- **Security Validation**: Respects all security restrictions

**Security:**

- **Read Tool Requirement**: Requires Read tool to be used first on the file
- **Approval Required**: MultiEdit operations require approval by default
- **Path Exclusions**: Respects configured excluded paths
- **Validation**: Validates all edits before execution

**Examples:**

```json
{
  "file_path": "config.yaml",
  "edits": [
    {
      "old_string": "port: 3000",
      "new_string": "port: 8080"
    },
    {
      "old_string": "debug: true",
      "new_string": "debug: false"
    }
  ]
}
```

### Grep Tool

A powerful search tool with configurable backend (ripgrep or Go implementation).

**Parameters:**

- `pattern` (required): The regular expression pattern to search for
- `path` (optional): File or directory to search in (default: current directory)
- `output_mode` (optional): Output mode - "content", "files_with_matches", or "count" (default: "files_with_matches")
- `-i` (optional): Case insensitive search
- `-n` (optional): Show line numbers in output
- `-A` (optional): Number of lines to show after each match
- `-B` (optional): Number of lines to show before each match
- `-C` (optional): Number of lines to show before and after each match
- `glob` (optional): Glob pattern to filter files (e.g., "*.js", "*.{ts,tsx}")
- `type` (optional): File type to search (e.g., "js", "py", "rust")
- `multiline` (optional): Enable multiline mode where patterns can span lines
- `head_limit` (optional): Limit output to first N results

**Features:**

- **Dual Backend**: Uses ripgrep when available for optimal performance, falls back to Go implementation
- **Full Regex Support**: Supports complete regex syntax
- **Multiple Output Modes**: Content matching, file lists, or count results
- **Context Lines**: Show lines before and after matches
- **File Filtering**: Filter by glob patterns or file types
- **Multiline Matching**: Patterns can span multiple lines
- **Automatic Exclusions**: Automatically excludes common directories and files (.git, node_modules, .infer, etc.)
- **Gitignore Support**: Respects .gitignore patterns in your repository
- **User-Configurable Exclusions**: Additional exclusion patterns can be configured by users (not by the LLM)

**Security & Exclusions:**

- **Path Exclusions**: Respects configured excluded paths and patterns
- **Automatic Exclusions**: The tool automatically excludes:
  - Version control directories (.git, .svn, etc.)
  - Dependency directories (node_modules, vendor, etc.)
  - Build artifacts (dist, build, target, etc.)
  - Cache and temp files (.cache, \*.tmp, \*.log, etc.)
  - Security-sensitive files (.env, secrets, etc.)
- **Gitignore Integration**: Automatically reads and respects .gitignore patterns
- **Validation**: Validates search patterns and file access
- **Performance Limits**: Configurable result limits to prevent overwhelming output

**Examples:**

- Basic search: `pattern: "error", output_mode: "content"`
- Case insensitive: `pattern: "TODO", -i: true, output_mode: "content"`
- With context: `pattern: "function", -C: 3, output_mode: "content"`
- File filtering: `pattern: "interface", glob: "*.go", output_mode: "files_with_matches"`
- Count results: `pattern: "log.*Error", output_mode: "count"`

### TodoWrite Tool

Create and manage structured task lists for LLM-assisted development workflows.

**Parameters:**

- `todos` (required): Array of todo items with status tracking
  - `id` (required): Unique identifier for the task
  - `content` (required): Task description
  - `status` (required): Task status - "pending", "in_progress", or "completed"

**Features:**

- **Structured Task Management**: Organized task tracking with status
- **Real-time Updates**: Mark tasks as in_progress/completed during execution
- **Progress Tracking**: Visual representation of task completion
- **LLM Integration**: Designed for LLM-assisted development workflows

**Security:**

- **No File System Access**: Pure memory-based operation
- **Validation**: Validates todo structure and status values
- **Size Limits**: Configurable limits on todo list size

**Examples:**

```json
{
  "todos": [
    {
      "id": "1",
      "content": "Update README with new tool documentation",
      "status": "in_progress"
    },
    {
      "id": "2",
      "content": "Add test cases for new features",
      "status": "pending"
    }
  ]
}
```

### A2A Tools (Agent-to-Agent Communication)

The A2A (Agent-to-Agent) tools enable communication between the CLI client and specialized A2A server agents, allowing for task delegation, distributed processing, and agent coordination.

**Core A2A Tools:**

#### A2A_SubmitTask Tool

Submit tasks to specialized A2A agents for distributed processing.

**Parameters:**

- `agent_url` (required): URL of the A2A agent server
- `task_description` (required): Description of the task to perform
- `metadata` (optional): Additional task metadata as key-value pairs

**Features:**

- **Task Delegation**: Submit complex tasks to specialized agents
- **Streaming Responses**: Real-time task execution updates
- **Metadata Support**: Include contextual information with tasks
- **Task Tracking**: Automatic tracking of submitted tasks with IDs
- **Error Handling**: Comprehensive error reporting and retry logic

**Examples:**

- Code analysis: `agent_url: "http://security-agent:8080", task_description: "Analyze codebase for security vulnerabilities"`
- Documentation: `agent_url: "http://docs-agent:8080", task_description: "Generate API documentation"`
- Testing: `agent_url: "http://test-agent:8080", task_description: "Create unit tests for UserService class"`

#### A2A_QueryAgent Tool

Retrieve agent capabilities and metadata for discovery and validation.

**Parameters:**

- `agent_url` (required): URL of the A2A agent to query

**Features:**

- **Agent Discovery**: Query agent capabilities and supported task types
- **Health Checks**: Verify agent availability and status
- **Metadata Retrieval**: Get agent configuration and feature information
- **Connection Validation**: Test connectivity before task submission

**Examples:**

- Capability check: `agent_url: "http://agent:8080"` - Returns agent card with available features
- Health status: Query agent before submitting critical tasks

#### A2A_QueryTask Tool

Query the status and results of previously submitted tasks.

**Parameters:**

- `agent_url` (required): URL of the A2A agent server
- `context_id` (required): Context ID for the task
- `task_id` (required): ID of the task to query

**Features:**

- **Status Monitoring**: Check task completion status and progress
- **Result Retrieval**: Access task outputs and generated content
- **Error Diagnostics**: Get detailed error information for failed tasks
- **Artifact Discovery**: List available artifacts from completed tasks

**Examples:**

- Status check: `agent_url: "http://agent:8080", context_id: "ctx-123", task_id: "task-456"`
- Result access: Retrieve task outputs and completion details

#### A2A_DownloadArtifacts Tool

Download artifacts and files generated by completed A2A tasks.

**Parameters:**

- `agent_url` (required): URL of the A2A agent server
- `context_id` (required): Context ID for the task
- `task_id` (required): ID of the completed task

**Features:**

- **Artifact Download**: Download files, reports, and outputs from completed tasks
- **Configurable Directory**: Downloads to configurable directory (default: `./downloads`)
- **Progress Tracking**: Track download status per artifact
- **Validation**: Ensures task completion before attempting downloads
- **HTTP Client**: Direct HTTP download with timeout and error handling

**Security:**

- **Task Validation**: Verifies task completion status before downloads
- **Path Safety**: Downloads to safe, configurable directories
- **Timeout Protection**: 30-second timeout for download operations
- **Error Handling**: Comprehensive error reporting for failed downloads

**Examples:**

- Download results: `agent_url: "http://agent:8080", context_id: "ctx-123", task_id: "task-456"`
- Retrieve generated files: Access documentation, reports, or analysis outputs

**A2A Workflow Example:**

```text
1. Query agent capabilities:        A2A_QueryAgent
2. Submit task for processing:      A2A_SubmitTask  
3. Monitor task progress:           A2A_QueryTask
4. Download completed artifacts:    A2A_DownloadArtifacts
```

**Configuration:**

A2A tools are configured in the tools section:

```yaml
a2a:
  enabled: true
  tools:
    submit_task:
      enabled: true
    query_agent:
      enabled: true  
    query_task:
      enabled: true
    download_artifacts:
      enabled: true
      download_dir: "./downloads"  # Configurable download directory
```

**Use Cases:**

- **Code Analysis**: Submit codebases to security or quality analysis agents
- **Documentation Generation**: Generate API docs, README files, or technical documentation
- **Testing**: Create comprehensive test suites with specialized testing agents
- **Data Processing**: Process large datasets with specialized data analysis agents
- **Content Creation**: Generate content with specialized writing or design agents

For detailed A2A documentation and examples, see [docs/a2a-connections.md](docs/a2a-connections.md).

**Security Notes:**

- All tools respect configured safety settings and exclusion patterns
- Commands require approval when safety approval is enabled
- File access is restricted to allowed paths and excludes sensitive directories

## Configuration

The CLI supports a **2-layer configuration system** that allows for both user-level and project-level
configuration with proper precedence handling. For detailed configuration documentation and examples, see [CONFIG.md](CONFIG.md).

### Configuration Layers

1. **Userspace Configuration** (`~/.infer/config.yaml`)
   - Global configuration for the user across all projects
   - Used as a fallback when no project-level configuration exists
   - Can be created with: `infer init --userspace` or `infer config init --userspace`

2. **Project Configuration** (`.infer/config.yaml` in current directory)
   - Project-specific configuration that takes precedence over userspace config
   - Default location for most commands
   - Can be created with: `infer init` or `infer config init`

### Configuration Precedence

Configuration values are merged with the following precedence (highest to lowest):

1. **Project-level config** (`.infer/config.yaml`) - **Highest Priority**
2. **Userspace config** (`~/.infer/config.yaml`)
3. **Built-in defaults** - **Lowest Priority**

**Example**: If your userspace config sets `agent.model: "anthropic/claude-4"` and your project config sets
`agent.model: "deepseek/deepseek-chat"`, the project config wins and `deepseek/deepseek-chat` will be used.
However, if the project config doesn't specify a model but does specify other settings, the userspace model will be preserved
while project settings take precedence for their specific values.

### Usage Examples

```bash
# Create userspace configuration (global fallback)
infer init --userspace

# Create project configuration (takes precedence)
infer init

# Both configurations will be automatically merged when commands are run
```

You can also specify a custom config file using the `--config` flag which will override the automatic 2-layer loading.

### Default Configuration

```yaml
gateway:
  url: http://localhost:8080
  api_key: ""
  timeout: 200
client:
  timeout: 200
  retry:
    enabled: true
    max_attempts: 3
    initial_backoff_sec: 5
    max_backoff_sec: 60
    backoff_multiplier: 2
    retryable_status_codes: [400, 408, 429, 500, 502, 503, 504]
logging:
  debug: false
tools:
  enabled: true # Tools are enabled by default with safe read-only commands
  sandbox:
    directories: [".", "/tmp"] # Allowed directories for tool operations
    protected_paths: # Paths excluded from tool access for security
      - .infer/
      - .git/
      - *.env
  bash:
    enabled: true
    whitelist:
      commands: # Exact command matches
        - ls
        - pwd
        - echo
        - wc
        - sort
        - uniq
        - gh
        - task
      patterns: # Regex patterns for more complex commands
        - ^git branch( --show-current)?$
        - ^git checkout -b [a-zA-Z0-9/_-]+( [a-zA-Z0-9/_-]+)?$
        - ^git checkout [a-zA-Z0-9/_-]+
        - ^git add [a-zA-Z0-9/_.-]+
        - ^git diff+
        - ^git remote -v$
        - ^git status$
        - ^git log --oneline -n [0-9]+$
        - ^git commit -m ".+"$
        - ^git push( --set-upstream)?( origin)?( [a-zA-Z0-9/_-]+)?$
  read:
    enabled: true
    require_approval: false
  write:
    enabled: true
    require_approval: true # Write operations require approval by default for security
  edit:
    enabled: true
    require_approval: true # Edit operations require approval by default for security
  delete:
    enabled: true
    require_approval: true # Delete operations require approval by default for security
  grep:
    enabled: true
    backend: auto # "auto", "ripgrep", or "go"
    require_approval: false
  tree:
    enabled: true
    require_approval: false
  web_fetch:
    enabled: true
    whitelisted_domains:
      - golang.org
    safety:
      max_size: 4096 # 4KB
      timeout: 30 # 30 seconds
      allow_redirect: true
    cache:
      enabled: true
      ttl: 3600 # 1 hour
      max_size: 52428800 # 50MB
  web_search:
    enabled: true
    default_engine: duckduckgo
    max_results: 10
    engines:
      - duckduckgo
      - google
    timeout: 10
  todo_write:
    enabled: true
    require_approval: false
  github:
    enabled: true
    token: "%GITHUB_TOKEN%"
    base_url: "https://api.github.com"
    owner: ""
    safety:
      max_size: 1048576  # 1MB
      timeout: 30        # 30 seconds
    require_approval: false
  safety:
    require_approval: true
compact:
  output_dir: .infer # Directory for compact command exports
  summary_model: "" # Model to use for summarization (optional)
agent:
  model: "" # Default model for agent operations
  system_prompt: | # System prompt for agent sessions
    Autonomous software engineering agent. Execute tasks iteratively until completion.

    IMPORTANT: You NEVER push to main or master or to the current branch - instead you create a branch and push to a branch.
    IMPORTANT: You NEVER read all the README.md - start by reading 300 lines

    RULES:
    - Security: Defensive only (analysis, detection, docs)
    - Style: no emojis/comments unless asked, use conventional commits
    - Code: Follow existing patterns, check deps, no secrets
    - Tasks: Use TodoWrite, mark progress immediately
    - Chat exports: Read only "## Summary" to "---" section
    - Tools: Batch calls, prefer Grep for search

    WORKFLOW:
    When asked to implement features or fix issues:
    1. Plan with TodoWrite
    2. Search codebase to understand context
    3. Implement solution
    4. Run tests with: task test
    5. Run lint/format with: task fmt and task lint
    6. Commit changes (only if explicitly asked)
    7. Create a pull request (only if explicitly asked)
  system_reminders:
    enabled: true
    interval: 4
    reminder_text: |
      System reminder text for maintaining context
  verbose_tools: false
  max_turns: 50 # Maximum number of turns for agent sessions
  max_tokens: 4096 # The maximum number of tokens that can be generated per request
  optimization:
    enabled: false
    max_history: 10
    compact_threshold: 20
    truncate_large_outputs: true
    skip_redundant_confirmations: true
chat:
  theme: tokyo-night
```

### Configuration Options

**Gateway Settings:**

- **gateway.url**: The URL of the inference gateway
- **gateway.api_key**: API key for authentication (if required)
- **gateway.timeout**: Request timeout in seconds

**Client Settings:**

- **client.timeout**: HTTP client timeout in seconds
- **client.retry.enabled**: Enable automatic retries for failed requests
- **client.retry.max_attempts**: Maximum number of retry attempts
- **client.retry.initial_backoff_sec**: Initial delay between retries in seconds
- **client.retry.max_backoff_sec**: Maximum delay between retries in seconds
- **client.retry.backoff_multiplier**: Backoff multiplier for exponential delay
- **client.retry.retryable_status_codes**: HTTP status codes that trigger retries (e.g., [400, 408, 429, 500, 502, 503, 504])

**Logging Settings:**

- **logging.debug**: Enable debug logging for verbose output

**Tool Settings:**

- **tools.enabled**: Enable/disable tool execution for LLMs (default: true)
- **tools.sandbox.directories**: Allowed directories for tool operations (default: [".", "/tmp"])
- **tools.sandbox.protected_paths**: Paths excluded from tool access for security
  (default: [".infer/", ".git/", "*.env"])
- **tools.whitelist.commands**: List of allowed commands (supports arguments)
- **tools.whitelist.patterns**: Regex patterns for complex command validation
- **tools.safety.require_approval**: Prompt user before executing any command
  (default: true)
- **Individual tool settings**: Each tool (Bash, Read, Write, Edit, Delete, Grep, Tree, WebFetch, WebSearch, TodoWrite) has:
  - **enabled**: Enable/disable the specific tool
  - **require_approval**: Override global safety setting for this tool (optional)

**Compact Settings:**

- **compact.output_dir**: Directory for compact command exports
  (default: ".infer")

**Chat Settings:**

- **chat.default_model**: Default model for chat sessions (skips model
  selection when set)
- **chat.system_prompt**: System prompt included with every chat session
- **chat.system_reminders.enabled**: Enable/disable system reminders (default: true)
- **chat.system_reminders.interval**: Number of messages between reminders (default: 10)
- **chat.system_reminders.text**: Custom reminder text to provide contextual guidance

**Agent Settings:**

- **agent.model**: Default model for agent operations
- **agent.system_prompt**: System prompt for agent sessions
- **agent.system_reminders.enabled**: Enable system reminders during agent sessions
- **agent.system_reminders.interval**: Number of messages between reminders (default: 4)
- **agent.system_reminders.reminder_text**: Custom reminder text for agent context
- **agent.verbose_tools**: Enable verbose tool output (default: false)
- **agent.max_turns**: Maximum number of turns for agent sessions (default: 50)
- **agent.max_tokens**: Maximum tokens per agent request (default: 8192)
- **agent.optimization.enabled**: Enable optimization features (default: false)
- **agent.optimization.max_history**: Maximum conversation history to maintain (default: 10)
- **agent.optimization.compact_threshold**: Threshold for compacting conversation (default: 20)
- **agent.optimization.truncate_large_outputs**: Truncate large tool outputs (default: true)
- **agent.optimization.skip_redundant_confirmations**: Skip redundant confirmation messages (default: true)

**Web Search Settings:**

- **web_search.enabled**: Enable/disable web search tool for LLMs (default: true)
- **web_search.default_engine**: Default search engine to use ("duckduckgo" or "google", default: "duckduckgo")
- **web_search.max_results**: Maximum number of search results to return (1-50, default: 10)
- **web_search.engines**: List of available search engines
- **web_search.timeout**: Search timeout in seconds (default: 10)

**Chat Interface Settings:**

- **chat.theme**: Chat interface theme name (default: "tokyo-night")
  - Available themes: `tokyo-night`, `github-light`, `dracula`
  - Can be changed during chat using `/theme [theme-name]` shortcut
  - Affects colors and styling of the chat interface

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

### Environment Variables

The CLI supports environment variable configuration with the `INFER_` prefix. Environment variables override
configuration file settings and are particularly useful for containerized deployments and CI/CD environments.

#### Core Environment Variables

- `INFER_GATEWAY_URL`: Override gateway URL (e.g., `http://localhost:8080`)
- `INFER_GATEWAY_API_KEY`: Set gateway API key for authentication
- `INFER_LOGGING_DEBUG`: Enable debug logging (`true`/`false`)
- `INFER_AGENT_MODEL`: Default model for agent operations (e.g., `openai/gpt-4`)

#### Tools Configuration

- `INFER_TOOLS_ENABLED`: Enable/disable all local tools (`true`/`false`)

#### A2A (Agent-to-Agent) Configuration

- `INFER_A2A_ENABLED`: Enable/disable A2A tools (`true`/`false`)
- `INFER_A2A_AGENTS`: Configure A2A agent endpoints (supports comma-separated or newline-separated format)

**A2A Agents Configuration Examples:**

```bash
# Comma-separated format
export INFER_A2A_AGENTS="http://agent1:8080,http://agent2:8080,http://agent3:8080"

# Newline-separated format (useful in docker-compose)
export INFER_A2A_AGENTS="
http://google-calendar-agent:8080
http://n8n-agent:8080
http://documentation-agent:8080
http://browser-agent:8080
"
```

#### Individual Tool Configuration

You can also configure individual tools via environment variables using the pattern `INFER_TOOLS_<TOOL>_ENABLED`:

- `INFER_TOOLS_BASH_ENABLED`: Enable/disable Bash tool
- `INFER_TOOLS_READ_ENABLED`: Enable/disable Read tool
- `INFER_TOOLS_WRITE_ENABLED`: Enable/disable Write tool
- `INFER_TOOLS_GREP_ENABLED`: Enable/disable Grep tool
- `INFER_TOOLS_WEBSEARCH_ENABLED`: Enable/disable WebSearch tool
- `INFER_TOOLS_GITHUB_ENABLED`: Enable/disable Github tool

And individual A2A tools:

- `INFER_A2A_TOOLS_SUBMIT_TASK_ENABLED`: Enable/disable A2A SubmitTask tool
- `INFER_A2A_TOOLS_QUERY_AGENT_ENABLED`: Enable/disable A2A QueryAgent tool
- `INFER_A2A_TOOLS_QUERY_TASK_ENABLED`: Enable/disable A2A QueryTask tool

#### Environment Variable Precedence

Environment variables have the highest precedence in the configuration system:

1. **Environment Variables** (e.g., `INFER_TOOLS_ENABLED`) - **Highest Priority**
2. **Project-level config** (`.infer/config.yaml`)
3. **Userspace config** (`~/.infer/config.yaml`)
4. **Built-in defaults** - **Lowest Priority**

**Example**: If your config file sets `tools.enabled: true` but you set `INFER_TOOLS_ENABLED=false`, the
environment variable wins and tools will be disabled.

## Global Flags

- `-c, --config`: Config file (default is `./.infer/config.yaml`)
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

# Set default model for chat sessions
infer config agent set-model openai/gpt-4-turbo

# Set system prompt
infer config agent set-system "You are a helpful assistant."

# Enable tool execution with safety approval
infer config tools enable
infer config tools safety enable

# Configure sandbox directories for security
infer config tools sandbox add "/home/user/projects"
infer config tools sandbox add "/tmp/work"

# Add protected paths to prevent accidental modification
infer config tools sandbox add ".env"
infer config tools sandbox add ".git/"

# Configure individual tool safety settings
infer config tools safety set Read disabled    # Skip approval for Read tool
infer config tools safety set Write enabled    # Require approval for Write tool
infer config tools safety set Delete enabled   # Require approval for Delete tool
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

# Extensible Shortcuts System

The CLI provides an extensible shortcuts system that allows you to quickly execute common commands with
`/shortcut-name` syntax.

## Built-in Shortcuts

### Core Shortcuts

- `/clear` - Clear conversation history
- `/exit` - Exit the chat session
- `/help [shortcut]` - Show available shortcuts or specific shortcut help
- `/switch [model]` - Switch to a different model
- `/theme [theme-name]` - Switch chat interface theme or list available themes
- `/config <show|get|set|reload> [key] [value]` - Manage configuration settings
- `/compact [format]` - Export conversation to markdown

### Git Shortcuts

- `/git <command> [args...]` - Execute git commands (supports commit, push, status, etc.)
- `/git commit [flags]` - **NEW**: Commit staged changes with AI-generated message
- `/git push [remote] [branch] [flags]` - **NEW**: Push commits to remote repository

The git shortcuts provide intelligent commit message generation using AI when no message is provided with `/git commit`.

## User-Defined Shortcuts

You can create custom shortcuts by adding YAML configuration files in the `.infer/shortcuts/` directory.

### Configuration File Format

Create files named `custom-*.yaml` (e.g., `custom-1.yaml`, `custom-dev.yaml`) in `.infer/shortcuts/`:

```yaml
shortcuts:
  - name: "tests"
    description: "Run all tests in the project"
    command: "go"
    args: ["test", "./..."]
    working_dir: "."  # Optional: set working directory

  - name: "build"
    description: "Build the project"
    command: "go"
    args: ["build", "-o", "infer", "."]

  - name: "lint"
    description: "Run linter on the codebase"
    command: "golangci-lint"
    args: ["run"]
```

### Configuration Fields

- **name** (required): The shortcut name (used as `/name`)
- **description** (required): Human-readable description shown in `/help`
- **command** (required): The executable command to run
- **args** (optional): Array of arguments to pass to the command
- **working_dir** (optional): Working directory for the command (defaults to current)

### Using Shortcuts

With the configuration above, you can use:

- `/tests` - Runs `go test ./...`
- `/build` - Runs `go build -o infer .`
- `/lint` - Runs `golangci-lint run`

You can also pass additional arguments:

- `/tests -v` - Runs `go test ./... -v`
- `/build --race` - Runs `go build -o infer . --race`

## Example Custom Shortcuts

Here are some useful shortcuts you might want to add:

### Development Shortcuts (`custom-dev.yaml`)

```yaml
shortcuts:
  - name: "fmt"
    description: "Format all Go code"
    command: "go"
    args: ["fmt", "./..."]

  - name: "mod tidy"
    description: "Tidy up go modules"
    command: "go"
    args: ["mod", "tidy"]

  - name: "version"
    description: "Show current version"
    command: "git"
    args: ["describe", "--tags", "--always", "--dirty"]
```

### Docker Shortcuts (`custom-docker.yaml`)

```yaml
shortcuts:
  - name: "docker build"
    description: "Build Docker image"
    command: "docker"
    args: ["build", "-t", "myapp", "."]

  - name: "docker run"
    description: "Run Docker container"
    command: "docker"
    args: ["run", "-p", "8080:8080", "myapp"]
```

### Project-Specific Shortcuts (`custom-project.yaml`)

```yaml
shortcuts:
  - name: "migrate"
    description: "Run database migrations"
    command: "./scripts/migrate.sh"
    working_dir: "."

  - name: "seed"
    description: "Seed database with test data"
    command: "go"
    args: ["run", "cmd/seed/main.go"]
```

## Tips

1. **File Organization**: Use descriptive names for your config files (e.g., `custom-dev.yaml`, `custom-docker.yaml`)
2. **Command Discovery**: Use `/help` to see all available shortcuts including your custom ones
3. **Error Handling**: If a custom shortcut fails to load, it will be skipped with a warning
4. **Reloading**: Restart the chat session to reload custom shortcuts after making changes
5. **Security**: Be careful with custom shortcuts as they execute system commands

## Troubleshooting

- **Shortcut not appearing**: Check YAML syntax and file naming (`custom-*.yaml`)
- **Command not found**: Ensure the command is available in your PATH
- **Permission denied**: Check file permissions and executable rights
- **Invalid YAML**: Use a YAML validator to check your configuration syntax

## License

This project is licensed under the MIT License.
