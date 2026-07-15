# A2A Traces Example

End-to-end distributed telemetry between the `infer` CLI and an A2A agent
(mock-agent), with an OpenTelemetry Collector in the middle. It demonstrates
both telemetry models for traces **and** metrics:

- **Push (OTLP)**: the CLI exports its traces and metrics to the collector;
  the mock-agent exports its traces to the collector.
- **Pull (Prometheus)**: the collector scrapes the mock-agent's
  `:9090/metrics` endpoint.

```text
infer CLI ──(OTLP push: traces+metrics)──► otel-collector ──(traces, otlphttp)──► cli:4318
mock-agent ──(OTLP push: traces)─────────►      │              (CLI's local OTLP receiver
mock-agent :9090/metrics ◄─(Prometheus pull)────┘               → `infer traces`)
```

The CLI injects W3C trace context (`traceparent`, `tracestate`, `baggage`)
into every outgoing A2A request, so the mock-agent's spans share the CLI's
trace ID and parent under the CLI's `execute_tool` span. The collector fans
all traces back to the CLI's local OTLP receiver
(`INFER_TELEMETRY_RECEIVER_ADDRESS: 0.0.0.0:4318`), so `infer traces` shows
the **full distributed trace**, including the mock-agent's spans.

## Quick start

1. Configure provider API keys:

   ```bash
   cp .env.example .env
   # set the key matching INFER_AGENT_MODEL in docker-compose.yaml
   ```

2. Start everything (the CLI image is built from source):

   ```bash
   docker compose up -d --build
   ```

3. Attach to the CLI's chat session:

   ```bash
   docker compose attach cli
   ```

4. Delegate a task to the mock-agent:

   ```text
   Ask the mock-agent to summarize the current project.
   ```

   The model uses `A2A_SubmitTask`; trace context flows to the mock-agent,
   whose spans are exported to the collector and fanned back to the CLI.

5. Detach with `ctrl-p ctrl-q` (keep the session running), then view the
   distributed trace:

   ```bash
   docker compose exec cli infer traces
   ```

   ```text
   session (standard, success)                 152ms
   ├── chat deepseek/deepseek-v4-pro             5ms
   ├── execute_tool A2A_SubmitTask              89ms
   │   ╰── a2a.request                          52ms
   ╰── chat deepseek/deepseek-v4-pro            12ms
   ```

   Ingested spans are labeled `name [service]` when the producing agent
   reports a `service.name` resource attribute.

   `infer traces --list` shows all sessions, `infer traces --format json`
   emits the tree for programmatic use, and `infer stats` shows the CLI's
   local metrics.

## Where the telemetry goes

| Signal | Producer | Path |
| --- | --- | --- |
| Traces | CLI | local session file (always) + OTLP push to collector |
| Traces | mock-agent | OTLP push to collector → fanned back to the CLI receiver → `infer traces` |
| Metrics | CLI | local session file (`infer stats`) + OTLP push to collector every 10s |
| Metrics | mock-agent | Prometheus `:9090/metrics`, scraped by the collector every 10s |

The collector's `debug` exporter logs everything it receives:

```bash
docker compose logs -f otel-collector
```

You should see spans with `service.name: infer` and `service.name: mock-agent`
sharing a trace ID, plus `a2a.*` metrics from the scrape and `infer`/`gen_ai`
metrics from the CLI's push.

## Configuration notes

- `INFER_TELEMETRY_OTLP_ENDPOINT` enables the CLI's OTLP push (traces and
  metrics); without it the CLI still records everything locally.
- `INFER_TELEMETRY_RECEIVER_ADDRESS: 0.0.0.0:4318` binds the CLI's local OTLP
  receiver on a fixed address so the collector can feed external spans into
  the session's trace file. Unset, the receiver only serves loopback
  subprocesses.
- The mock-agent enables telemetry via `A2A_TELEMETRY_ENABLE`, pushes traces
  with `A2A_OTEL_TRACES_EXPORTER: otlp` to `A2A_OTEL_EXPORTER_OTLP_ENDPOINT`,
  and serves Prometheus metrics on `A2A_OTEL_EXPORTER_PROMETHEUS_PORT`.
- Use `docker compose exec` (not `run`) for `infer traces`/`infer stats`: the
  trace files live in the chat container's filesystem, and the collector
  reaches the receiver via the `cli` service hostname.

## Troubleshooting

```bash
docker compose logs cli
docker compose logs mock-agent
docker compose logs otel-collector
docker compose exec cli infer traces --list
docker compose exec cli infer tools execute A2A_QueryAgent '{"agent_url":"http://mock-agent:8080"}'
```

## See Also

- [A2A Protocol Documentation](https://github.com/inference-gateway/schemas)
- [mock-agent](https://github.com/inference-gateway/mock-agent)
- [W3C Trace Context Specification](https://www.w3.org/TR/trace-context/)
- [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
