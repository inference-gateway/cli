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
   - Global configuration for the user across all projects
   - Used as a fallback when no project-level configuration exists
   - Can be created with: `infer init --userspace` or `infer config init --userspace`

2. **Project Configuration** (`.infer/config.yaml` in current directory)
   - Project-specific configuration that takes precedence over userspace config
   - Default location for most commands
   - Can be created with: `infer init` or `infer config init`

---

## Configuration Precedence

Configuration values are resolved in the following order (highest to lowest priority):

1. **Environment Variables** (`INFER_*` prefix) - **Highest Priority**
2. **Command Line Flags**
3. **Project Config** (`.infer/config.yaml`)
4. **Userspace Config** (`~/.infer/config.yaml`)
5. **Built-in Defaults** - **Lowest Priority**

**Example**: If your userspace config sets `agent.model: "anthropic/claude-4"` and your project config
sets `agent.model: "deepseek/deepseek-chat"`, the project config wins. However, if you also set
`INFER_AGENT_MODEL="openai/gpt-4"`, the environment variable takes precedence over both config files.

### Usage Examples

```bash
# Create userspace configuration (global fallback)
infer init --userspace

# Create project configuration (takes precedence)
infer init

# Both configurations will be automatically merged when commands are run
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
  exclude_models:
    - ollama_cloud/cogito-2.1:671b
    - ollama_cloud/kimi-k2:1t
    - ollama_cloud/kimi-k2-thinking
    - ollama_cloud/deepseek-v3.1:671b # Block specific models by default
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
        - docker ps
        - kubectl get pods
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
      max_size: 8192 # 8KB
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
  enabled: false # Enable automatic conversation compaction
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
  - Example: `["deepseek/deepseek-reasoner", "deepseek/deepseek-chat"]`
  - This is passed to the gateway as the `ALLOWED_MODELS` environment variable
- **gateway.exclude_models**: Block specific models (blocklist approach, default: `[]`, blocks none)
  - When set, all models are allowed except those in the list
  - Example: `["openai/gpt-4", "anthropic/claude-4-opus"]`
  - This is passed to the gateway as the `DISALLOWED_MODELS` environment variable
  - Note: `include_models` and `exclude_models` can be used together - the gateway will apply both filters

### Client Settings

- **client.timeout**: HTTP client timeout in seconds
- **client.retry.enabled**: Enable automatic retries for failed requests
- **client.retry.max_attempts**: Maximum number of retry attempts
- **client.retry.initial_backoff_sec**: Initial delay between retries in seconds
- **client.retry.max_backoff_sec**: Maximum delay between retries in seconds
- **client.retry.backoff_multiplier**: Backoff multiplier for exponential delay
- **client.retry.retryable_status_codes**: HTTP status codes that trigger retries (e.g., [400, 408, 429, 500, 502, 503, 504])

### Logging Settings

- **logging.debug**: Enable debug logging for verbose output

### Tool Settings

- **tools.enabled**: Enable/disable tool execution for LLMs (default: true)
- **tools.sandbox.directories**: Allowed directories for tool operations (default: [".", "/tmp"])
- **tools.sandbox.protected_paths**: Paths excluded from tool access for security (default: [".infer/", ".git/", "*.env"])
- **tools.whitelist.commands**: List of allowed commands (supports arguments)
- **tools.whitelist.patterns**: Regex patterns for complex command validation
- **tools.safety.require_approval**: Prompt user before executing any command (default: true)
- **Individual tool settings**: Each tool (Bash, Read, Write, Edit, Delete, Grep, Tree, WebFetch, WebSearch, TodoWrite) has:
  - **enabled**: Enable/disable the specific tool
  - **require_approval**: Override global safety setting for this tool (optional)

### Compact Settings

- **compact.enabled**: Enable automatic conversation compaction to reduce token usage (default: false)
- **compact.auto_at**: Percentage of context window (20-100) at which to automatically trigger compaction (default: 80)

### Agent Settings

- **agent.model**: Default model for agent operations
- **agent.system_prompt**: System prompt included with every agent session
- **agent.system_reminders.enabled**: Enable/disable system reminders (default: true)
- **agent.system_reminders.interval**: Number of messages between reminders (default: 10)
- **agent.system_reminders.text**: Custom reminder text to provide contextual guidance
- **agent.verbose_tools**: Enable verbose tool output (default: false)
- **agent.max_turns**: Maximum number of turns for agent sessions (default: 50)
- **agent.max_tokens**: Maximum tokens per agent request (default: 8192)
- **agent.max_concurrent_tools**: Maximum number of tools that can execute concurrently (default: 5)

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
    - **session_tokens**: Session token usage statistics (default: `true`)
    - **git_branch**: Current Git branch name (default: `true`)
      - Only displays when in a Git repository
      - Uses 5-second cache for performance
      - Automatically updates after Git operations in bash mode
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

The CLI supports customizable keybindings for the chat interface. Keybindings are **disabled by
default** and must be explicitly enabled.

- **chat.keybindings.enabled**: Enable/disable custom keybindings (default: `false`)
- **chat.keybindings.bindings**: Map of keybinding configurations

**Features:**

- **Namespace-Based Organization**: Action IDs use format `namespace_action` (e.g., `global_quit`, `mode_cycle_agent_mode`)
- **Context-Aware Conflict Detection**: Validates conflicts only within the same namespace
- **Self-Documenting**: All keybindings are visible in config with descriptions
- **No Runtime Validation**: Config loaded once at startup for performance
- **Explicit Validation**: Run `infer keybindings validate` to check config
- **Environment Variable Support**: Configure keybindings via comma-separated env vars

**Example Configuration:**

```yaml
chat:
  theme: tokyo-night
  keybindings:
    enabled: false  # Set to true to enable
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
- `INFER_CLIENT_RETRY_ENABLED`: Enable retry logic (default: `true`)
- `INFER_CLIENT_RETRY_MAX_ATTEMPTS`: Maximum retry attempts (default: `3`)
- `INFER_CLIENT_RETRY_INITIAL_BACKOFF_SEC`: Initial backoff delay in seconds (default: `5`)
- `INFER_CLIENT_RETRY_MAX_BACKOFF_SEC`: Maximum backoff delay in seconds (default: `60`)
- `INFER_CLIENT_RETRY_BACKOFF_MULTIPLIER`: Backoff multiplier (default: `2`)

