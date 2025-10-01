# A2A Connections

This document describes the Agent-to-Agent (A2A) connection functionality that allows the CLI to
connect to A2A server agents using the ADK (Agent Development Kit) client.

## Overview

The A2A connection feature enables:

- Communication between the CLI client and A2A server agents via URL
- Task submission with streaming responses
- Agent querying for server information
- Simple agent-to-agent communication patterns

## Architecture

### Current Architecture

```text
CLI Client → A2A Agent (Connection via URL)
```

The CLI connects to A2A agents using their URL endpoints through the ADK client library.

## Usage

### Using the A2A Tools

The A2A functionality is exposed through multiple tools that can be used in conversations:

#### SubmitTask Tool - Submit a Task

The `SubmitTask` tool submits tasks to A2A agents:

```text
Submit a task to analyze this code
```

The LLM will use the `SubmitTask` tool:

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

#### SubmitTask Tool

- **Name**: `SubmitTask`
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

#### DownloadArtifacts Tool - Download Task Artifacts

The `DownloadArtifacts` tool downloads artifacts from completed A2A tasks:

```text
Download artifacts from the completed task
```

The LLM will use the `DownloadArtifacts` tool:

```json
{
  "agent_url": "http://localhost:8081",
  "context_id": "context-123",
  "task_id": "task-456"
}
```

**Important Requirements:**
- The task must be in "completed" status before artifacts can be downloaded
- The agent must first use the QueryTask tool to verify completion status
- Only works with tasks that have generated artifacts

Tool Details:
- **Name**: `A2A_DownloadArtifacts`
- **Parameters**:
  - `agent_url` (required): URL of the A2A agent server
  - `context_id` (required): Context ID for the task
  - `task_id` (required): ID of the completed task to download artifacts from
- **Returns**: List of artifacts with metadata and content
- **Behavior**: Validates task completion status, then downloads available artifacts

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

### A2A Tool Configuration

A2A tools are configured in the tools section:

```yaml
a2a:
  tools:
    submit_task:
      enabled: true  # Enable A2A SubmitTask tool
    query_task:
      enabled: true  # Enable A2A QueryTask tool
    download_artifacts:
      enabled: true  # Enable A2A DownloadArtifacts tool
```

## Security Considerations

### Configuration Validation

- Tools validate required parameters before execution
- Invalid configurations result in clear error messages

### Network Security

- A2A connections require proper URL validation
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

1. **Invalid Parameters**: Missing or invalid `agent_url` or `task_description`
2. **Connection Failures**: Network timeouts or unreachable agents
3. **Streaming Errors**: Issues with ADK client streaming

### Error Messages

Tools provide descriptive error messages:

- "A2A connections are disabled in configuration"
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

Test agent connectivity using the SubmitTask tool with a simple description:

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

### Artifact Download

```text
First check if task task-456 is completed, then download its artifacts from the agent at http://localhost:8081
```

This will:
1. Use QueryTask to verify the task is completed
2. Use DownloadArtifacts to retrieve any generated files, documents, or other outputs from the task
