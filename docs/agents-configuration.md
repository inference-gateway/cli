# A2A Agents Configuration

This document describes how to configure Agent-to-Agent (A2A) communication using the `agents.yaml` configuration file.

## Overview

The Inference Gateway CLI supports delegating tasks to other AI agents through the A2A protocol. Instead of
configuring agents directly in `config.yaml`, you can now use a dedicated `agents.yaml` file that provides a
cleaner, more focused configuration experience.

## Configuration Files

A2A agents can be configured in two locations:

1. **Project-level**: `.infer/agents.yaml` - Agents specific to the current project
2. **Userspace**: `~/.infer/agents.yaml` - Global agents available across all projects

Project-level configuration takes precedence over userspace configuration.

## Agent Configuration Structure

Each agent entry in `agents.yaml` has the following fields:

```yaml
agents:
  - name: agent-name          # Required: Unique identifier for the agent
    url: https://agent.url    # Required: Agent's HTTP endpoint
    oci: registry/image:tag   # Optional: OCI image reference for local execution
    run: false                # Optional: Whether to run agent locally with Docker
    environment:              # Optional: Environment variables for the agent
      KEY: VALUE
```

### Field Descriptions

- **name**: A unique identifier for the agent. Used in CLI commands and logs.
- **url**: The HTTP endpoint where the agent is accessible.
- **oci**: OCI (Docker) image reference for running the agent locally.
- **run**: Boolean flag indicating whether this agent should be run locally with Docker (default: `false`).
- **environment**: Key-value pairs of environment variables to pass to the agent when running locally.
  Supports environment variable substitution using `$VAR` or `${VAR}` syntax.

## CLI Commands

### Initialize Agents Configuration

Create a new `agents.yaml` file:

```bash
# Initialize in current project
infer agents init

# Initialize in userspace
infer agents init --userspace
```

### Add an Agent

Add a new agent to the configuration:

```bash
# Basic remote agent
infer agents add code-reviewer https://agent.example.com

# Local agent with OCI image
infer agents add test-runner https://localhost:8081 \
  --oci ghcr.io/org/test-runner:latest \
  --run

# Agent with environment variables
infer agents add analyzer https://agent.example.com \
  --environment API_KEY=secret \
  --environment MODEL=gpt-4

# Add to userspace configuration
infer agents add global-helper https://helper.example.com --userspace
```

### List Agents

View all configured agents:

```bash
# List agents (text format)
infer agents list

# List agents in JSON format
infer agents list --format json

# List userspace agents
infer agents list --userspace
```

### Show Agent Details

Display detailed information about a specific agent:

```bash
# Show agent details
infer agents show code-reviewer

# Show in JSON format
infer agents show code-reviewer --format json
```

### Remove an Agent

Remove an agent from the configuration:

```bash
# Remove agent
infer agents remove code-reviewer

# Remove from userspace configuration
infer agents remove global-helper --userspace
```

## Environment Variable Substitution

The agents configuration supports environment variable substitution, allowing you to reference environment
variables in your `agents.yaml` file without hardcoding sensitive values.

### Syntax

You can use either syntax:

- `$VAR_NAME` - Simple variable reference
- `${VAR_NAME}` - Bracketed variable reference (useful when concatenating with other text)

### Where It Works

Environment variable substitution is supported in:

- `environment` values
- `model` field
- Any string field in the configuration

### .env File Support

The CLI automatically loads environment variables from a `.env` file in the current working directory when
starting agents. This provides a convenient way to manage environment variables without cluttering your shell
environment.

**Priority order for environment variable resolution:**

1. `.env` file (highest priority)
2. System environment variables
3. Literal values from `agents.yaml`

**Important:** Only variables listed in the `environment` section of `agents.yaml` will be injected into the
agent container. The CLI will not inject all variables from `.env` - you must explicitly list which variables
each agent needs.

### Example: API Keys

Instead of hardcoding API keys:

```yaml
agents:
  - name: openai-agent
    url: http://localhost:8080
    oci: ghcr.io/org/openai-agent:latest
    run: true
    environment:
      OPENAI_API_KEY: $OPENAI_API_KEY
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
```

Create a `.env` file in your project root:

```bash
# .env
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
```

Or set the environment variables in your shell:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
```

When the agent starts, the CLI will:

1. Check `.env` file for the variables
2. Fall back to system environment if not found in `.env`
3. Use the literal value if not found in either

### Example: Dynamic Configuration

You can use environment variables for any configuration value:

```yaml
agents:
  - name: dynamic-agent
    url: http://localhost:8080
    oci: ghcr.io/org/agent:latest
    run: true
    model: ${AGENT_PROVIDER}/${AGENT_MODEL}
    environment:
      LOG_LEVEL: ${LOG_LEVEL:-info}
      API_ENDPOINT: https://api.example.com/v${API_VERSION}
      COMBINED: prefix-${VAR1}-${VAR2}-suffix
