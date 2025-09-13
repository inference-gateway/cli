# A2A Direct Connections

This document describes the Agent-to-Agent (A2A) direct connection functionality that allows the CLI to
connect directly to A2A server agents using the ADK (Agent Development Kit) client.

## Overview

The A2A direct connection feature enables:

- Direct communication between the CLI client and A2A server agents via URL
- Task submission with streaming responses
- Agent querying for server information
- Simple agent-to-agent communication patterns

## Architecture

### Current Architecture

```text
CLI Client → A2A Agent (Direct connection via URL)
```

The CLI connects directly to A2A agents using their URL endpoints through the ADK client library.

## Configuration

### Enabling A2A Direct Connections

Add the following to your `.infer/config.yaml`:

```yaml
a2a:
  enabled: true
```

## Usage

### Using the A2A Tools

The A2A functionality is exposed through two tools that can be used in conversations:

#### Task Tool - Submit a Task

The `Task` tool submits tasks directly to A2A agents:

```text
Submit a task to analyze this code
```

The LLM will use the `Task` tool:

```json
{
  "agent_url": "http://localhost:8081",
  "task_description": "Analyze the code in the current repository for potential security issues"
}
```

Optional metadata can be included:

```json
{
  "agent_url": "http://localhost:8081",
  "task_description": "Review pull request for best practices",
  "metadata": {
    "pull_request_id": "123",
    "focus_areas": ["security", "performance"]
  }
}
```

#### Query Tool - Get Agent Information

The `Query` tool gets information from A2A agents:

```text
Query the agent at localhost:8081 for its capabilities
```

```json
{
  "agent_url": "http://localhost:8081"
}
```

### Tool Implementation Details

#### Task Tool

- **Name**: `Task`
- **Parameters**:
  - `agent_url` (required): URL of the A2A agent
  - `task_description` (required): Description of the task to perform
  - `metadata` (optional): Additional task metadata as key-value pairs
- **Returns**: Task result with ID, status, and response content
- **Behavior**: Submits task and waits for streaming completion

#### Query Tool

- **Name**: `Query`
- **Parameters**:
  - `agent_url` (required): URL of the A2A agent to query
- **Returns**: Agent card information
- **Behavior**: Currently returns placeholder response (TODO: implement actual query logic)

## A2A Integration

### Shortcut Command

Use the `/a2a` shortcut to view A2A server status:

```bash
/a2a
/a2a list
```

This opens the A2A servers view showing:

- Gateway URL configuration
- A2A middleware status (enabled/disabled)
- API key configuration status
- Connection timeout settings

### Middleware Configuration

> **⚠️ Deprecation Notice**: The A2A middleware configuration will be deprecated in a future release.
> Direct client execution will become the default behavior.

A2A tools can be configured to execute on the Gateway instead of the client:

```yaml
gateway:
  middlewares:
    a2a: true  # Tools execute on Gateway (default, deprecated)
    # a2a: false  # Tools execute on client (future default)
```

When `gateway.middlewares.a2a` is `true`:

- In chat mode: Tools are visualized but execution is skipped (handled by Gateway)
- In agent mode: Tools execute normally on the client

## Security Considerations

### Configuration Validation

- A2A tools are only enabled when `a2a.enabled: true` in configuration
- Tools validate required parameters before execution
- Invalid configurations result in clear error messages

### Network Security

- Direct connections require proper URL validation
- Consider using HTTPS for production agent URLs
- Implement proper timeout handling for network requests

## Monitoring and Logging

### Debug Logging

Enable debug logging to monitor A2A operations:

```bash
INFER_LOGGING_DEBUG=true infer chat
```

Check the logs:

```bash
tail -f .infer/logs/debug-*.log
```

### Task Tracking

The CLI logs:

- Task submissions with agent URLs
- Task IDs and completion status
- Duration and event counts
- Error conditions and failures

## Error Handling

### Common Error Conditions

1. **A2A Disabled**: When `a2a.enabled: false` in configuration
2. **Invalid Parameters**: Missing or invalid `agent_url` or `task_description`
3. **Connection Failures**: Network timeouts or unreachable agents
4. **Streaming Errors**: Issues with ADK client streaming

### Error Messages

Tools provide descriptive error messages:

- "A2A direct connections are disabled in configuration"
- "agent_url parameter is required and must be a string"
- "Streaming failed: [specific error]"

## Troubleshooting

### Configuration Issues

Check A2A configuration:

```yaml
a2a:
  enabled: true
```

### Connection Testing

Test agent connectivity using the Task tool with a simple description:

```text
Test connection to agent at http://localhost:8081
```

### Debug Information

Enable verbose logging and check for:

- ADK client connection attempts
- Streaming event processing
- Task completion status
- Error stack traces

## Examples

### Code Review Task

```text
Submit a code review task to the agent at http://localhost:8081 for the current pull request
```

### Security Analysis

```text
Ask the security agent at http://localhost:8082 to analyze this codebase for vulnerabilities
```

### Agent Capability Query

```text
Query the documentation agent at http://localhost:8083 for its available features
```
