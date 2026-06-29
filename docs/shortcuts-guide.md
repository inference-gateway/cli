# Shortcuts Guide

[← Back to README](../README.md)

This document provides comprehensive documentation for the Inference Gateway CLI shortcuts system,
including built-in shortcuts, AI-powered snippets, and custom shortcut creation.

## Table of Contents

- [Overview](#overview)
- [Built-in Shortcuts](#built-in-shortcuts)
- [Git Shortcuts](#git-shortcuts)
- [SCM Shortcuts](#scm-shortcuts)
- [Init-Created Shortcuts](#init-created-shortcuts)
- [AI-Powered Snippets](#ai-powered-snippets)
- [User-Defined Shortcuts](#user-defined-shortcuts)
- [Advanced Usage](#advanced-usage)
- [Troubleshooting](#troubleshooting)

---

## Overview

The CLI provides an extensible shortcuts system that allows you to quickly execute common commands
with `/shortcut-name` syntax during chat sessions.

**Key Features:**

- Quick command execution with `/` prefix
- Built-in shortcuts for common operations
- Git and GitHub integration
- AI-powered snippets for intelligent automation
- Fully customizable with YAML configuration
- Support for command chaining and complex workflows

---

## Built-in Shortcuts

These shortcuts are available out of the box:

### Core Shortcuts

**Conversation & session:**

- `/new [title]` - Start a new conversation (optionally titled)
- `/clear` - Save the current conversation and start a new one
- `/compact` - Save the conversation and start a new session seeded with a summary
- `/conversations` - Open the conversation selection dropdown
- `/context` - Show context-window usage
- `/cost` - Show session cost breakdown with per-model details
- `/copy [format]` - Copy the current conversation to the system clipboard (formats: `text`, `markdown`, `json`; default `text`)
- `/model [model-name] [prompt]` - Switch model, or run a single prompt against a specific model then restore
- `/theme` - Switch chat interface theme or list available themes
- `/voice [seconds]` - Record from the microphone and transcribe to the input field using Whisper (only available when `speech_to_text.enabled` is `true`)
- `/help [shortcut]` - Show available shortcuts or specific shortcut help
- `/exit` - Exit the chat session

**Panels & views:**

- `/diff` - Open the changes panel (interactive diff viewer)
- `/explorer` - Open the file explorer (tree + fuzzy finder)
- `/tasks` - Show the A2A task-management interface (requires A2A)
- `/release-notes [version]` - Show GitHub release notes for a version or the latest (requires the `gh` CLI installed and authenticated)

**Project setup:**

- `/init` - Set input with project analysis prompt for AGENTS.md generation
- `/init-github-action` - Set up a GitHub Action via an interactive wizard

### Project Initialization Shortcut

The `/init` shortcut populates the input field with a configurable prompt for generating an AGENTS.md
file. This allows you to:

1. Type `/init` to populate the input with the project analysis prompt
2. Review and optionally modify the prompt before sending
3. Press Enter to send the prompt and watch the agent analyze your project interactively

The prompt is configurable in your config file under `init.prompt`. The default prompt instructs the agent to:

- Analyze your project structure, build tools, and configuration files
- Create comprehensive documentation for AI agents
- Generate an AGENTS.md file with project overview, commands, and conventions

### Copy Shortcut

The `/copy` shortcut copies the current conversation to your system clipboard, so you can move a
session to another terminal or machine and continue it there. It pairs well with `/compact`:

1. Run `/compact` to summarize the conversation and reduce its size
2. Run `/copy` to place the (now compact) session on the clipboard
3. Paste it into another terminal or chat to continue the work

By default `/copy` uses plain `text`; pass a format to override it - `/copy markdown` or
`/copy json`. The shortcut shells out to your platform's native clipboard utility:

- **macOS:** `pbcopy`
- **Linux:** one of `wl-copy` (Wayland), `xclip`, or `xsel` (X11) - install at least one
- **Windows:** `clip`
- **WSL:** `clip.exe` (writes to the Windows host clipboard)

If none of these utilities is available, `/copy` reports an error naming the ones it looked for.

### Voice Shortcut

The `/voice` shortcut records audio from your microphone, transcribes it locally with
[whisper.cpp](https://github.com/ggml-org/whisper.cpp), and places the transcription into the
input field - ready to review and send. It is **disabled by default** and gated behind the
`speech_to_text.enabled` feature flag (see [Speech-to-Text](speech-to-text.md) for full setup).

1. Enable it: set `speech_to_text.enabled: true` in `.infer/config.yaml`
2. Type `/voice` and press Enter - recording starts immediately and stops a couple of
   seconds after you go quiet (`speech_to_text.silence_timeout`), or at the
   `max_recording_seconds` cap, or pass an override like `/voice 8`
3. The transcribed text appears in the input field; edit if needed and press Enter to send

`/voice` shells out to `ffmpeg` (or `arecord`/`sox` on Linux) to capture 16 kHz mono audio and to a
`whisper-cli`/`whisper-cpp` binary to transcribe it. The GGML model (default `tiny`) is downloaded
on first use. If a required tool is missing, `/voice` reports an actionable error with install
hints. The same speech-to-text engine also transcribes inbound Telegram voice messages when running
`infer channels-manager`.

---

## Git Shortcuts

When you run `infer init`, a `.infer/shortcuts/git.yaml` file is created with common git operations:

- `/git status` - Show working tree status
- `/git pull` - Pull changes from remote repository
- `/git push` - Push commits to remote repository
- `/git log` - Show commit logs (last 5 commits)
- `/git commit` - Generate AI commit message from staged changes

### AI-Powered Commit Messages

The `/git commit` shortcut uses the **snippet feature** to generate conventional commit messages:

1. Analyzes your staged changes (`git diff --cached`)
2. Sends the diff to the LLM with a prompt to generate a conventional commit message
3. Automatically commits with the AI-generated message

**Example Usage:**

```bash
# Stage your changes
git add .

# Generate commit message and commit
/git commit
```

The AI will generate a commit message following the conventional commit format (e.g.,
`feat: add user authentication`, `fix: resolve memory leak`).

**Requirements:**

- Run `infer init` to create the shortcuts file
- Stage changes with `git add` before using `/git commit`
- The shortcut uses `jq` to format JSON output

---

## SCM Shortcuts

The SCM (Source Control Management) shortcuts provide seamless integration with GitHub and git workflows.

When you run `infer init`, a `.infer/shortcuts/scm.yaml` file is created with the following shortcuts:

- `/scm issues` - List all GitHub issues for the repository
- `/scm issue <number>` - Show details for a specific GitHub issue with comments
- `/scm pr-create [optional context]` - Generate AI-powered PR plan with branch name, commit, and description

### AI-Powered PR Creation

The `/scm pr-create` shortcut uses the **snippet feature** to analyze your changes and generate a complete PR plan:

1. Analyzes staged or unstaged changes (`git diff`)
2. Sends the diff to the LLM with context about the current and base branches
3. Optionally accepts additional context to help the AI understand the purpose of the changes
4. Generates a comprehensive PR plan including:
   - Suggested branch name (following conventional format: `feat/`, `fix/`, etc.)
   - Conventional commit message
   - PR title and description

This provides a deterministic way to fetch GitHub data and AI assistance for PR planning.

**Example Usage:**

```bash
# List all open issues
/scm issues

# View details for issue #123 including comments
/scm issue 123

# Generate PR plan (basic)
/scm pr-create

# Generate PR plan with additional context
/scm pr-create This fixes the timing issue where conversations were loading too slowly

# Generate PR plan with quoted context (for complex explanations)
/scm pr-create "This implements user-requested feature for dark mode support"
```

**Requirements:**

- [GitHub CLI (`gh`)](https://cli.github.com) must be installed and authenticated
- Run `infer init` to create the shortcuts file
- The commands work in any git repository with a GitHub remote

### Customization

You can customize these shortcuts by editing `.infer/shortcuts/scm.yaml`:

```yaml
shortcuts:
  - name: scm
    description: "Source control management operations"
    command: gh
    subcommands:
      - name: issues
        description: "List all GitHub issues for the repository"
        args:
          - issue
          - list
          - --json
          - number,title,state,author,labels,createdAt,updatedAt
          - --limit
          - "20"
```

**Use Cases:**

- Quickly get context on what issues need to be worked on
- Fetch issue details and comments before implementing a fix
- Let the LLM analyze issue discussions to understand requirements
- Customize the shortcuts to add filters, change limits, or modify output format

---

## Init-Created Shortcuts

Beyond `/git` and `/scm`, `infer init` seeds several more shortcut files in
`.infer/shortcuts/` that wrap common `infer` subcommands and tools:

| Shortcut | File | Description |
| -------- | ---- | ----------- |
| `/mcp <list\|add\|remove\|enable\|disable>` | `mcp.yaml` | Manage MCP servers |
| `/shells` | `shells.yaml` | List running and recent background shell processes |
| `/export` | `export.yaml` | Export the current conversation to markdown |
| `/env` | `env.yaml` | Generate a `.env.example` with all provider API keys |
| `/agents <list\|add\|remove\|enable\|disable>` | `a2a.yaml` | Manage A2A agents |
| `/skills <list\|install\|uninstall>` | `skills.yaml` | Manage Agent Skills |

These are regular YAML shortcuts - edit or remove them like any other file in
`.infer/shortcuts/`.

---

## AI-Powered Snippets

Shortcuts can use the **snippet feature** to integrate LLM-powered workflows directly into YAML
configuration. This enables complex AI-assisted tasks without writing Go code.

### How Snippets Work

1. **Command Execution**: The shortcut runs a command that outputs JSON data
2. **Prompt Generation**: A prompt template is filled with the JSON data and sent to the LLM
3. **Template Filling**: The final template is filled with both JSON data and the LLM response
4. **Result Display**: The filled template is shown to the user or executed

### Snippet Configuration

```yaml
shortcuts:
  - name: example-snippet
    description: "Example AI-powered shortcut"
    command: bash
    args:
      - -c
      - |
        # Command must output JSON
        jq -n --arg data "Hello" '{message: $data}'
    snippet:
      prompt: |
        You are given this data: {message}
        Generate a response based on it.
      template: |
        ## AI Response
        {llm}
```

### Placeholder Syntax

- `{fieldname}` - Replaced with values from the command's JSON output
- `{llm}` - Replaced with the LLM's response to the prompt

### Real-World Example: AI Commit Messages

The `/git commit` shortcut demonstrates the snippet feature:

```yaml
shortcuts:
  - name: git
    description: "Common git operations"
    command: git
    subcommands:
      - name: commit
        description: "Generate AI commit message from staged changes"
        command: bash
        args:
          - -c
          - |
            if ! git diff --cached --quiet 2>/dev/null; then
              diff=$(git diff --cached)
              jq -n --arg diff "$diff" '{diff: $diff}'
            else
              echo '{"error": "No staged changes found."}'
              exit 1
            fi
        snippet:
          prompt: |
            Generate a conventional commit message.

            Changes:
            ```diff
            {diff}
            ```

            Format: "type: Description"
            - Type: feat, fix, docs, refactor, etc.
            - Description: "Capital first letter, under 50 chars"

            Output ONLY the commit message.
          template: "!git commit -m \"{llm}\""
```

**How This Works:**

1. Command runs `git diff --cached` and outputs JSON: `{"diff": "..."}`
2. Prompt template receives the diff via `{diff}` placeholder
3. LLM generates commit message (e.g., `feat: Add user authentication`)
4. Template receives LLM response via `{llm}` placeholder
5. Final command executed: `git commit -m "feat: Add user authentication"`

### Command Execution Prefix

If the template starts with `!`, the result is executed as a shell command:

```yaml
template: "!git commit -m \"{llm}\""  # Executes the command
template: "{llm}"                      # Just displays the result
```

### Use Cases for Snippets

- Generate commit messages from diffs
- Create PR descriptions from changes
- Analyze test output and suggest fixes
- Generate code documentation from source
- Transform data formats with AI assistance
- Automate complex workflows with AI decision-making

---

## User-Defined Shortcuts

You can create custom shortcuts by adding YAML configuration files in the `.infer/shortcuts/` directory.

### Configuration File Format

Create files named `custom-*.yaml` (e.g., `custom-1.yaml`, `custom-dev.yaml`) in `.infer/shortcuts/`:

```yaml
shortcuts:
  - name: tests
    description: "Run all tests in the project"
    command: go
    args:
      - test
      - ./...
    working_dir: .  # Optional: set working directory

  - name: build
    description: "Build the project"
    command: go
    args:
      - build
      - -o
      - infer
      - .

  - name: lint
    description: "Run linter on the codebase"
    command: golangci-lint
    args:
      - run
```

### Configuration Fields

- **name** (required): The shortcut name (used as `/name`)
- **description** (required): Human-readable description shown in `/help`
- **command** (required): The executable command to run
- **args** (optional): Array of arguments to pass to the command
- **working_dir** (optional): Working directory for the command (defaults to current)
- **snippet** (optional): AI-powered snippet configuration with `prompt` and `template` fields

### Using Shortcuts

With the configuration above, you can use:

- `/tests` - Runs `go test ./...`
- `/build` - Runs `go build -o infer .`
- `/lint` - Runs `golangci-lint run`

You can also pass additional arguments:

- `/tests -v` - Runs `go test ./... -v`
- `/build --race` - Runs `go build -o infer . --race`

---

## Advanced Usage

### Example Custom Shortcuts

Here are some useful shortcuts you might want to add:

**Development Shortcuts (`custom-dev.yaml`):**

```yaml
shortcuts:
  - name: fmt
    description: "Format all Go code"
    command: go
    args:
      - fmt
      - ./...

  - name: "mod tidy"
    description: "Tidy up go modules"
    command: go
    args:
      - mod
      - tidy

  - name: version
    description: "Show current version"
    command: git
    args:
      - describe
      - --tags
      - --always
      - --dirty
```

**Docker Shortcuts (`custom-docker.yaml`):**

```yaml
shortcuts:
  - name: "docker build"
    description: "Build Docker image"
    command: docker
    args:
      - build
      - -t
      - myapp
      - .

  - name: "docker run"
    description: "Run Docker container"
    command: docker
    args:
      - run
      - -p
      - "8080:8080"
      - myapp
```

**Project-Specific Shortcuts (`custom-project.yaml`):**

```yaml
shortcuts:
  - name: migrate
    description: "Run database migrations"
    command: ./scripts/migrate.sh
    working_dir: .

  - name: seed
    description: "Seed database with test data"
    command: go
    args:
      - run
      - cmd/seed/main.go
```

### Tips

1. **File Organization**: Use descriptive names for your config files (e.g., `custom-dev.yaml`, `custom-docker.yaml`)
2. **Command Discovery**: Use `/help` to see all available shortcuts including your custom ones
3. **Error Handling**: If a custom shortcut fails to load, it will be skipped with a warning
4. **Reloading**: Restart the chat session to reload custom shortcuts after making changes
5. **Security**: Be careful with custom shortcuts as they execute system commands

---

## Troubleshooting

### Shortcut Not Appearing

- **Check YAML syntax**: Ensure your configuration file is valid YAML
- **Check file naming**: Files must be named `custom-*.yaml` (not `shortcut-*.yaml` or other patterns)
- **Check location**: Files must be in `.infer/shortcuts/` directory
- **Restart chat**: Restart the chat session to reload shortcuts

### Command Not Found

- **Check PATH**: Ensure the command is available in your system PATH
- **Use absolute paths**: For custom scripts, use absolute paths or `./script.sh`
- **Test manually**: Try running the command directly in your terminal first

### Permission Denied

- **Check file permissions**: Ensure script files are executable (`chmod +x script.sh`)
- **Check directory permissions**: Ensure the working directory is accessible
- **Check user permissions**: Ensure you have permission to run the command

### Invalid YAML

- **Use a validator**: Use an online YAML validator or `yamllint` to check syntax
- **Check indentation**: YAML is sensitive to indentation (use spaces, not tabs)
- **Check quotes**: Use quotes for strings with special characters
- **Check arrays**: Ensure arrays are properly formatted with `-` prefix

### Snippet Not Working

- **Check JSON output**: Ensure your command outputs valid JSON
- **Check placeholders**: Ensure placeholders match JSON fields exactly
- **Check template syntax**: Ensure template uses correct placeholder syntax `{field}`
- **Test command separately**: Run the command manually to verify JSON output

---

[← Back to README](../README.md)