```

### Important Notes

1. **Undefined variables**: If an environment variable is not set, it will expand to an empty string
2. **No default values**: Unlike shell syntax, `${VAR:-default}` is not supported - the entire string including
   `:-default` is treated as the variable name
3. **Escaping**: There is no escape mechanism - all `$` characters trigger substitution
4. **Load time**: Variables are expanded when the configuration is loaded, not when agents start

## Usage Examples

### Example 1: Remote Agent

Configure a remote code review agent:

```bash
infer agents add code-reviewer https://code-review.example.com
```

This creates an entry in `.infer/agents.yaml`:

```yaml
agents:
  - name: code-reviewer
    url: https://code-review.example.com
    run: false
```

### Example 2: Local Agent with Docker

Configure an agent that runs locally in a Docker container:

```bash
infer agents add test-runner https://localhost:8081 \
  --oci ghcr.io/myorg/test-runner:v1.0 \
  --run \
  --environment GITHUB_TOKEN=${GITHUB_TOKEN} \
  --environment TEST_FRAMEWORK=pytest
```

This creates:

```yaml
agents:
  - name: test-runner
    url: https://localhost:8081
    oci: ghcr.io/myorg/test-runner:v1.0
    run: true
    environment:
      GITHUB_TOKEN: ${GITHUB_TOKEN}
      TEST_FRAMEWORK: pytest
```

When the agent starts, `${GITHUB_TOKEN}` will be automatically expanded to the value from your environment.

### Example 3: Multiple Agents

Configure multiple specialized agents:

```bash
# Security auditor
infer agents add security-audit https://security.example.com \
  --environment SEVERITY_LEVEL=high

# Performance analyzer
infer agents add perf-analyzer https://perf.example.com \
  --environment METRICS_BACKEND=prometheus

# Documentation generator
infer agents add doc-gen https://docs.example.com
```

## System Prompt Integration

Configured agents are automatically included in the system prompt for chat and agent sessions. When you start a
session, the AI is aware of available agents and can delegate tasks using the A2A tools.

Example system prompt section:

```text
Available A2A Agents:
- https://code-review.example.com
- https://security.example.com
- https://perf.example.com

You can delegate tasks to these agents using the A2A tools (A2A_SubmitTask, A2A_QueryAgent, A2A_QueryTask, A2A_DownloadArtifacts).
```

## Using Agents in Chat

Once configured, you can ask the AI to delegate tasks to specific agents:

```text
User: Please have the code-reviewer agent review my latest commit
```

The AI will use the A2A tools to delegate this task to the configured code-reviewer agent.

## Local Agent Execution (Docker)

When `run: true` is set, the CLI expects the agent to be available as a Docker container. The container
lifecycle is managed externally - the CLI only communicates with the agent via HTTP.

**Requirements for local agents:**

- Docker must be installed and running
- The OCI image must be pulled or available locally
- The agent must expose an HTTP endpoint (specified in `url`)
- Environment variables are passed to the container

**Note:** The current implementation focuses on configuration. Full Docker lifecycle management (pull, run,
stop) will be added in future versions. For now, you must manually start local agents:

```bash
docker run -d -p 8081:8080 \
  -e API_KEY=secret \
  -e MODEL=gpt-4 \
  ghcr.io/org/test-runner:latest
```

## Best Practices

1. **Use descriptive names**: Choose names that clearly indicate the agent's purpose (e.g., `code-reviewer`, `test-runner`)
2. **Project vs Userspace**: Use project-level configuration for project-specific agents, userspace for general-purpose agents
3. **Environment variables**: Always use environment variable substitution (`$VAR` or `${VAR}`) for sensitive
   data like API keys instead of hardcoding values
4. **Use .env files**: Store sensitive environment variables in a `.env` file and add it to `.gitignore`
5. **Explicit environment listing**: Only list the environment variables each agent actually needs in the `environment` section
6. **Version OCI images**: Always specify a tag (not `latest`) for reproducible builds
7. **Document custom agents**: Add comments or external documentation for custom agent configurations
8. **Security**: Never commit `.env` files or `agents.yaml` files with hardcoded secrets to version control

## Troubleshooting

### Agents not appearing in system prompt

- Ensure `agents.yaml` exists in `.infer/` or `~/.infer/`
- Run `infer agents list` to verify configuration
- Check that A2A is enabled: `a2a.enabled: true` in `config.yaml`

### Cannot add agent (duplicate name)

Each agent must have a unique name. Remove the existing agent first or choose a different name:

```bash
infer agents remove old-name
infer agents add new-name https://agent.url
```

### Local agent not connecting

- Verify Docker container is running: `docker ps`
- Check container logs: `docker logs <container-id>`
- Ensure port mappings are correct
- Verify the URL matches the exposed port

### Environment variables not being injected

- Ensure the variable is listed in the `environment` section of `agents.yaml`
- Check that `.env` file exists in the current working directory
- Run with `--verbose` flag to see debug logs about environment variable resolution
- Verify `.env` file format (should be `KEY=value` on each line)
- Check that variable names match exactly (case-sensitive)

## Related Documentation

- [A2A Tools Documentation](../README.md#a2a-tools-agent-to-agent-communication)
- [Configuration Guide](../README.md#configuration)
- [Agent Architecture](../AGENTS.md)
