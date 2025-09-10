# A2A Direct Connections

This document describes the Agent-to-Agent (A2A) direct connection functionality that allows the CLI to
connect directly to A2A server agents, bypassing the Gateway for efficient microservice communication.

## Overview

The A2A direct connection feature enables:

- Direct communication between the CLI client and A2A server agents
- Background task submission and asynchronous result collection
- Multi-agent architecture support
- Microservice-style communication patterns
- Task status monitoring and input-required handling

## Architecture

### Traditional Architecture (Gateway-based)

```text
CLI Client → Gateway (A2A middleware) → A2A Agents
```

### New Direct Architecture

```text
CLI Client ←─┐
             ├─→ A2A Agent 1 (Direct connection)
             ├─→ A2A Agent 2 (Direct connection)
             └─→ Gateway (For internal LLM requests)
```

## Configuration

### Enabling A2A Direct Connections

Add the following to your `.infer/config.yaml`:

```yaml
a2a:
  enabled: true
  agents:
    code-review-agent:
      name: "Code Review Agent"
      url: "http://code-review.internal:8081"
      description: "Performs automated code reviews"
      timeout: 60
      enabled: true
      metadata:
        team: "platform"
        version: "v1.2.0"

    deployment-agent:
      name: "Deployment Agent"
      url: "http://deploy.internal:8082"
      description: "Handles deployment tasks"
      timeout: 120
      enabled: true
      metadata:
        environment: "production"
        region: "us-west-2"

  tasks:
    max_concurrent: 3
    timeout_seconds: 300
    retry_count: 2
    status_poll_seconds: 5
```

## Usage

### Using the A2A Task Tool

The A2A functionality is exposed through the `a2a_task` tool that can be used in conversations:

#### Submit a Task

```text
Submit a code review task to the code-review-agent for the current pull request
```

The LLM will use the `a2a_task` tool:

```json
{
  "operation": "submit",
  "agent_name": "code-review-agent",
  "task_type": "code_review",
  "task_description": "Review pull request #123 for security and best practices",
  "parameters": {
    "pull_request_id": "123",
    "focus_areas": ["security", "performance", "best_practices"]
  }
}
```

#### Check Task Status

```text
Check the status of task-abc-123
```

```json
{
  "operation": "status",
  "task_id": "task-abc-123"
}
```

#### Collect Results

```text
Collect the results from task-abc-123
```

```json
{
  "operation": "collect",
  "task_id": "task-abc-123"
}
```

#### List Available Agents

```text
Show me all available A2A agents
```

```json
{
  "operation": "list_agents"
}
```

#### Test Connection

```text
Test connection to the deployment-agent
```

```json
{
  "operation": "test_connection",
  "agent_name": "deployment-agent"
}
```

### Chat History Integration

A2A task operations are fully integrated into the chat history:

1. **Task Submission**: Shows when a task is submitted to an agent
2. **Status Updates**: Displays progress and status changes
3. **Results**: Shows completed task results in the conversation
4. **Input Required**: Prompts user when agent needs additional input

## A2A Agent API

### Agent Requirements

A2A agents must implement the following endpoints:

#### Health Check

```http
GET /api/v1/health
```

Returns agent health status.

#### Submit Task

```http
POST /api/v1/tasks
Content-Type: application/json
Authorization: Bearer {api_key}

{
  "id": "task-123",
  "type": "code_review",
  "description": "Review pull request",
  "parameters": {...},
  "priority": 5,
  "timeout": 300
}
```

#### Get Task Status

```http
GET /api/v1/tasks/{task_id}/status
Authorization: Bearer {api_key}
```

Response:

```json
{
  "task_id": "task-123",
  "status": "running",
  "progress": 75.0,
  "message": "Processing files...",
  "created_at": "2024-01-15T10:00:00Z",
  "updated_at": "2024-01-15T10:05:00Z",
  "input_request": null
}
```

#### Get Task Result

```http
GET /api/v1/tasks/{task_id}/result
Authorization: Bearer {api_key}
```

#### Cancel Task

```http
POST /api/v1/tasks/{task_id}/cancel
Authorization: Bearer {api_key}
```

### Task Status Values

- `pending`: Task is queued but not started
- `running`: Task is currently executing
- `completed`: Task finished successfully
- `failed`: Task failed with an error
- `input_required`: Task needs user input to continue
- `cancelled`: Task was cancelled by user

## Input Required Handling

When an agent needs user input, it sets status to `input_required` with an `input_request`:

```json
{
  "task_id": "task-123",
  "status": "input_required",
  "input_request": {
    "type": "choice",
    "message": "Which deployment environment?",
    "options": ["staging", "production"],
    "required": true,
    "default": "staging"
  }
}
```

The CLI will prompt the user and can submit the input back to the agent.

## Security Considerations

### Authentication

- Each agent connection requires an API key
- Keys should be stored as environment variables
- Use secure key rotation practices

### Network Security

- A2A agents should be deployed in secure network segments
- Use network policies to restrict agent-to-agent communication
- Consider mutual TLS for production deployments

### Path Protection

- A2A agents have restricted file system access
- Sandbox directories prevent unauthorized file access
- Protected paths include `.git/`, `.infer/`, `*.env`

## Monitoring and Logging

### Task Tracking

The CLI tracks all active A2A tasks:

- Task submission times and agents
- Status updates and progress
- Completion times and results
- Error conditions and retry attempts

### Debugging

Enable debug logging:

```bash
INFER_LOGGING_DEBUG=true infer chat
```

Check the logs:

```bash
tail -f .infer/logs/debug-*.log
```

## Error Handling

### Connection Failures

- Automatic retry with exponential backoff
- Configurable retry count and timeout
- Graceful degradation when agents unavailable

### Task Failures

- Task-level error reporting
- Failed task cleanup and resource management
- User notification of failures with actionable messages

## Best Practices

### Agent Configuration

1. **Descriptive Names**: Use clear, descriptive agent names
2. **Appropriate Timeouts**: Set timeouts based on expected task duration
3. **Metadata**: Include useful metadata for monitoring and debugging
4. **Health Checks**: Implement robust health check endpoints

### Task Design

1. **Idempotent Tasks**: Design tasks to be safely retried
2. **Progress Reporting**: Provide meaningful progress updates
3. **Error Messages**: Return clear, actionable error messages
4. **Resource Cleanup**: Ensure tasks clean up resources on completion

### Security

1. **API Key Management**: Use secure key storage and rotation
2. **Input Validation**: Validate all task parameters
3. **Access Control**: Implement proper authorization
4. **Audit Logging**: Log all task operations for security monitoring

## Troubleshooting

### Common Issues

#### Agent Not Found

```text
Error: agent 'my-agent' not found in configuration
```

Solution: Check agent name in config and ensure it's enabled.

#### Connection Refused

```text
Error: connection to agent 'my-agent' failed: connection refused
```

Solution: Verify agent URL and ensure agent is running.

#### Authentication Failed

```text
Error: failed to submit task: 401 Unauthorized
```

Solution: Check API key configuration and environment variables.

#### Task Timeout

```text
Error: task execution timeout after 300 seconds
```

Solution: Increase timeout in agent configuration or task parameters.

### Diagnostic Commands

Test agent connectivity:

```bash
infer chat
> Test connection to my-agent
```

List configured agents:

```bash
infer chat
> Show all A2A agents
```

Check active tasks:

```bash
infer chat
> List all running A2A tasks
```
