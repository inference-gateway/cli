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

1. **Command Line Flags** - **Highest Priority**
2. **Environment Variables** (`INFER_*` prefix)
3. **Project Config** (`.infer/config.yaml`)
4. **Userspace Config** (`~/.infer/config.yaml`)
5. **Built-in Defaults** - **Lowest Priority**

**Example**: If your userspace config sets `agent.model: "anthropic/claude-4"` and your project config
sets `agent.model: "deepseek/deepseek-v4-pro"`, the project config wins. However, if you also set
`INFER_AGENT_MODEL="openai/gpt-4"`, the environment variable takes precedence over both config files -
though a command-line flag (e.g. `infer headless --model`) still wins over all of them.

**Exceptions**: exactly two keys let the environment variable beat the flag:
`INFER_TOOLS_BASH_ALLOW_APPEND` (over `--tools-bash-allow-append`) and
`INFER_REMINDERS_CONFIG` (over `--reminders-file`).

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
  standalone_binary: true  # Run the gateway as a standalone binary (default; set false for Docker mode)
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
  insights_min_level: warn # Lowest level `infer insights` folds into its report (debug|info|warn|error|dpanic|panic|fatal)
tools:
  enabled: true # Tools are enabled by default with safe read-only commands
  sandbox:
    directories: [".", "/tmp"] # Allowed directories for tool operations
    protected_paths: # Paths excluded from tool access for security
      - .infer/
      - .git/
      - *.env
      - .environment
      - auth.yaml
      - *.key
      - *.pem
      - id_rsa
      - id_dsa
      - id_ecdsa
      - id_ed25519
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
          - tree( .*)?
          - wc( .*)?
          - sort( .*)?
          - uniq( .*)?
          - head( .*)?
          - tail( .*)?
          - find( .*)?
          - sleep( .*)?
          - mkdir( .*)?
          - ln -s( [^ -][^ ]*)+
          - git status( .*)?
          - git branch( --show-current)?( -[alrvd])?
          - git log( .*)?
          - git diff( .*)?
          - git remote( -v)?
          - git show( .*)?
          - gh (issue|pr|repo|release|run|workflow) (list|view|status|diff|checks)( .*)?
          - gh auth status( .*)?
          - gh search (issues|code|prs|repos|commits)( .*)?
          - gh project (list|view|item-list|field-list)( .*)?
          - gh api repos/[^ ]+/contents/[^ ]+
          - gh api '?user/repos[^ ]*'?( --paginate)?( --jq [^ ]+)?
          - infer binaries status( .*)?
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
      - localhost
      - github.com
      - raw.githubusercontent.com
      - agents.md
    safety:
      max_size: 10485760 # 10MB
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
  # System prompts and custom instructions live in prompts.yaml (prompts.agent.*), not in config.yaml
  max_turns: 50 # Maximum number of turns for agent sessions
  max_tokens: 8192 # The maximum number of tokens that can be generated per request
  max_concurrent_tools: 5 # Maximum concurrent tool executions
chat:
  theme: "" # "" applies the built-in default, tokyo-night
  status_bar:
    enabled: true
    indicators:
      model: true
      effort: true
      theme: true
      max_output: false
      a2a_agents: true
      tools: true
      queue: true
      mcp: true
      context_usage: true
      session_tokens: true
      cost: true
      git_branch: true
      git_pr: true
      subagents: true
compact:
  enabled: true # Enable automatic conversation compaction
  auto_at: 80 # Compact when context reaches this percentage (20-100)
telemetry:
  enabled: true # Record OTel metrics locally; written as OTLP/semconv JSON under ~/.infer/telemetry
  retention_days: 7 # Archive telemetry files older than this many days (0 disables archiving)
  otlp:
    endpoint: "" # OTLP/HTTP collector base URL; empty (and OTEL_EXPORTER_OTLP_ENDPOINT unset) disables export
    headers: {} # Headers sent on every export request
    interval: 60 # Periodic export interval in seconds
  receiver_address: "" # Address for the CLI's in-process OTLP receiver to listen on; empty disables it
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
- **gateway.standalone_binary**: Run the gateway as a standalone binary instead of a Docker container (default: `true`)
  - `true` (default): Downloads and runs the gateway as a binary (no Docker required)
  - `false`: Uses Docker to run the gateway container (requires Docker installed; the image comes from `gateway.oci`)
- **gateway.oci**: OCI image to use for Docker mode (default: `ghcr.io/inference-gateway/inference-gateway:latest`)
- **gateway.mock**: Run against the bundled mock gateway instead of a real one (default: `false`). Set via `INFER_GATEWAY_MOCK`.
- **gateway.debug**: Start the supervised gateway in development mode with detailed logging (default: `false`).
  Set via `INFER_GATEWAY_DEBUG`.
- **gateway.vision_enabled**: Pass `VISION_ENABLED=true` to the gateway process, whether it runs as a standalone binary
  or a container (default: `true`). Unrelated to the CLI-side `vision.*` annotator settings below.
  Set via `INFER_GATEWAY_VISION_ENABLED`.
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
- **client.stall_threshold_sec**: Seconds without a chunk on an open stream before the request counts as stalled (default:
  `30`, `0` disables). The chat UI shows a reconnecting indicator and the agent drops the connection and retries, up to
  `client.retry.max_attempts` times with exponential backoff. Keep it above the longest silence between chunks your provider
  produces - a stall retry restarts the response from scratch. Connecting is not covered: connection errors are retried by
  the HTTP client under `client.retry.*` and then reported as they are, and the wait for the first token is bounded only by
  `client.timeout` and `gateway.timeout`
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
- **logging.insights_min_level**: Lowest log level `infer insights` ingests when
  folding `~/.infer/logs` into its report (default: `warn`). Lower it to `info` to
  surface lifecycle events too, at the cost of a noisier digest. Set via
  `INFER_LOGGING_INSIGHTS_MIN_LEVEL`.

### Tool Settings

- **tools.enabled**: Enable/disable tool execution for LLMs (default: true)
- **tools.max_result_bytes**: Byte cap on a single tool result before it is truncated for the model (default: `250000`).
  Set via `INFER_TOOLS_MAX_RESULT_BYTES`.
