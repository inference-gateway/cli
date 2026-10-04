# A2A Connections

This document describes the Agent-to-Agent (A2A) connection functionality that allows the CLI to
connect to A2A server agents using the ADK (Agent Development Kit) client.

## Overview

The A2A connection feature enables:

- Communication between the CLI client and A2A server agents via URL
- Task submission that returns immediately and polls the remote task in the background
- Agent querying for server information
- Simple agent-to-agent communication patterns

## Architecture

### Current Architecture

```text
CLI Client → A2A Agent (Connection via URL)
```

The CLI connects to A2A agents using their URL endpoints through the ADK client library.

## Usage

### Managing Agent Configuration

Use the `infer agents` commands to manage A2A agent configurations:

#### List A2A Agents

```bash
infer agents list
```

`infer agents list` shows the A2A agents in their own table - agent name, source (agents.yaml or `INFER_A2A_AGENTS`),
URL endpoint, OCI container image, run-locally status, model and environment variables - followed by a second table
with the Markdown-defined subagent presets.

#### Add an Agent

```bash
infer agents add my-agent http://localhost:8081 --run --model openai/gpt-4
```

Options:

- `--oci IMAGE`: Specify OCI container image
- `--artifacts-url URL`: Artifacts download URL
- `--run`: Run the agent locally
- `--model MODEL`: Model to use for the agent
- `--environment KEY=VALUE`: Environment variables
- `--tag TAG`: Replace the tag of the agent's default image (known agents only, mutually exclusive with `--oci`)

#### Remove an Agent

```bash
infer agents remove my-agent
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

#### Background Task Visualisation

While a remote A2A task is running in the background, the CLI shows a live,
sticky list of rows pinned **right below the input box**. It is always visible
regardless of where you've scrolled the conversation, so you never lose sight of
in-flight delegations.

A typical list looks like:

```text
… conversation viewport (scrollable) …
> _   (input)
─────────────────────────────────────────────
┌ weather.example.com a2a external     12s
└ calendar-agent:8080 a2a local ✓  1.2s
```

The rows update in place as the task progresses through its lifecycle:
`submitted` → `working` → `completed` / `failed` / `cancelled`. A task in
`input-required` is paused rather than finished, so it keeps its row and resumes
when the agent is sent input.

When the remote task reports its `metadata` (ADK ≥ 0.19.0 agents with
`EnableUsageMetadata` enabled, the default), the finished row gains one child
line with the tool calls that succeeded and failed and the tokens consumed:

```text
└ 2 ✓ 0 ✗ · 950 tokens
```

The child line is omitted when the remote agent emits no such metadata, for
older ADK versions for example. A failed task shows the cross icon in place of
the check mark and no further detail, with the error available through
`A2A_QueryTask`.

A row lingers for `chat.status_bar.subagent_linger_seconds` (default 5) after the
task reaches a terminal state (`completed`, `failed`, or `cancelled`), keeping the
list tidy while still giving you time to glance at the outcome.

Multiple concurrent tasks each get their own row, sorted newest first by start
time, so a single assistant turn that submits several A2A tasks shows independent
progress rows side-by-side. At most five rows are drawn and the overflow
collapses into a `+N more` row.

Notes:

- The rows are purely a UI element - they are not persisted with the
  conversation. Reloading a session will not bring back rows for
  tasks that have already completed.
- The list follows `chat.status_bar.indicators.subagents`, so it can be switched
  off like every other status-bar element.
- The error text of a failed task is not shown in the row. Use
  `A2A_QueryTask` or the logs for the reason.
- For a full historical record of past tasks, use `A2A_QueryTask` or the
  in-session task management view.

### Tool Implementation Details

#### A2A_SubmitTask Tool

- **Name**: `A2A_SubmitTask`
- **Parameters**:
  - `agent_url` (required): URL of the A2A agent
  - `task_description` (required): Description of the task to perform
  - `context_id` (optional): Context ID from an earlier task to continue that conversation with the agent; omitting it starts an independent task
  - `tenant` (optional): The agent to address behind a gateway that fronts several agents, taken from the `tenant` of an
    interface on the gateway's agent card. See the [a2a-gateway example](../examples/a2a-gateway/)
- **Returns**: Task result with ID, status, and response content
- **Behavior**: Sends the message with `returnImmediately: true`, returns the task ID as soon as the agent creates the task, and polls the
  remote task in the background

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
- **Behavior**: Queries task status and returns detailed information. Refused while the named task is
  already being polled in the background.

## A2A Integration

### A2A Tool Configuration

**Note**: The `infer agents` commands are used for **agent configuration management**,
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
    completed_task_retention: 5      # Number of completed tasks to retain
    agent_mode_max_wait_seconds: 300 # Longest an agent-mode turn waits for a task
    artifacts_auto_download: false   # Download finished tasks' artifacts automatically
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

Check the logs (the logger writes one `app-<date>.log` file per day, or `daemon-<date>.log` under `infer daemon`):

```bash
tail -f ~/.infer/logs/app-*.log
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
3. **Submission Errors**: Issues while sending a task or polling its result

### Error Messages

Tools provide descriptive error messages:

- "A2A connections are disabled in configuration"
- "agent_url parameter is required and must be a string"
- "A2A task submission failed: [specific error]"

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
- Background task polling
- Task completion status
- Error stack traces

## Examples

### Agent Configuration with the infer agents CLI

```bash
# First, configure an agent with the CLI
infer agents add code-reviewer http://localhost:8081 --run --model openai/gpt-4 --environment GITHUB_TOKEN=xxx

# List configured agents
infer agents list
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

With `a2a.task.artifacts_auto_download: true` (the default is `false`), the CLI
downloads a finished task's artifacts for you into the session artifacts
directory `~/.infer/projects/<project-slug>/artifacts/<session-id>/` and the
completion notification names each saved path.

With the option off, the model fetches the artifact URLs itself:

```text
Use WebFetch to download http://localhost:8081/artifacts/task-456/report.pdf
```

WebFetch saves the file and returns the local file path it chose.