### Logging Configuration

- `INFER_LOGGING_DEBUG`: Enable debug logging (default: `false`)
- `INFER_LOGGING_DIR`: Log directory path (default: `.infer/logs`)

### Agent Configuration

- `INFER_AGENT_MODEL`: Default model for agent operations (e.g., `deepseek/deepseek-chat`)
- `INFER_AGENT_SYSTEM_PROMPT`: Custom system prompt for agent
- `INFER_AGENT_SYSTEM_PROMPT_PLAN`: Custom system prompt for plan mode
- `INFER_AGENT_VERBOSE_TOOLS`: Enable verbose tool output (default: `false`)
- `INFER_AGENT_MAX_TURNS`: Maximum agent turns (default: `100`)
- `INFER_AGENT_MAX_TOKENS`: Maximum tokens per response (default: `8192`)
- `INFER_AGENT_MAX_CONCURRENT_TOOLS`: Maximum concurrent tool executions (default: `5`)

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
- `INFER_TOOLS_GITHUB_ENABLED`: Enable/disable Github tool (default: `true`)
- `INFER_TOOLS_TODO_WRITE_ENABLED`: Enable/disable TodoWrite tool (default: `true`)

**Tool Approval Configuration:**

- `INFER_TOOLS_BASH_REQUIRE_APPROVAL`: Require approval for Bash tool (default:
  `false`)
- `INFER_TOOLS_WRITE_REQUIRE_APPROVAL`: Require approval for Write tool (default: `true`)
- `INFER_TOOLS_EDIT_REQUIRE_APPROVAL`: Require approval for Edit tool (default: `true`)
- `INFER_TOOLS_DELETE_REQUIRE_APPROVAL`: Require approval for Delete tool (default:
  `true`)

**Bash Tool Whitelist Configuration:**

The Bash tool supports whitelisting commands and patterns for security. These environment variables
accept comma-separated or newline-separated values:

- `INFER_TOOLS_BASH_WHITELIST_COMMANDS`: Comma-separated list of whitelisted commands
- `INFER_TOOLS_BASH_WHITELIST_PATTERNS`: Comma-separated list of regex patterns for whitelisted commands