- **tools.sandbox.directories**: Allowed directories for tool operations (default: [".", "/tmp"])
- **tools.sandbox.protected_paths**: Paths excluded from tool access for security. Default:
  [".infer/", ".git/", "*.env", ".environment", "auth.yaml", "*.key", "*.pem", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"]
- **tools.bash.mode.\<mode\>.allow**: Per-mode bash allow-list (regexes matched against the whole command). `<mode>` is one of `all`
  (baseline applied in every mode), `plan`, `standard`, or `auto`. The effective list is `mode.all.allow` unioned with the active mode's
  list. Anything unmatched is denied (approval in chat, rejection in headless agent mode). The `.*` sentinel (default for `auto`) means
  unrestricted.
- **tools.bash.timeout**: Timeout in seconds applied to a foreground Bash command (default: `120`).
  Set via `INFER_TOOLS_BASH_TIMEOUT`.
- **tools.bash.background_shells**: Background shell manager settings: `enabled` (default: `true`), `max_concurrent`
  (default: `5`, further shells queue), `retention_minutes` (default: `60`) and `completed_retention`
  (default: `5`) finished shells kept for inspection. Set via `INFER_TOOLS_BASH_BACKGROUND_SHELLS_*`.
- **tools.custom_dir**: Directory your user [custom tools](custom-tools.md) load from (default: `""`, meaning
  `~/.infer/tools/`). Project tools in `.infer/tools/` and `.agents/tools/` load as well. Env: `INFER_TOOLS_CUSTOM_DIR`.
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
- **Individual tool settings**: Each tool (Read, Write, Edit, Delete, Grep, Tree, WebFetch, WebSearch, TodoWrite)
  has:
  - **enabled**: Enable/disable the specific tool
  - **require_approval**: Override global safety setting for this tool (optional)
- **tools.multiedit.require_approval**: Approval override for MultiEdit, which shares Edit's matcher
  (default: `true`). MultiEdit carries no `enabled` key. Env: `INFER_TOOLS_MULTIEDIT_REQUIRE_APPROVAL`.
- **tools.ask_user_question.enabled**: Enable/disable AskUserQuestion (default: `true`). The tool is read-only,
  so it carries no `require_approval` key. Env: `INFER_TOOLS_ASK_USER_QUESTION_ENABLED`.
- **tools.schedule**: Enable/disable the Schedule tool (default: `false`), plus `require_approval` (default: `true`)
  and `max_jobs` (default: `100`), the cap on persisted recurring jobs. Env: `INFER_TOOLS_SCHEDULE_ENABLED`,
  `INFER_TOOLS_SCHEDULE_REQUIRE_APPROVAL`, `INFER_TOOLS_SCHEDULE_MAX_JOBS`.
- **tools.wait**: Enable/disable the Wait tool (default: `true`), plus `max_timeout_seconds` (default: `600`)
  and `command_poll_interval_ms` (default: `2000`). Env: `INFER_TOOLS_WAIT_ENABLED`,
  `INFER_TOOLS_WAIT_MAX_TIMEOUT_SECONDS`, `INFER_TOOLS_WAIT_COMMAND_POLL_INTERVAL_MS`.
- **tools.image_edit** and **tools.image_variation**: `enabled`, `model` (both default to `openai/gpt-image-2`)
  and `require_approval`. Env: `INFER_TOOLS_IMAGE_EDIT_*`, `INFER_TOOLS_IMAGE_VARIATION_*`.
- **tools.edit.strict_whitespace**: `false` (default) enables indentation-tolerant matching for Edit/MultiEdit; `true` requires byte-exact
- **tools.agent.wait**: `false` (default) makes the Agent tool return as soon as its headless subagents are
  dispatched, so later tool calls in the same turn run right away and each subagent reports back with its own
  `[Subagent Completed: ...]` notification. `true` blocks the call until every subagent finishes and returns their
  aggregated results (fan-out / fan-in). Interactive subagents never block. Env: `INFER_TOOLS_AGENT_WAIT`.
- **tools.agent.max_parallel**: Most subagents one Agent call may dispatch (default: `10`). Tasks past the cap
  are dropped, not queued, and the tool result names how many. Env: `INFER_TOOLS_AGENT_MAX_PARALLEL`.
- **tools.agent.idle_timeout**: Seconds an interactive (tmux-pane) subagent may sit idle - no harvested
  result turn, no pane change and no pending approval - before the parent monitor closes its pane automatically
  (default: `300`; `0` disables the auto-close). A subagent reports done on its task turn, which closes the pane
  right away, so the timeout only catches one that never reports done. Env: `INFER_TOOLS_AGENT_IDLE_TIMEOUT`.
- **tools.agent.mode**: Whether spawned subagents run `headless` (background, the default) or `interactive`
  (a tmux pane you can watch). Env: `INFER_TOOLS_AGENT_MODE`.
- **tools.agent.max_depth**: How deep the Agent tool may nest. A listed Agent tool is disabled inside a subagent
  until this is raised above its default of `1`. Env: `INFER_TOOLS_AGENT_MAX_DEPTH`.
- **tools.agent.model**: Model override for subagents; empty inherits the session model. Env: `INFER_TOOLS_AGENT_MODEL`.
- **tools.agent.inherit_mock**: Pass the mock-gateway setting down to headless subagents (default: `true`).
  Env: `INFER_TOOLS_AGENT_INHERIT_MOCK`.
- **tools.agent.completed_retention**: Number of finished subagent results kept for later retrieval
  (default: `5`). Env: `INFER_TOOLS_AGENT_COMPLETED_RETENTION`.

### Vision Settings

Frame sources and the image-annotation pipeline that lets text-only models "see" screen and camera
frames. Not to be confused with **gateway.vision_enabled**, which is an unrelated gateway-side flag.

- **vision.annotator.enabled**: Enable the image annotator (default: false). When enabled,
  `GetLatestFrame` defaults to annotated (text) output and `ImageDecode` adds a text description to
  the image it returns, so text-only models can understand frames without any per-model
  configuration. `ImageDecode` itself is always available: vision models get the image.
- **vision.annotator.model**: `provider/model` reference of the vision model to side-call through
  the configured gateway (default: `anthropic/claude-haiku-4-5-20251001`). The gateway also serves fully local
  models, so offline annotation is just a local provider (e.g. `ollama/qwen3-vl:2b`)
- **vision.annotator.max_tokens**: Annotation response budget (default: 4096)
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

### Computer Use (computer_use.yaml)

Settings for the [Computer Use](computer-use.md) tools in **`computer_use.yaml`** (project
`./.infer/computer_use.yaml` overrides userspace `~/.infer/computer_use.yaml`).

```yaml
enabled: false # register the Computer tool
approval: never # never | destructive | always
rate_limit:
  enabled: true
  max_actions_per_minute: 60
  window_seconds: 60
```

- **computer_use.approval**: When a computer-use action needs approval (default: `never`). `destructive`
  gates input actions (click, type, key, move, scroll) and lets observations such as screenshots through.
  `always` gates every action.
- **computer_use.rate_limit**: Caps actions to `max_actions_per_minute` within a sliding `window_seconds`
  window (defaults: on, 60, 60).

### Screen Recording (computer_use.yaml)

Settings for the [RecordStart and RecordStop](tools-reference.md#recordstart-and-recordstop-tools)
tools, under `recording` in **`computer_use.yaml`** (project `./.infer/computer_use.yaml` overrides
userspace `~/.infer/computer_use.yaml`). Recording works without `computer_use.enabled`.

```yaml
recording:
  enabled: false # register RecordStart/RecordStop
  max_duration: 120 # seconds; a recording stops and finalizes itself at this cap
  output_dir: "" # empty = ~/.infer/tmp/recordings
  framerate: 24 # frames per second
  require_approval: true # unset = true; false lets RecordStart run unattended (headless CI)
  hide_cursor: false # true leaves the mouse pointer out of recordings
```

- **computer_use.recording.enabled**: Register the recording tools (default: false)
- **computer_use.recording.max_duration**: Maximum recording length in seconds (default: 120; must
  be positive when enabled)
- **computer_use.recording.output_dir**: Where recordings are written (default:
  `~/.infer/tmp/recordings`, created on first use)
- **computer_use.recording.framerate**: Capture frame rate (default: 24; must be positive when enabled)
- **computer_use.recording.require_approval**: Whether `RecordStart` needs approval outside
  auto-accept mode (default: true). Set it to false for unattended runs that have no approver,
  such as headless CI recording a virtual display; `RecordStart` then follows `computer_use.approval`
- **computer_use.recording.hide_cursor**: Leave the mouse pointer out of recordings (default:
  false). Useful for terminal demos on a virtual display, where the idle pointer sits mid-screen

Environment overrides: `INFER_COMPUTER_USE_RECORDING_ENABLED`,
`INFER_COMPUTER_USE_RECORDING_MAX_DURATION`, `INFER_COMPUTER_USE_RECORDING_OUTPUT_DIR`,
`INFER_COMPUTER_USE_RECORDING_FRAMERATE`, `INFER_COMPUTER_USE_RECORDING_REQUIRE_APPROVAL`,
`INFER_COMPUTER_USE_RECORDING_HIDE_CURSOR`.

### Compact Settings

- **compact.enabled**: Enable automatic mid-conversation compaction at the `auto_at`
  threshold to reduce token usage (default: true). This flag does **not** gate
  compaction on plan approval - approving a plan in [Plan Mode](plan-mode.md) always
  summarizes the exploration-heavy planning conversation and continues execution in a
  fresh, smaller session, regardless of this setting.
- **compact.auto_at**: Percentage of context window (20-100) at which to automatically trigger compaction (default: 80)
- **compact.keep_first_messages**: Number of leading messages the compactor always preserves (default: `2`).
  A value at or below zero falls back to `2`. Env: `INFER_COMPACT_KEEP_FIRST_MESSAGES`.
- **compact.rollover_on_idle_minutes**: Minutes a session may sit idle before a rollover starts a fresh one
  (default: `30`). Env: `INFER_COMPACT_ROLLOVER_ON_IDLE_MINUTES`.
- **compact.summary_max_tokens**: Token budget for the compaction summary (default: `1024`).
  Env: `INFER_COMPACT_SUMMARY_MAX_TOKENS`.

### Telemetry Settings

- **telemetry.enabled**: Record OpenTelemetry metrics locally (default: true). The recorded data is written as OTLP/semconv JSON
  under `~/.infer/telemetry` (always, private - no prompt/response content). Telemetry is user-global: the store never moves
  with the working directory or a project-local `.infer/`
- **telemetry.retention_days**: How long a session's telemetry file stays active before `infer stats`
  archives it (default: 7; `0` disables archiving)
- **telemetry.otlp.endpoint**: OTLP/HTTP collector base URL (e.g. `http://localhost:4318`). Empty - and
  `OTEL_EXPORTER_OTLP_ENDPOINT` unset - means no export
- **telemetry.otlp.headers**: Headers sent on every export request (e.g. auth tokens)
- **telemetry.otlp.interval**: Periodic export interval in seconds (default: 60)
- **telemetry.receiver_address**: Address for the CLI's in-process OTLP receiver to listen on (e.g. `0.0.0.0:0`); empty disables the receiver
- **telemetry.attr_session_id_key**: Baggage member name carrying the session id into subprocess `BAGGAGE` env and
  outgoing HTTP requests (default: `session.id`). Env: `INFER_TELEMETRY_ATTR_SESSION_ID_KEY`.
- **telemetry.attr_tool_call_id_key**: Baggage member name carrying the tool-call id (default: `gen_ai.tool.call.id`).
  Both names must match what the consumer (e.g. the ADK) reads. Env: `INFER_TELEMETRY_ATTR_TOOL_CALL_ID_KEY`.

See [Telemetry](telemetry.md) for the baggage keys and mixed CLI/ADK deployment guidance.

### Agent Settings

- **agent.model**: Default model for agent operations
- System prompts and custom instructions are not `config.yaml` `agent.*` keys: they live in `prompts.yaml` under
  `prompts.agent.system_prompt` and `prompts.agent.custom_instructions` (env: `INFER_PROMPTS_AGENT_SYSTEM_PROMPT`,
  `INFER_PROMPTS_AGENT_CUSTOM_INSTRUCTIONS`). The system prompt stays byte-stable for the whole session - including
  across agent-mode switches (Shift+Tab) - so local LLM servers keep KV-cache prefix hits.
- **prompts.agent.mode_adjustment_plan** (prompts.yaml): Optional per-mode instructions (NOT a system prompt) delivered as the `{guidance}` of the mode-change
  reminder when the agent enters Plan Mode. Ships empty; the built-ins live in the mode-change-reminder guidance in reminders.yaml.
- **prompts.agent.mode_adjustment_auto** (prompts.yaml): Same for auto-accept mode, carrying the destructive-action policy.
- System reminders are configured in their own `reminders.yaml`, not under `agent:` - see [System Reminders](#system-reminders-remindersyaml) below.
- **agent.max_turns**: Maximum number of turns for agent sessions (default: 50)
- **agent.max_tokens**: Maximum tokens per agent request (default: 8192)
- **agent.max_concurrent_tools**: Maximum number of tools that can execute concurrently (default: 5)
- **agent.system_prompt_with_defaults**: Prepend the built-in system prompt to the `prompts.agent.system_prompt`
  value instead of replacing it (default: `true`). Env: `INFER_AGENT_SYSTEM_PROMPT_WITH_DEFAULTS`.
- **agent.context**: Which context sources feed the prompt - `git_context_enabled` (default: `true`),
  `working_dir_enabled` (default: `true`), `git_context_refresh_turns` (default: `10`) and `tree_enabled`
  (default: `true`). Env: `INFER_AGENT_CONTEXT_*`.
- **agent.skills**: Skill catalog settings - `enabled` (default: `true`), `disabled_skills`, `max_chars`
  (default: `4000`) and `repository` (default: `inference-gateway/skills`). Env: `INFER_AGENT_SKILLS_*`.
- **agent.reasoning_effort**: Reasoning effort for models that expose one (`minimal`, `low`, `medium`, `high`,
  `xhigh`, `max`). Unset leaves the provider default in place. Env: `INFER_AGENT_REASONING_EFFORT`.
- **agent.agents_md** (config.yaml): Injects the working directory's `AGENTS.md` into the system prompt as a
  `PROJECT INSTRUCTIONS (AGENTS.md)` section. Sub-keys: `enabled` (default true), `max_lines` (default 399)
  and `max_chars` (default 8000) cap that file. Env: `INFER_AGENT_AGENTS_MD_ENABLED`,
  `INFER_AGENT_AGENTS_MD_MAX_LINES`, `INFER_AGENT_AGENTS_MD_MAX_CHARS`.
- Nested AGENTS.md files: following the [agents.md](https://agents.md) standard, an `AGENTS.md` in a
  subdirectory takes precedence over the root file for that directory. `infer` points the model at them
  without injecting their content, and the model reads one when it works there. When the project tree
  (`agent.context.tree_enabled`) is in the prompt it already shows them, so the section adds only the
  precedence rule. Otherwise it lists their paths, up to 4 levels deep and at most 20 entries, skipping
  hidden trees and `node_modules`/`vendor`.

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
| `once_after` | Once, after `threshold` session turns have elapsed (requires `threshold > 0`). |
| `on_mode_change` | Mode changed before the next stream; substitutes `{prev_mode}`, `{new_mode}` and `{guidance}`. Requires `hook: pre_stream`. |
| `on_repeated_failure` | **`post_tool` only** - same tool call failed identically `threshold` times; `{tool_name}`/`{count}` substituted. |
| `on_truncation` | **`post_stream` only** - the previous response hit the token limit (`finish_reason: length`). Requires `hook: post_stream`. |
| `on_stalled_todos` | **`post_stream` only** - no tool call while todos are incomplete; substitutes `{todo_list}`. Requires `hook: post_stream`. |
| `on_empty_response` | **`post_stream` only** - the response had neither text nor a tool call. Requires `hook: post_stream`. |

The `on_failure` trigger lets a consumer nudge the model only when a change did not
happen (a failed tool call), instead of paying the per-turn cost of an `always` reminder.
The `on_repeated_failure`, `on_stalled_todos` and `on_empty_response` triggers default
`threshold` to 3 when it is omitted.

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

By default, a supplied reminders config **replaces** the built-in defaults entirely:
`todo-hygiene`, `mode-change-reminder`, `user-intent-focus`, `repeated-failure`,
`todo-continuation`, `truncation-continuation`, `empty-response-continuation`,
`memory-consult` and `memory-hygiene`. Set `merge: true` at the top level to
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

- **tools.web_search.enabled**: Enable/disable web search tool for LLMs (default: true)
- **tools.web_search.default_engine**: Default search engine to use ("duckduckgo" or "google", default: "duckduckgo")
- **tools.web_search.max_results**: Maximum number of search results to return (1-50, default: 10)
- **tools.web_search.engines**: List of available search engines
- **tools.web_search.timeout**: Search timeout in seconds (default: 10)

### Chat Interface Settings

- **chat.theme**: Chat interface theme name. The config default is the empty string, and the TUI
  applies `tokyo-night` when it is unset.
  - Available themes: `tokyo-night`, `github-light`, `dracula`, `charm`
  - Can be changed during chat using `/theme [theme-name]` shortcut
  - Affects colors and styling of the chat interface

- **chat.status_bar.enabled**: Enable/disable the entire status bar (default: `true`)
  - When disabled, no status indicators will be shown
  - When enabled, individual indicators can be configured

- **chat.status_bar.indicators**: Configuration for individual status bar indicators
  - All indicators are enabled by default except `max_output` to maintain current behavior
  - Available indicators:
    - **model**: Current AI model name (default: `true`)
    - **effort**: The active model's reasoning effort, when it exposes one (default: `true`)
    - **theme**: Current theme name (default: `true`)
    - **max_output**: Maximum output tokens (default: `false`)
    - **a2a_agents**: A2A agent readiness (ready/total) (default: `true`)
    - **tools**: Tool count and token usage (default: `true`)
    - **queue**: Messages waiting in the queue while the agent is busy (default: `true`)
      - Shows a `☰ N queued` segment in the accent color while the shared message queue has entries; hidden once it drains
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
    - **cost**: Running cost of the session (default: `true`)
    - **git_pr**: The pull request attached to the current branch, when there is one (default: `true`)
    - **subagents**: The stacked list of background jobs, one row per subagent, A2A task, shell or
      recording (default: `true`). `chat.status_bar.subagent_linger_seconds` (default 5) controls
      how long a finished row lingers before it drops off

**Example Configuration:**

```yaml
chat:
  theme: tokyo-night
  status_bar:
    enabled: true
    indicators:
      model: true
      effort: true
      theme: false           # Hide theme indicator
      max_output: false
      a2a_agents: true
      tools: true
      queue: true            # Show queued-message indicator while the agent is busy
      mcp: true
      context_usage: true
      session_tokens: true
      cost: true
      git_branch: true       # Show current Git branch
      git_pr: true           # Show the branch's pull request when there is one
      subagents: true        # Show the stacked background-job rows
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

- **Namespace-Based Organization**: Action IDs use format `namespace_action` (e.g., `global_quit`,
  `mode_cycle_agent_mode`). `explorer` holds the file explorer keys and `diff_viewer` the diff viewer keys
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
>
> **Note (Ctrl+R / Ctrl+M):** In the chat view `ctrl+r` opens a fuzzy search over your
> prompt history (the `text_editing_history_search` action): type to filter, ↑/↓ to select,
> Enter loads the highlighted prompt into the input for editing, Esc closes. The
> raw/rendered markdown toggle (`display_toggle_raw_format`) previously bound there moved to
> `ctrl+m`, with `alt+m` as a fallback. In terminals without the Kitty keyboard protocol
> (Kitty, Ghostty, WezTerm, foot and iTerm2 with CSI u report `ctrl+m` distinctly) `ctrl+m`
> sends the same byte as Enter, so use `alt+m` there to toggle markdown.

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
- **text_editing**: Text manipulation (e.g., `text_editing_move_cursor_left`,
  `text_editing_history_up`, `text_editing_history_search`)
- **navigation**: Viewport navigation (e.g., `navigation_scroll_to_top`, `navigation_page_down`)
- **clipboard**: Copy/paste operations (e.g., `clipboard_copy_text`, `clipboard_paste_text`)
- **plan_approval**: Plan approval navigation (e.g.,
  `plan_approval_plan_approval_accept`)
- **help**: Help system (e.g., `help_toggle_help`)
- **diff_viewer**: Keys for the `/diff` changes panel, resolved directly by the component (e.g., `diff_viewer_nav_down`)
- **explorer**: Keys for the `/explorer` file panel, resolved directly by the component (e.g., `explorer_open`)

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

### Blocks Not Itemised Above

These top-level blocks are read from `config.yaml` and are not spelled out key by key above. Each
entry lists its keys and points at the guide that owns the behaviour.

- **`container_runtime`**: `type` - `docker`, `podman`, or `""` to auto-detect.
- **`image`**: `max_size` (bytes), `timeout` (seconds), `allow_local`, and `clipboard_optimize` for
  images pasted straight from the clipboard.
- **`client`**: `timeout` (seconds), `stall_threshold_sec`, and `retry` for gateway calls.
- **`export`**: `output_dir` - where exported conversations are written.
- **`web`**: `enabled`, `port`, `host`, `session_inactivity_mins`, `tmux`, `ssh`, `servers`. See
  [Web Terminal](web-terminal.md).
- **`pricing`**: `enabled`, `currency`, and `custom_prices.<model>` overrides. See
  [Cost Tracking](cost-tracking.md).
- **`context_windows`**: a map of model id to context window in tokens, for models the CLI does not
  know about.
- **`provisioner`**: `provider`, `gpu_type`, `model`, `image`, `cloud_type`, `disk_gb`, `max_hourly`,
  and `runpod` for the management-plane credential. See the [GPU command](commands-reference.md#infer-gpu).
- **`speech_to_text`**: `enabled`, `engine`, `binary_path`, `model`, `models_dir`, `language`,
  `auto_download`, `timeout`, `max_recording_seconds`, `silence_timeout`, `ffmpeg_path`,
  `input_device`, `retain_recordings`, `recordings_dir`. See [Speech-to-Text](speech-to-text.md).
- **`text_to_speech`**: `enabled`, `engine`, `binary_path`, `model`, `voice`, `models_dir`,
  `output_dir`, `auto_download`, `timeout`, `ffmpeg_path`, `require_approval`. See
  [Text-to-Speech](text-to-speech.md).
- **`text_to_music`**, **`text_to_sfx`**: `enabled`, `model`, `output_dir`, `require_approval`. See
  [Text-to-Music](text-to-music.md).
- **`text_to_video`**: `enabled`, `model`, `avatar_model`, `size`, `output_dir`, `timeout`,
  `poll_interval`, `create_avatar`, `require_approval`. See [Text-to-Video](text-to-video.md).

### Blocks in Their Own File

The blocks below live in a file of their own rather than in `config.yaml`. Naming each key here would
duplicate the guide that owns it, so the keys are listed once and the guide carries the detail.

- **`channels.yaml`** - `enabled`, `max_workers`, `image_retention`, `require_approval`, `telegram`,
  `whatsapp`. See [Channels](channels.md).
- **`heartbeat.yaml`** - `enabled`, `interval`, `initial_delay`, `model`, `prompt`. See
  [Heartbeat](heartbeat.md).
- **`reminders.yaml`** - `enabled`, `merge`, `reminders`. See
  [Reminders & Command Hooks](hooks.md).
- **`hooks.yaml`** - `enabled`, `hooks`. See [Reminders & Command Hooks](hooks.md).
- **`judge.yaml`** - `model`, `gateway_url`, `timeout`, `max_tokens`, `on_error`, `system_prompt`,
  `prompt`. See [Judge Mode](judge-mode.md).
- **`memory.yaml`** - `enabled`, `dir`, `max_chars`, `max_entry_chars`, `backend`. See
  [Persistent Memory](memory.md).
- **`plugins.yaml`** - `enabled`, `dir`, `max_instructions_chars`, `max_instructions_lines`,
  `plugins`. See [Plugins](plugins.md).
- **`computer_use.yaml`** - `enabled`, `screenshot`, `rate_limit`, `approval`, `recording`. See
  [Computer Use](computer-use.md).
- **`browser_use.yaml`** - `enabled`, `backend`, `browser`, `extension`, `rate_limit`, `tools`. See
  [Daemon Binding Protocol](browser-extension-protocol.md).
- **`mcp.yaml`** - `enabled`, `connection_timeout`, `discovery_timeout`, `liveness_probe_enabled`,
  `liveness_probe_interval`, `max_retries`, `servers`. See [MCP Integration](mcp-integration.md).
- **`daemon.yaml`** - `binding.enabled`, `binding.port` (falling back to `browser_use.extension.port`)
  and `binding.token` (falling back to `browser_use.extension.token`). See [infer daemon](daemon.md).
- **`prompts.yaml`** - `agent`, `git`, `conversation`, `init`, `vision`. Prompts are edited directly
  rather than through `config set`. See the [Commands Reference](commands-reference.md).

## Environment Variables

The CLI supports environment variable configuration with the `INFER_` prefix. Environment variables
override configuration file settings and are particularly useful for containerized deployments and CI/CD
environments.

All configuration fields can be set via environment variables by converting the YAML path to uppercase
and replacing dots (`.`) with underscores (`_`), then prefixing with `INFER_`.

**Example:** `gateway.url` → `INFER_GATEWAY_URL`, `tools.bash.enabled` → `INFER_TOOLS_BASH_ENABLED`

### Provider API Keys

Provider API keys resolve in this order, first hit wins: the system environment,
the project `.env`, then the userspace fallback `~/.infer/auth.yaml` - a flat
YAML map of provider key env vars. The fallback applies wherever keys are passed
to child processes: the gateway (container and binary modes) and A2A agent
containers.

```yaml
ANTHROPIC_API_KEY: sk-ant-...
OPENAI_API_KEY: sk-...
```

A missing or unreadable `auth.yaml` changes nothing, and a malformed one is
ignored with a logged warning. Keep the file private (`chmod 600 ~/.infer/auth.yaml`); it is on the sandbox
`protected_paths` list, so agent tools cannot read or edit it.

### Gateway Configuration

- `INFER_GATEWAY_URL`: Gateway URL (default: `http://localhost:8080`)
- `INFER_GATEWAY_API_KEY`: Gateway API key for authentication
- `INFER_GATEWAY_TIMEOUT`: Gateway request timeout in seconds (default: `200`)
- `INFER_GATEWAY_OCI`: OCI image for gateway (default: `ghcr.io/inference-gateway/inference-gateway:latest`)
- `INFER_GATEWAY_RUN`: Auto-run gateway if not running (default: `true`)
- `INFER_GATEWAY_STANDALONE_BINARY`: Run the gateway as a standalone binary instead of a Docker container (default: `true`)

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
- `INFER_AGENT_MODE`: Default agent mode for `infer headless` - `standard`, `plan`, `auto` or
  `auto-with-judge`. Same as `--mode`
- `INFER_PROMPTS_AGENT_SYSTEM_PROMPT`: Custom system prompt for agent
- `INFER_PROMPTS_AGENT_SYSTEM_PROMPT_HEARTBEAT`: Custom system prompt for heartbeat
- `INFER_PROMPTS_AGENT_SYSTEM_PROMPT_REMOTE`: Custom system prompt for remote agent
- `INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_PLAN`: Custom plan-mode adjustment instructions (delivered by the mode-change reminder, not the system prompt)
- `INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_AUTO`: Custom auto-accept adjustment instructions (delivered by the mode-change reminder, not the system prompt)
- `INFER_PROMPTS_AGENT_CUSTOM_INSTRUCTIONS`: Custom instructions for agent
- `INFER_PROMPTS_GIT_COMMIT_MESSAGE_SYSTEM_PROMPT`: System prompt for generated commit messages
- `INFER_PROMPTS_CONVERSATION_TITLE_GENERATION_SYSTEM_PROMPT`: System prompt for conversation title generation
- `INFER_PROMPTS_VISION_ANNOTATOR_SCREEN_SYSTEM_PROMPT`: Annotation prompt for `screen` frames
- `INFER_PROMPTS_VISION_ANNOTATOR_SCENE_SYSTEM_PROMPT`: Annotation prompt for the other frame sources
- `INFER_PROMPTS_INIT_PROMPT`: Prompt `infer init` uses to explore the repository and write config

> **Migration note (v0.105.0+):** The old `INFER_AGENT_SYSTEM_PROMPT` and
> `INFER_AGENT_SYSTEM_PROMPT_PLAN` env vars were renamed to
> `INFER_PROMPTS_AGENT_SYSTEM_PROMPT` and `INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_PLAN`
> respectively when agent prompts moved under the `prompts.agent.*` config tree.
> Set `INFER_PROMPTS_AGENT_MODE_ADJUSTMENT_AUTO` for the auto-accept counterpart - if you are migrating an existing
> configuration, update your env vars to the new names above.

- `INFER_AGENT_MAX_TURNS`: Maximum agent turns (default: `50`)
- `INFER_AGENT_MAX_TOKENS`: Maximum tokens per response (default: `8192`)
- `INFER_AGENT_MAX_CONCURRENT_TOOLS`: Maximum concurrent tool executions (default: `5`)

### Reminders Configuration

Reminders live in their own `reminders.yaml` (see [System Reminders](#system-reminders-remindersyaml)); these env vars layer on top of it:

- `INFER_REMINDERS_ENABLED`: Master switch for all reminders (default: `true`)
- `INFER_REMINDERS_CONFIG`: Inline reminders YAML (same schema as `reminders.yaml`); when set it
  replaces the file-loaded reminders so embedded consumers need not write `~/.infer/reminders.yaml`

### Chat Configuration

- `INFER_CHAT_THEME`: Chat UI theme (`tokyo-night`, `github-light`, `dracula` or `charm`). The config default is
  `""`, and the TUI applies `tokyo-night` when it is unset
- `INFER_CHAT_INPUT_MAX_LINES`: Number of lines the chat input grows to before it starts scrolling (default: `20`)

### Tools Configuration

- `INFER_TOOLS_ENABLED`: Enable/disable all local tools (default: `true`)
- `INFER_TOOLS_MAX_RESULT_BYTES`: Byte cap on a single tool result before it is truncated (default: `250000`)
- `INFER_TOOLS_CUSTOM_DIR`: Directory to load your user [custom tools](custom-tools.md) from (default: `~/.infer/tools/`)

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
- `INFER_TOOLS_AGENT_WAIT`: Block the Agent tool call until every headless subagent finishes instead of
  returning at dispatch (default: `false`)
- `INFER_TOOLS_AGENT_MAX_PARALLEL`: Most subagents one Agent call may dispatch, tasks past the cap are
  dropped (default: `10`)
- `INFER_TOOLS_AGENT_IDLE_TIMEOUT`: Close an interactive subagent's tmux pane after N seconds of
  inactivity without a done signal (default: `300`; `0` disables the auto-close)
- `INFER_TOOLS_BASH_TIMEOUT`: Timeout in seconds for a foreground Bash command (default: `120`)
- `INFER_TOOLS_BASH_BACKGROUND_SHELLS_ENABLED`: Enable/disable the background shell manager (default: `true`)
- `INFER_TOOLS_BASH_BACKGROUND_SHELLS_MAX_CONCURRENT`: Most background shells running at once (default: `5`)
- `INFER_TOOLS_BASH_BACKGROUND_SHELLS_RETENTION_MINUTES`: Minutes a live shell is retained (default: `60`)
- `INFER_TOOLS_BASH_BACKGROUND_SHELLS_COMPLETED_RETENTION`: Finished shells kept for inspection (default: `5`)
- `INFER_TOOLS_WAIT_ENABLED`: Enable/disable the Wait tool (default: `true`)
- `INFER_TOOLS_WAIT_MAX_TIMEOUT_SECONDS`: Longest a single Wait call may block (default: `600`)
- `INFER_TOOLS_WAIT_COMMAND_POLL_INTERVAL_MS`: Poll interval for a `command` check, in milliseconds (default: `2000`)

**Tool Approval Configuration:**

- `INFER_TOOLS_BASH_REQUIRE_APPROVAL`: Require approval for Bash tool (default: unset)
- `INFER_TOOLS_WRITE_REQUIRE_APPROVAL`: Require approval for Write tool (default: `true`)
- `INFER_TOOLS_EDIT_REQUIRE_APPROVAL`: Require approval for Edit tool (default: `true`)
- `INFER_TOOLS_DELETE_REQUIRE_APPROVAL`: Require approval for Delete tool (default:
  `true`)
- `INFER_TEXT_TO_SPEECH_REQUIRE_APPROVAL`: Require approval for the TextToSpeech tool
  (default: unset, meaning no approval)
- `INFER_TEXT_TO_MUSIC_REQUIRE_APPROVAL`: Require approval for the TextToMusic tool
  (default: unset, meaning no approval)
- `INFER_TEXT_TO_SFX_REQUIRE_APPROVAL`: Require approval for the TextToSFX tool
  (default: unset, meaning no approval)
- `INFER_TEXT_TO_VIDEO_REQUIRE_APPROVAL`: Require approval for the TextToVideo tool
  (default: unset, meaning no approval) and the CreateAvatar tool (default: unset,
  meaning approval required)

Approval variables are tri-state: leaving one unset is not the same as setting it to
`false`. An unset tool falls back to the policy baked into the tool, while an explicit
value pins it either way.

**TextToMusic Tool Configuration:**

- `INFER_TEXT_TO_MUSIC_ENABLED`: Enable/disable the TextToMusic tool (default: `false`)
- `INFER_TEXT_TO_MUSIC_MODEL`: Gateway `provider/model` id used for music generation (default: `elevenlabs/music_v2_5`)
- `INFER_TEXT_TO_MUSIC_OUTPUT_DIR`: Directory the generated MP3 is written to (default: `~/.infer/tmp/music`)

These mirror the top-level `text_to_music:` YAML block: `enabled`, `model`, `output_dir`
and `require_approval`. The tool itself is documented in the
[Tools Reference](tools-reference.md#texttomusic-tool) and its output directory in the
[Directory Structure](directory-structure.md).

**TextToSFX Tool Configuration:**

- `INFER_TEXT_TO_SFX_ENABLED`: Enable/disable the TextToSFX tool (default: `false`)
- `INFER_TEXT_TO_SFX_MODEL`: Gateway `provider/model` id used for sound-effect generation
  (default: `elevenlabs/eleven_text_to_sound_v2`)
- `INFER_TEXT_TO_SFX_OUTPUT_DIR`: Directory the generated MP3 is written to (default:
  `~/.infer/tmp/sfx`)

These mirror the top-level `text_to_sfx:` YAML block: `enabled`, `model`, `output_dir`
and `require_approval`. The tool itself is documented in the
[Tools Reference](tools-reference.md#texttosfx-tool) and its output directory in the
[Directory Structure](directory-structure.md).

**TextToVideo Tool Configuration:**

- `INFER_TEXT_TO_VIDEO_ENABLED`: Enable/disable the TextToVideo tool (default: `false`)
- `INFER_TEXT_TO_VIDEO_MODEL`: Gateway `provider/model` id used for prompt renders
  (default: `elevenlabs/veo-3.1-fast-generate-001`)
- `INFER_TEXT_TO_VIDEO_AVATAR_MODEL`: Gateway `provider/model` id used for lip-synced
  avatar renders (default: `elevenlabs/creatify-aurora`)
- `INFER_TEXT_TO_VIDEO_SIZE`: Optional `widthxheight` passthrough, e.g. `720x1280`
  (default: empty, the provider default)
- `INFER_TEXT_TO_VIDEO_OUTPUT_DIR`: Directory the generated MP4 is written to (default:
  `~/.infer/tmp/video`)
- `INFER_TEXT_TO_VIDEO_TIMEOUT`: Whole-render timeout in seconds (default: `900`)
- `INFER_TEXT_TO_VIDEO_POLL_INTERVAL`: Job status poll interval in seconds (default: `5`)
- `INFER_TEXT_TO_VIDEO_CREATE_AVATAR`: Also register the CreateAvatar tool, which builds a
  library avatar from a photo (default: `false`; needs `INFER_TEXT_TO_VIDEO_ENABLED`)

These mirror the top-level `text_to_video:` YAML block: `enabled`, `model`,
`avatar_model`, `size`, `output_dir`, `timeout`, `poll_interval`, `create_avatar` and
`require_approval`. A gateway the CLI starts gets `VIDEOS_ENABLED=true` while the
tool is enabled. The tool itself is
documented in the [Tools Reference](tools-reference.md#texttovideo-tool) and its output
directory in the [Directory Structure](directory-structure.md).

**Bash Tool Allow-List Configuration:**

The Bash allow-list is **per agent mode** and configured in YAML. Set
`tools.bash.mode.<mode>.allow` in `config.yaml`, where `<mode>` is `all`
(baseline applied in every mode), `plan`, `standard`, or `auto`. The effective
list for a mode is `mode.all.allow` unioned with that mode's list; anything
unmatched is denied (it prompts for approval in chat, or is rejected with a
reason in headless agent mode).

The defaults are deliberately **explicit, non-destructive commands** - the
read-only `gh` subcommands (`gh issue/pr/... list|view`, `gh project
list|view|item-list|field-list`, `gh search`) plus exactly two narrow `gh api`
reads: `gh api repos/<owner>/<repo>/contents/<path>` and `gh api user/repos`
(with an optional `--paginate` and `--jq`). Every other `gh api` path is not
auto-approved, so prefer the structured subcommands or add a narrowly-scoped
`gh api` regex to a mode's `allow` if you genuinely need the raw API. One
notable consumer: the opentask browser extension performs its GitHub access as
`gh api` tool requests over the bridge
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
> (`|`, `&&`, `||`, `;`), file-write redirects (`>`, `>>`) and file-writing
> options (`sort -o`, `tree -o`, `git --output`), dangerous `find` actions, and
> printing/publishing an expanded `$VAR` (secret leak); benign redirects
> (`2>&1`, `>/dev/null`) are permitted. An allowed command also runs without
> approval only when every path it names is inside the sandbox. The single
> sentinel `.*` (default for `auto`) means unrestricted and skips the guard.
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

- `INFER_TOOLS_GREP_BACKEND`: Grep backend to use (`auto`, `ripgrep` or `go`, default: `auto`)

**WebSearch Tool Configuration:**

- `INFER_TOOLS_WEB_SEARCH_DEFAULT_ENGINE`: Default search engine (`duckduckgo` or `google`, default: `duckduckgo`)
- `INFER_TOOLS_WEB_SEARCH_MAX_RESULTS`: Maximum search results (default: `10`)
- `INFER_TOOLS_WEB_SEARCH_TIMEOUT`: Search timeout in seconds (default: `10`)

**WebFetch Tool Configuration:**

- `INFER_TOOLS_WEB_FETCH_SAFETY_MAX_SIZE`: Maximum fetch size in bytes (default: `10485760`)
- `INFER_TOOLS_WEB_FETCH_SAFETY_TIMEOUT`: Fetch timeout in seconds (default: `30`)
- `INFER_TOOLS_WEB_FETCH_CACHE_ENABLED`: Enable fetch caching (default: `true`)
- `INFER_TOOLS_WEB_FETCH_CACHE_TTL`: Cache TTL in seconds (default: `3600`)
- `INFER_TOOLS_WEB_FETCH_CACHE_MAX_SIZE`: Maximum cache size in bytes (default: `52428800`)

**Sandbox Configuration:**

- `INFER_TOOLS_SANDBOX_DIRECTORIES`: Comma-separated list of allowed directories (default: `.,/tmp`)

### Storage Configuration

- `INFER_STORAGE_ENABLED`: Enable conversation storage (default: `true`)
- `INFER_STORAGE_TYPE`: Storage backend type (`memory`, `jsonl`, `sqlite`, `postgres`, `redis` or `d1`, default: `jsonl`)

**JSONL Storage:**

- `INFER_STORAGE_JSONL_PATH`: Where JSONL conversations are written. Empty keeps the per-project default,
  `~/.infer/projects/<project-slug>/conversations`

**SQLite Storage:**

- `INFER_STORAGE_SQLITE_PATH`: SQLite database path (default: `~/.infer/conversations.db`)

**PostgreSQL Storage:**

- `INFER_STORAGE_POSTGRES_HOST`: PostgreSQL host
- `INFER_STORAGE_POSTGRES_PORT`: PostgreSQL port (default: `5432`)
- `INFER_STORAGE_POSTGRES_DATABASE`: PostgreSQL database name
- `INFER_STORAGE_POSTGRES_USERNAME`: PostgreSQL username
- `INFER_STORAGE_POSTGRES_PASSWORD`: PostgreSQL password
- `INFER_STORAGE_POSTGRES_SSL_MODE`: PostgreSQL SSL mode (default: `prefer`)

**Redis Storage:**

- `INFER_STORAGE_REDIS_HOST`: Redis host
- `INFER_STORAGE_REDIS_PORT`: Redis port (default: `6379`)
- `INFER_STORAGE_REDIS_PASSWORD`: Redis password
- `INFER_STORAGE_REDIS_DB`: Redis database number (default: `0`)

**Cloudflare D1 Storage:**

- `INFER_STORAGE_D1_ACCOUNT_ID`: Cloudflare account id owning the database
- `INFER_STORAGE_D1_DATABASE_ID`: D1 database id
- `INFER_STORAGE_D1_API_TOKEN`: Cloudflare API token - normally injected here rather than written to `config.yaml`
- `INFER_STORAGE_D1_BASE_URL`: Override the Cloudflare API base URL

### Scheduler Configuration

- `INFER_SCHEDULER_BACKEND`: Scheduling backend, `local` or `github` (default: `local`). See the [Scheduling Guide](scheduling.md#github-backend)
- `INFER_SCHEDULER_GITHUB_REPOSITORY`: Repository for GitHub-backed schedules (default: `<login>/.routines`)
- `INFER_SCHEDULER_GITHUB_APP_CLIENT_ID_SECRET`: Name of the Actions secret holding the GitHub App client id,
  passed to `actions/create-github-app-token` (default: the built-in name)
- `INFER_SCHEDULER_GITHUB_APP_PRIVATE_KEY_SECRET`: Name of the Actions secret holding the GitHub App private key
- `INFER_SCHEDULER_GITHUB_BOT_NAME`: Git author/committer name on deploy commits; use `<app-slug>[bot]` to attribute
  them to the app
- `INFER_SCHEDULER_GITHUB_BOT_EMAIL`: Git author/committer email (`<user-id>+<app-slug>[bot]@users.noreply.github.com` for a bot)
- `INFER_SCHEDULER_GITHUB_PULL_REQUESTS`: Deploy schedule changes via pull request instead of pushing to the default branch (default: `false`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_ENABLED`: Pull conversation artifacts from GitHub-backed runs into local storage (default: `true`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_POLL_INTERVAL`: Artifact poll interval (default: `10m`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_INITIAL_DELAY`: Delay before the first poll (default: `1m`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_MAX_ATTEMPTS`: Download attempts per artifact before it is skipped (default: `3`)
- `INFER_SCHEDULER_GITHUB_ARTIFACTS_RATE_LIMIT_BACKOFF`: Polling pause after a rate-limited GitHub API call (default: `1h`)

### Conversation Configuration

- `INFER_CONVERSATION_TITLE_GENERATION_ENABLED`: Enable AI-powered title generation (default: `true`)
- `INFER_CONVERSATION_TITLE_GENERATION_MODEL`: Model for title generation (default: empty, falls back to `agent.model`)
- `INFER_CONVERSATION_TITLE_GENERATION_BATCH_SIZE`: Batch size for title generation (default: `10`)
- `INFER_CONVERSATION_TITLE_GENERATION_INTERVAL`: Interval in seconds between title generation attempts (default: unset, falls back to 5 minutes)

### A2A (Agent-to-Agent) Configuration

- `INFER_A2A_ENABLED`: Enable/disable A2A tools (default: `true`)
- `INFER_A2A_AGENTS_READY_TIMEOUT_SEC`: Seconds to wait for configured A2A agents to become ready
  (default: `600`)
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

- `INFER_A2A_TASK_STATUS_POLL_SECONDS`: Status polling interval in seconds (default: `5`)
- `INFER_A2A_TASK_POLLING_STRATEGY`: Polling strategy (`fixed` or `exponential`, default: `exponential`)
- `INFER_A2A_TASK_INITIAL_POLL_INTERVAL_SEC`: Initial polling interval for exponential strategy (default: `2`)
- `INFER_A2A_TASK_MAX_POLL_INTERVAL_SEC`: Maximum polling interval for exponential strategy (default: `60`)
- `INFER_A2A_TASK_BACKOFF_MULTIPLIER`: Backoff multiplier for exponential strategy (default: `2.0`)
- `INFER_A2A_TASK_COMPLETED_TASK_RETENTION`: Number of completed tasks kept in the tracker (default: `5`)
- `INFER_A2A_TASK_AGENT_MODE_MAX_WAIT_SECONDS`: Longest a task may wait for an agent-mode change mid-run
  (default: `300`)
- `INFER_A2A_TASK_ARTIFACTS_AUTO_DOWNLOAD`: Download a task's artifacts as soon as it completes
  (default: `false`)

**A2A Individual Tool Configuration:**

- `INFER_A2A_TOOLS_SUBMIT_TASK_ENABLED`: Enable/disable A2A SubmitTask tool (default: `true`)
- `INFER_A2A_TOOLS_SUBMIT_TASK_REQUIRE_APPROVAL`: Require approval for SubmitTask (default: `true`)
- `INFER_A2A_TOOLS_QUERY_AGENT_ENABLED`: Enable/disable A2A QueryAgent tool (default: `true`)
- `INFER_A2A_TOOLS_QUERY_AGENT_REQUIRE_APPROVAL`: Require approval for QueryAgent (default: `false`)
- `INFER_A2A_TOOLS_QUERY_TASK_ENABLED`: Enable/disable A2A QueryTask tool (default: `true`)
- `INFER_A2A_TOOLS_QUERY_TASK_REQUIRE_APPROVAL`: Require approval for QueryTask (default: `false`)

### Export Configuration

- `INFER_EXPORT_OUTPUT_DIR`: Output directory for exported conversations (default: empty, writes to `~/.infer/projects/<project-slug>/exports`)

### Compact Configuration

- `INFER_COMPACT_ENABLED`: Enable automatic conversation compaction (default: `true`)
- `INFER_COMPACT_AUTO_AT`: Percentage of the context window (20-100) at which to auto-compact (default: `80`)

### Keybinding Environment Variables

Keybindings can be configured via environment variables (supports comma-separated or newline-separated lists):

```bash
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

Values in `config.yaml` are **not** expanded - the file is read with Viper and values are passed
through literally, so a placeholder such as `%VAR_NAME%` would reach your services as-is. Keep
secrets out of `config.yaml` and supply them via `INFER_*` environment variables instead:

```bash
export INFER_GATEWAY_API_KEY=...
```

Any config key can be overridden from the environment with `INFER_<PATH_WITH_UNDERSCORES>` (for
example `INFER_STORAGE_POSTGRES_PASSWORD` for `storage.postgres.password`); environment values
take precedence over file values.

Only the split sidecar YAML files (`channels.yaml`, `heartbeat.yaml`, `computer_use.yaml`,
`browser_use.yaml`, ...) support environment substitution, using `${VAR}` syntax
(see `config/utils/yamlfile.go`).

---

## Configuration Best Practices

### Security

- **Never commit sensitive data** (API keys, tokens) to configuration files
- Use `INFER_*` environment variables for sensitive values - `config.yaml` values are not expanded
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

Startup validation does **not** catch unknown keys. Invalid YAML syntax fails
startup with an error, and `Config.Validate` rejects specific invalid values
(e.g. `tools.safety.approval_behaviour`, `agent.reasoning_effort`), but unknown
configuration keys are silently ignored: `loadConfigFromViper` decodes with
non-strict `v.Unmarshal` and `Config.Validate` only checks specific settings, so
a misspelled or removed key in `config.yaml` produces no warning (this is also
what lets `infer init` migrate legacy `channels:` / `computer_use:` blocks out
of `config.yaml`). Only `infer config set` rejects unknown keys - use
`infer config get <key>` below to confirm a key actually resolves.

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
