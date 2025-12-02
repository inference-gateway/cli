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

### Using the /a2a Shortcut

The `/a2a` shortcut provides command-line interface for managing A2A agent configurations:

#### List A2A Servers

```bash
/a2a
/a2a list
```

This opens the A2A servers view showing:

- Gateway URL configuration
- A2A middleware status (enabled/disabled)
- API key configuration status
- Connection timeout settings
- Configured agents and their status

#### Add an Agent

```bash
/a2a add my-agent http://localhost:8081 --run --model openai/gpt-4
```

Options:

- `--oci IMAGE`: Specify OCI container image
- `--artifacts-url URL`: Artifacts download URL
- `--run`: Run the agent locally
- `--model MODEL`: Model to use for the agent
- `--environment KEY=VALUE`: Environment variables

#### Remove an Agent

```bash
/a2a remove my-agent
```

### Using the A2A Tools

The A2A functionality is exposed through multiple tools that can be used in conversations:

#### A2A_SubmitTask Tool - Submit a Task

The `A2A_SubmitTask` tool submits tasks to A2A agents:

```text
Submit a task to analyze this code
```

The LLM will use the `A2A_SubmitTask` tool:

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

#### A2A_QueryAgent Tool - Get Agent Information

The `A2A_QueryAgent` tool gets information from A2A agents:

```text
Query the agent at localhost:8081 for its capabilities
```

```json
{
  "agent_url": "http://localhost:8081"
}
```

#### A2A_QueryTask Tool - Query Task Status

The `A2A_QueryTask` tool queries the status and result of a specific A2A task:

```text
Check the status of task task-456 from the agent at http://localhost:8081
```

```json
{
  "agent_url": "http://localhost:8081",
  "context_id": "context-123",
  "task_id": "task-456"
}
```

**Important:** When you submit a task via `A2A_SubmitTask`, it automatically monitors the task in the
background. Only use `A2A_QueryTask` to:

1. Check tasks from previous conversations
2. Check tasks submitted outside this session
3. Get detailed results AFTER you receive a completion notification

### Tool Implementation Details

#### A2A_SubmitTask Tool

- **Name**: `A2A_SubmitTask`
- **Parameters**:
  - `agent_url` (required): URL of the A2A agent
  - `task_description` (required): Description of the task to perform
  - `metadata` (optional): Additional task metadata as key-value pairs
- **Returns**: Task result with ID, status, and response content
- **Behavior**: Submits task and waits for streaming completion

#### A2A_QueryAgent Tool

- **Name**: `A2A_QueryAgent`
- **Parameters**:
  - `agent_url` (required): URL of the A2A agent to query
- **Returns**: Agent card information with capabilities and configuration
- **Behavior**: Retrieves agent metadata for discovery and validation

#### A2A_QueryTask Tool

- **Name**: `A2A_QueryTask`
- **Parameters**:
  - `agent_url` (required): URL of the A2A agent server
  - `context_id` (required): Context ID for the task
  - `task_id` (required): ID of the task to query
- **Returns**: Complete task object including status, artifacts, and message data
- **Behavior**: Queries task status and returns detailed information. Cannot be used while background
  polling is active for the same agent.

#### A2A_DownloadArtifacts Tool - Download Task Artifacts

The `A2A_DownloadArtifacts` tool downloads artifacts from completed A2A tasks:

```text
Download artifacts from the completed task
```

The LLM will use the `A2A_DownloadArtifacts` tool:

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

### A2A Tool Configuration

**Note**: The `/a2a` shortcut is used for **agent configuration management**,
while the A2A tools below are used for **runtime interaction** with configured agents.

A2A tools are configured in the `a2a.tools` section of your configuration:

```yaml
a2a:
  enabled: true  # Enable A2A functionality
  cache:
    enabled: true  # Enable agent card caching
    ttl: 300       # Cache TTL in seconds
  task:
    status_poll_seconds: 5       # Background task polling interval
    polling_strategy: "exponential"  # Polling strategy
    initial_poll_interval_sec: 2     # Initial poll interval
    max_poll_interval_sec: 60        # Maximum poll interval
    backoff_multiplier: 2.0          # Backoff multiplier
    background_monitoring: true      # Enable background monitoring
    completed_task_retention: 5      # Number of completed tasks to retain
  tools:
    query_agent:
      enabled: true         # Enable A2A_QueryAgent tool
      require_approval: false  # Whether approval is required
    query_task:
      enabled: true         # Enable A2A_QueryTask tool
      require_approval: false  # Whether approval is required
    submit_task:
      enabled: true         # Enable A2A_SubmitTask tool
      require_approval: true   # Whether approval is required
    download_artifacts:
      enabled: true         # Enable A2A_DownloadArtifacts tool
      download_dir: "/tmp/downloads"  # Directory for downloaded artifacts
      timeout_seconds: 30   # Download timeout in seconds
      require_approval: true   # Whether approval is required
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

### Agent Configuration with /a2a Shortcut

```bash
# First, configure an agent using the /a2a shortcut
/a2a add code-reviewer http://localhost:8081 --run --model openai/gpt-4 --environment GITHUB_TOKEN=xxx

# List configured agents
/a2a list
```

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

1. Use A2A_QueryTask to verify the task is completed
2. Use A2A_DownloadArtifacts to retrieve any generated files, documents, or other outputs from the task
