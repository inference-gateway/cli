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
- `/insights [24h|7d|30d]` - Analyze past sessions for repeatable workflows worth a skill and recurring tool failures; saves a report to `~/.infer/insights/`.
  Vendored as `~/.infer/shortcuts/insights.yaml`, wrapping `infer insights` - edit it to pin a model or change the windows.
- `/reset [insights|confirm]` - Wipe the local runtime state (conversations, plans, scratch, artifacts, history, backups, exports, logs) of
  **every project on this machine**.
  `/reset` previews and deletes nothing, `/reset insights` analyzes the sessions first, `/reset confirm` performs the wipe.
  Config and saved insights are preserved, remote stores are skipped.
  Vendored as `~/.infer/shortcuts/reset.yaml`, wrapping `infer reset` - edit or delete it like any other shortcut.
  A chat session running during the wipe keeps the conversation it already has in memory; run `/new` or restart to be fully fresh.
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

- `/diff` - Open the changes panel (interactive diff viewer); keybindings show as a legend at the bottom, updated per view (tree / patch / PR tab)
- `/explorer` - Open the file explorer (tree + fuzzy finder)
- `/tools` - Show the tools available to the agent (read-only, filterable list)
- `/a2a` - Show registered A2A agents and their status (requires A2A)
- `/tasks` - Show the A2A task-management interface (requires A2A)
- `/release-notes [version]` - Show GitHub release notes for a version or the latest (requires the `gh` CLI installed and authenticated)

**Project setup:**

- `/init` - Set input with project analysis prompt for AGENTS.md generation
- `/install-opentask [owner/repo] [extra context...]` - Install the OpenTask GitHub workflow via the
  chat agent. The shortcut sends an install task to the agent as a regular chat message, so it streams
  in the conversation like any other turn: the agent creates or updates `.github/workflows/tasks.yml`
  on an install branch and opens (or updates) a pull request. `owner/repo` targets another repository
  (omit it to use the current checkout's) and any extra text is passed along as workflow configuration.

### Project Initialization Shortcut

The `/init` shortcut populates the input field with a configurable prompt for generating an AGENTS.md
file. This allows you to:

1. Type `/init` to populate the input with the project analysis prompt
2. Review and optionally modify the prompt before sending
3. Press Enter to send the prompt and watch the agent analyze your project interactively

The prompt is configurable in `prompts.yaml` under `init.prompt` (env `INFER_PROMPTS_INIT_PROMPT`).
The default prompt instructs the agent to:

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
`infer daemon`.

### Image Generation

There is no `/image` shortcut. Just ask for the image in plain language while
chatting with any model - the chat model calls the `ImageGeneration` tool when
it recognises the intent:

1. Ask for `a cat in a spacesuit` (or a meme of whatever is in the context)
2. The tool sends the prompt as a plain one-off request to the gateway's
   `POST /v1/images/generations` endpoint using the configured image model
   (`tools.image_generation.model`, default `openai/gpt-image-2`) - no system
   prompt, no tools
3. The returned image is decoded (base64 payload) or downloaded (URL), written to
   the session's artifacts dir under `~/.infer/projects/<project-slug>/artifacts/`,
   and the saved path is returned

Editing and variations work the same way. Ask to edit an existing image and the
chat model calls the `ImageEdit` tool, which reads the image from a local file
path and sends it with your prompt to `POST /v1/images/edits`
(`tools.image_edit.model`). Ask for variations of an image and the
`ImageVariation` tool sends the local file to `POST /v1/images/edits`
(`tools.image_variation.model`). Results are saved under the session's artifacts
dir the same way as generation.

Image models are not selectable from the `/model` selector - they are recognised
by gateway-reported modalities, not by name. The model list keeps only models
whose modalities are chat-capable (text in, text out) and drops models that
report no modalities at all, so image-generation models are filtered out of the
selectable list (non-chat models can still be listed as view-only rows); they
are only reachable through these tools. `quality` defaults to
`low` and `size` to `1024x1024` - ask explicitly for high quality or a larger
size to pay for it. Disable the tools with `tools.image_generation.enabled: false`,
`tools.image_edit.enabled: false`, or `tools.image_variation.enabled: false`.
Inline terminal rendering and the `n` option are not supported yet.

---

## Git Shortcuts

When you run `infer init`, a `~/.infer/shortcuts/git.yaml` file is created with common git operations:

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
`feat(chat): add user authentication`, `fix(parser): resolve memory leak`).

**Requirements:**

- Run `infer init` to create the shortcuts file
- Stage changes with `git add` before using `/git commit`
- The shortcut uses `jq` to format JSON output

---

## SCM Shortcuts

The SCM (Source Control Management) shortcuts provide seamless integration with GitHub and git workflows.