**Examples:**

```bash
# Whitelist specific commands
export INFER_TOOLS_BASH_WHITELIST_COMMANDS="gh,git,npm,task,make"

# Whitelist command patterns (regex)
export INFER_TOOLS_BASH_WHITELIST_PATTERNS="^gh .*,^git .*,^npm .*,^task .*"

# Combined example for GitHub Actions
export INFER_TOOLS_BASH_WHITELIST_COMMANDS="gh,git,npm"
export INFER_TOOLS_BASH_WHITELIST_PATTERNS="^gh .*,^git .*,^npm (install|test|run).*"
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

**GitHub Tool Configuration:**

- `INFER_TOOLS_GITHUB_TOKEN`: GitHub personal access token
- `INFER_TOOLS_GITHUB_BASE_URL`: GitHub API base URL (default: `https://api.github.com`)
- `INFER_TOOLS_GITHUB_OWNER`: Default GitHub owner/organization
- `INFER_TOOLS_GITHUB_REPO`: Default GitHub repository
- `INFER_TOOLS_GITHUB_SAFETY_MAX_SIZE`: Maximum GitHub file size in bytes (default: `10485760`)
- `INFER_TOOLS_GITHUB_SAFETY_TIMEOUT`: GitHub API timeout in seconds (default: `30`)

**Sandbox Configuration:**

- `INFER_TOOLS_SANDBOX_DIRECTORIES`: Comma-separated list of allowed directories (default: `.,/tmp`)

### Storage Configuration

- `INFER_STORAGE_ENABLED`: Enable conversation storage (default: `true`)
- `INFER_STORAGE_TYPE`: Storage backend type (`memory`, `sqlite`, `postgres`, `redis`, default: `sqlite`)

**SQLite Storage:**

- `INFER_STORAGE_SQLITE_PATH`: SQLite database path (default: `.infer/conversations.db`)

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
- `INFER_EXPORT_SUMMARY_MODEL`: Model for generating export summaries (default: `anthropic/claude-4.1-haiku`)

### Compact Configuration

- `INFER_COMPACT_ENABLED`: Enable automatic conversation compaction (default: `false`)
- `INFER_COMPACT_AUTO_AT`: Auto-compact after N messages (default: `100`)

### Git Configuration

- `INFER_GIT_COMMIT_MESSAGE_MODEL`: Model for AI-generated commit messages (default: `deepseek/deepseek-chat`)

### SCM Configuration

- `INFER_SCM_PR_CREATE_BASE_BRANCH`: Base branch for PR creation (default: `main`)
- `INFER_SCM_PR_CREATE_BRANCH_PREFIX`: Branch prefix for PR creation (default: `feature/`)
- `INFER_SCM_PR_CREATE_MODEL`: Model for PR creation (default: `deepseek/deepseek-chat`)
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

tools:
  github:
    token: "%GITHUB_TOKEN%"
```

This allows sensitive values to be stored as environment variables while keeping them out of configuration files.

---

## Configuration Best Practices

### Security

- **Never commit sensitive data** (API keys, tokens) to configuration files
- Use environment variable substitution (`%VAR_NAME%`) for sensitive values
- Use environment variables (`INFER_*`) for CI/CD environments

### Organization

- Use **project config** (`.infer/config.yaml`) for project-specific settings
- Use **userspace config** (`~/.infer/config.yaml`) for personal preferences
- Commit project configs to version control, exclude userspace configs

### Example Workflow

```bash
# 1. Setup userspace defaults
infer config --userspace agent set-model "deepseek/deepseek-chat"
infer config --userspace agent set-system "You are a helpful assistant"

# 2. Project-specific overrides
infer config agent set-model "deepseek/deepseek-chat"  # Project-specific model
infer config tools bash enable  # Enable bash tools for this project

# 3. Runtime overrides
INFER_AGENT_VERBOSE_TOOLS=true infer chat  # Temporary verbose mode
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
# Enable verbose logging
infer -v config show

# Enable debug logging
INFER_LOGGING_DEBUG=true infer config show

# Check which config file is being used
infer config show | grep "Configuration file"
```

---

[← Back to README](../README.md)
