# A2A Traces Example

This example demonstrates W3C trace context propagation between the `infer` CLI
and A2A agents. When the CLI delegates a task to a remote A2A agent (via
`A2A_SubmitTask`), the trace context flows through HTTP headers
(`traceparent`, `tracestate`, `baggage`) so the remote agent can create child
spans that parent under the CLI's `execute_tool` span.

With ADK 0.22.0+, the mock-agent natively receives and propagates these trace
headers, and can export its own spans via standard OTel environment variables.

## Prerequisites

1. Configure the Inference Gateway server:

```bash
cp .env.example .env
# Edit .env with your API keys
```

2. Start the services:

```bash
docker compose up -d
```

3. Verify everything is running:

```bash
docker compose ps
docker compose logs -f
```

## How It Works

### Trace Context Propagation

The CLI injects W3C trace context into every outgoing A2A HTTP request:

- **`traceparent`**: The current span's trace ID, span ID, and trace flags
- **`tracestate`**: Vendor-specific trace data
- **`baggage`**: Application context (`infer.session.id`, `infer.tool.call.id`)

The mock-agent (ADK 0.22.0+) receives these headers and can create child spans
that parent under the CLI's `execute_tool` span, forming a single distributed
trace across process boundaries.

### Viewing Traces

The CLI records its own spans locally (no OTLP collector required). After
interacting with the mock-agent, view the span tree:

```bash
# List sessions with trace data
docker compose run --rm cli infer traces --list

# View the most recent session's span tree
docker compose run --rm cli infer traces

# View a specific session
docker compose run --rm cli infer traces <session-id>

# JSON output for programmatic use
docker compose run --rm cli infer traces --format json
```

The span tree shows the full call chain:

```text
session (standard, success)            152ms
|-- chat openai/gpt-4o                   5ms
|-- execute_tool A2A_SubmitTask         89ms
|   `-- session (readonly, success)     52ms
|       |-- chat openai/gpt-4o           5ms
|       `-- chat openai/gpt-4o          13ms
`-- chat openai/gpt-4o                  12ms
```

### Mock-Agent OTel Export

The mock-agent can export its own spans to a shared OTLP collector. Configure
it via standard OTel environment variables:

```yaml
OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
OTEL_EXPORTER_OTLP_PROTOCOL: http/protobuf
OTEL_SERVICE_NAME: mock-agent
```

When no collector is configured, the mock-agent writes spans to stdout (visible
in `docker compose logs mock-agent`).

## Configuration

### CLI Environment

```yaml
INFER_GATEWAY_URL: http://inference-gateway:8080
INFER_A2A_ENABLED: true
INFER_AGENT_MODEL: deepseek/deepseek-v4-pro
INFER_TELEMETRY_ENABLED: true
```

`INFER_TELEMETRY_ENABLED: true` enables local trace recording. The CLI stores
spans in `<config-dir>/telemetry/<session-id>-traces.jsonl` and exposes them
via `infer traces`.

### Mock-Agent Environment

```yaml
A2A_AGENT_URL: http://mock-agent:8080
A2A_AGENT_CLIENT_PROVIDER: ""
A2A_AGENT_CLIENT_MODEL: ""
A2A_AGENT_CLIENT_BASE_URL: ""
```

The mock-agent runs without an LLM backend (it responds with canned data), so
no API keys are needed. With ADK 0.22.0+, it automatically reads incoming
`traceparent` headers and creates child spans.

## Usage

### 1. Start the services

```bash
docker compose up -d
```

### 2. Enter the CLI container

```bash
docker compose run --rm cli
```

### 3. Query the mock-agent

Inside the CLI, ask the model to query the mock-agent:

```text
Can you ask the mock-agent at http://mock-agent:8080 what it can do?
```

The model will use `A2A_QueryAgent` to fetch the agent card. The trace context
flows from the CLI to the mock-agent via HTTP headers.

### 4. Delegate a task

```text
Ask the mock-agent to summarize the current project.
```

The model will use `A2A_SubmitTask` to delegate the task. The trace context
flows with the request, and the mock-agent's spans (if it exports them) parent
under the CLI's `execute_tool` span.

### 5. View the traces

Exit the CLI (Ctrl+D) and view the trace tree:

```bash
docker compose run --rm cli infer traces
```

You should see the `execute_tool A2A_SubmitTask` span with its children,
showing the full distributed trace.

## Viewing Mock-Agent Logs

The mock-agent logs show incoming trace context:

```bash
docker compose logs mock-agent
```

With ADK 0.22.0+, you will see trace IDs in the agent's request logs,
confirming that the W3C trace context was received.

## Troubleshooting

```bash
# Check CLI logs
docker compose logs cli

# Check mock-agent logs
docker compose logs mock-agent

# Verify telemetry is working
docker compose run --rm cli infer traces --list

# Run a direct A2A query
docker compose run --rm cli infer tools execute A2A_QueryAgent '{"agent_url":"http://mock-agent:8080"}'
```

## See Also

- [A2A Protocol Documentation](https://github.com/inference-gateway/schemas)
- [ADK 0.22.0 Release Notes](https://github.com/inference-gateway/adk/releases/tag/v0.22.0)
- [W3C Trace Context Specification](https://www.w3.org/TR/trace-context/)
- [OpenTelemetry](https://opentelemetry.io/docs/)
