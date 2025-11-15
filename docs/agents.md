# A2A Agents Configuration

This document describes how to configure and manage Agent-to-Agent (A2A) agents using the new `agents.yaml` configuration system.

## Overview

The Inference Gateway CLI now supports a separate configuration file for A2A agents, making it easy to manage multiple agents with rich configuration options including local Docker execution.

## Configuration Files

### agents.yaml Structure

Agents are configured in `.infer/agents.yaml` (project-specific) or `~/.infer/agents.yaml` (user-global):

```yaml
agents:
  - name: "code-assistant"
    url: "http://localhost:8080"
    oci: "my-registry/code-assistant:latest"
    run: true
    description: "Code analysis and generation assistant"
    enabled: true
    environment:
      API_KEY: "your-api-key"
      DEBUG: "true"
      LOG_LEVEL: "info"
    docker:
      port: 8080
      host_port: 8080
      volumes:
        - "/local/path:/container/path"
      network_mode: "bridge"
      restart_policy: "unless-stopped"
      health_check:
        test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
        interval: 30s
        timeout: 10s
        retries: 3
        start_period: 40s

  - name: "external-agent"
    url: "https://agent.example.com"
    description: "External agent service"
    enabled: true
    run: false
    metadata:
      provider: "external"
      region: "us-east-1"
```

### Configuration Fields

#### Required Fields
- `name`: Unique identifier for the agent
- `url`: Endpoint where the agent is accessible

#### Optional Fields
- `oci`: Docker image for local execution
- `run`: Whether to run the agent locally with Docker (default: false)
- `description`: Brief description of the agent's capabilities
- `enabled`: Whether the agent is currently active (default: true)
- `environment`: Environment variables for Docker execution
- `metadata`: Additional key-value metadata
- `docker`: Docker-specific configuration (see below)

#### Docker Configuration
- `image`: Docker image (defaults to `oci` if not specified)
- `port`: Port to expose inside the container
- `host_port`: Host port to bind to (defaults to `port`)
- `volumes`: Volume mounts in `host:container` format
- `network_mode`: Docker network mode
- `restart_policy`: Container restart policy
- `health_check`: Health check configuration

## CLI Commands

### Adding Agents

#### Add an External Agent
```bash
infer agents add my-agent --url https://agent.example.com --description "External agent service"
```

#### Add a Local Docker Agent
```bash
infer agents add code-assistant \
  --url http://localhost:8080 \
  --oci my-registry/code-assistant:latest \
  --run \
  --env API_KEY=secret \
  --env DEBUG=true \
  --port 8080 \
  --volume "/local/code:/workspace" \
  --description "Local code assistant"
```

#### Add with Docker Configuration
```bash
infer agents add data-processor \
  --url http://localhost:9000 \
  --oci data-processor:latest \
  --run \
  --port 9000 \
  --host-port 9000 \
  --network host \
  --restart unless-stopped \
  --env DATABASE_URL=postgres://user:pass@localhost/db
```

### Managing Agents

#### List All Agents
```bash
# Basic list
infer agents list

# With status information
infer agents list --status

# JSON output
infer agents list --json
```

#### Remove an Agent
```bash
infer agents remove my-agent
```

### Running Local Agents

#### Start a Local Agent
```bash
infer agents start code-assistant
```

#### Stop a Running Agent
```bash
infer agents stop code-assistant
```

#### Check Agent Status
```bash
infer agents status code-assistant
```

## Integration with System Prompt

Configured agents are automatically injected into the system prompt when using the CLI. The system prompt will include:

```
AVAILABLE A2A AGENTS:
- code-assistant (http://localhost:8080) (running locally) - Local code assistant
- external-agent (https://agent.example.com) - External agent service
```

This allows the AI to know which agents are available and how to reach them.

## Docker Requirements

To use local agent execution with Docker:

1. **Docker Installation**: Ensure Docker is installed and running
2. **Image Availability**: Make sure the Docker image exists locally or in an accessible registry
3. **Port Availability**: Ensure the specified ports are not in use
4. **Permissions**: User must have Docker permissions

### Checking Docker Availability

The CLI automatically detects Docker availability. You can verify manually:

```bash
docker --version
docker info
```

## Migration from Legacy Configuration

The new agents.yaml system works alongside the legacy A2A configuration in `config.yaml`. Both systems are supported:

### Legacy Configuration (config.yaml)
```yaml
a2a:
  enabled: true
  agents:
    - "http://localhost:8080"
    - "https://agent.example.com"
```

### New Configuration (agents.yaml)
```yaml
agents:
  - name: "local-agent"
    url: "http://localhost:8080"
    run: true
    oci: "my-agent:latest"
  - name: "external-agent"
    url: "https://agent.example.com"
```

When both are present, both types of agents will be shown in the system prompt.

## Example Workflows

### Setting up a Development Environment

1. **Add a local code assistant:**
```bash
infer agents add code-assistant \
  --url http://localhost:8080 \
  --oci codeassistant:latest \
  --run \
  --env API_KEY=your-key \
  --port 8080 \
  --volume "$PWD:/workspace" \
  --description "Local code analysis agent"
```

2. **Start the agent:**
```bash
infer agents start code-assistant
```

3. **Verify it's running:**
```bash
infer agents status code-assistant
```

4. **Use in chat:**
```bash
infer chat
# The agent will be available in the system prompt
```

### Managing Multiple Environments

Create different agent configurations for different projects by using project-specific `.infer/agents.yaml` files:

```bash
# In project A
cd /path/to/project-a
infer agents add project-a-agent --url http://localhost:8080 --run --oci project-a:latest

# In project B  
cd /path/to/project-b
infer agents add project-b-agent --url http://localhost:8081 --run --oci project-b:latest
```

## Troubleshooting

### Common Issues

1. **Docker not available:**
   - Ensure Docker is installed and running
   - Check user permissions for Docker

2. **Port conflicts:**
   - Use different host ports for multiple agents
   - Check for existing services on the port

3. **Image not found:**
   - Verify the Docker image exists
   - Pull the image manually: `docker pull <image>`

4. **Agent not responding:**
   - Check container logs: `docker logs <container-id>`
   - Verify health check configuration
   - Check network connectivity

### Debugging Commands

```bash
# Check Docker status
docker ps

# View container logs
docker logs infer-agent-<name>

# Check agent status
infer agents status <name>

# List all agents with status
infer agents list --status
```

## Best Practices

1. **Use descriptive names** for agents to make them easy to identify
2. **Set appropriate resource limits** in Docker configuration
3. **Use health checks** for production agents
4. **Keep environment variables secure** - avoid hardcoding secrets
5. **Use project-specific configurations** when working on multiple projects
6. **Regularly update Docker images** for security and features
7. **Monitor agent performance** and resource usage

## Security Considerations

- **Environment Variables**: Sensitive data in environment variables is visible in container inspect
- **Volume Mounts**: Be careful with volume mounts to avoid exposing sensitive files
- **Network Access**: Consider network isolation for production environments
- **Image Security**: Use trusted base images and keep them updated
- **Resource Limits**: Set appropriate CPU and memory limits to prevent resource exhaustion