When you run `infer init`, a `~/.infer/shortcuts/scm.yaml` file is created with the following shortcuts:

- `/scm issues` - List all GitHub issues for the repository
- `/scm issue <number>` - Show details for a specific GitHub issue with comments

**Example Usage:**

```bash
# List all open issues
/scm issues

# View details for issue #123 including comments
/scm issue 123
```

**Requirements:**

- [GitHub CLI (`gh`)](https://cli.github.com) must be installed and authenticated
- Run `infer init` to create the shortcuts file
- The commands work in any git repository with a GitHub remote

### Customization

You can customize these shortcuts by editing `~/.infer/shortcuts/scm.yaml`:

```yaml
shortcuts:
  - name: scm
    description: "Source control management operations"
    command: gh
    subcommands:
      - name: issues
        description: "List all GitHub issues for the repository"
        command: gh
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
`~/.infer/shortcuts/` that wrap common `infer` subcommands and tools:

| Shortcut | File | Description |
| -------- | ---- | ----------- |
| `/mcp <list\|add\|remove\|enable\|disable>` | `mcp.yaml` | Manage MCP servers |
| `/shells` | `shells.yaml` | List running and recent background shell processes |
| `/export` | `export.yaml` | Export the current conversation to markdown |
| `/env` | `env.yaml` | Generate a `.env.example` with all provider API keys |
| `/agents <list\|add\|remove>` | `a2a.yaml` | Manage A2A agents |
| `/skills <list\|install\|uninstall>` | `skills.yaml` | Manage Agent Skills |

These are regular YAML shortcuts - edit or remove them like any other file in
`~/.infer/shortcuts/`.

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

            Format: "type(scope): Description"
            - Type: feat, fix, docs, refactor, etc.
            - Scope: the domain being worked on (e.g. chat, parser)
            - Description: "lowercase start, under 50 chars"

            Output ONLY the commit message.
          template: "!git commit -m \"{llm}\""
```

**How This Works:**

1. Command runs `git diff --cached` and outputs JSON: `{"diff": "..."}`
2. Prompt template receives the diff via `{diff}` placeholder
3. LLM generates commit message (e.g., `feat(chat): Add user authentication`)
4. Template receives LLM response via `{llm}` placeholder
5. Final command executed: `git commit -m "feat(chat): Add user authentication"`

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

You can create custom shortcuts by adding YAML configuration files in the
`~/.infer/shortcuts/` directory. A project `./.infer/shortcuts/` is also read and
overlaid on top by shortcut name, so a repo can add its own or replace a single
userspace entry without losing the rest.

### Configuration File Format

Create `*.yaml` files in `~/.infer/shortcuts/` (or the project `./.infer/shortcuts/`). The file name
does not matter - every `.yaml` file in the directory is loaded, which is how the seeded `git.yaml`
and `scm.yaml` load. Note that `.yml` files are not picked up:

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
- `/build` - Runs `go build -o infer ./cmd/infer`
- `/lint` - Runs `golangci-lint run`

You can also pass additional arguments:

- `/tests -v` - Runs `go test ./... -v`
- `/build --race` - Runs `go build -race -o infer ./cmd/infer`

---

## Advanced Usage

### Example Custom Shortcuts

Here are some useful shortcuts you might want to add:

A shortcut name cannot contain spaces: the first whitespace-separated token of the input is always the
shortcut name, so `/docker build` looks up a shortcut called `docker` and passes `build` as an argument.
To expose several related operations, declare one shortcut with `subcommands:` and invoke it as
`/name subcommand`. When a subcommand does not define its own `command:`, the final args are the
shortcut's `args:`, then the subcommand's name, then the subcommand's `args:` - so a subcommand's
`name:` is usually the actual CLI subcommand and its `args:` carries only the extra flags.

**Development Shortcuts (`custom-dev.yaml`):**

```yaml
shortcuts:
  - name: fmt
    description: "Format all Go code"
    command: go
    args:
      - fmt
      - ./...

  - name: mod
    description: "Go module operations"
    command: go
    args:
      - mod
    subcommands:
      - name: tidy
        description: "Tidy up go modules"

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
  - name: docker
    description: "Docker operations"
    command: docker
    subcommands:
      - name: build
        description: "Build Docker image"
        args:
          - -t
          - myapp
          - .

      - name: run
        description: "Run Docker container"
        args:
          - -p
          - "8080:8080"
          - myapp
```

`/mod tidy` runs `go mod tidy`; `/docker build` runs `docker build -t myapp .` and
`/docker run` runs `docker run -p 8080:8080 myapp`.

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
- **Check file naming**: Files must end in `.yaml` - any file name works (not just `custom-*.yaml`),
  but `.yml` files are not picked up
- **Check location**: Files must be in `~/.infer/shortcuts/` (or the project `./.infer/shortcuts/`)
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
