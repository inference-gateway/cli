# Configuration System

The Inference Gateway CLI uses a powerful 2-layer configuration system built on
[Viper](https://github.com/spf13/viper), supporting multiple configuration sources with proper precedence handling.

## Configuration Precedence

Configuration values are resolved in the following order (highest to lowest priority):

1. **Environment Variables** (INFER_* prefix) - **Highest Priority**
2. **Command Line Flags**
3. **Project Config** (`.infer/config.yaml`)
4. **Userspace Config** (`~/.infer/config.yaml`)
5. **Built-in Defaults** - **Lowest Priority**

## Configuration Locations

### Project-Level Configuration

- **Path**: `.infer/config.yaml` (relative to your project directory)
- **Purpose**: Project-specific settings that should be version-controlled
- **Priority**: Higher than userspace config

### Userspace Configuration

- **Path**: `~/.infer/config.yaml` (user's home directory)
- **Purpose**: Personal global settings that apply across all projects
- **Priority**: Fallback when project config doesn't specify a value

## Environment Variables

All configuration options can be overridden using environment variables with the `INFER_` prefix:

```bash
# Gateway configuration
export INFER_GATEWAY_URL="https://api.inference.example.com"
export INFER_GATEWAY_API_KEY="your-api-key"
export INFER_GATEWAY_TIMEOUT=300

# Agent configuration
export INFER_AGENT_MODEL="anthropic/claude-4.1"
export INFER_AGENT_MAX_TURNS=100
export INFER_AGENT_MAX_CONCURRENT_TOOLS=3
export INFER_AGENT_VERBOSE_TOOLS=true

# Tools configuration
export INFER_TOOLS_ENABLED=true
export INFER_TOOLS_SAFETY_REQUIRE_APPROVAL=false

# Nested configuration using underscores
export INFER_TOOLS_BASH_ENABLED=true
export INFER_TOOLS_WEB_FETCH_ENABLED=false

# A2A (Agent-to-Agent) configuration
export INFER_A2A_ENABLED=true
export INFER_A2A_AGENTS="http://agent1:8080,http://agent2:8080"
export INFER_A2A_CACHE_ENABLED=true
export INFER_A2A_CACHE_TTL=300

# A2A Tools configuration
export INFER_A2A_TOOLS_SUBMIT_TASK_ENABLED=true
export INFER_A2A_TOOLS_QUERY_AGENT_ENABLED=true
export INFER_A2A_TOOLS_QUERY_TASK_ENABLED=true
export INFER_A2A_TOOLS_DOWNLOAD_ARTIFACTS_ENABLED=true
export INFER_A2A_DOWNLOAD_ARTIFACTS_DOWNLOAD_DIR="/tmp/downloads"
export INFER_A2A_DOWNLOAD_ARTIFACTS_TIMEOUT_SECONDS=30

# Storage configuration (for database passwords)
export INFER_STORAGE_POSTGRES_PASSWORD="your-postgres-password"
export INFER_STORAGE_REDIS_PASSWORD="your-redis-password"
```

### Conversion Rules

- Nested configuration: dots (`.`) become underscores (`_`)
- Case insensitive: `INFER_gateway_url` = `INFER_GATEWAY_URL`
- Boolean values: `true`, `false`, `1`, `0`, `yes`, `no`

## Configuration Structure

The configuration is organized into logical sections:

```yaml
# Gateway connection settings
gateway:
  url: "http://localhost:8080"
  api_key: "%GITHUB_TOKEN%"  # Environment variable substitution
  timeout: 200

# HTTP client settings
client:
  timeout: 200
  retry:
    enabled: true
    max_attempts: 3
    initial_backoff_sec: 5
    max_backoff_sec: 60
    backoff_multiplier: 2
    retryable_status_codes: [400, 408, 429, 500, 502, 503, 504]

# Logging configuration
logging:
  debug: false
  dir: ""  # Defaults to .infer/logs

# Agent behavior
agent:
  model: ""  # Default model to use
  system_prompt: "..."  # Custom system prompt
  verbose_tools: false
  max_turns: 50
  max_tokens: 4096
  max_concurrent_tools: 5 
  optimization:
    enabled: false
    max_history: 10
    compact_threshold: 20
    truncate_large_outputs: true
    skip_redundant_confirmations: true

# Tool system configuration
tools:
  enabled: true
  sandbox:
    directories: [".", "/tmp"]
    protected_paths: [".infer/", ".git/", "*.env"]

  # Individual tool settings
  bash:
    enabled: true
    require_approval: true  # Override global safety setting
    whitelist:
      commands: ["ls", "pwd", "echo", "git"]
      patterns: ["^git status$", "^git diff.*"]

  read:
    enabled: true
    require_approval: false

  write:
    enabled: true
    require_approval: true

  # ... other tools

  safety:
    require_approval: true  # Global approval setting

# A2A (Agent-to-Agent) configuration
a2a:
  enabled: false  # Enable/disable A2A functionality
  agents: []      # List of A2A agent endpoints
  
  # Agent card caching settings
  cache:
    enabled: true
    ttl: 300      # Cache TTL in seconds
  
  # Task monitoring configuration
  task:
    status_poll_seconds: 5
    idle_timeout_sec: 60
    polling_strategy: "exponential"
    initial_poll_interval_sec: 2
    max_poll_interval_sec: 60
    backoff_multiplier: 2.0
    background_monitoring: true
  
  # Individual A2A tool settings
  tools:
    query_agent:
      enabled: true
      require_approval: false
    
    query_task:
      enabled: true
      require_approval: false
    
    submit_task:
      enabled: true
      require_approval: false
    
    download_artifacts:
      enabled: true
      download_dir: "/tmp/downloads"
      timeout_seconds: 30
      require_approval: false

# Token optimization settings
compact:
  output_dir: ".infer"
  summary_model: ""

# Storage configuration (for conversation history)
storage:
  enabled: true
  type: "sqlite"  # Options: memory, sqlite, postgres, redis

  sqlite:
    path: ".infer/conversations.db"

  postgres:
    host: "localhost"
    port: 5432
    database: "infer_conversations"
    username: ""
    password: ""  # Use INFER_STORAGE_POSTGRES_PASSWORD env var
    ssl_mode: "prefer"

  redis:
    host: "localhost"
    port: 6379
    password: ""  # Use INFER_STORAGE_REDIS_PASSWORD env var
    db: 0
```

## Command Usage

### Configuration Management

```bash
# Initialize project config
infer init

# Initialize userspace config
infer init --userspace

# View current configuration
infer config show

# Set agent model (project config)
infer config agent set-model "anthropic/claude-4.1"

# Set agent model (userspace config)
infer config agent set-model --userspace "anthropic/claude-4.1"

# Set system prompt
infer config agent set-system "You are a helpful coding assistant"

# Configure max turns
infer config agent set-max-turns 100

# Enable/disable verbose tools
infer config agent verbose-tools enable

# Enable/disable tools globally
infer config tools enable
infer config tools disable
```

### Global vs Local Flags

The `--userspace` flag can be used in two ways:

```bash
# As a command-specific flag
infer config agent set-model --userspace "claude-4.1"

# As a global flag (applies to all subcommands)
infer config --userspace agent set-model "claude-4.1"
```

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

## Best Practices

### Security

- **Never commit sensitive data** (API keys, tokens) to configuration files
- Use environment variable substitution (`%VAR_NAME%`) for sensitive values
- Use environment variables (`INFER_*`) for CI/CD environments

### Organization

- Use **project config** (`.infer/config.yaml`) for project-specific settings
- Use **userspace config** (`~/.infer/config.yaml`) for personal preferences
- Commit project configs to version control, exclude userspace configs

### Example Workflow

1. **Setup userspace defaults**:

```bash
infer config --userspace agent set-model "anthropic/claude-4.1"
infer config --userspace agent set-system "You are a helpful assistant"
```

2. **Project-specific overrides**:

```bash
infer config agent set-model "deepseek/deepseek-chat"  # Project-specific model
infer config tools bash enable  # Enable bash tools for this project
```

3. **Runtime overrides**:

```bash
INFER_AGENT_VERBOSE_TOOLS=true infer chat  # Temporary verbose mode
```

## Configuration Validation

The CLI validates configuration on startup and provides helpful error messages:

- Invalid YAML syntax
- Unknown configuration keys
- Invalid value types (string vs boolean vs integer)
- Missing required values

## Migration from Legacy Config

If you have an existing configuration, the CLI automatically migrates to the new Viper-based
system while preserving all settings and maintaining backward compatibility.

## Troubleshooting

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
