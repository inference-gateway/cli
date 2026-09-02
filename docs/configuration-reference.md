# Configuration Reference

[← Back to README](../README.md)

This document provides comprehensive configuration documentation for the Inference Gateway CLI, including
all configuration options, environment variables, and best practices.

## Table of Contents

- [Configuration System Overview](#configuration-system-overview)
- [Configuration Layers](#configuration-layers)
- [Configuration Precedence](#configuration-precedence)
- [Default Configuration](#default-configuration)
- [Configuration Options](#configuration-options)
- [Environment Variables](#environment-variables)
- [Environment Variable Substitution](#environment-variable-substitution)
- [Configuration Best Practices](#configuration-best-practices)
- [Configuration Validation and Troubleshooting](#configuration-validation-and-troubleshooting)

---

## Configuration System Overview

The CLI uses a powerful 2-layer configuration system built on [Viper](https://github.com/spf13/viper),
supporting multiple configuration sources with proper precedence handling.

---

## Configuration Layers

1. **Userspace Configuration** (`~/.infer/config.yaml`)
   - The shared baseline for every project, and the CLI's only default write
     location - all runtime state (conversations, logs, history, artifacts)
     lives under `~/.infer/` too
   - Created with: `infer init` (full baseline) or `infer config init`
     (`config.yaml` only)

2. **Project Configuration** (`.infer/config.yaml` in current directory)
   - An *optional*, sparse override layer that takes precedence over the
     userspace baseline. It only exists if you create it; the CLI never
     populates a project `.infer/` on its own
   - Created with: `infer config set --project <key> <value>`, which writes
     only the keys you set (or by hand). Include just the keys you want to
     override - everything else is inherited

---

## Configuration Precedence

Configuration values are resolved in the following order (highest to lowest priority):

1. **Environment Variables** (`INFER_*` prefix) - **Highest Priority**
2. **Command Line Flags**
3. **Project Config** (`.infer/config.yaml`)
4. **Userspace Config** (`~/.infer/config.yaml`)
5. **Built-in Defaults** - **Lowest Priority**

**Example**: If your userspace config sets `agent.model: "anthropic/claude-4"` and your project config
sets `agent.model: "deepseek/deepseek-v4-pro"`, the project config wins. However, if you also set
`INFER_AGENT_MODEL="openai/gpt-4"`, the environment variable takes precedence over both config files.

> **List-valued keys replace, they do not merge.** Viper's `MergeInConfig`
> deep-merges maps but substitutes slices wholesale, so a list in the project
> layer (e.g. `tools.sandbox.directories`, `tools.bash.mode.*.allow`,
> `tools.web_fetch.allowed_domains`) *replaces* the userspace value rather than
> extending it. Keep project overrides sparse for this reason.

### Usage Examples

```bash
# Seed the userspace baseline (shared across all projects)
infer init

# Add a sparse project override on top (takes precedence)
infer config set agent.model "deepseek/deepseek-v4-pro" --project

# Both layers are automatically merged when commands are run
```

You can also specify a custom config file using the `--config` flag which will override the automatic 2-layer loading.

---

## Default Configuration

Below is the complete default configuration with all available options:

```yaml
gateway:
  url: http://localhost:8080
  api_key: ""
  timeout: 200
  oci: ghcr.io/inference-gateway/inference-gateway:latest  # OCI image for Docker mode
  run: true    # Automatically run the gateway (enabled by default)
  docker: true  # Use Docker mode by default (set to false for binary mode)
  include_models: []  # Optional: only allow specific models (allowlist)
  exclude_models: []  # Optional: blocklist of specific models (opt-in; the picker already hides non-chat models by modalities)
client:
  timeout: 200
  stall_threshold_sec: 30
  retry:
    enabled: true
    max_attempts: 5
    initial_backoff_sec: 5
    max_backoff_sec: 60
    backoff_multiplier: 2
    retryable_status_codes: [408, 429, 500, 502, 503, 504]
logging:
  debug: false
  dir: "" # Override log directory (defaults to ~/.infer/logs)
  stdout: false # Also write logs to stdout/stderr in addition to the log file
  archive:
    enabled: true # Automatically archive oversized log files (default: true)
    max_size_mb: 1024 # Threshold in MB; files exceeding this are gzip-compressed and truncated (default: 1024 = 1 GB)
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
    # Per-mode allow-list (default-deny). The effective list for a mode is
    # mode.all.allow unioned with that mode's own list. Each entry is a regex
    # matched against the WHOLE command (so " .*" allows arguments and a bare
    # token matches only itself). A clean-command guard still blocks command
    # substitution, pipes/chains, file-write redirects, dangerous find, and
    # leaking a $VAR - except in a mode whose list is the ".*" sentinel.
    mode:
      all: # baseline applied in every mode (read-only / non-mutating)
        allow:
          - echo( .*)?
          - ls( .*)?
          - pwd( .*)?
          - git status( .*)?
          - git log( .*)?
          - git diff( .*)?
          - gh (issue|pr|repo|release|run|workflow) (list|view|status|diff|checks)( .*)?
      plan: # read-only planning mode adds nothing
        allow: []
      standard: # interactive default: baseline only (same as plan)
        allow: []
      auto: # headless `infer headless`: full autonomy (commit/push/etc.). Replace
        # ".*" with a curated list for CI with secrets so the guard re-applies.
        allow:
          - .*
  read:
    enabled: true
    require_approval: false
  write:
    enabled: true
    require_approval: true # Write operations require approval by default for security
  edit:
    enabled: true
    require_approval: true # Edit operations require approval by default for security
    strict_whitespace: false # When true, disable the indentation-tolerant fallback (byte-exact matching only)
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
    allowed_domains:
      - golang.org
      - agents.md
    safety:
      max_size: 8192 # 8KB
      timeout: 30 # 30 seconds
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
  image_generation:
    enabled: true
    model: openai/gpt-image-2 # Image model for one-off /v1/images/generations requests
    require_approval: false
  safety:
    require_approval: true
    # How an action that needs approval is delivered: prompt (TUI in chat, IPC
    # under the channel manager, else blocked), ipc (force IPC), judge (LLM
    # judge decides, see judge.yaml), or block (reject).
    approval_behaviour: prompt
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
  max_turns: 50 # Maximum number of turns for agent sessions
  max_tokens: 4096 # The maximum number of tokens that can be generated per request
  max_concurrent_tools: 5 # Maximum concurrent tool executions
chat:
  theme: tokyo-night
  status_bar:
    enabled: true
    indicators:
      model: true
      theme: true
      max_output: false
      a2a_agents: true
      tools: true
      background_shells: true
      mcp: true
      context_usage: true
      session_tokens: true
      git_branch: true
compact:
  enabled: true # Enable automatic conversation compaction
  auto_at: 80 # Compact when context reaches this percentage (20-100)
```

---

## Configuration Options

### Gateway Settings

- **gateway.url**: The URL of the inference gateway (default: `http://localhost:8080`)
- **gateway.api_key**: API key for authentication (if required)
- **gateway.timeout**: Request timeout in seconds (default: 200)
- **gateway.run**: Automatically run the gateway on startup (default: `true`)
  - When enabled, the CLI automatically starts the gateway before running commands
  - The gateway runs in the background and shuts down when the CLI exits
- **gateway.docker**: Use Docker instead of binary mode (default: `true`)
  - `true` (default): Uses Docker to run the gateway container (requires Docker installed)
  - `false`: Downloads and runs the gateway as a binary (no Docker required)
- **gateway.oci**: OCI image to use for Docker mode (default: `ghcr.io/inference-gateway/inference-gateway:latest`)
- **gateway.include_models**: Only allow specific models (allowlist approach, default: `[]`, allows all models)
  - When set, only the specified models will be allowed by the gateway
  - Example: `["deepseek/deepseek-v4-pro", "deepseek/deepseek-v4-flash"]`
  - This is passed to the gateway as the `ALLOWED_MODELS` environment variable
- **gateway.exclude_models**: Block specific models (blocklist approach, default: `[]`, blocks none)
  - The model picker itself lists only chat-capable models: it keeps a model only when the gateway
    reports modalities for it *and* those modalities are text in, text out. Speech-to-text
    (e.g. `groq/whisper-*`), text-to-speech (e.g. `openai/tts-*`, `groq/playai-tts*`),
    image-generation and embedding models are hidden without any configuration
  - A model the gateway reports no modalities for (`"modalities": null`) is treated as not
    chat-capable and hidden too, since that is how most speech and embedding models arrive
  - Opt-in only: `exclude_models` starts empty and is a user knob for hiding specific models the
    gateway reports (large or costly chat models, for example) - there is no shipped default blocklist
  - When set, all models are allowed except those in the list
  - Example: `["openai/gpt-4", "anthropic/claude-4-opus"]`
  - This is passed to the gateway as the `DISALLOWED_MODELS` environment variable
  - Note: `include_models` and `exclude_models` can be used together - the gateway will apply both filters

### Client Settings

- **client.timeout**: HTTP client timeout in seconds
- **client.stall_threshold_sec**: Seconds without progress - no response while connecting, no chunk while streaming - before
  the request counts as stalled (default: `30`, `0` disables). The chat UI shows a reconnecting indicator and the agent drops
  the connection and retries, up to `client.retry.max_attempts` times with exponential backoff. Keep it above your provider's
  worst first-token latency - a stall retry restarts the response from scratch
- **client.retry.enabled**: Enable automatic retries for failed requests
- **client.retry.max_attempts**: Maximum number of retry attempts (default: `5`)
- **client.retry.initial_backoff_sec**: Initial delay between retries in seconds
- **client.retry.max_backoff_sec**: Maximum delay between retries in seconds
- **client.retry.backoff_multiplier**: Backoff multiplier for exponential delay
- **client.retry.retryable_status_codes**: HTTP status codes that trigger retries (default: `[408, 429, 500, 502, 503, 504]`);
  non-transient errors such as `401` are deliberately excluded so they fail fast with the real message

### Logging Settings

- **logging.debug**: Enable debug logging for verbose output
- **logging.dir**: Override the log directory (defaults to `~/.infer/logs`)
- **logging.stdout**: Also write logs to stdout/stderr in addition to the log file (default: `false`)
- **logging.archive.enabled**: Enable automatic log archiving (default: `true`).
  When enabled, log files exceeding the size threshold are gzip-compressed and
  truncated.
- **logging.archive.max_size_mb**: Maximum log file size in MB before archiving is triggered (default: `1024`, i.e. 1 GB). Set via `INFER_LOGGING_ARCHIVE_MAX_SIZE_MB`.

### Tool Settings

- **tools.enabled**: Enable/disable tool execution for LLMs (default: true)
- **tools.sandbox.directories**: Allowed directories for tool operations (default: [".", "/tmp"])
- **tools.sandbox.protected_paths**: Paths excluded from tool access for security (default: [".infer/", ".git/", "*.env"])
- **tools.bash.mode.\<mode\>.allow**: Per-mode bash allow-list (regexes matched against the whole command). `<mode>` is one of `all`
  (baseline applied in every mode), `plan`, `standard`, or `auto`. The effective list is `mode.all.allow` unioned with the active mode's
  list. Anything unmatched is denied (approval in chat, rejection in headless agent mode). The `.*` sentinel (default for `auto`) means
  unrestricted.
- **tools.safety.require_approval**: Whether a tool needs approval at all (default: true; a per-tool `require_approval` overrides it)
- **tools.safety.approval_behaviour**: *How* a needed approval is delivered (default: `prompt`). Env: `INFER_TOOLS_SAFETY_APPROVAL_BEHAVIOUR`.
  - `prompt` - ask an interactive approver via whatever channel is attached: a TUI prompt in chat, IPC under the channel manager
    (Telegram); if none is reachable (CI/heartbeat) the action is **blocked** with a reason.
  - `ipc` - force stdin/stdout IPC approval; blocked when no broker is attached.
  - `judge` - one LLM judge call decides (see [Judge Approval](#judge-approval-judgeyaml)). The judge is always reachable
    (headless and CI included), so unlike `ipc` it is never downgraded to block.
  - `block` - reject immediately with a reason, never ask.

  The default makes headless runs **secure by default**: an off-allow-list or mutating action is blocked in CI and sent for approval under
  the channel manager, instead of running unattended. For a controlled-autonomy CI profile, set `block` and grant only what the agent needs
  (e.g. `tools.write.require_approval: false` plus a curated bash allow-list / the `mode.all` append override).
- **Individual tool settings**: Each tool (Bash, Read, Write, Edit, Delete, Grep, Tree, WebFetch, WebSearch, TodoWrite) has:
  - **enabled**: Enable/disable the specific tool
  - **require_approval**: Override global safety setting for this tool (optional)
- **tools.edit.strict_whitespace**: `false` (default) enables indentation-tolerant matching for Edit/MultiEdit; `true` requires byte-exact

### Vision Settings

Frame sources and the image-annotation pipeline that lets text-only models "see" screen and camera
frames. Not to be confused with **gateway.vision_enabled**, which is an unrelated gateway-side flag.

- **vision.annotator.enabled**: Enable the image annotator (default: false). When enabled,
  `GetLatestFrame` defaults to annotated (text) output and the `ImageDecode` tool is registered, so
  text-only models can understand frames without any per-model configuration.
- **vision.annotator.model**: `provider/model` reference of the vision model to side-call through
  the configured gateway (default: `anthropic/claude-haiku-4-5-20251001`). The gateway also serves fully local
  models, so offline annotation is just a local provider (e.g. `ollama/qwen3-vl:2b`)
- **vision.annotator.max_tokens**: Annotation response budget (default: 1024)
- **vision.annotator.timeout**: Annotation timeout in seconds (default: 120)
- **vision.sources.\<name\>**: Named frame sources beyond the built-in `screen` source (registered
  when computer-use screenshot streaming is on). Each entry: **type** (`directory`), **path** (newest
  image file by mtime is served), optional **prompt** (per-source annotator prompt override), and
  optional **retention** (`max_files`, `max_age` e.g. `24h`) pruning the directory after reads.

```yaml
vision:
  annotator:
    enabled: true
    model: anthropic/claude-haiku-4-5-20251001 # any vision model served by your gateway
  sources:
    camera-front:
      type: directory
      path: .infer/frames/front # wherever your camera process writes frames
      retention:
        max_files: 100
        max_age: 24h
```

### Compact Settings

- **compact.enabled**: Enable automatic mid-conversation compaction at the `auto_at`
  threshold to reduce token usage (default: true). This flag does **not** gate
  compaction on plan approval - approving a plan in [Plan Mode](plan-mode.md) always
  summarizes the exploration-heavy planning conversation and continues execution in a
  fresh, smaller session, regardless of this setting.
- **compact.auto_at**: Percentage of context window (20-100) at which to automatically trigger compaction (default: 80)

### Agent Settings

- **agent.model**: Default model for agent operations
- **agent.system_prompt**: System prompt included with every agent session. It stays byte-stable for the whole session - including
  across agent-mode switches (Shift+Tab) - so local LLM servers keep KV-cache prefix hits
- **agent.mode_adjustment_plan**: Optional per-mode instructions (NOT a system prompt) delivered as the `{guidance}` of the mode-change
  reminder when the agent enters Plan Mode. Ships empty; the built-ins live in the mode-change-reminder guidance in reminders.yaml.
- **agent.mode_adjustment_auto**: Same for auto-accept mode, carrying the destructive-action policy.
- System reminders are configured in their own `reminders.yaml`, not under `agent:` - see [System Reminders](#system-reminders-remindersyaml) below.
- **agent.max_turns**: Maximum number of turns for agent sessions (default: 50)
- **agent.max_tokens**: Maximum tokens per agent request (default: 8192)
- **agent.max_concurrent_tools**: Maximum number of tools that can execute concurrently (default: 5)

### System Reminders (reminders.yaml)

System reminders inject short `<system-reminder>` messages into the conversation at
defined points of the agent loop, keeping durable guidance in context without bloating
the system prompt. They live in their own file, **`reminders.yaml`** (userspace
`~/.infer/reminders.yaml`, seeded by `infer init`, with an optional project
`./.infer/reminders.yaml` override).
When the file is absent the built-in defaults are used.

```yaml
enabled: true # master switch for all reminders
merge: false  # merge=true: merge entries onto built-in defaults by name instead of replacing them
reminders:
  - name: todo-hygiene # unique identifier (required)
    text: | # reminder body injected into the conversation (required)
      <system-reminder>Your todo list is empty ...</system-reminder>
    hook: pre_stream # where in the loop it fires (default: pre_stream)
    trigger: interval # when it fires at that hook (default: always)
    interval: 10 # trigger: interval - fire every Nth session turn
    threshold: 3 # trigger: turns_before_max - fire once within N turns of max_turns
```

**Hook points** (`hook`): `pre_session`, `post_session`, `pre_stream`, `post_stream`,
`pre_tool`, `post_tool`, `pre_queue_drain`, `post_queue_drain`.

**Triggers** (`trigger`) gate which firings of a hook a reminder acts on:

| Trigger | Fires |
| --- | --- |
| `always` | Every time the hook point fires (default). |
| `interval` | Every Nth session turn (`interval`, default 10). |
| `turns_before_max` | Within `threshold` turns of `max_turns` (requires `threshold > 0`). |
| `once` | The first firing of its hook point this run. |
| `on_failure` | **`post_tool` only** - fires only when the tool call that just ran failed. Requires `hook: post_tool`. |

The `on_failure` trigger lets a consumer nudge the model only when a change did not
happen (a failed tool call), instead of paying the per-turn cost of an `always` reminder.

#### Supplying reminders without a file

Embedded/CI consumers can provide reminders without writing `reminders.yaml`:

- **`INFER_REMINDERS_CONFIG`** - inline YAML with the same schema as the file; when set it
  replaces the file-loaded config.
- **`--reminders-file PATH`** - load reminders from an arbitrary path (not constrained to
  `~/.infer/`), available on `infer headless` and `infer chat`.

Precedence, highest first: `INFER_REMINDERS_CONFIG` → `--reminders-file` → project
`./.infer/reminders.yaml` → `~/.infer/reminders.yaml` → built-in defaults.
`INFER_REMINDERS_ENABLED` toggles the master switch on top of whichever source is used.

#### Merging onto defaults (`merge: true`)

By default, a supplied reminders config **replaces** the built-in defaults entirely
(`todo-hygiene` plus the memory reminders). Set `merge: true` at the top level to
**merge** onto the built-in set by name instead:

- A supplied entry whose `name` matches a built-in **overrides** that entry in-place.
- Entries with new names are **appended** to the built-in list.
- Built-in entries not overridden survive untouched.

This lets consumers add a custom reminder without re-declaring `memory-consult` and
`memory-hygiene`:

```yaml
enabled: true
merge: true
reminders:
  - name: my-custom-reminder
    text: "<system-reminder>Custom nudge</system-reminder>"
    hook: pre_stream
    trigger: interval
    interval: 5
```

The `merge` flag works with all three supply paths (`INFER_REMINDERS_CONFIG`,
`--reminders-file`, and file-based). `pruneMemoryRemindersIfDisabled` (which strips
`memory-consult`/`memory-hygiene` by name when memory is off) continues to work
correctly against the merged list.

> **Caveat:** `pruneMemoryRemindersIfDisabled` prunes by name, so any reminder
> named `memory-consult` or `memory-hygiene` is dropped when memory is disabled,
> **even if you overrode its content via `merge: true`**. If you override a
> memory-named reminder and need it to survive with memory off, either rename it
> or enable memory (`memory.enabled: true` in `memory.yaml`).

### Judge Approval (judge.yaml)

Tool calls that need approval can be decided by an LLM judge instead of a human -
selected by the `auto-with-judge` agent mode or by `tools.safety.approval_behaviour:
judge`. The judge call is a one-shot side call through the configured gateway; see
[Judge Mode](judge-mode.md) for the behaviour and the verdict contract.
The judge is configured in its own file, **`judge.yaml`** (project
`./.infer/judge.yaml` overrides userspace `~/.infer/judge.yaml`; when the file is
absent the built-in defaults are used).

```yaml
model: "" # "provider/model" id for judge calls; empty falls back to agent.model
gateway_url: "" # send judge calls to another gateway (e.g. real judge, mock driver); empty shares the agent's
timeout: 30 # per-call timeout in seconds
max_tokens: 2048 # response budget; reasoning models spend their thinking against it too
on_error: deny # what a failed judge call means: deny (default) or allow
system_prompt: |- # judge instructions (system message)
      You are the approver for an autonomous coding agent. ...
prompt: |- # user message template with {root_intent} (first user message), {intent} (latest) and {action} (tool call)
      <root_request>
      {root_intent}
      </root_request>

      <latest_request>
      {intent}
      </latest_request>

      <tool_call>
      {action}
      </tool_call>
```

- **judge.model**: `provider/model` reference for judge calls; empty falls back to
      `agent.model` (same precedent as conversation title generation). Selecting the
      judge with neither resolvable fails config validation at startup.
- **judge.timeout**: per-call timeout in seconds (default: 30)
- **judge.max_tokens**: response budget per judge call (default: 2048; reasoning models spend their thinking against it)
- **judge.on_error**: what a failed judge call means - `deny` (default, fail closed,
      same default as the no-approver block path) or `allow`
- **judge.system_prompt**: the judge's instructions, sent as the system message so
      the user text and tool arguments stay data rather than instructions
- **judge.prompt**: user-message template; `{root_intent}` is the first non-hidden
      user message of the session, `{intent}` the latest one (a bare "continue" is
      judged next to the root it continues) and `{action}` the pending tool call

Environment overrides (env wins over the file): `INFER_JUDGE_MODEL`, `INFER_JUDGE_GATEWAY_URL`,
`INFER_JUDGE_TIMEOUT`, `INFER_JUDGE_MAX_TOKENS`, `INFER_JUDGE_ON_ERROR`,
`INFER_JUDGE_SYSTEM_PROMPT`, `INFER_JUDGE_PROMPT`.

### Web Search Settings

- **web_search.enabled**: Enable/disable web search tool for LLMs (default: true)
- **web_search.default_engine**: Default search engine to use ("duckduckgo" or "google", default: "duckduckgo")
- **web_search.max_results**: Maximum number of search results to return (1-50, default: 10)
- **web_search.engines**: List of available search engines
- **web_search.timeout**: Search timeout in seconds (default: 10)

### Chat Interface Settings

- **chat.theme**: Chat interface theme name (default: "tokyo-night")
  - Available themes: `tokyo-night`, `github-light`, `dracula`
  - Can be changed during chat using `/theme [theme-name]` shortcut
  - Affects colors and styling of the chat interface

- **chat.status_bar.enabled**: Enable/disable the entire status bar (default: `true`)
  - When disabled, no status indicators will be shown
  - When enabled, individual indicators can be configured

- **chat.status_bar.indicators**: Configuration for individual status bar indicators
  - All indicators are enabled by default except `max_output` to maintain current behavior
  - Available indicators:
    - **model**: Current AI model name (default: `true`)
    - **theme**: Current theme name (default: `true`)
    - **max_output**: Maximum output tokens (default: `false`)
    - **a2a_agents**: A2A agent readiness (ready/total) (default: `true`)
    - **tools**: Tool count and token usage (default: `true`)
    - **background_shells**: Running background shell count (default: `true`)
    - **mcp**: MCP server status and tool count (default: `true`)
    - **context_usage**: Token consumption percentage (default: `true`)
    - **session_tokens**: Session token usage statistics, plus the `C.` cached-tokens segment when the provider reports cache hits (default: `true`)
    - **git_branch**: Current Git branch name (default: `true`)
      - Only displays when in a Git repository
      - Uses 5-second cache for performance
      - Automatically updates after Git operations in bash mode, after every tool run, and on the app's 10-second heartbeat
      - The `⎇` icon turns the theme warning color when there are uncommitted changes, and the accent color
        when local commits are unpushed (or the branch has no upstream); uncommitted wins when both apply
      - Long branch names are truncated with "..." indicator

**Example Configuration:**

```yaml
chat:
  theme: tokyo-night
  status_bar:
    enabled: true
    indicators:
      model: true
      theme: false           # Hide theme indicator
      max_output: false
      a2a_agents: true
      tools: true
      background_shells: false # Hide background shells indicator
      mcp: true
      context_usage: true
      session_tokens: true
      git_branch: true       # Show current Git branch
```

### Keybinding Configuration

Keybindings live in their own file at `<configDir>/keybindings.yaml` (userspace:
`~/.infer/keybindings.yaml`, seeded by `infer init`; an optional project
`.infer/keybindings.yaml` overrides it when present). The main `config.yaml` no longer contains a
`chat.keybindings` block.

- **enabled**: Enable/disable custom keybindings (default: `true` in the
  generated file)
- **bindings**: Map of keybinding configurations

**Features:**

- **Namespace-Based Organization**: Action IDs use format `namespace_action` (e.g., `global_quit`, `mode_cycle_agent_mode`)
- **Context-Aware Conflict Detection**: Validates conflicts only within the same namespace
- **Self-Documenting**: All keybindings are visible in config with descriptions
- **No Runtime Validation**: Config loaded once at startup for performance
- **Explicit Validation**: Run `infer keybindings validate` to check config
- **Environment Variable Support**: Configure keybindings via comma-separated env vars

**Example Configuration (`<configDir>/keybindings.yaml`):**

```yaml
---
enabled: true
bindings:
  global_quit:  # Namespace: global, Action: quit
    keys:
      - ctrl+c
    description: "exit application"
    category: "global"
    enabled: true
  mode_cycle_agent_mode:  # Namespace: mode, Action: cycle_agent_mode
    keys:
      - shift+tab
    description: "cycle agent mode"
    category: "mode"
    enabled: true
```

**Resolution order:** project `.infer/keybindings.yaml` → user
`~/.infer/keybindings.yaml` → in-code defaults (when no file exists).
Environment variables override whichever file was loaded.

> **Note (macOS):** Word-wise delete in the chat input is bound to `ctrl+w`, `opt+backspace`
> (`alt+backspace`), and `ctrl+backspace`. Some terminals only send `opt+backspace` as
> `alt+backspace` when "Use Option as Meta key" is enabled (iTerm2: Profiles → Keys; Terminal.app:
> Settings → Profiles → Keyboard → "Use Option as Meta key"). `ctrl+w` always works.

**Available Commands:**

```bash
# List all keybindings
infer keybindings list

# Set custom key for an action (use namespaced action ID)
infer keybindings set mode_cycle_agent_mode ctrl+m

# Disable/enable specific actions
infer keybindings disable display_toggle_raw_format
infer keybindings enable display_toggle_raw_format

# Reset to defaults
infer keybindings reset

# Validate configuration (checks for conflicts within namespaces)
infer keybindings validate
```

**Key Action Namespaces:**

Actions are organized by namespace to distinguish between different contexts. The same key can be used
in different namespaces without conflict.

- **global**: Application-level actions (e.g., `global_quit`, `global_cancel`)
- **chat**: Chat-specific actions (e.g., `chat_enter_key_handler`)
- **mode**: Agent mode controls (e.g., `mode_cycle_agent_mode`)
- **tools**: Tool-related actions (e.g., `tools_toggle_tool_expansion`)
- **display**: Display toggles (e.g., `display_toggle_raw_format`, `display_toggle_todo_box`, `display_toggle_thinking`)
- **text_editing**: Text manipulation (e.g., `text_editing_move_cursor_left`, `text_editing_history_up`)
- **navigation**: Viewport navigation (e.g., `navigation_scroll_to_top`, `navigation_page_down`)
- **clipboard**: Copy/paste operations (e.g., `clipboard_copy_text`, `clipboard_paste_text`)
- **selection**: Selection mode controls (e.g., `selection_toggle_mouse_mode`)
- **plan_approval**: Plan approval navigation (e.g.,
  `plan_approval_plan_approval_accept`)
- **help**: Help system (e.g., `help_toggle_help`)

### Web Search API Setup (Optional)

Both search engines work out of the box, but for better reliability and performance in production, you
can configure API keys:

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
   - Restrict the API key to the Custom Search JSON API for
     security

4. **Configure Environment Variables:**

   ```bash
   export GOOGLE_SEARCH_API_KEY="your_api_key_here"
   export GOOGLE_SEARCH_ENGINE_ID="your_search_engine_id_here"
   ```

**DuckDuckGo API (Optional):**

```bash
export DUCKDUCKGO_SEARCH_API_KEY="your_api_key_here"
```

**Note:** Both engines have built-in fallback methods that work without API configuration. However,
using official APIs provides better reliability and performance for production use.

---

## Environment Variables

The CLI supports environment variable configuration with the `INFER_` prefix. Environment variables
override configuration file settings and are particularly useful for containerized deployments and CI/CD
environments.

All configuration fields can be set via environment variables by converting the YAML path to uppercase
and replacing dots (`.`) with underscores (`_`), then prefixing with `INFER_`.

**Example:** `gateway.url` → `INFER_GATEWAY_URL`, `tools.bash.enabled` → `INFER_TOOLS_BASH_ENABLED`

### Gateway Configuration

- `INFER_GATEWAY_URL`: Gateway URL (default: `http://localhost:8080`)
- `INFER_GATEWAY_API_KEY`: Gateway API key for authentication
- `INFER_GATEWAY_TIMEOUT`: Gateway request timeout in seconds (default: `200`)
- `INFER_GATEWAY_OCI`: OCI image for gateway (default: `ghcr.io/inference-gateway/inference-gateway:latest`)
- `INFER_GATEWAY_RUN`: Auto-run gateway if not running (default: `true`)
- `INFER_GATEWAY_DOCKER`: Use Docker to run gateway (default: `true`)

### Client Configuration

- `INFER_CLIENT_TIMEOUT`: HTTP client timeout in seconds (default: `200`)
- `INFER_CLIENT_STALL_THRESHOLD_SEC`: Seconds without stream progress before reconnecting (default: `30`, `0` disables)
- `INFER_CLIENT_RETRY_ENABLED`: Enable retry logic (default: `true`)
- `INFER_CLIENT_RETRY_MAX_ATTEMPTS`: Maximum retry attempts (default: `5`)
- `INFER_CLIENT_RETRY_INITIAL_BACKOFF_SEC`: Initial backoff delay in seconds (default: `5`)
- `INFER_CLIENT_RETRY_MAX_BACKOFF_SEC`: Maximum backoff delay in seconds (default: `60`)
- `INFER_CLIENT_RETRY_BACKOFF_MULTIPLIER`: Backoff multiplier (default: `2`)

### Logging Configuration

- `INFER_LOGGING_DEBUG`: Enable debug logging (default: `false`)
- `INFER_LOGGING_DIR`: Log directory path (default: `~/.infer/logs`)
- `INFER_LOGGING_STDOUT`: Also write logs to stdout/stderr (default: `false`)

### Agent Configuration

- `INFER_AGENT_MODEL`: Default model for agent operations (e.g., `deepseek/deepseek-v4-pro`)
- `INFER_PROMPTS_AGENT_SYSTEM_PROMPT`: Custom system prompt for agent
- `INFER_PROMPTS_AGENT_SYSTEM_PROMPT_HEARTBEAT`: Custom system prompt for heartbeat
- `INFER_PROMPTS_AGENT_SYSTEM_PROMPT_REMOTE`: Custom system prompt for remote agent
- `INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_PLAN`: Custom plan-mode adjustment instructions (delivered by the mode-change reminder, not the system prompt)
- `INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_AUTO`: Custom auto-accept adjustment instructions (delivered by the mode-change reminder, not the system prompt)
- `INFER_PROMPTS_AGENT_CUSTOM_INSTRUCTIONS`: Custom instructions for agent

> **Migration note (v0.105.0+):** The old `INFER_AGENT_SYSTEM_PROMPT` and
> `INFER_AGENT_SYSTEM_PROMPT_PLAN` env vars were renamed to
> `INFER_PROMPTS_AGENT_SYSTEM_PROMPT` and `INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_PLAN`
> respectively when agent prompts moved under the `prompts.agent.*` config tree.
> Set `INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_AUTO` for the auto-accept counterpart - if you are migrating an existing
> configuration, update your env vars to the new names above.

- `INFER_AGENT_MAX_TURNS`: Maximum agent turns (default: `100`)
- `INFER_AGENT_MAX_TOKENS`: Maximum tokens per response (default: `8192`)
- `INFER_AGENT_MAX_CONCURRENT_TOOLS`: Maximum concurrent tool executions (default: `5`)

### Reminders Configuration

Reminders live in their own `reminders.yaml` (see [System Reminders](#system-reminders-remindersyaml)); these env vars layer on top of it:

- `INFER_REMINDERS_ENABLED`: Master switch for all reminders (default: `true`)
- `INFER_REMINDERS_CONFIG`: Inline reminders YAML (same schema as `reminders.yaml`); when set it
  replaces the file-loaded reminders so embedded consumers need not write `~/.infer/reminders.yaml`

### Chat Configuration

- `INFER_CHAT_THEME`: Chat UI theme (`light`, `dark`, `dracula`, `nord`, `solarized`, default: `dark`)

### Tools Configuration

- `INFER_TOOLS_ENABLED`: Enable/disable all local tools (default: `true`)

**Individual Tool Enablement:**

- `INFER_TOOLS_BASH_ENABLED`: Enable/disable Bash tool (default: `true`)
- `INFER_TOOLS_READ_ENABLED`: Enable/disable Read tool (default: `true`)
- `INFER_TOOLS_WRITE_ENABLED`: Enable/disable Write tool (default: `true`)
- `INFER_TOOLS_EDIT_ENABLED`: Enable/disable Edit tool (default: `true`)
- `INFER_TOOLS_DELETE_ENABLED`: Enable/disable Delete tool (default: `true`)
- `INFER_TOOLS_GREP_ENABLED`: Enable/disable Grep tool (default: `true`)
- `INFER_TOOLS_TREE_ENABLED`: Enable/disable Tree tool (default: `true`)
- `INFER_TOOLS_WEB_FETCH_ENABLED`: Enable/disable WebFetch tool (default: `true`)
- `INFER_TOOLS_WEB_SEARCH_ENABLED`: Enable/disable WebSearch tool (default: `true`)
- `INFER_TOOLS_TODO_WRITE_ENABLED`: Enable/disable TodoWrite tool (default: `true`)

**Tool Approval Configuration:**

- `INFER_TOOLS_BASH_REQUIRE_APPROVAL`: Require approval for Bash tool (default:
  `false`)
- `INFER_TOOLS_WRITE_REQUIRE_APPROVAL`: Require approval for Write tool (default: `true`)
- `INFER_TOOLS_EDIT_REQUIRE_APPROVAL`: Require approval for Edit tool (default: `true`)
- `INFER_TOOLS_DELETE_REQUIRE_APPROVAL`: Require approval for Delete tool (default:
  `true`)
- `INFER_TEXT_TO_SPEECH_REQUIRE_APPROVAL`: Require approval for the TextToSpeech tool
  (default: unset, meaning no approval)

Approval variables are tri-state: leaving one unset is not the same as setting it to
`false`. An unset tool falls back to the policy baked into the tool, while an explicit
value pins it either way.

**Bash Tool Allow-List Configuration:**

The Bash allow-list is **per agent mode** and configured in YAML. Set
`tools.bash.mode.<mode>.allow` in `config.yaml`, where `<mode>` is `all`
(baseline applied in every mode), `plan`, `standard`, or `auto`. The effective
list for a mode is `mode.all.allow` unioned with that mode's list; anything
unmatched is denied (it prompts for approval in chat, or is rejected with a
reason in headless agent mode).

The defaults are deliberately **explicit, non-destructive commands** - the
read-only `gh` subcommands (`gh issue/pr/... list|view`, `gh project
list|view|item-list|field-list`, `gh search`), not a raw `gh api <path>`
wildcard. `gh api` is **not** auto-approved by default; prefer the structured
subcommands, or add a narrowly-scoped `gh api` regex to a mode's `allow` if you
genuinely need the raw API. One notable consumer: the opentask browser
extension performs its GitHub access as `gh api` tool requests over the bridge
(see [browser-extension-protocol.md](browser-extension-protocol.md)), so
allowlist `gh api( .*)?` in the modes you use it with to avoid a per-call
approval prompt.

The one exception to YAML-only configuration is an **append override** for the
`mode.all` baseline, so CI (and `infer-action`) can add a few commands without
rewriting config or relaxing a mode to `.*`:

- `INFER_TOOLS_BASH_ALLOW_APPEND`: comma/newline-separated commands
  appended to `tools.bash.mode.all.allow` (and therefore allowed in every mode).
  Equivalent flag: `--tools-bash-allow-append`; the env var wins when
  both are set. **Append only** - it merges onto the curated defaults rather than
  replacing them, and there is no replace override.

> The matcher is shell-aware and matches each entry against the WHOLE command
> (so a bare token matches only itself; use `( .*)?` to allow arguments). A
> clean-command guard rejects command substitution (`$(...)`), pipes/chains
> (`|`, `&&`, `||`, `;`), file-write redirects (`>`, `>>`), dangerous `find`
> actions, and printing/publishing an expanded `$VAR` (secret leak); benign
> redirects (`2>&1`, `>/dev/null`) are permitted. The single sentinel `.*`
> (default for `auto`) means unrestricted and skips the guard.
> See [Bash Tool restricted operators](tools-reference.md#bash-tool) for details.

**Example (`config.yaml`):**

```yaml
tools:
  bash:
    mode:
      all:
        allow:
          - gh (issue|pr) (list|view)( .*)?
          - git status( .*)?
      standard: # opt-in: baseline-only by default; add writes here to skip approval
        allow:
          - gh pr create( .*)?
      auto: # headless `infer headless`: full autonomy (commit, push, etc.)
        allow:
          - .*
```

**Grep Tool Configuration:**

- `INFER_TOOLS_GREP_BACKEND`: Grep backend to use (`ripgrep` or `grep`, default: `ripgrep`)

**WebSearch Tool Configuration:**

- `INFER_TOOLS_WEB_SEARCH_DEFAULT_ENGINE`: Default search engine (`duckduckgo` or `google`, default: `duckduckgo`)
- `INFER_TOOLS_WEB_SEARCH_MAX_RESULTS`: Maximum search results (default: `10`)
- `INFER_TOOLS_WEB_SEARCH_TIMEOUT`: Search timeout in seconds (default: `30`)

**WebFetch Tool Configuration:**

- `INFER_TOOLS_WEB_FETCH_SAFETY_MAX_SIZE`: Maximum fetch size in bytes (default: `10485760`)
- `INFER_TOOLS_WEB_FETCH_SAFETY_TIMEOUT`: Fetch timeout in seconds (default: `30`)
- `INFER_TOOLS_WEB_FETCH_SAFETY_ALLOW_REDIRECT`: Allow HTTP redirects (default: `true`)
- `INFER_TOOLS_WEB_FETCH_CACHE_ENABLED`: Enable fetch caching (default: `true`)
- `INFER_TOOLS_WEB_FETCH_CACHE_TTL`: Cache TTL in seconds (default: `900`)
- `INFER_TOOLS_WEB_FETCH_CACHE_MAX_SIZE`: Maximum cache size in bytes (default: `104857600`)

**Sandbox Configuration:**

- `INFER_TOOLS_SANDBOX_DIRECTORIES`: Comma-separated list of allowed directories (default: `.,/tmp`)

### Storage Configuration

- `INFER_STORAGE_ENABLED`: Enable conversation storage (default: `true`)
- `INFER_STORAGE_TYPE`: Storage backend type (`memory`, `sqlite`, `postgres`, `redis`, default: `sqlite`)

**SQLite Storage:**

- `INFER_STORAGE_SQLITE_PATH`: SQLite database path (default: `~/.infer/conversations.db`)

**PostgreSQL Storage:**

- `INFER_STORAGE_POSTGRES_HOST`: PostgreSQL host
- `INFER_STORAGE_POSTGRES_PORT`: PostgreSQL port (default: `5432`)
- `INFER_STORAGE_POSTGRES_DATABASE`: PostgreSQL database name
- `INFER_STORAGE_POSTGRES_USERNAME`: PostgreSQL username
- `INFER_STORAGE_POSTGRES_PASSWORD`: PostgreSQL password
- `INFER_STORAGE_POSTGRES_SSL_MODE`: PostgreSQL SSL mode (default: `disable`)

**Redis Storage:**

- `INFER_STORAGE_REDIS_HOST`: Redis host
- `INFER_STORAGE_REDIS_PORT`: Redis port (default: `6379`)
- `INFER_STORAGE_REDIS_PASSWORD`: Redis password
- `INFER_STORAGE_REDIS_DB`: Redis database number (default: `0`)

### Scheduler Configuration

- `INFER_SCHEDULER_BACKEND`: Scheduling backend, `local` or `github` (default: `local`). See the [Scheduling Guide](scheduling.md#github-backend)
- `INFER_SCHEDULER_GITHUB_REPOSITORY`: Repository for GitHub-backed schedules (default: `<login>/.routines`)
- `INFER_SCHEDULER_GITHUB_PULL_REQUESTS`: Deploy schedule changes via pull request instead of pushing to the default branch (default: `false`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_ENABLED`: Pull conversation artifacts from GitHub-backed runs into local storage (default: `true`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_POLL_INTERVAL`: Artifact poll interval (default: `10m`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_INITIAL_DELAY`: Delay before the first poll (default: `1m`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_MAX_ATTEMPTS`: Download attempts per artifact before it is skipped (default: `3`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_RATE_LIMIT_BACKOFF`: Polling pause after a rate-limited GitHub API call (default: `1h`)

### Conversation Configuration

- `INFER_CONVERSATION_TITLE_GENERATION_ENABLED`: Enable AI-powered title generation (default: `true`)
- `INFER_CONVERSATION_TITLE_GENERATION_MODEL`: Model for title generation (default: `anthropic/claude-4.1-haiku`)
- `INFER_CONVERSATION_TITLE_GENERATION_BATCH_SIZE`: Batch size for title generation (default: `5`)
- `INFER_CONVERSATION_TITLE_GENERATION_INTERVAL`: Interval in seconds between title generation attempts (default: `30`)

### A2A (Agent-to-Agent) Configuration

- `INFER_A2A_ENABLED`: Enable/disable A2A tools (default: `true`)
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

**A2A Cache Configuration:**

- `INFER_A2A_CACHE_ENABLED`: Enable/disable A2A agent card caching (default: `true`)
- `INFER_A2A_CACHE_TTL`: Cache TTL in seconds for A2A agent cards (default: `300`)

**A2A Task Configuration:**

- `INFER_A2A_TASK_STATUS_POLL_SECONDS`: Status polling interval in seconds (default: `10`)
- `INFER_A2A_TASK_POLLING_STRATEGY`: Polling strategy (`fixed` or `exponential`, default: `exponential`)
- `INFER_A2A_TASK_INITIAL_POLL_INTERVAL_SEC`: Initial polling interval for exponential strategy (default: `2`)
- `INFER_A2A_TASK_MAX_POLL_INTERVAL_SEC`: Maximum polling interval for exponential strategy (default: `30`)
- `INFER_A2A_TASK_BACKOFF_MULTIPLIER`: Backoff multiplier for exponential strategy (default: `1.5`)
- `INFER_A2A_TASK_BACKGROUND_MONITORING`: Enable background task monitoring (default: `true`)
- `INFER_A2A_TASK_COMPLETED_TASK_RETENTION`: Completed task retention in seconds (default: `3600`)

**A2A Individual Tool Configuration:**

- `INFER_A2A_TOOLS_SUBMIT_TASK_ENABLED`: Enable/disable A2A SubmitTask tool (default: `true`)
- `INFER_A2A_TOOLS_SUBMIT_TASK_REQUIRE_APPROVAL`: Require approval for SubmitTask (default: `false`)
- `INFER_A2A_TOOLS_QUERY_AGENT_ENABLED`: Enable/disable A2A QueryAgent tool (default: `true`)
- `INFER_A2A_TOOLS_QUERY_AGENT_REQUIRE_APPROVAL`: Require approval for QueryAgent (default: `false`)
- `INFER_A2A_TOOLS_QUERY_TASK_ENABLED`: Enable/disable A2A QueryTask tool (default: `true`)
- `INFER_A2A_TOOLS_QUERY_TASK_REQUIRE_APPROVAL`: Require approval for QueryTask (default: `false`)

### Export Configuration

- `INFER_EXPORT_OUTPUT_DIR`: Output directory for exported conversations (default: `./exports`)

### Compact Configuration

- `INFER_COMPACT_ENABLED`: Enable automatic conversation compaction (default: `true`)
- `INFER_COMPACT_AUTO_AT`: Auto-compact after N messages (default: `100`)

### Git Configuration

- `INFER_GIT_COMMIT_MESSAGE_MODEL`: Model for AI-generated commit messages (default: `deepseek/deepseek-v4-pro`)

### SCM Configuration

- `INFER_SCM_PR_CREATE_BASE_BRANCH`: Base branch for PR creation (default: `main`)
- `INFER_SCM_PR_CREATE_BRANCH_PREFIX`: Branch prefix for PR creation (default: `feature/`)
- `INFER_SCM_PR_CREATE_MODEL`: Model for PR creation (default: `deepseek/deepseek-v4-pro`)
- `INFER_SCM_CLEANUP_RETURN_TO_BASE`: Return to base branch after PR creation (default: `true`)
- `INFER_SCM_CLEANUP_DELETE_LOCAL_BRANCH`: Delete local branch after PR creation (default: `false`)

### Keybinding Environment Variables

Keybindings can be configured via environment variables (supports comma-separated or newline-separated lists):

```bash
# Enable keybindings
export INFER_CHAT_KEYBINDINGS_ENABLED=true

# Set keys for an action (comma-separated or newline-separated)
export INFER_CHAT_KEYBINDINGS_BINDINGS_GLOBAL_QUIT_KEYS="ctrl+q,ctrl+x"

# Multiline format
export INFER_CHAT_KEYBINDINGS_BINDINGS_MODE_CYCLE_AGENT_MODE_KEYS="shift+tab
ctrl+m"

# Enable/disable specific actions
export INFER_CHAT_KEYBINDINGS_BINDINGS_DISPLAY_TOGGLE_RAW_FORMAT_ENABLED=false
```

Format: `INFER_CHAT_KEYBINDINGS_BINDINGS_<ACTION_ID>_<FIELD>`

- `<ACTION_ID>`: Uppercase namespaced action ID (e.g., `GLOBAL_QUIT`, `MODE_CYCLE_AGENT_MODE`)
- `<FIELD>`: Either `KEYS` (comma/newline-separated) or `ENABLED` (true/false)

---

## Environment Variable Substitution

Configuration values support environment variable substitution using the `%VAR_NAME%` syntax:

```yaml
gateway:
  api_key: "%INFER_API_KEY%"
```

This allows sensitive values to be stored as environment variables while keeping them out of configuration files.

---

## Configuration Best Practices

### Security

- **Never commit sensitive data** (API keys, tokens) to configuration files
- Use environment variable substitution (`%VAR_NAME%`) for sensitive values
- Use environment variables (`INFER_*`) for CI/CD environments

### Organization

- Use **userspace config** (`~/.infer/config.yaml`) for your baseline and
  personal preferences - it is the default write target
- Use a **project config** (`.infer/config.yaml`) only for the handful of keys a
  repo genuinely needs to override; keep it sparse
- Commit project configs to version control; userspace configs stay on your
  machine

### Example Workflow

```bash
# 1. Setup userspace defaults (the shared baseline)
infer config set agent.model "deepseek/deepseek-v4-pro"

# 2. Project-specific overrides
infer config set agent.model "openai/gpt-4o" --project    # Project-specific model
infer config set tools.bash.enabled true --project        # Enable bash tools for this project

# 3. Runtime overrides
INFER_AGENT_MAX_TURNS=100 infer chat  # Temporary turn limit
```

---

## Configuration Validation and Troubleshooting

The CLI validates configuration on startup and provides helpful error messages for:

- Invalid YAML syntax
- Unknown configuration keys
- Invalid value types (string vs boolean vs integer)
- Missing required values

### Common Issues

1. **Configuration not found**: Check that the config file exists and has correct YAML syntax
2. **Environment variables not working**: Ensure proper `INFER_` prefix and underscore conversion
3. **Precedence confusion**: Remember that environment variables override config files

### Debugging

```bash
# Print the effective configuration (defaults + files merged + env)
infer config get

# Print a single resolved value
infer config get agent.model

# Enable debug logging while inspecting config
INFER_LOGGING_DEBUG=true infer config get
```

---

[← Back to README](../README.md)
